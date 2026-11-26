package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"fileforge-desktop/internal/mcp"
	"fileforge-desktop/internal/version"
)

// mcpStdin is the protocol input. It is a variable so tests can drive
// `mcp serve` without replacing os.Stdin.
var mcpStdin io.Reader = os.Stdin

// runMCP handles `mcp serve`, the folder allowlist commands and their help.
func runMCP(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printMCPUsage(stderr)
		return exitValidation
	}

	switch args[0] {
	case "help", "-h", "--help":
		printMCPUsage(stdout)
		return exitSuccess
	case "serve":
		return runMCPServe(ctx, args[1:], stdout, stderr)
	case "allow":
		return runMCPAllow(args[1:], stdout, stderr)
	case "deny":
		return runMCPDeny(args[1:], stdout, stderr)
	case "list":
		return runMCPList(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown mcp command %q\n\n", args[0])
		printMCPUsage(stderr)
		return exitValidation
	}
}

// runMCPServe composes the headless stack and speaks MCP on stdout. Nothing
// except JSON-RPC responses is written to stdout; startup and errors go to
// stderr.
func runMCPServe(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "usage: fileforge-cli mcp serve")
		return exitValidation
	}

	dir, err := mcp.DefaultDir()
	if err != nil {
		fmt.Fprintf(stderr, "mcp serve: %v\n", err)
		return exitInternal
	}

	store := mcp.NewStore(dir)
	config, loadErr := store.Load()
	if loadErr != nil {
		// A corrupt or unreadable allowlist must never be treated as a grant.
		// Start with an empty policy so every request is denied with a clear
		// message the user can act on.
		fmt.Fprintf(stderr, "mcp serve: %v; starting with an empty allowlist (all requests denied)\n", loadErr)
		config = mcp.Config{Version: mcp.ConfigVersion, AllowedRoots: []string{}}
	}

	stack := newStack()
	defer stack.close()

	policy := mcp.NewPolicy(config.AllowedRoots)
	server := mcp.NewServer(mcp.Deps{
		Registry: stack.registry,
		Tooling:  stack.tooling,
		Recipes:  stack.recipes,
		Policy:   policy,
		Audit:    mcp.NewAuditLog(mcp.AuditPath(dir)),
		Version:  version.Number(),
		Stderr:   stderr,
	})

	fmt.Fprintf(stderr, "fileforge mcp: serving stdio with %d allowed root(s)\n", len(policy.Roots()))

	if err := server.Serve(ctx, mcpStdin, stdout); err != nil {
		fmt.Fprintf(stderr, "mcp serve: %v\n", err)
		return exitInternal
	}

	return exitSuccess
}

// runMCPAllow grants a folder. It never runs a job.
func runMCPAllow(args []string, stdout, stderr io.Writer) int {
	folder, ok := singleMCPPathArg("allow", args, stderr)
	if !ok {
		return exitValidation
	}

	store, code := openMCPStore(stderr)
	if code != exitSuccess {
		return code
	}

	normalized, err := store.Allow(folder)
	if err != nil {
		fmt.Fprintf(stderr, "mcp allow: %v\n", err)
		return exitValidation
	}

	fmt.Fprintf(stdout, "allowed %s\n", normalized)
	return exitSuccess
}

// runMCPDeny revokes a folder. It never runs a job.
func runMCPDeny(args []string, stdout, stderr io.Writer) int {
	folder, ok := singleMCPPathArg("deny", args, stderr)
	if !ok {
		return exitValidation
	}

	store, code := openMCPStore(stderr)
	if code != exitSuccess {
		return code
	}

	removed, err := store.Deny(folder)
	if err != nil {
		fmt.Fprintf(stderr, "mcp deny: %v\n", err)
		return exitValidation
	}
	if len(removed) == 0 {
		fmt.Fprintf(stdout, "%s was not in the allowlist\n", strings.TrimSpace(folder))
		return exitSuccess
	}

	for _, root := range removed {
		fmt.Fprintf(stdout, "denied %s\n", root)
	}
	return exitSuccess
}

// runMCPList prints the stored roots, one per line. The output stays
// parseable: the empty case explains itself on stderr.
func runMCPList(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "usage: fileforge-cli mcp list")
		return exitValidation
	}

	store, code := openMCPStore(stderr)
	if code != exitSuccess {
		return code
	}

	roots, err := store.Roots()
	if err != nil {
		fmt.Fprintf(stderr, "mcp list: %v\n", err)
		return exitInternal
	}

	for _, root := range roots {
		fmt.Fprintln(stdout, root)
	}
	if len(roots) == 0 {
		fmt.Fprintln(stderr, "no allowed roots; every MCP request is denied")
	}

	return exitSuccess
}

func singleMCPPathArg(command string, args []string, stderr io.Writer) (string, bool) {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintf(stderr, "usage: fileforge-cli mcp %s <dir>\n", command)
		return "", false
	}
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintf(stderr, "usage: fileforge-cli mcp %s <dir>\n", command)
		return "", false
	}

	return args[0], true
}

func openMCPStore(stderr io.Writer) (*mcp.Store, int) {
	dir, err := mcp.DefaultDir()
	if err != nil {
		fmt.Fprintf(stderr, "mcp: %v\n", err)
		return nil, exitInternal
	}

	return mcp.NewStore(dir), exitSuccess
}

func printMCPUsage(w io.Writer) {
	fmt.Fprint(w, `fileforge-cli mcp serves the local Model Context Protocol server and manages its folder allowlist.

Usage:
  fileforge-cli mcp serve            speak MCP over stdio; stdout is protocol only
  fileforge-cli mcp allow <dir>      grant a folder to MCP tools
  fileforge-cli mcp deny <dir>       revoke a folder
  fileforge-cli mcp list             print the allowed roots

The allowlist lives in mcp.json in the FileForge config directory. Every input
path, output directory and path-valued option in a request must resolve, after
symbolic links, inside an allowed root. An empty allowlist denies everything.
No MCP tool can add, remove or list grants; only these commands can.
Restart the MCP client after changing the allowlist so the server reloads it.

Grant a folder before connecting a client:
  fileforge-cli mcp allow ~/Documents/Exports
`)
}
