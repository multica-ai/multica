package runtime

// mcp_http_client.go — FIR-2441 fix-list #4.
//
// An in-process MCP client for the streamable-HTTP transport. It exists because
// a cloud runtime (Firtal Gateway) runs its own server-side tool loop rather
// than launching an external agent CLI (Claude/Codex/…). On a LOCAL runtime the
// CLI is handed a --mcp-config document and it spins up its own MCP client per
// mcp_http connection; on the gateway there is no such CLI, so the resolver's
// out.MCPServers (the Allow-only mcp_http connections) previously reached
// nobody. This client is the missing half: the gateway dials each Allow-only
// connection directly (it lives inside the Sliplane network, so it can reach an
// *.internal host that a laptop cannot), discovers the tools via tools/list, and
// dispatches tools/call when the model picks one.
//
// Scope is deliberately small: initialize → tools/list → tools/call over the
// single-endpoint streamable-HTTP transport (JSON or SSE response, optional
// Mcp-Session-Id). No resumability, no server-initiated streams — the gateway
// tool loop is strictly request/response. Per-tool Deny is NOT re-enforced here:
// the resolver only lists a connection in out.MCPServers when EVERY tool on it
// is Allow (the Allow-only rule), so nothing exposed through this client can be
// a Deny/Ask tool by construction.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/mcp"
)

// mcpClientProtocolVersion is the protocol version the gateway advertises in
// initialize. A compliant server negotiates down if it only speaks an older
// revision; these are the same servers the local CLIs already talk to.
const mcpClientProtocolVersion = "2025-06-18"

// mcpHTTPMaxBody bounds how much of a single MCP response the gateway will read.
// MCP JSON-RPC payloads are small; this only guards against a hostile or broken
// server streaming unbounded data into the tool loop.
const mcpHTTPMaxBody = 8 << 20 // 8 MiB

// mcpHTTPClient is a minimal streamable-HTTP MCP client bound to one connection
// endpoint. It is single-use per gateway task (built, initialized, queried, then
// dropped) so it holds no long-lived state beyond the negotiated session id.
type mcpHTTPClient struct {
	url     string
	headers map[string]string
	http    *http.Client

	mu        sync.Mutex
	nextID    int
	sessionID string
	inited    bool
}

// newMCPHTTPClient builds a client for one connection endpoint. headers carries
// the connection's real upstream auth (Authorization / X-API-Key / CF-Access-*),
// exactly as connections.MCPServerEntry assembled it for the direct cloud path.
func newMCPHTTPClient(url string, headers map[string]string, hc *http.Client) *mcpHTTPClient {
	if hc == nil {
		hc = &http.Client{
			Timeout: 30 * time.Second,
			// Never follow redirects. do() sets the connection's live auth on
			// every request (X-API-Key, CF-Access-Client-Id/Secret); Go's
			// default redirect policy strips only Authorization/Cookie on a
			// cross-host hop, so a malicious or compromised upstream could 302
			// us to an attacker host and harvest those secrets. A streamable-
			// HTTP MCP endpoint is a single JSON-RPC POST target and has no
			// legitimate reason to redirect — fail closed instead.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return fmt.Errorf("mcp http: refusing to follow redirect (would leak connection auth to another host)")
			},
		}
	}
	return &mcpHTTPClient{url: url, headers: headers, http: hc}
}

// ensureInitialized runs the initialize handshake once. A second caller reuses
// the negotiated session. It is safe to call before every operation.
func (c *mcpHTTPClient) ensureInitialized(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inited {
		return nil
	}
	params, _ := json.Marshal(map[string]any{
		"protocolVersion": mcpClientProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "cerebro-firtal-gateway",
			"version": "1",
		},
	})
	if _, err := c.rpcLocked(ctx, "initialize", params); err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}
	// notifications/initialized is a fire-and-forget notification (no id, no
	// result). A server that rejects it is non-fatal — the session is already
	// established by the initialize response above — so its error is ignored.
	_ = c.notifyLocked(ctx, "notifications/initialized", json.RawMessage(`{}`))
	c.inited = true
	return nil
}

// ListTools discovers every tool the connection exposes, with input schemas the
// persisted connection row does not carry — which is exactly why the gateway
// must reach the live server rather than build tool defs from the DB.
func (c *mcpHTTPClient) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	if err := c.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result, err := c.rpcLocked(ctx, "tools/list", json.RawMessage(`{}`))
	if err != nil {
		return nil, fmt.Errorf("mcp tools/list: %w", err)
	}
	var out mcp.ToolsListResult
	if err := json.Unmarshal(result, &out); err != nil {
		return nil, fmt.Errorf("mcp tools/list decode: %w", err)
	}
	return out.Tools, nil
}

