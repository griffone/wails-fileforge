package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"fileforge-desktop/internal/recipes"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/services"
	appversion "fileforge-desktop/internal/version"
)

const (
	// ServerName is the serverInfo.name advertised during initialize.
	ServerName = "fileforge"

	// DefaultProtocolVersion is the newest MCP revision this server claims. It
	// is returned when the client asks for a revision the server does not know.
	DefaultProtocolVersion = "2025-11-25"

	// maxMessageBytes bounds one newline-delimited JSON-RPC message.
	maxMessageBytes = 8 << 20
)

// supportedProtocolVersions are the MCP revisions this server implements,
// newest first. Only initialize, tools/list, tools/call and ping are used, and
// all of them are unchanged across these revisions. Newer revisions that change
// request framing are intentionally not claimed.
var supportedProtocolVersions = []string{
	"2025-11-25",
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Deps wires the server to the same services the headless CLI uses.
type Deps struct {
	Registry *registry.Registry
	Tooling  *services.ToolingService
	Recipes  *recipes.Service
	Policy   *Policy
	Audit    *AuditLog
	Version  string
	Stderr   io.Writer
}

// Server speaks MCP over stdio. Handlers run sequentially: one tools/call at a
// time, which keeps stdout ordering trivial and avoids concurrent writes.
type Server struct {
	registry *registry.Registry
	tooling  *services.ToolingService
	recipes  *recipes.Service
	policy   *Policy
	audit    *AuditLog
	version  string
	stderr   io.Writer

	definitions []toolDefinition
	toolByName  map[string]toolDefinition
}

// NewServer builds the protocol surface. Nil services are tolerated so
// initialize and tools/list can be exercised without a full stack; a tool call
// that needs a missing service returns a canonical error instead of panicking.
func NewServer(deps Deps) *Server {
	version := strings.TrimSpace(deps.Version)
	if version == "" {
		version = appversion.Number()
	}
	stderr := deps.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	server := &Server{
		registry: deps.Registry,
		tooling:  deps.Tooling,
		recipes:  deps.Recipes,
		policy:   deps.Policy,
		audit:    deps.Audit,
		version:  version,
		stderr:   stderr,
	}

	server.definitions = server.buildToolDefinitions()
	server.toolByName = make(map[string]toolDefinition, len(server.definitions))
	for _, definition := range server.definitions {
		server.toolByName[definition.Name] = definition
	}

	return server
}

func (s *Server) logf(format string, args ...any) {
	if s == nil || s.stderr == nil {
		return
	}

	fmt.Fprintf(s.stderr, "fileforge mcp: "+format+"\n", args...)
}

// Serve reads newline-delimited JSON-RPC from in and writes one response line
// per request to out. Nothing else is ever written to out.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if in == nil {
		return errors.New("mcp: input is nil")
	}
	if out == nil {
		return errors.New("mcp: output is nil")
	}

	writer := bufio.NewWriter(out)
	defer func() { _ = writer.Flush() }()

	// A stdio read blocks until the client closes the pipe. Closing the reader
	// on cancellation lets a signal terminate the server promptly.
	if closer, ok := in.(io.Closer); ok {
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				_ = closer.Close()
			case <-done:
			}
		}()
	}

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)

	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}

		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		response := s.handleLine(ctx, line)
		if response == nil {
			continue
		}

		encoded, err := json.Marshal(response)
		if err != nil {
			s.logf("encode response: %v", err)
			continue
		}
		if _, err := writer.Write(encoded); err != nil {
			return fmt.Errorf("write stdio response: %w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			return fmt.Errorf("write stdio response: %w", err)
		}
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("flush stdio response: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("read stdio message: %w", err)
	}

	return nil
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func nullID() json.RawMessage {
	return json.RawMessage("null")
}

func (s *Server) handleLine(ctx context.Context, line []byte) *rpcResponse {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}

	var raw json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: nullID(), Error: &rpcError{Code: codeParseError, Message: "Parse error: invalid JSON"}}
	}
	trimmed = bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return &rpcResponse{JSONRPC: "2.0", ID: nullID(), Error: &rpcError{Code: codeParseError, Message: "Parse error: empty message"}}
	}
	if trimmed[0] == '[' {
		return &rpcResponse{JSONRPC: "2.0", ID: nullID(), Error: &rpcError{Code: codeInvalidRequest, Message: "Invalid Request: batch messages are not supported"}}
	}
	if trimmed[0] != '{' {
		return &rpcResponse{JSONRPC: "2.0", ID: nullID(), Error: &rpcError{Code: codeInvalidRequest, Message: "Invalid Request: expected a JSON object"}}
	}

	var request rpcRequest
	if err := json.Unmarshal(trimmed, &request); err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: nullID(), Error: &rpcError{Code: codeInvalidRequest, Message: "Invalid Request: malformed message"}}
	}

	id, idErr := normalizeID(request.ID)
	if idErr != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: nullID(), Error: &rpcError{Code: codeInvalidRequest, Message: idErr.Error()}}
	}
	if request.JSONRPC != "2.0" {
		return &rpcResponse{JSONRPC: "2.0", ID: idOrNull(id), Error: &rpcError{Code: codeInvalidRequest, Message: `Invalid Request: jsonrpc must be "2.0"`}}
	}
	if strings.TrimSpace(request.Method) == "" {
		return &rpcResponse{JSONRPC: "2.0", ID: idOrNull(id), Error: &rpcError{Code: codeInvalidRequest, Message: "Invalid Request: method is required"}}
	}

	response := s.dispatch(ctx, request, id != nil)
	if response == nil {
		return nil
	}
	response.ID = idOrNull(id)

	return response
}

