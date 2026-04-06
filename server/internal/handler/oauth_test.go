package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOAuthRedirectURLFrom(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		want      string
	}{
		{
			name:      "empty falls back to default",
			serverURL: "",
			want:      "http://localhost:8080/auth/callback",
		},
		{
			name:      "ws with path",
			serverURL: "ws://localhost:8080/ws",
			want:      "http://localhost:8080/auth/callback",
		},
		{
			name:      "wss with path",
			serverURL: "wss://example.com/ws",
			want:      "https://example.com/auth/callback",
		},
		{
			name:      "ws no path",
			serverURL: "ws://localhost:8080",
			want:      "http://localhost:8080/auth/callback",
		},
		{
			name:      "wss with port and path",
			serverURL: "wss://api.example.com:443/ws",
			want:      "https://api.example.com:443/auth/callback",
		},
		{
			name:      "http URL passes through",
			serverURL: "http://localhost:8080/api",
			want:      "http://localhost:8080/auth/callback",
		},
		{
			name:      "https URL passes through",
			serverURL: "https://api.example.com",
			want:      "https://api.example.com/auth/callback",
		},
		{
			name:      "invalid URL falls back to default",
			serverURL: "://not-valid",
			want:      "http://localhost:8080/auth/callback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := oauthRedirectURLFrom(tt.serverURL)
			if got != tt.want {
				t.Errorf("oauthRedirectURLFrom(%q) = %q, want %q", tt.serverURL, got, tt.want)
			}
		})
	}
}

func TestIsValidCLICallback(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   bool
	}{
		{
			name:   "localhost with port",
			rawURL: "http://localhost:12345/callback",
			want:   true,
		},
		{
			name:   "127.0.0.1 with port",
			rawURL: "http://127.0.0.1:54321/auth",
			want:   true,
		},
		{
			name:   "localhost no port",
			rawURL: "http://localhost/callback",
			want:   true,
		},
		{
			name:   "https not allowed",
			rawURL: "https://localhost:12345/callback",
			want:   false,
		},
		{
			name:   "remote host rejected",
			rawURL: "http://evil.com:12345/callback",
			want:   false,
		},
		{
			name:   "empty string",
			rawURL: "",
			want:   false,
		},
		{
			name:   "relative path",
			rawURL: "/callback",
			want:   false,
		},
		{
			name:   "javascript scheme",
			rawURL: "javascript:alert(1)",
			want:   false,
		},
		{
			name:   "ftp scheme",
			rawURL: "ftp://localhost/file",
			want:   false,
		},
		{
			name:   "localhost as subdomain of external host",
			rawURL: "http://localhost.evil.com:8080/callback",
			want:   false,
		},
		{
			name:   "IPv6 loopback rejected (only explicit localhost/127.0.0.1)",
			rawURL: "http://[::1]:8080/callback",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidCLICallback(tt.rawURL)
			if got != tt.want {
				t.Errorf("isValidCLICallback(%q) = %v, want %v", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestIsValidNextPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "root path",
			path: "/",
			want: true,
		},
		{
			name: "dashboard path",
			path: "/dashboard",
			want: true,
		},
		{
			name: "nested path",
			path: "/issues/123",
			want: true,
		},
		{
			name: "path with query",
			path: "/issues?status=open",
			want: true,
		},
		{
			name: "protocol-relative URL (open redirect)",
			path: "//evil.com",
			want: false,
		},
		{
			name: "protocol-relative with path",
			path: "//evil.com/steal-token",
			want: false,
		},
		{
			name: "absolute URL",
			path: "https://evil.com",
			want: false,
		},
		{
			name: "empty string",
			path: "",
			want: false,
		},
		{
			name: "relative path no leading slash",
			path: "dashboard",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidNextPath(tt.path)
			if got != tt.want {
				t.Errorf("isValidNextPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsSecureRequest(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*http.Request)
		want  bool
	}{
		{
			name:  "plain HTTP",
			setup: func(r *http.Request) {},
			want:  false,
		},
		{
			name: "X-Forwarded-Proto https",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "https")
			},
			want: true,
		},
		{
			name: "X-Forwarded-Proto HTTPS (case insensitive)",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "HTTPS")
			},
			want: true,
		},
		{
			name: "X-Forwarded-Proto http",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "http")
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			tt.setup(r)
			got := isSecureRequest(r)
			if got != tt.want {
				t.Errorf("isSecureRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOAuthParamsRoundTrip(t *testing.T) {
	// Verify that the oauthParams struct can round-trip through base64-encoded JSON
	// (the format used in the oauth_params cookie).
	tests := []struct {
		name   string
		params oauthParams
	}{
		{
			name: "all fields",
			params: oauthParams{
				NextURL:     "/dashboard",
				CLICallback: "http://localhost:12345/callback",
				CLIState:    "some-random-state",
			},
		},
		{
			name: "only next URL",
			params: oauthParams{
				NextURL: "/issues/abc-123",
			},
		},
		{
			name: "cli state with pipe characters",
			params: oauthParams{
				CLICallback: "http://localhost:8080/cb",
				CLIState:    "state|with|pipes",
			},
		},
		{
			name: "cli state with special characters",
			params: oauthParams{
				CLICallback: "http://127.0.0.1:9999/auth",
				CLIState:    "key=value&foo=bar|baz",
			},
		},
		{
			name:   "empty params",
			params: oauthParams{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode (same as OAuthStart)
			paramsJSON, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			encoded := base64.RawURLEncoding.EncodeToString(paramsJSON)

			// Decode (same as OAuthCallback)
			decoded, err := base64.RawURLEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("base64 decode failed: %v", err)
			}
			var got oauthParams
			if err := json.Unmarshal(decoded, &got); err != nil {
				t.Fatalf("json.Unmarshal failed: %v", err)
			}

			if got.NextURL != tt.params.NextURL {
				t.Errorf("NextURL = %q, want %q", got.NextURL, tt.params.NextURL)
			}
			if got.CLICallback != tt.params.CLICallback {
				t.Errorf("CLICallback = %q, want %q", got.CLICallback, tt.params.CLICallback)
			}
			if got.CLIState != tt.params.CLIState {
				t.Errorf("CLIState = %q, want %q", got.CLIState, tt.params.CLIState)
			}
		})
	}
}
