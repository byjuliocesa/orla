// Package mcp is the MCP server CLIs connect to. For now it carries permission prompts from
// headless runs to the owner; shared memory, notify and delegate come later.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"
)

// This is the part of the Model Context Protocol Orla needs: a server over Streamable HTTP that
// offers tools. Each request is answered with a single JSON response; nothing is pushed to the
// client, so the optional SSE stream is refused.

// Tool is one tool the server offers.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	// Call runs the tool. ctx ends if the client goes away. The text is the tool's answer;
	// isError marks it as a failure the model should see.
	Call func(ctx context.Context, args json.RawMessage) (text string, isError bool)
}

type Server struct {
	Name    string
	Version string
	Tools   []Tool
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// JSON-RPC error codes.
const (
	parseError     = -32700
	methodNotFound = -32601
	invalidParams  = -32602
)

// fallbackVersion is offered to a client that names no protocol version.
const fallbackVersion = "2025-06-18"

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "this server answers POST only", http.StatusMethodNotAllowed)
		return
	}
	var req request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		writeResponse(w, response{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &rpcError{parseError, "invalid JSON-RPC request"}})
		return
	}
	// A notification has no id and gets no answer.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	res := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		// Echoing the client's version is allowed when the server supports it; the subset
		// used here is the same in every version so far.
		version := p.ProtocolVersion
		if version == "" {
			version = fallbackVersion
		}
		res.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
	case "ping":
		res.Result = map[string]any{}
	case "tools/list":
		tools := []any{}
		for _, t := range s.Tools {
			tools = append(tools, map[string]any{
				"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema,
			})
		}
		res.Result = map[string]any{"tools": tools}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			res.Error = &rpcError{invalidParams, "invalid tools/call params"}
			break
		}
		tool := s.tool(p.Name)
		if tool == nil {
			res.Error = &rpcError{invalidParams, "unknown tool: " + p.Name}
			break
		}
		text, isError := tool.Call(r.Context(), p.Arguments)
		res.Result = map[string]any{
			"content": []any{map[string]any{"type": "text", "text": text}},
			"isError": isError,
		}
	default:
		res.Error = &rpcError{methodNotFound, "method not found: " + req.Method}
	}
	writeResponse(w, res)
}

func (s *Server) tool(name string) *Tool {
	for i := range s.Tools {
		if s.Tools[i].Name == name {
			return &s.Tools[i]
		}
	}
	return nil
}

func writeResponse(w http.ResponseWriter, res response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
