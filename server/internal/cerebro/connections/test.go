package connections

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

const testTimeout = 15 * time.Second

type testConnectionRequest struct {
	URL          string     `json:"url"`
	Type         string     `json:"type"`
	AuthConfig   AuthConfig `json:"auth_config"`
	ConnectionID string     `json:"connection_id,omitempty"` // when set, stored credentials are merged over empty form fields
	// SpecURL is an explicit OpenAPI/Swagger document URL (api type). When set,
	// endpoint discovery fetches this URL instead of probing well-known paths,
	// and failures are reported instead of silently ignored.
	SpecURL string `json:"spec_url,omitempty"`
	// SpecContent is the raw text of an uploaded OpenAPI/Swagger document (JSON
	// or YAML). Takes precedence over SpecURL; parsed without any network fetch.
	SpecContent string `json:"spec_content,omitempty"`
}

type testConnectionResult struct {
	Reachable  bool                 `json:"reachable"`
	StatusCode int                  `json:"status_code,omitempty"`
	Tools      []toolInfo           `json:"tools,omitempty"`
	Endpoints  []discoveredEndpoint `json:"endpoints,omitempty"`
	Error      string               `json:"error,omitempty"`
}

type toolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func doTestConnection(ctx context.Context, req testConnectionRequest) testConnectionResult {
	url := strings.TrimRight(req.URL, "/")
	client := &http.Client{Timeout: testTimeout}
	if req.Type == TypeMCPHTTP {
		return testMCPConnection(ctx, client, url, req.AuthConfig)
	}
	return testAPIConnection(ctx, client, url, req.AuthConfig, req.SpecURL, req.SpecContent)
}

func addAuthHeaders(httpReq *http.Request, auth AuthConfig) {
	if auth.BearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+auth.BearerToken)
	} else if auth.APIKey != "" {
		header := auth.APIKeyHeader
		if header == "" {
			header = "X-API-Key"
		}
		httpReq.Header.Set(header, auth.APIKey)
	}
	if auth.CFAccessID != "" && auth.CFAccessSecret != "" {
		httpReq.Header.Set("CF-Access-Client-Id", auth.CFAccessID)
		httpReq.Header.Set("CF-Access-Client-Secret", auth.CFAccessSecret)
	}
}

// mcpClientProtocolVersion is the protocol version this probe proposes on
// initialize. Servers negotiate down by replying with the version they speak;
// the reply's version is echoed back in the MCP-Protocol-Version header, so
// both newer and older Streamable HTTP servers are handled.
const mcpClientProtocolVersion = "2025-06-18"

// testMCPConnection probes an MCP server over HTTP. It first speaks the
// Streamable HTTP transport (single endpoint, POST initialize → initialized →
// tools/list). When the server rejects the POST — the signature of the legacy
// HTTP+SSE transport (protocol 2024-11-05), which only accepts GET on its base
// URL — it falls back to that transport, so every remote MCP server type is
// discoverable, not just Streamable HTTP ones.
func testMCPConnection(ctx context.Context, client *http.Client, url string, auth AuthConfig) testConnectionResult {
	result, tryLegacy := testMCPStreamableHTTP(ctx, client, url, auth)
	if !tryLegacy {
		return result
	}
	if legacy, ok := testMCPLegacySSE(ctx, url, auth); ok {
		return legacy
	}
	// The legacy transport didn't work either — report the streamable attempt,
	// which carries the more meaningful status code / error.
	return result
}

