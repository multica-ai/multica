package remotemcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// maxToolPages bounds tools/list pagination against a server that never stops
// returning a cursor.
const maxToolPages = 20

// Session is an initialized MCP session with one remote endpoint. Results are
// returned as the server sent them, so content such as images reaches the
// caller unchanged.
type Session struct {
	client    *http.Client
	endpoint  *url.URL
	headers   http.Header
	sessionID string
	nextID    int
}

// OpenSession validates the endpoint, then runs the MCP initialize handshake.
func OpenSession(ctx context.Context, rawEndpoint string, headers http.Header) (*Session, error) {
	endpoint, err := ValidatePublicHTTPSEndpoint(ctx, rawEndpoint, nil, nil)
	if err != nil {
		return nil, err
	}
	s := &Session{client: NewSecureHTTPClient(endpoint), endpoint: endpoint, headers: headers}
	versions := SupportedProtocolVersions()
	result, responseHeaders, err := s.request(ctx, "initialize", map[string]any{
		"protocolVersion": versions[0],
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "multica", "version": "1"},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize remote MCP: %w", err)
	}
	s.sessionID = responseHeaders.Get("Mcp-Session-Id")
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(result, &initialized); err != nil || !containsString(versions, initialized.ProtocolVersion) {
		return nil, fmt.Errorf("remote MCP negotiated unsupported protocol version %q", initialized.ProtocolVersion)
	}
	if err := notify(ctx, s.client, s.endpoint, s.headers, s.sessionID, map[string]any{
		"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{},
	}); err != nil {
		return nil, fmt.Errorf("confirm remote MCP initialization: %w", err)
	}
	return s, nil
}

func (s *Session) request(ctx context.Context, method string, params any) (json.RawMessage, http.Header, error) {
	s.nextID++
	response, headers, err := call(ctx, s.client, s.endpoint, s.headers, s.sessionID, map[string]any{
		"jsonrpc": "2.0", "id": s.nextID, "method": method, "params": params,
	})
	if err != nil {
		return nil, headers, err
	}
	return response.Result, headers, nil
}

// ListTools returns every tool object the server lists, following cursors.
func (s *Session) ListTools(ctx context.Context) ([]json.RawMessage, error) {
	var tools []json.RawMessage
	cursor := ""
	for page := 0; page < maxToolPages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, _, err := s.request(ctx, "tools/list", params)
		if err != nil {
			return nil, fmt.Errorf("list remote MCP tools: %w", err)
		}
		var result struct {
			Tools      []json.RawMessage `json:"tools"`
			NextCursor string            `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("decode tools/list result: %w", err)
		}
		tools = append(tools, result.Tools...)
		if result.NextCursor == "" {
			return tools, nil
		}
		cursor = result.NextCursor
	}
	return nil, errors.New("remote MCP tools/list did not finish paging")
}

// CallTool runs one tool and returns the raw tools/call result.
func (s *Session) CallTool(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if args == nil {
		args = map[string]any{}
	}
	raw, _, err := s.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("remote MCP tools/call returned no result")
	}
	return raw, nil
}
