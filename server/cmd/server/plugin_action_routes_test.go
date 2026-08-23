package main

import (
	"io"
	"net/http"
	"testing"
)

func TestPluginActionRouteTrustBoundaries(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		authorization string
		wantStatus    int
		wantHandler   bool
	}{
		{
			name:       "public API rejects a missing token before the handler",
			path:       "/v1/context",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "public API rejects a browser session token",
			path:          "/v1/context",
			authorization: "Bearer " + testToken,
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "public API passes plugin tokens to the Action handler",
			path:          "/v1/context",
			authorization: "Bearer mpi_invalid",
			wantHandler:   true,
		},
		{
			name:          "surface bridge accepts a browser session",
			path:          "/api/plugin-bridge/v1/context",
			authorization: "Bearer " + testToken,
			wantHandler:   true,
		},
		{
			name:       "removed legacy prefix is not routed",
			path:       "/api/v1/plugin/context",
			wantStatus: http.StatusNotFound,
		},
		{
			name:          "public API does not expose person-triggered hooks",
			path:          "/v1/hooks/summarize",
			authorization: "Bearer mpi_invalid",
			wantStatus:    http.StatusNotFound,
		},
		{
			name:          "surface bridge retains person-triggered hooks",
			path:          "/api/plugin-bridge/v1/hooks/summarize",
			authorization: "Bearer " + testToken,
			wantStatus:    http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, testServer.URL+tt.path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer response.Body.Close()
			_, _ = io.Copy(io.Discard, response.Body)

			if tt.wantHandler {
				if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusNotFound {
					t.Fatalf("status = %d, request did not reach the Action handler", response.StatusCode)
				}
				return
			}
			if response.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestPluginActionBaseURLPrefersDedicatedVersionedBase(t *testing.T) {
	t.Setenv("MULTICA_PLUGIN_API_URL", " https://plugin-api.example.com/v1/ ")

	if got := pluginActionBaseURL("https://api.example.com/"); got != "https://plugin-api.example.com/v1" {
		t.Fatalf("pluginActionBaseURL() = %q", got)
	}
}

func TestPluginActionBaseURLFallsBackToPublicURL(t *testing.T) {
	t.Setenv("MULTICA_PLUGIN_API_URL", "")

	if got := pluginActionBaseURL(" https://api.example.com/ "); got != "https://api.example.com/v1" {
		t.Fatalf("pluginActionBaseURL() = %q", got)
	}
}

func TestPluginActionBaseURLOmittedWithoutPublicOrigin(t *testing.T) {
	t.Setenv("MULTICA_PLUGIN_API_URL", "")

	if got := pluginActionBaseURL(""); got != "" {
		t.Fatalf("pluginActionBaseURL() = %q, want empty", got)
	}
}