// testMCPStreamableHTTP implements the MCP Streamable HTTP transport:
//  1. POST initialize (proposing our protocol version), capture Mcp-Session-Id
//     and the server's negotiated protocol version
//  2. POST notifications/initialized (spec-required; strict servers reject
//     tools/list without it)
//  3. POST tools/list with the session ID + negotiated protocol version header
//
// Handles both application/json and text/event-stream responses. The second
// return is true when the failure mode suggests the server speaks the legacy
// HTTP+SSE transport instead (POST rejected), so the caller can fall back.
func testMCPStreamableHTTP(ctx context.Context, client *http.Client, url string, auth AuthConfig) (testConnectionResult, bool) {
	// Step 1: initialize
	initPayload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcpClientProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "cerebro-connection-test",
				"version": "1.0",
			},
		},
	})

	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(initPayload))
	if err != nil {
		return testConnectionResult{Reachable: false, Error: fmt.Sprintf("build request: %s", err)}, false
	}
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	addAuthHeaders(initReq, auth)

	initResp, err := client.Do(initReq)
	if err != nil {
		return testConnectionResult{Reachable: false, Error: fmt.Sprintf("connect: %s", err)}, false
	}
	defer initResp.Body.Close()

	if initResp.StatusCode >= 400 {
		return testConnectionResult{
			Reachable:  true,
			StatusCode: initResp.StatusCode,
			Error:      fmt.Sprintf("server returned %d on initialize", initResp.StatusCode),
		}, true // POST rejected — likely a legacy HTTP+SSE server; worth the fallback
	}

	sessionID := initResp.Header.Get("Mcp-Session-Id")
	initBody := readRPCBody(initResp)
	protocolVersion := negotiatedProtocolVersion(initBody)

	setMCPHeaders := func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		if sessionID != "" {
			r.Header.Set("Mcp-Session-Id", sessionID)
		}
		if protocolVersion != "" {
			r.Header.Set("MCP-Protocol-Version", protocolVersion)
		}
		addAuthHeaders(r, auth)
	}

	// Step 2: notifications/initialized — required by the spec before normal
	// operation; best-effort because permissive servers work without it.
	notifPayload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	if notifReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(notifPayload)); err == nil {
		setMCPHeaders(notifReq)
		if notifResp, err := client.Do(notifReq); err == nil {
			_, _ = io.Copy(io.Discard, notifResp.Body)
			notifResp.Body.Close()
		}
	}

	// Step 3: tools/list
	toolsPayload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})

	toolsReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(toolsPayload))
	if err != nil {
		return testConnectionResult{Reachable: true, StatusCode: initResp.StatusCode, Error: fmt.Sprintf("build tools/list: %s", err)}, false
	}
	setMCPHeaders(toolsReq)

	resp, err := client.Do(toolsReq)
	if err != nil {
		return testConnectionResult{Reachable: true, StatusCode: initResp.StatusCode, Error: fmt.Sprintf("tools/list: %s", err)}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return testConnectionResult{
			Reachable:  true,
			StatusCode: resp.StatusCode,
			Error:      fmt.Sprintf("server returned %d", resp.StatusCode),
		}, false
	}

	return parseToolsResponse(readRPCBody(resp), resp.StatusCode), false
}

// readRPCBody returns the JSON-RPC payload of a response, unwrapping SSE
// framing when the server replied with text/event-stream.
func readRPCBody(resp *http.Response) []byte {
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return extractSSEData(resp.Body)
	}
	body, _ := io.ReadAll(resp.Body)
	return body
}

// negotiatedProtocolVersion extracts result.protocolVersion from an initialize
// response. Empty when the body isn't a parsable initialize result.
func negotiatedProtocolVersion(body []byte) string {
	var rpc struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &rpc); err != nil {
		return ""
	}
	return rpc.Result.ProtocolVersion
}

// extractSSEData reads SSE lines and concatenates the first data: payload that looks like JSON-RPC.
func extractSSEData(r io.Reader) []byte {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if strings.HasPrefix(payload, "{") {
				return []byte(payload)
			}
		}
	}
	return nil
}

// sseEvent is one parsed Server-Sent Event (name + data payload).
type sseEvent struct {
	name string
	data string
}

// readSSEEvents parses an SSE stream into events on ch, closing ch when the
// stream ends. Multi-line data fields are joined with newlines per the SSE spec.
func readSSEEvents(r io.Reader, ch chan<- sseEvent) {
	defer close(ch)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	ev := sseEvent{}
	var dataLines []string
	flush := func() {
		if len(dataLines) == 0 && ev.name == "" {
			return
		}
		ev.data = strings.Join(dataLines, "\n")
		ch <- ev
		ev = sseEvent{}
		dataLines = nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "event:"):
			ev.name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
}

