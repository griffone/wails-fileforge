package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	_ "fileforge-desktop/internal/audio"
	_ "fileforge-desktop/internal/doc"
	_ "fileforge-desktop/internal/image"
	_ "fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/recipes"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/services"
	_ "fileforge-desktop/internal/video"
)

// newIntegrationServer wires the real headless services, like the CLI does.
func newIntegrationServer(t *testing.T, roots []string) (*Server, string) {
	t.Helper()

	reg := registry.GetGlobalRegistry()
	tooling := services.NewHeadlessToolingService(reg)
	recipeService := recipes.NewServiceWithPersistence(reg, tooling.JobHistory(), nil, "")
	recipeService.SetEngine(tooling.Engine())

	t.Cleanup(func() {
		recipeService.Shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tooling.Shutdown(ctx)
	})

	auditPath := filepath.Join(t.TempDir(), "mcp_audit.log")
	server := NewServer(Deps{
		Registry: reg,
		Tooling:  tooling,
		Recipes:  recipeService,
		Policy:   NewPolicy(roots),
		Audit:    NewAuditLog(auditPath),
		Version:  "test",
		Stderr:   &bytes.Buffer{},
	})

	return server, auditPath
}

func callIntegrationTool(t *testing.T, server *Server, name string, args any) (map[string]any, bool) {
	t.Helper()

	arguments, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("encode arguments: %v", err)
	}
	request := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`,
		name, arguments,
	)

	responses := serveExchange(t, server, request)
	if len(responses) != 1 {
		t.Fatalf("expected one response, got %d", len(responses))
	}
	if rpcErr, ok := responses[0]["error"].(map[string]any); ok {
		t.Fatalf("unexpected protocol error: %#v", rpcErr)
	}

	result := rpcResult(t, responses[0])
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent, got %#v", result)
	}
	isError, _ := result["isError"].(bool)

	return structured, isError
}

func payloadErrorCode(payload map[string]any) string {
	jobErr, ok := payload["error"].(map[string]any)
	if !ok {
		return ""
	}
	if code, ok := jobErr["detail_code"].(string); ok && code != "" {
		return code
	}
	code, _ := jobErr["code"].(string)

	return code
}

func payloadErrorMessage(payload map[string]any) string {
	jobErr, ok := payload["error"].(map[string]any)
	if !ok {
		return ""
	}
	message, _ := jobErr["message"].(string)

	return message
}

// writePDFFixture creates a valid one-page PDF from a generated PNG.
func writePDFFixture(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}

	pngPath := filepath.Join(t.TempDir(), "page.png")
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 90, A: 255})
		}
	}

	file, err := os.Create(pngPath)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png: %v", err)
	}

	if err := api.ImportImagesFile(context.Background(), []string{pngPath}, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

func mergeInputs(t *testing.T, dir string) (string, string) {
	t.Helper()

	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	return first, second
}

func readAuditLines(t *testing.T, path string) []map[string]any {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	entries := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("audit line is not JSON: %q: %v", line, err)
		}
		entries = append(entries, entry)
	}

	return entries
}

func TestRunJobDeniedWithEmptyAllowlist(t *testing.T) {
	server, auditPath := newIntegrationServer(t, nil)

	dir := t.TempDir()
	first, second := mergeInputs(t, dir)

	payload, isError := callIntegrationTool(t, server, "run_job", map[string]any{
		"toolId":     "tool.pdf.merge",
		"inputPaths": []string{first, second},
		"outputDir":  dir,
	})
	if !isError {
		t.Fatalf("expected an empty allowlist to deny the run, got %#v", payload)
	}
	if code := payloadErrorCode(payload); code != "MCP_PATH_NOT_ALLOWED" {
		t.Fatalf("expected MCP_PATH_NOT_ALLOWED, got %q", code)
	}
	message := payloadErrorMessage(payload)
	for _, want := range []string{"no allowed roots are configured", "fileforge-cli mcp allow <dir>"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q must contain %q", message, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "first_merged.pdf")); !os.IsNotExist(err) {
		t.Fatalf("a denied run must not write an output, got %v", err)
	}

	entries := readAuditLines(t, auditPath)
	if len(entries) != 1 {
		t.Fatalf("expected one audit line, got %d", len(entries))
	}
	entry := entries[0]
	if entry["tool"] != "tool.pdf.merge" || entry["status"] != "denied" || entry["errorCode"] != "MCP_PATH_NOT_ALLOWED" {
		t.Fatalf("unexpected audit entry: %#v", entry)
	}
	if entry["inputCount"] != float64(2) {
		t.Fatalf("expected inputCount 2, got %#v", entry["inputCount"])
	}
}

func TestRunJobAllowedInsideGrantedRoot(t *testing.T) {
	dir := t.TempDir()
	server, auditPath := newIntegrationServer(t, []string{dir})

	first, second := mergeInputs(t, dir)

	payload, isError := callIntegrationTool(t, server, "run_job", map[string]any{
		"toolId":     "tool.pdf.merge",
		"inputPaths": []string{first, second},
		"outputDir":  dir,
	})
	if isError {
		t.Fatalf("expected the run to succeed, got %#v", payload)
	}
	if payload["status"] != "success" || payload["success"] != true {
		t.Fatalf("unexpected terminal report: %#v", payload)
	}

	outputs, ok := payload["outputs"].([]any)
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one output, got %#v", payload["outputs"])
	}
	output := outputs[0].(map[string]any)
	outputPath, _ := output["path"].(string)
	if outputPath != filepath.Join(dir, "first_merged.pdf") {
		t.Fatalf("unexpected output path %q", outputPath)
	}
	if !strings.HasPrefix(filepath.Clean(outputPath), filepath.Clean(dir)+string(os.PathSeparator)) {
		t.Fatalf("output %q must land inside the granted root %q", outputPath, dir)
	}
	if size, _ := output["sizeBytes"].(float64); size <= 0 {
		t.Fatalf("expected a non-zero output size, got %#v", output["sizeBytes"])
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("output does not exist: %v", err)
	}

	entries := readAuditLines(t, auditPath)
	if len(entries) != 1 {
		t.Fatalf("expected one audit line, got %d", len(entries))
	}
	entry := entries[0]
	if entry["status"] != "success" || entry["tool"] != "tool.pdf.merge" || entry["inputCount"] != float64(2) {
		t.Fatalf("unexpected audit entry: %#v", entry)
	}
	if entry["outputDir"] != dir {
		t.Fatalf("expected outputDir %q, got %#v", dir, entry["outputDir"])
	}
	if _, hasError := entry["errorCode"]; hasError {
		t.Fatalf("a successful run must not record an error code: %#v", entry)
	}
	for _, forbidden := range []string{"contents", "input", "outputs"} {
		if _, exists := entry[forbidden]; exists {
			t.Fatalf("audit entry must not contain %q: %#v", forbidden, entry)
		}
	}
}

func TestRunJobDeniedOutsideAllowedRoot(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatalf("create allowed: %v", err)
	}
	server, _ := newIntegrationServer(t, []string{allowed})

	first, second := mergeInputs(t, outside)

	payload, isError := callIntegrationTool(t, server, "run_job", map[string]any{
		"toolId":     "tool.pdf.merge",
		"inputPaths": []string{first, second},
		"outputDir":  allowed,
	})
	if !isError {
		t.Fatalf("expected the outside input to be denied, got %#v", payload)
	}
	if code := payloadErrorCode(payload); code != "MCP_PATH_NOT_ALLOWED" {
		t.Fatalf("expected MCP_PATH_NOT_ALLOWED, got %q", code)
	}
	message := payloadErrorMessage(payload)
	for _, want := range []string{allowed, "outside the allowed roots", "fileforge-cli mcp allow <dir>"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q must contain %q", message, want)
		}
	}
}

func TestRunJobDeniedThroughDotDot(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatalf("create allowed: %v", err)
	}
	outsideFile := filepath.Join(base, "outside", "first.pdf")
	writePDFFixture(t, outsideFile)

	server, _ := newIntegrationServer(t, []string{allowed})

	escape := allowed + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "outside" + string(os.PathSeparator) + "first.pdf"
	payload, isError := callIntegrationTool(t, server, "run_job", map[string]any{
		"toolId":     "tool.pdf.merge",
		"inputPaths": []string{escape},
		"outputDir":  allowed,
	})
	if !isError {
		t.Fatalf("expected the .. escape to be denied, got %#v", payload)
	}
	if code := payloadErrorCode(payload); code != "MCP_PATH_NOT_ALLOWED" {
		t.Fatalf("expected MCP_PATH_NOT_ALLOWED, got %q", code)
	}
}

func TestRunJobDeniedThroughEscapingSymlink(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatalf("create allowed: %v", err)
	}
	first, _ := mergeInputs(t, outside)

	link := filepath.Join(allowed, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	server, _ := newIntegrationServer(t, []string{allowed})
	payload, isError := callIntegrationTool(t, server, "run_job", map[string]any{
		"toolId":     "tool.pdf.merge",
		"inputPaths": []string{filepath.Join(link, filepath.Base(first))},
		"outputDir":  allowed,
	})
	if !isError {
		t.Fatalf("expected the symlink escape to be denied, got %#v", payload)
	}
	if code := payloadErrorCode(payload); code != "MCP_PATH_NOT_ALLOWED" {
		t.Fatalf("expected MCP_PATH_NOT_ALLOWED, got %q", code)
	}
}

func TestRunJobDeniesOutputDirectoryOutsideRoot(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatalf("create allowed: %v", err)
	}
	first, second := mergeInputs(t, allowed)

	server, _ := newIntegrationServer(t, []string{allowed})
	payload, isError := callIntegrationTool(t, server, "run_job", map[string]any{
		"toolId":     "tool.pdf.merge",
		"inputPaths": []string{first, second},
		"outputDir":  outside,
	})
	if !isError {
		t.Fatalf("expected the outside output directory to be denied, got %#v", payload)
	}
	if code := payloadErrorCode(payload); code != "MCP_OUTPUT_NOT_ALLOWED" {
		t.Fatalf("expected MCP_OUTPUT_NOT_ALLOWED, got %q", code)
	}
	if message := payloadErrorMessage(payload); !strings.Contains(message, "outside") {
		t.Fatalf("message %q must name the rejected path", message)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("a denied run must not create the output directory, got %v", err)
	}
}

func TestPlanJobWritesNothing(t *testing.T) {
	dir := t.TempDir()
	server, auditPath := newIntegrationServer(t, []string{dir})
	first, second := mergeInputs(t, dir)

	payload, isError := callIntegrationTool(t, server, "plan_job", map[string]any{
		"toolId":     "tool.pdf.merge",
		"inputPaths": []string{first, second},
		"outputDir":  dir,
	})
	if isError {
		t.Fatalf("expected the plan to succeed, got %#v", payload)
	}
	if payload["success"] != true {
		t.Fatalf("unexpected plan payload: %#v", payload)
	}

	outputs, ok := payload["plannedOutputs"].([]any)
	if !ok || len(outputs) != 1 {
		t.Fatalf("expected one planned output, got %#v", payload["plannedOutputs"])
	}
	planned := outputs[0].(map[string]any)
	plannedPath, _ := planned["outputPath"].(string)
	if plannedPath != filepath.Join(dir, "first_merged.pdf") {
		t.Fatalf("unexpected planned output %q", plannedPath)
	}
	if _, err := os.Stat(plannedPath); !os.IsNotExist(err) {
		t.Fatalf("plan_job must not write its output, got %v", err)
	}
	if _, err := os.Stat(auditPath); !os.IsNotExist(err) {
		t.Fatalf("plan_job must not write an audit line, got %v", err)
	}
}

func TestRunJobRejectsUnknownOption(t *testing.T) {
	dir := t.TempDir()
	server, auditPath := newIntegrationServer(t, []string{dir})
	first, second := mergeInputs(t, dir)

	payload, isError := callIntegrationTool(t, server, "run_job", map[string]any{
		"toolId":     "tool.pdf.merge",
		"inputPaths": []string{first, second},
		"outputDir":  dir,
		"options":    map[string]any{"nope": true},
	})
	if !isError {
		t.Fatalf("expected the unknown option to fail, got %#v", payload)
	}
	if code := payloadErrorCode(payload); code != "TOOL_OPTION_UNKNOWN" {
		t.Fatalf("expected TOOL_OPTION_UNKNOWN, got %q", code)
	}
	message := payloadErrorMessage(payload)
	for _, want := range []string{"nope", "not supported", "outputDir"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q must contain %q", message, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "first_merged.pdf")); !os.IsNotExist(err) {
		t.Fatalf("a rejected option must not write an output, got %v", err)
	}

	entries := readAuditLines(t, auditPath)
	if len(entries) != 1 || entries[0]["status"] != "rejected" || entries[0]["errorCode"] != "TOOL_OPTION_UNKNOWN" {
		t.Fatalf("unexpected audit entries: %#v", entries)
	}
}

func TestRunJobEnforcesTheInputLimit(t *testing.T) {
	server, _ := newIntegrationServer(t, nil)

	inputs := make([]string, 0, maxInputsPerCall+1)
	for index := 0; index <= maxInputsPerCall; index++ {
		inputs = append(inputs, fmt.Sprintf("/tmp/input-%d.pdf", index))
	}

	payload, isError := callIntegrationTool(t, server, "run_job", map[string]any{
		"toolId":     "tool.pdf.merge",
		"inputPaths": inputs,
	})
	if !isError {
		t.Fatalf("expected the input limit to fail, got %#v", payload)
	}
	if code := payloadErrorCode(payload); code != "MCP_INPUT_LIMIT_EXCEEDED" {
		t.Fatalf("expected MCP_INPUT_LIMIT_EXCEEDED, got %q", code)
	}
	if message := payloadErrorMessage(payload); !strings.Contains(message, fmt.Sprintf("At most %d", maxInputsPerCall)) {
		t.Fatalf("message %q must document the cap", message)
	}
}

func TestRunJobRejectsExcludedFileMovingTools(t *testing.T) {
	server, _ := newIntegrationServer(t, nil)

	payload, isError := callIntegrationTool(t, server, "run_job", map[string]any{
		"toolId":     "tool.files.organize",
		"inputPaths": []string{"/tmp/whatever.pdf"},
	})
	if !isError {
		t.Fatalf("expected an excluded tool to be refused, got %#v", payload)
	}
	if code := payloadErrorCode(payload); code != "TOOL_NOT_FOUND" {
		t.Fatalf("expected TOOL_NOT_FOUND, got %q", code)
	}
	if message := payloadErrorMessage(payload); !strings.Contains(message, "not available over MCP") {
		t.Fatalf("message %q must explain the exclusion", message)
	}
}

func TestListToolsCatalogExcludesFileMovingTools(t *testing.T) {
	server, _ := newIntegrationServer(t, nil)

	payload, isError := callIntegrationTool(t, server, "list_tools", map[string]any{})
	if isError {
		t.Fatalf("expected list_tools to succeed, got %#v", payload)
	}

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("expected a non-empty catalog, got %#v", payload["tools"])
	}
	for _, raw := range tools {
		entry := raw.(map[string]any)
		id, _ := entry["id"].(string)
		for _, forbidden := range []string{"automation", "organize", "duplicate"} {
			if strings.Contains(strings.ToLower(id), forbidden) {
				t.Fatalf("catalog must not expose %q, got %q", forbidden, id)
			}
		}
		for _, field := range []string{"id", "name", "description", "inputs", "outputs", "batch", "options"} {
			if _, ok := entry[field]; !ok {
				t.Fatalf("%s: missing %q in %#v", id, field, entry)
			}
		}
	}
}

func TestListRecipesAndPlanRecipe(t *testing.T) {
	dir := t.TempDir()
	server, _ := newIntegrationServer(t, []string{dir})
	first, second := mergeInputs(t, dir)

	listed, isError := callIntegrationTool(t, server, "list_recipes", map[string]any{})
	if isError {
		t.Fatalf("expected list_recipes to succeed, got %#v", listed)
	}
	recipesPayload, ok := listed["recipes"].([]any)
	if !ok || len(recipesPayload) == 0 {
		t.Fatalf("expected a non-empty recipe list, got %#v", listed["recipes"])
	}

	planned, isError := callIntegrationTool(t, server, "plan_recipe", map[string]any{
		"recipeId":   "recipe.email_pdf",
		"inputPaths": []string{first, second},
		"outputDir":  dir,
	})
	if isError {
		t.Fatalf("expected the recipe plan to succeed, got %#v", planned)
	}
	if planned["success"] != true {
		t.Fatalf("unexpected recipe plan: %#v", planned)
	}
	outputs, ok := planned["plannedOutputs"].([]any)
	if !ok || len(outputs) == 0 {
		t.Fatalf("expected planned recipe outputs, got %#v", planned["plannedOutputs"])
	}
	plannedPath, _ := outputs[0].(map[string]any)["outputPath"].(string)
	if _, err := os.Stat(plannedPath); !os.IsNotExist(err) {
		t.Fatalf("plan_recipe must not write its output, got %v", err)
	}
}

func TestRunRecipeDeniedWithEmptyAllowlist(t *testing.T) {
	server, _ := newIntegrationServer(t, nil)
	dir := t.TempDir()
	first, _ := mergeInputs(t, dir)

	payload, isError := callIntegrationTool(t, server, "run_recipe", map[string]any{
		"recipeId":   "recipe.email_pdf",
		"inputPaths": []string{first},
		"outputDir":  dir,
	})
	if !isError {
		t.Fatalf("expected the recipe run to be denied, got %#v", payload)
	}
	if code := payloadErrorCode(payload); code != "MCP_PATH_NOT_ALLOWED" {
		t.Fatalf("expected MCP_PATH_NOT_ALLOWED, got %q", code)
	}
}

func TestServeStdoutCarriesOnlyJSONRPC(t *testing.T) {
	dir := t.TempDir()
	server, _ := newIntegrationServer(t, []string{dir})
	first, second := mergeInputs(t, dir)

	arguments, err := json.Marshal(map[string]any{
		"toolId":     "tool.pdf.merge",
		"inputPaths": []string{first, second},
		"outputDir":  dir,
	})
	if err != nil {
		t.Fatalf("encode arguments: %v", err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run_job","arguments":%s}}`, arguments),
	}, "\n")

	var stdout bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &stdout); err != nil {
		t.Fatalf("serve: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSON-RPC lines, got %d: %q", len(lines), stdout.String())
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

	result := rpcResult(t, mustDecodeLine(t, lines[2]))
	structured := result["structuredContent"].(map[string]any)
	if structured["status"] != "success" {
		t.Fatalf("expected a successful run, got %#v", structured)
	}
}

func mustDecodeLine(t *testing.T, line string) map[string]any {
	t.Helper()

	var response map[string]any
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}

	return response
}
