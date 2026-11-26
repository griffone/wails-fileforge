package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func protocolServer() *Server {
	return NewServer(Deps{Policy: NewPolicy(nil), Stderr: io.Discard})
}

// serveExchange runs one in-memory stdio session and returns every stdout line
// as decoded JSON. Every line must parse: that is the stdout purity contract.
func serveExchange(t *testing.T, server *Server, input string) []map[string]any {
	t.Helper()

	var stdout bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &stdout); err != nil {
		t.Fatalf("serve: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		return nil
	}

	responses := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("stdout line is not JSON: %q: %v", line, err)
		}
		if response["jsonrpc"] != "2.0" {
			t.Fatalf("stdout line is not JSON-RPC 2.0: %q", line)
		}
		responses = append(responses, response)
	}

	return responses
}

func rpcResult(t *testing.T, response map[string]any) map[string]any {
	t.Helper()

	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected a result, got %#v", response)
	}

	return result
}

func rpcErrorCode(t *testing.T, response map[string]any) int {
	t.Helper()

	rpcErr, ok := response["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected an error, got %#v", response)
	}
	code, ok := rpcErr["code"].(float64)
	if !ok {
		t.Fatalf("error code is not a number: %#v", rpcErr)
	}

	return int(code)
}

func TestInitializeHandshake(t *testing.T) {
	responses := serveExchange(t, protocolServer(), strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}, "\n"))

	if len(responses) != 1 {
		t.Fatalf("expected one response for a request plus a notification, got %d", len(responses))
	}

	result := rpcResult(t, responses[0])
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("expected the requested protocol version, got %#v", result["protocolVersion"])
	}
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("expected capabilities, got %#v", result["capabilities"])
	}
	if _, ok := capabilities["tools"]; !ok {
		t.Fatalf("expected the tools capability, got %#v", capabilities)
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok || serverInfo["name"] != ServerName {
		t.Fatalf("unexpected serverInfo: %#v", result["serverInfo"])
	}
	instructions, _ := result["instructions"].(string)
	if !strings.Contains(instructions, "fileforge-cli mcp allow <dir>") {
		t.Fatalf("instructions must explain the grant command, got %q", instructions)
	}
}

func TestInitializeNegotiatesUnknownProtocolVersion(t *testing.T) {
	responses := serveExchange(t, protocolServer(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)

	result := rpcResult(t, responses[0])
	if result["protocolVersion"] != DefaultProtocolVersion {
		t.Fatalf("expected %q, got %#v", DefaultProtocolVersion, result["protocolVersion"])
	}
}

func TestToolsListReturnsExactlyTheSixSafeTools(t *testing.T) {
	responses := serveExchange(t, protocolServer(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	result := rpcResult(t, responses[0])

	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("expected a tools array, got %#v", result["tools"])
	}

	want := []string{"list_tools", "plan_job", "run_job", "list_recipes", "plan_recipe", "run_recipe"}
	if len(tools) != len(want) {
		t.Fatalf("expected %d tools, got %d: %#v", len(want), len(tools), tools)
	}

	for index, name := range want {
		tool, ok := tools[index].(map[string]any)
		if !ok {
			t.Fatalf("tool %d is not an object: %#v", index, tools[index])
		}
		if tool["name"] != name {
			t.Fatalf("tool %d: expected %q, got %#v", index, name, tool["name"])
		}
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok || schema["type"] != "object" {
			t.Fatalf("%s: expected an object input schema, got %#v", name, tool["inputSchema"])
		}
		description, _ := tool["description"].(string)
		if strings.TrimSpace(description) == "" {
			t.Fatalf("%s: description is required", name)
		}

		lowered := strings.ToLower(name)
		for _, forbidden := range []string{"allow", "deny", "grant", "permission", "automation", "organize", "duplicate"} {
			if strings.Contains(lowered, forbidden) {
				t.Fatalf("tool %q looks like a permission or file-moving tool", name)
			}
		}
	}
}

func TestNotificationsNeverReceiveAResponse(t *testing.T) {
	responses := serveExchange(t, protocolServer(), strings.Join([]string{
		`{"jsonrpc":"2.0","method":"ping"}`,
		`{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/list"}`,
	}, "\n"))

	if len(responses) != 1 {
		t.Fatalf("expected only the request with an id to be answered, got %d responses", len(responses))
	}
	if responses[0]["id"] != float64(9) {
		t.Fatalf("expected the tools/list response, got %#v", responses[0])
	}
}

func TestUnknownMethodReturnsMethodNotFound(t *testing.T) {
	responses := serveExchange(t, protocolServer(), strings.Join([]string{
		`{"jsonrpc":"2.0","id":7,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","method":"notifications/unknown"}`,
		`{"jsonrpc":"2.0","id":8,"method":"ping"}`,
	}, "\n"))

	if len(responses) != 2 {
		t.Fatalf("expected two responses (unknown notification is silent), got %d", len(responses))
	}
	if code := rpcErrorCode(t, responses[0]); code != codeMethodNotFound {
		t.Fatalf("expected %d, got %d", codeMethodNotFound, code)
	}
	if responses[0]["id"] != float64(7) {
		t.Fatalf("expected the request id to be echoed, got %#v", responses[0]["id"])
	}
	rpcResult(t, responses[1])
}

func TestMalformedJSONReturnsParseErrorAndKeepsServing(t *testing.T) {
	responses := serveExchange(t, protocolServer(), strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	}, "\n"))

	if len(responses) != 2 {
		t.Fatalf("expected a parse error and a ping response, got %d", len(responses))
	}
	if code := rpcErrorCode(t, responses[0]); code != codeParseError {
		t.Fatalf("expected %d, got %d", codeParseError, code)
	}
	if responses[0]["id"] != nil {
		t.Fatalf("a parse error must use id null, got %#v", responses[0]["id"])
	}
	rpcResult(t, responses[1])
}

