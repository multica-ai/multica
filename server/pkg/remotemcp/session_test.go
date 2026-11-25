package remotemcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionListsPagesAndCallsThroughSSE(t *testing.T) {
	var sawSession, sawAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sawAuth = true
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if req.Method != "initialize" && r.Header.Get("Mcp-Session-Id") == "s-1" {
			sawSession = true
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "s-1")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-03-26"}}`, req.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if strings.Contains(string(req.Params), `"cursor":"p2"`) {
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"b"}]}}`, req.ID)
				return
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"a","annotations":{"readOnlyHint":true}}],"nextCursor":"p2"}}`, req.ID)
		case "tools/call":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"image\",\"data\":\"AAA\",\"mimeType\":\"image/png\"}]}}\n\n", req.ID)
		}
	}))
	defer server.Close()
	t.Setenv(DevOriginsEnv, server.URL)

	headers := http.Header{"Authorization": {"Bearer tok"}}
	session, err := OpenSession(t.Context(), server.URL, headers)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || !strings.Contains(string(tools[0]), "readOnlyHint") {
		t.Fatalf("tools = %s", tools)
	}
	result, err := session.CallTool(t.Context(), "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), `"type":"image"`) {
		t.Fatalf("result = %s", result)
	}
	if !sawAuth || !sawSession {
		t.Fatalf("auth=%v session=%v", sawAuth, sawSession)
	}
}

func TestSessionReportsUnauthorizedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	t.Setenv(DevOriginsEnv, server.URL)

	_, err := OpenSession(t.Context(), server.URL, nil)
	var status *StatusError
	if !errors.As(err, &status) || status.Code != http.StatusUnauthorized {
		t.Fatalf("err = %v, want StatusError 401", err)
	}
}

func TestOAuthErrorCarriesCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"refresh token already used"}`)
	}))
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPost, server.URL, nil)
	err := doOAuthJSON(server.Client(), request, &struct{}{})
	var oauthErr *OAuthError
	if !errors.As(err, &oauthErr) || oauthErr.Code != "invalid_grant" || !oauthErr.Rejected() {
		t.Fatalf("err = %#v", err)
	}
	if transient := (&OAuthError{Status: http.StatusServiceUnavailable}); transient.Rejected() {
		t.Fatal("a 503 must not count as a rejected grant")
	}
}

func TestRegisteredAuthMethodDefaultsPerRFC7591(t *testing.T) {
	for _, tc := range []struct{ method, secret, want string }{
		{"", "", "none"},
		{"", "s", "client_secret_basic"},
		{"client_secret_post", "s", "client_secret_post"},
	} {
		if got := registeredAuthMethod(tc.method, tc.secret); got != tc.want {
			t.Fatalf("registeredAuthMethod(%q, %q) = %q, want %q", tc.method, tc.secret, got, tc.want)
		}
	}
}
