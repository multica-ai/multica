package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const testTimeout = 10 * time.Second

type testConnectionRequest struct {
	URL          string     `json:"url"`
	Type         string     `json:"type"`
	AuthConfig   AuthConfig `json:"auth_config"`
	ConnectionID string     `json:"connection_id,omitempty"` // when set, stored credentials are merged over empty form fields
}

type testConnectionResult struct {
	Reachable  bool       `json:"reachable"`
	StatusCode int        `json:"status_code,omitempty"`
	Tools      []toolInfo `json:"tools,omitempty"`
	Error      string     `json:"error,omitempty"`
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

func testMCPConnection(ctx context.Context, client *http.Client, url string, auth AuthConfig) testConnectionResult {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return testConnectionResult{Reachable: false, Error: fmt.Sprintf("build request: %s", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	addAuthHeaders(req, auth)

	resp, err := client.Do(req)
	if err != nil {
		return testConnectionResult{Reachable: false, Error: fmt.Sprintf("connect: %s", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return testConnectionResult{
			Reachable:  true,
			StatusCode: resp.StatusCode,
			Error:      fmt.Sprintf("server returned %d", resp.StatusCode),
		}
	}

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
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		return testConnectionResult{Reachable: true, StatusCode: resp.StatusCode}
	}
	if rpc.Error != nil {
		return testConnectionResult{Reachable: true, StatusCode: resp.StatusCode, Error: rpc.Error.Message}
	}
	if rpc.Result == nil {
		return testConnectionResult{Reachable: true, StatusCode: resp.StatusCode}
	}
	tools := make([]toolInfo, 0, len(rpc.Result.Tools))
	for _, t := range rpc.Result.Tools {
		tools = append(tools, toolInfo{Name: t.Name, Description: t.Description})
	}
	return testConnectionResult{Reachable: true, StatusCode: resp.StatusCode, Tools: tools}
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

	return testConnectionResult{Reachable: resp.StatusCode < 500, StatusCode: resp.StatusCode}
}