func TestMalformedJSONRecoversOnTheNextValidRequest(t *testing.T) {
	responses := serveExchange(t, protocolServer(), strings.Join([]string{
		`not json at all`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	}, "\n"))

	if len(responses) != 2 {
		t.Fatalf("expected two responses, got %d", len(responses))
	}
	if code := rpcErrorCode(t, responses[0]); code != codeParseError {
		t.Fatalf("expected %d, got %d", codeParseError, code)
	}
	rpcResult(t, responses[1])
}

func TestInvalidRequestShapes(t *testing.T) {
	responses := serveExchange(t, protocolServer(), strings.Join([]string{
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`,
		`{"jsonrpc":"1.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":true,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":5}`,
	}, "\n"))

	if len(responses) != 4 {
		t.Fatalf("expected four responses, got %d", len(responses))
	}
	for index, response := range responses {
		if code := rpcErrorCode(t, response); code != codeInvalidRequest {
			t.Fatalf("response %d: expected %d, got %d", index, codeInvalidRequest, code)
		}
	}
}

func TestToolsCallUnknownToolIsInvalidParams(t *testing.T) {
	responses := serveExchange(t, protocolServer(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"grant_folder","arguments":{}}}`)

	if code := rpcErrorCode(t, responses[0]); code != codeInvalidParams {
		t.Fatalf("expected %d, got %d", codeInvalidParams, code)
	}
}

func TestToolsCallRequiresAnArgumentsObject(t *testing.T) {
	responses := serveExchange(t, protocolServer(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"plan_job","arguments":[]}}`)

	if code := rpcErrorCode(t, responses[0]); code != codeInvalidParams {
		t.Fatalf("expected %d, got %d", codeInvalidParams, code)
	}
}

func TestToolsCallReturnsTextAndStructuredContent(t *testing.T) {
	server := protocolServer()
	responses := serveExchange(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_tools","arguments":{}}}`)

	result := rpcResult(t, responses[0])
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("expected structuredContent, got %#v", result)
	}
	if _, ok := structured["tools"].([]any); !ok {
		t.Fatalf("expected a tools array, got %#v", structured["tools"])
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("expected one content item, got %#v", result["content"])
	}
	item := content[0].(map[string]any)
	text, _ := item["text"].(string)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("content text is not JSON: %v", err)
	}
	if len(decoded["tools"].([]any)) != 0 {
		t.Fatalf("a nil registry must return an empty catalog, got %#v", decoded["tools"])
	}
}