// testMCPLegacySSE implements the deprecated MCP HTTP+SSE transport (protocol
// 2024-11-05), still used by many deployed servers:
//  1. GET the base URL with Accept: text/event-stream — the server opens a
//     stream whose first "endpoint" event announces the message-POST URL
//  2. POST initialize / notifications/initialized / tools/list to that URL
//     (each returns 202); JSON-RPC responses arrive back on the stream
//
// Returns ok=false when the server doesn't speak this transport, so the caller
// keeps the streamable-HTTP attempt's diagnostics.
func testMCPLegacySSE(ctx context.Context, baseURL string, auth AuthConfig) (testConnectionResult, bool) {
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	streamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return testConnectionResult{}, false
	}
	streamReq.Header.Set("Accept", "text/event-stream")
	addAuthHeaders(streamReq, auth)

	// The stream stays open for the whole exchange, so this client must not
	// carry a whole-response timeout — the ctx deadline bounds it instead.
	streamClient := &http.Client{}
	streamResp, err := streamClient.Do(streamReq)
	if err != nil {
		return testConnectionResult{}, false
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK ||
		!strings.Contains(streamResp.Header.Get("Content-Type"), "text/event-stream") {
		_, _ = io.Copy(io.Discard, io.LimitReader(streamResp.Body, 4096))
		return testConnectionResult{}, false
	}

	events := make(chan sseEvent, 16)
	go readSSEEvents(streamResp.Body, events)

	// The first event must announce the message endpoint (relative or absolute).
	messageURL := ""
	for messageURL == "" {
		select {
		case <-ctx.Done():
			return testConnectionResult{}, false
		case ev, open := <-events:
			if !open {
				return testConnectionResult{}, false
			}
			if ev.name == "endpoint" && ev.data != "" {
				resolved, err := resolveSSEEndpoint(baseURL, ev.data)
				if err != nil {
					return testConnectionResult{}, false
				}
				messageURL = resolved
			}
		}
	}

	client := &http.Client{Timeout: testTimeout}
	post := func(payload map[string]any) bool {
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, messageURL, bytes.NewReader(body))
		if err != nil {
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		addAuthHeaders(req, auth)
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode < 400
	}
	// waitForResponse consumes stream events until the JSON-RPC response with
	// the wanted id arrives.
	waitForResponse := func(id float64) []byte {
		for {
			select {
			case <-ctx.Done():
				return nil
			case ev, open := <-events:
				if !open {
					return nil
				}
				var rpc struct {
					ID any `json:"id"`
				}
				if err := json.Unmarshal([]byte(ev.data), &rpc); err != nil {
					continue
				}
				if got, ok := rpc.ID.(float64); ok && got == id {
					return []byte(ev.data)
				}
			}
		}
	}

	if !post(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "cerebro-connection-test", "version": "1.0"},
		},
	}) {
		return testConnectionResult{}, false
	}
	if waitForResponse(1) == nil {
		return testConnectionResult{}, false
	}
	_ = post(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if !post(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}}) {
		return testConnectionResult{}, false
	}
	toolsBody := waitForResponse(2)
	if toolsBody == nil {
		return testConnectionResult{}, false
	}
	return parseToolsResponse(toolsBody, http.StatusOK), true
}

// resolveSSEEndpoint resolves the endpoint-event payload (an absolute URL or a
// path relative to the SSE base URL) into an absolute message URL.
func resolveSSEEndpoint(baseURL, endpoint string) (string, error) {
	base, err := neturl.Parse(baseURL)
	if err != nil {
		return "", err
	}
	ref, err := neturl.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", err
	}
	return base.ResolveReference(ref).String(), nil
}

func parseToolsResponse(body []byte, statusCode int) testConnectionResult {
	var rpc struct {
		Result *struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &rpc); err != nil {
		return testConnectionResult{Reachable: true, StatusCode: statusCode}
	}
	if rpc.Error != nil {
		return testConnectionResult{Reachable: true, StatusCode: statusCode, Error: rpc.Error.Message}
	}
	if rpc.Result == nil {
		return testConnectionResult{Reachable: true, StatusCode: statusCode}
	}
	tools := make([]toolInfo, 0, len(rpc.Result.Tools))
	for _, t := range rpc.Result.Tools {
		tools = append(tools, toolInfo{Name: t.Name, Description: t.Description})
	}
	return testConnectionResult{Reachable: true, StatusCode: statusCode, Tools: tools}
}

func testAPIConnection(ctx context.Context, client *http.Client, url string, auth AuthConfig, specURL, specContent string) testConnectionResult {
	explicitSpec := strings.TrimSpace(specContent) != "" || strings.TrimSpace(specURL) != ""

	var result testConnectionResult
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		result = testConnectionResult{Reachable: false, Error: fmt.Sprintf("build request: %s", err)}
	} else {
		addAuthHeaders(req, auth)
		resp, err := client.Do(req)
		if err != nil {
			result = testConnectionResult{Reachable: false, Error: fmt.Sprintf("connect: %s", err)}
		} else {
			result = testConnectionResult{Reachable: resp.StatusCode < 500, StatusCode: resp.StatusCode}
			// Drain the reachability probe's body before reusing the client for
			// the spec fetches, so the connection can be pooled.
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}

	// An explicitly provided spec (uploaded document or direct URL) is resolved
	// even when the API base URL is unreachable — an uploaded document needs no
	// network at all, and the spec may live on a different host. Explicit input
	// gets explicit errors, unlike the best-effort well-known-path probing.
	if explicitSpec {
		eps, errMsg := resolveExplicitSpec(ctx, client, specURL, specContent, auth)
		result.Endpoints = eps
		if errMsg != "" {
			if result.Error != "" {
				result.Error += "; " + errMsg
			} else {
				result.Error = errMsg
			}
		}
		return result
	}

	if !result.Reachable && result.StatusCode == 0 {
		return result
	}

	// Discover the API's endpoints from its OpenAPI/Swagger spec (TECH-3410).
	// Best-effort: a reachable API with no machine-readable spec still tests
	// green — the admin just adds endpoints by hand.
	if eps := discoverAPIEndpoints(ctx, client, url, auth); len(eps) > 0 {
		result.Endpoints = eps
	}
	return result
}
