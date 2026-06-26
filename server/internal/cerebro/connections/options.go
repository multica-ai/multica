package connections

// options.go powers the search + multi-select picker in the tool-policy WHEN
// layer (FIR-2083). When a connection declares a scopable argument, the picker
// needs the live list of allowed values (e.g. the registry's data sources). This
// endpoint calls the connection's declared options-source tool (e.g.
// data_sources_list) through the same MCP Streamable HTTP protocol the test
// path uses, and returns a normalized {id, name, folder, tags} list.
//
// Security: the caller may only ask for a tool the connection has DECLARED as an
// options source (Connection.OptionsSourceFor). It cannot drive an arbitrary
// tool through the connection's stored credentials.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Option is one selectable value for a scopable argument, normalized for the
// grouped multi-select picker. FolderID is the stable group key the picker
// writes for a folder-level ("auto-cover") scope rule, while Folder is the
// human folder name shown as the group heading (FIR-2083).
type Option struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Folder   string   `json:"folder,omitempty"`
	FolderID string   `json:"folder_id,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// Options — GET /api/workspaces/{id}/connections/{connId}/options?tool=<options_source_tool>
//
// Returns the choices for a scopable argument's picker by calling the named
// options-source tool on the connection. The tool MUST be declared on the
// connection's scopable_args; otherwise 400.
func (h *Handler) Options(w http.ResponseWriter, r *http.Request) {
	wsID, ok := h.workspaceID(w, r)
	if !ok {
		return
	}
	connID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connId"), "connId")
	if !ok {
		return
	}
	toolName := strings.TrimSpace(r.URL.Query().Get("tool"))
	if toolName == "" {
		writeError(w, http.StatusBadRequest, "tool query parameter required")
		return
	}
	conn, err := h.Store.Get(r.Context(), connID, wsID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "connection not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get connection failed")
		return
	}
	if conn.Type != TypeMCPHTTP {
		writeError(w, http.StatusBadRequest, "options are only available for mcp_http connections")
		return
	}
	if _, declared := conn.OptionsSourceFor(toolName); !declared {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("tool %q is not declared as an options source on this connection", toolName))
		return
	}

	raw, err := callMCPTool(r.Context(), strings.TrimRight(conn.URL, "/"), conn.AuthConfig, toolName, map[string]any{})
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("options source call failed: %s", err))
		return
	}
	opts := parseOptions(raw)
	writeJSON(w, http.StatusOK, map[string]any{"options": opts})
}

// callMCPTool runs the MCP Streamable HTTP handshake (initialize → tools/call)
// against an mcp_http connection and returns the raw JSON-RPC result payload
// (the "result" object's bytes). It mirrors testMCPConnection but invokes a
// named tool instead of tools/list.
func callMCPTool(ctx context.Context, url string, auth AuthConfig, toolName string, args map[string]any) (json.RawMessage, error) {
	client := &http.Client{Timeout: testTimeout}

	// Step 1: initialize, to obtain an Mcp-Session-Id.
	initPayload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "cerebro-connection-options", "version": "1.0"},
		},
	})
	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(initPayload))
	if err != nil {
		return nil, fmt.Errorf("build initialize: %w", err)
	}
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	addAuthHeaders(initReq, auth)
	initResp, err := client.Do(initReq)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	defer initResp.Body.Close()
	if initResp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned %d on initialize", initResp.StatusCode)
	}
	sessionID := initResp.Header.Get("Mcp-Session-Id")
	_, _ = io.Copy(io.Discard, initResp.Body)

	// Step 2: tools/call.
	callPayload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params":  map[string]any{"name": toolName, "arguments": args},
	})
	callReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(callPayload))
	if err != nil {
		return nil, fmt.Errorf("build tools/call: %w", err)
	}
	callReq.Header.Set("Content-Type", "application/json")
	callReq.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		callReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	addAuthHeaders(callReq, auth)
	resp, err := client.Do(callReq)
	if err != nil {
		return nil, fmt.Errorf("tools/call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var body []byte
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		body = extractSSEData(resp.Body)
	} else {
		body, _ = io.ReadAll(resp.Body)
	}

	var rpc struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &rpc); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("%s", rpc.Error.Message)
	}
	return rpc.Result, nil
}

// parseOptions normalizes an MCP tools/call result into the picker's option
// list. It tolerates the common MCP shapes: a result with structuredContent, or
// a result with text content carrying the tool's JSON return value. The payload
// itself may be an array of records or an object wrapping one (e.g.
// {"data_sources": [...]}), so it digs for the first array of objects.
func parseOptions(result json.RawMessage) []Option {
	records := extractRecords(result)
	out := make([]Option, 0, len(records))
	for _, rec := range records {
		opt := Option{
			ID:       firstString(rec, "id", "value", "key"),
			Name:     firstString(rec, "name", "display_name", "label", "title"),
			Folder:   firstString(rec, "folder_name", "folder", "folder_path", "group"),
			FolderID: firstString(rec, "folder_id", "folderId"),
			Tags:     firstStringSlice(rec, "publish_tags", "tags"),
		}
		if opt.ID == "" {
			continue
		}
		if opt.Name == "" {
			opt.Name = opt.ID
		}
		out = append(out, opt)
	}
	return out
}

// extractRecords digs a list of object records out of an MCP tools/call result.
func extractRecords(result json.RawMessage) []map[string]any {
	if len(result) == 0 {
		return nil
	}
	var envelope struct {
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(result, &envelope)

	// Prefer structuredContent, then any text content payload, then the raw
	// result itself (tools that return JSON directly without an envelope).
	candidates := make([]json.RawMessage, 0, 3)
	if len(envelope.StructuredContent) > 0 {
		candidates = append(candidates, envelope.StructuredContent)
	}
	for _, c := range envelope.Content {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			candidates = append(candidates, json.RawMessage(c.Text))
		}
	}
	candidates = append(candidates, result)

	for _, c := range candidates {
		if recs := recordsFromValue(c); len(recs) > 0 {
			return recs
		}
	}
	return nil
}

// recordsFromValue finds the first array-of-objects in a JSON value: the value
// itself if it is such an array, otherwise the first array-valued field of an
// object whose elements are objects.
func recordsFromValue(raw json.RawMessage) []map[string]any {
	var asArray []map[string]any
	if err := json.Unmarshal(raw, &asArray); err == nil && len(asArray) > 0 {
		return asArray
	}
	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asObject); err == nil {
		// Stable preference for the registry's known wrapper key first.
		for _, key := range []string{"data_sources", "items", "results", "data", "options"} {
			if v, ok := asObject[key]; ok {
				if recs := recordsFromValue(v); len(recs) > 0 {
					return recs
				}
			}
		}
		for _, v := range asObject {
			if recs := recordsFromValue(v); len(recs) > 0 {
				return recs
			}
		}
	}
	return nil
}

func firstString(rec map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := rec[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func firstStringSlice(rec map[string]any, keys ...string) []string {
	for _, k := range keys {
		if v, ok := rec[k]; ok {
			if arr, ok := v.([]any); ok {
				out := make([]string, 0, len(arr))
				for _, e := range arr {
					if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
						out = append(out, s)
					}
				}
				if len(out) > 0 {
					return out
				}
			}
		}
	}
	return nil
}
