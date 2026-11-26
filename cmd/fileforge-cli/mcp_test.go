package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/mcp"
)

func TestMCPAllowDenyListCommandsPersistNormalizedPaths(t *testing.T) {
	isolateEnvironment(t)

	dir := t.TempDir()
	exports := filepath.Join(dir, "exports")
	if err := os.MkdirAll(exports, 0o755); err != nil {
		t.Fatalf("create exports: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"mcp", "allow", exports + string(os.PathSeparator) + "."}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), exports) {
		t.Fatalf("expected the normalized path in %q", stdout.String())
	}

	configDir, err := mcp.DefaultDir()
	if err != nil {
		t.Fatalf("resolve config dir: %v", err)
	}
	configPath := mcp.ConfigPath(configDir)
	payload, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	var config struct {
		Version      int      `json:"version"`
		AllowedRoots []string `json:"allowedRoots"`
	}
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatalf("decode mcp.json: %v", err)
	}
	if config.Version != 1 {
		t.Fatalf("expected version 1, got %d", config.Version)
	}
	if len(config.AllowedRoots) != 1 || config.AllowedRoots[0] != exports {
		t.Fatalf("unexpected allowed roots: %v", config.AllowedRoots)
	}

	// Granting again, through a symlink, must not duplicate the root.
	alias := filepath.Join(dir, "exports-alias")
	if err := os.Symlink(exports, alias); err == nil {
		stdout.Reset()
		stderr.Reset()
		if code := run(context.Background(), []string{"mcp", "allow", alias}, &stdout, &stderr); code != exitSuccess {
			t.Fatalf("expected alias allow to exit 0, got %d (stderr=%s)", code, stderr.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{"mcp", "list"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("expected list to exit 0, got %d (stderr=%s)", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != exports {
		t.Fatalf("expected one listed root %q, got %q", exports, stdout.String())
	}

	// Denying through the alias removes the resolved stored root.
	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{"mcp", "deny", alias}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("expected deny to exit 0, got %d (stderr=%s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), exports) {
		t.Fatalf("expected the removed root in %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{"mcp", "list"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("expected list to exit 0, got %d", code)
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("expected no roots after deny, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "every MCP request is denied") {
		t.Fatalf("expected the empty allowlist warning on stderr, got %q", stderr.String())
	}
}

func TestMCPAllowRejectsMissingFolder(t *testing.T) {
	isolateEnvironment(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"mcp", "allow", filepath.Join(t.TempDir(), "missing")}, &stdout, &stderr)
	if code != exitValidation {
		t.Fatalf("expected exit 2, got %d (stderr=%s)", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
}

func TestMCPDenyUnknownFolderIsANoOp(t *testing.T) {
	isolateEnvironment(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"mcp", "deny", filepath.Join(t.TempDir(), "unknown")}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "was not in the allowlist") {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestMCPHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"mcp", "help"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{"mcp serve", "mcp allow <dir>", "empty allowlist denies everything"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help %q must contain %q", stdout.String(), want)
		}
	}
}

func TestMCPServeSpeaksProtocolOnStdout(t *testing.T) {
	isolateEnvironment(t)

	original := mcpStdin
	mcpStdin = strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n"))
	t.Cleanup(func() { mcpStdin = original })

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"mcp", "serve"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two JSON-RPC lines, got %d: %q", len(lines), stdout.String())
	}
	for index, line := range lines {
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("stdout line %d is not JSON: %q: %v", index, line, err)
		}
		if response["jsonrpc"] != "2.0" {
			t.Fatalf("stdout line %d is not JSON-RPC: %q", index, line)
		}
	}
	if strings.Contains(stdout.String(), "serving stdio") {
		t.Fatalf("startup logging leaked to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "serving stdio") {
		t.Fatalf("expected startup logging on stderr, got %q", stderr.String())
	}
}
