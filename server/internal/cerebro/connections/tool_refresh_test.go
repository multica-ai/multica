package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A failed probe must never reach the store. Persisting an empty list would
// delete every per-tool permission row, and toolpolicy resolves a tool with no
// row to Allow — so blanking the cache silently ungates the whole connection.
// A nil store makes that contract enforceable: any write would panic.
func TestRefreshTools_FailedProbeDoesNotWrite(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "unreachable",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
		{
			name: "reachable but reports no tools",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			n, err := RefreshTools(context.Background(), nil, Connection{
				Name: "probe-fails",
				Type: TypeMCPHTTP,
				URL:  srv.URL,
			})
			if err == nil {
				t.Fatal("expected an error so the caller leaves the cached list untouched")
			}
			if n != 0 {
				t.Fatalf("expected 0 tools on failure, got %d", n)
			}
		})
	}
}

// Only mcp_http connections carry a tool list. An api connection is governed by
// endpoint_permissions instead, so the sweeper must skip it rather than probe
// it and log a false failure every night.
func TestRefreshTools_SkipsNonMCPConnections(t *testing.T) {
	n, err := RefreshTools(context.Background(), nil, Connection{
		Name: "some-api",
		Type: "api",
		URL:  "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("expected a skip, got error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 tools, got %d", n)
	}
}
