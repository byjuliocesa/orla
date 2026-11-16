package library

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MCPCheck is what a server answered when Orla asked it for its tools.
type MCPCheck struct {
	OK    bool   `json:"ok"`
	Tools int    `json:"tools"`
	Error string `json:"error,omitempty"`
}

// CheckMCP connects to a library server the way Claude would and counts its tools. Only HTTP
// servers are reached; a command one would have to be started, so it is not checked.
func (m *Manager) CheckMCP(ctx context.Context, name string) (MCPCheck, error) {
	m.mcpMu.Lock()
	list, err := m.readServers()
	var sec sealed
	ok := false
	if err == nil {
		sec, ok, err = m.openSecrets()
	}
	m.mcpMu.Unlock()
	if err != nil {
		return MCPCheck{}, err
	}
	var s *MCPServer
	for i := range list {
		if list[i].Name == name {
			s = &list[i]
		}
	}
	if s == nil {
		return MCPCheck{}, fmt.Errorf("the library has no server named %s", name)
	}
	if !ok && len(s.Headers) > 0 {
		return MCPCheck{}, ErrLocked
	}
	if s.Type != "http" {
		return MCPCheck{Error: "only HTTP servers are checked"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	n, err := probe(ctx, s.URL, sec.Servers[name].Headers)
	if err != nil {
		return MCPCheck{Error: err.Error()}, nil
	}
	return MCPCheck{OK: true, Tools: n}, nil
}

// probe opens an MCP session over streamable HTTP and lists the tools.
func probe(ctx context.Context, url string, headers map[string]string) (int, error) {
	session := ""
	call := func(body map[string]any) (json.RawMessage, error) {
		b, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2025-06-18")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, errors.New("the server did not answer in 15 seconds")
			}
			return nil, errors.New("the server cannot be reached")
		}
		defer res.Body.Close()
		if sid := res.Header.Get("Mcp-Session-Id"); sid != "" {
			session = sid
		}
		switch {
		case res.StatusCode == http.StatusUnauthorized, res.StatusCode == http.StatusForbidden:
			return nil, fmt.Errorf("the server refused the key (%d)", res.StatusCode)
		case res.StatusCode == http.StatusNotFound:
			return nil, errors.New("no MCP server at that URL (404)")
		case res.StatusCode >= 300:
			return nil, fmt.Errorf("the server answered %d", res.StatusCode)
		}
		if _, notice := body["id"]; !notice {
			return nil, nil
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
			raw = firstEvent(raw)
		}
		var reply struct {
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			return nil, errors.New("the server's answer is not MCP")
		}
		if reply.Error != nil {
			return nil, errors.New(reply.Error.Message)
		}
		return reply.Result, nil
	}
	if _, err := call(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "orla", "version": "1"},
	}}); err != nil {
		return 0, err
	}
	if _, err := call(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return 0, err
	}
	res, err := call(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	if err != nil {
		return 0, err
	}
	var tools struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(res, &tools); err != nil {
		return 0, errors.New("the server's tool list does not read")
	}
	return len(tools.Tools), nil
}

// firstEvent is the data of the first event in a server-sent stream.
func firstEvent(raw []byte) []byte {
	var data []byte
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		if d, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimSpace(d)...)
		} else if line == "" && len(data) > 0 {
			return data
		}
	}
	return data
}