// CallTool invokes one tool by name and returns the raw MCP result. A transport
// error is returned as err; a tool-level failure surfaces as a CallToolResult
// with IsError set, matching the mcp.Server contract the bridge already flattens.
func (c *mcpHTTPClient) CallTool(ctx context.Context, name string, args map[string]any) (mcp.CallToolResult, error) {
	if err := c.ensureInitialized(ctx); err != nil {
		return mcp.CallToolResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	params, err := json.Marshal(mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return mcp.CallToolResult{}, err
	}
	result, err := c.rpcLocked(ctx, "tools/call", params)
	if err != nil {
		return mcp.CallToolResult{}, fmt.Errorf("mcp tools/call %q: %w", name, err)
	}
	var out mcp.CallToolResult
	if err := json.Unmarshal(result, &out); err != nil {
		return mcp.CallToolResult{}, fmt.Errorf("mcp tools/call %q decode: %w", name, err)
	}
	return out, nil
}

// rpcLocked sends one JSON-RPC request and returns its result payload. The
// caller holds c.mu. It handles both a plain application/json response and a
// text/event-stream (SSE) response, and captures a negotiated Mcp-Session-Id.
func (c *mcpHTTPClient) rpcLocked(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	body, err := json.Marshal(mcp.Request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	result, rpcErr, err := decodeRPCResult(resp, id)
	if err != nil {
		return nil, err
	}
	if rpcErr != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcErr.Code, rpcErr.Message)
	}
	return result, nil
}

// notifyLocked sends a JSON-RPC notification (no id, no expected result). The
// caller holds c.mu. Any non-2xx or transport error is returned but callers may
// ignore it — a notification is advisory.
func (c *mcpHTTPClient) notifyLocked(ctx context.Context, method string, params json.RawMessage) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification http %d", resp.StatusCode)
	}
	return nil
}

// do posts one JSON-RPC frame with the connection auth headers and the MCP
// content negotiation the streamable-HTTP transport requires.
func (c *mcpHTTPClient) do(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", mcpClientProtocolVersion)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	return c.http.Do(req)
}

// rpcEnvelope is the slice of a JSON-RPC response the client reads back: the id
// (to match the request), the raw result, and any error object.
type rpcEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *mcp.RPCError   `json:"error"`
}

// decodeRPCResult extracts the result for request id from either a plain JSON
// response or an SSE stream. For SSE it returns the first data frame that is a
// JSON-RPC response matching id, so unrelated server notifications interleaved
// on the stream are skipped.
func decodeRPCResult(resp *http.Response, id int) (json.RawMessage, *mcp.RPCError, error) {
	reader := io.LimitReader(resp.Body, mcpHTTPMaxBody)
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return decodeSSEResult(reader, id)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, err
	}
	var env rpcEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, nil, fmt.Errorf("decode json-rpc response: %w", err)
	}
	return env.Result, env.Error, nil
}

// decodeSSEResult scans an SSE body for the JSON-RPC response whose id matches.
// Each event's data lines are concatenated per the SSE spec before decoding.
func decodeSSEResult(r io.Reader, id int) (json.RawMessage, *mcp.RPCError, error) {
	want := fmt.Sprintf("%d", id)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), mcpHTTPMaxBody)
	var data strings.Builder
	flush := func() (json.RawMessage, *mcp.RPCError, bool) {
		if data.Len() == 0 {
			return nil, nil, false
		}
		payload := data.String()
		data.Reset()
		var env rpcEnvelope
		if err := json.Unmarshal([]byte(payload), &env); err != nil {
			return nil, nil, false
		}
		if strings.TrimSpace(string(env.ID)) != want {
			return nil, nil, false
		}
		return env.Result, env.Error, true
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if result, rpcErr, ok := flush(); ok {
				return result, rpcErr, nil
			}
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		// Any other SSE field (event:, id:, retry:, comment) is irrelevant here.
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	// A stream that ended without a blank-line terminator on the last event.
	if result, rpcErr, ok := flush(); ok {
		return result, rpcErr, nil
	}
	return nil, nil, fmt.Errorf("mcp sse stream ended without a response for id %d", id)
}