func idOrNull(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return nullID()
	}

	return id
}

// normalizeID validates a JSON-RPC id. A missing or null id marks a
// notification that must not receive a response.
func normalizeID(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	var value any
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return nil, errors.New("Invalid Request: id must be a string, a number or null")
	}
	switch value.(type) {
	case string, float64:
		return append(json.RawMessage(nil), trimmed...), nil
	default:
		return nil, errors.New("Invalid Request: id must be a string, a number or null")
	}
}

func (s *Server) dispatch(ctx context.Context, request rpcRequest, hasID bool) *rpcResponse {
	// A JSON-RPC notification never receives a reply, including when its method
	// is unknown. This server has no notification methods with side effects.
	if !hasID {
		return nil
	}

	switch request.Method {
	case "initialize":
		return resultResponse(s.initialize(request.Params))
	case "ping":
		return resultResponse(map[string]any{})
	case "tools/list":
		return resultResponse(map[string]any{"tools": s.toolList()})
	case "tools/call":
		result, rpcErr := s.callTool(ctx, request.Params)
		if rpcErr != nil {
			return &rpcResponse{JSONRPC: "2.0", Error: rpcErr}
		}
		return resultResponse(result)
	default:
		return &rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: codeMethodNotFound, Message: fmt.Sprintf("Method not found: %s", request.Method)}}
	}
}

func resultResponse(result any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", Result: result}
}

func (s *Server) initialize(params json.RawMessage) any {
	var request struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(bytes.TrimSpace(params)) > 0 {
		_ = json.Unmarshal(params, &request)
	}

	return map[string]any{
		"protocolVersion": negotiateProtocolVersion(request.ProtocolVersion),
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    ServerName,
			"version": s.version,
		},
		"instructions": "FileForge local file tools. Every input path, output directory and path-valued option must " +
			"resolve inside a folder the user granted from the terminal with `" + grantCommand + "`. This server cannot " +
			"add, remove or list grants. Call plan_job or plan_recipe first, then run_job or run_recipe.",
	}
}

func negotiateProtocolVersion(requested string) string {
	trimmed := strings.TrimSpace(requested)
	for _, version := range supportedProtocolVersions {
		if version == trimmed {
			return version
		}
	}

	return DefaultProtocolVersion
}

func (s *Server) toolList() []map[string]any {
	tools := make([]map[string]any, 0, len(s.definitions))
	for _, definition := range s.definitions {
		tools = append(tools, map[string]any{
			"name":        definition.Name,
			"description": definition.Description,
			"inputSchema": definition.InputSchema,
			"annotations": definition.Annotations,
		})
	}

	return tools
}

func (s *Server) callTool(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	if len(bytes.TrimSpace(params)) == 0 {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Invalid params: tools/call requires params"}
	}

	var request struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Invalid params: malformed tools/call params"}
	}

	name := strings.TrimSpace(request.Name)
	if name == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Invalid params: tool name is required"}
	}

	definition, found := s.toolByName[name]
	if !found {
		return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("Invalid params: unknown tool %q", name)}
	}

	arguments := bytes.TrimSpace(request.Arguments)
	if len(arguments) == 0 {
		arguments = []byte("{}")
	}
	if arguments[0] != '{' {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Invalid params: arguments must be a JSON object"}
	}

	payload, isError := definition.Handler(ctx, json.RawMessage(arguments))
	text, err := json.Marshal(payload)
	if err != nil {
		return nil, &rpcError{Code: codeInternalError, Message: fmt.Sprintf("encode %s result: %v", name, err)}
	}

	result := map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": string(text),
		}},
		"structuredContent": payload,
	}
	if isError {
		result["isError"] = true
	}

	return result, nil
}
