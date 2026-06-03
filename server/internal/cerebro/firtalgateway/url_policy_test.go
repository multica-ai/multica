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

func TestValidateTrustedBaseURLAllowsInternalWhenOptedIn(t *testing.T) {
	t.Setenv(allowInternalEnvVar, "true")

	got, err := ValidateTrustedBaseURL("http://firtal-data-registry-private.internal:3000")
	if err != nil {
		t.Fatalf("ValidateTrustedBaseURL() error = %v", err)
	}
	if got != "http://firtal-data-registry-private.internal:3000" {
		t.Fatalf("normalized URL = %q", got)
	}
}

func TestValidateTrustedBaseURLStrictByDefault(t *testing.T) {
	t.Setenv(allowInternalEnvVar, "")

	if _, err := ValidateTrustedBaseURL("http://firtal-data-registry-private.internal:3000"); err == nil {
		t.Fatal("ValidateTrustedBaseURL() without opt-in succeeded, want error")
	}
}

// ValidateBaseURL guards untrusted workspace-supplied URLs and must never relax,
// even when the operator opted the trusted server URL into internal addresses.
func TestValidateBaseURLStaysStrictRegardlessOfOptIn(t *testing.T) {
	t.Setenv(allowInternalEnvVar, "true")

	for _, raw := range []string{
		"http://firtal-data-registry-private.internal:3000",
		"https://registry.internal",
		"https://10.0.0.5",
		"http://registry.example",
	} {
		if _, err := ValidateBaseURL(raw); err == nil {
			t.Fatalf("ValidateBaseURL(%q) with opt-in succeeded, want error", raw)
		}
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
