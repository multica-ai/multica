package connections

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const testTimeout = 15 * time.Second

type testConnectionRequest struct {
	URL          string     `json:"url"`
	Type         string     `json:"type"`
	AuthConfig   AuthConfig `json:"auth_config"`
	ConnectionID string     `json:"connection_id,omitempty"` // when set, stored credentials are merged over empty form fields
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
	return testAPIConnection(ctx, client, url, req.AuthConfig)
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

// testMCPConnection implements the MCP Streamable HTTP protocol:
// 1. POST initialize to get Mcp-Session-Id
// 2. POST tools/list with that session ID
// Handles both application/json and text/event-stream responses.
func testMCPConnection(ctx context.Context, client *http.Client, url string, auth AuthConfig) testConnectionResult {
	// Step 1: initialize
	initPayload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "cerebro-connection-test",
				"version": "1.0",
			},
		},
	})

	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(initPayload))
	if err != nil {
		return testConnectionResult{Reachable: false, Error: fmt.Sprintf("build request: %s", err)}
	}
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	addAuthHeaders(initReq, auth)

	initResp, err := client.Do(initReq)
	if err != nil {
		return testConnectionResult{Reachable: false, Error: fmt.Sprintf("connect: %s", err)}
	}
	defer initResp.Body.Close()

	if initResp.StatusCode >= 400 {
		return testConnectionResult{
			Reachable:  true,
			StatusCode: initResp.StatusCode,
			Error:      fmt.Sprintf("server returned %d on initialize", initResp.StatusCode),
		}
	}

	// Drain init response body so connection can be reused.
	sessionID := initResp.Header.Get("Mcp-Session-Id")
	_, _ = io.Copy(io.Discard, initResp.Body)

	// Step 2: tools/list
	toolsPayload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})

	toolsReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(toolsPayload))
	if err != nil {
		return testConnectionResult{Reachable: true, StatusCode: initResp.StatusCode, Error: fmt.Sprintf("build tools/list: %s", err)}
	}
	toolsReq.Header.Set("Content-Type", "application/json")
	toolsReq.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		toolsReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	addAuthHeaders(toolsReq, auth)

	resp, err := client.Do(toolsReq)
	if err != nil {
		return testConnectionResult{Reachable: true, StatusCode: initResp.StatusCode, Error: fmt.Sprintf("tools/list: %s", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return testConnectionResult{
			Reachable:  true,
			StatusCode: resp.StatusCode,
			Error:      fmt.Sprintf("server returned %d", resp.StatusCode),
		}
	}

	ct := resp.Header.Get("Content-Type")
	var body []byte
	if strings.Contains(ct, "text/event-stream") {
		body = extractSSEData(resp.Body)
	} else {
		body, _ = io.ReadAll(resp.Body)
	}

	return parseToolsResponse(body, resp.StatusCode)
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

func testAPIConnection(ctx context.Context, client *http.Client, url string, auth AuthConfig) testConnectionResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return testConnectionResult{Reachable: false, Error: fmt.Sprintf("build request: %s", err)}
	}
	addAuthHeaders(req, auth)

	resp, err := client.Do(req)
	if err != nil {
		return testConnectionResult{Reachable: false, Error: fmt.Sprintf("connect: %s", err)}
	}
	defer resp.Body.Close()

	result := testConnectionResult{Reachable: resp.StatusCode < 500, StatusCode: resp.StatusCode}
	// Drain the reachability probe's body before reusing the client for the
	// spec fetches, so the connection can be pooled.
	_, _ = io.Copy(io.Discard, resp.Body)

	// Discover the API's endpoints from its OpenAPI/Swagger spec (TECH-3410).
	// Best-effort: a reachable API with no machine-readable spec still tests
	// green — the admin just adds endpoints by hand.
	if eps := discoverAPIEndpoints(ctx, client, url, auth); len(eps) > 0 {
		result.Endpoints = eps
	}
	return result
}
