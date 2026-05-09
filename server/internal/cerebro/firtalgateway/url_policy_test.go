package firtalgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateBaseURLRequiresPublicHTTPSFQDN(t *testing.T) {
	t.Parallel()

	tests := []string{
		"http://registry.example",
		"https://user:pass@registry.example",
		"https://registry.example?x=1",
		"https://registry.example#fragment",
		"https://localhost",
		"https://registry",
		"https://127.0.0.1",
		"https://10.0.0.5",
		"https://100.64.0.1",
		"https://169.254.169.254",
		"https://198.18.0.1",
		"https://[2001:db8::1]",
		"https://registry.internal",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if _, err := ValidateBaseURL(raw); err == nil {
				t.Fatalf("ValidateBaseURL(%q) succeeded, want error", raw)
			}
		})
	}
}

func TestValidateBaseURLNormalizesValidGatewayURL(t *testing.T) {
	t.Parallel()

	got, err := ValidateBaseURL(" https://Registry.Example.Com/ ")
	if err != nil {
		t.Fatalf("ValidateBaseURL() error = %v", err)
	}
	if got != "https://registry.example.com" {
		t.Fatalf("normalized URL = %q", got)
	}
}

func TestNewHTTPClientBlocksLoopbackDial(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("loopback request should be blocked before reaching test server")
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	if _, err := NewHTTPClient().Do(req); err == nil {
		t.Fatal("NewHTTPClient().Do(loopback) succeeded, want error")
	}
}
