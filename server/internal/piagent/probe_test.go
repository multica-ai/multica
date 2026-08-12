package piagent

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// probeAgainst runs a probe against a test server. NewProber's dialer refuses
// loopback on purpose, so tests inject the httptest client instead.
func probeAgainst(t *testing.T, handler http.HandlerFunc, cfg Config, apiKey string) ProbeResult {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg.BaseURL = srv.URL
	prober := &Prober{Client: srv.Client()}
	return prober.Probe(context.Background(), cfg, apiKey)
}

func TestProbeOpenAICompletionsSendsCappedRequest(t *testing.T) {
	var gotPath, gotAuth string
	var body map[string]any
	result := probeAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}, Config{Provider: "deepseek", API: "openai-completions", Model: "deepseek-v4-flash"}, "sk-secret-value")

	if result.Outcome != OutcomeOK {
		t.Fatalf("outcome = %q, want ok (detail=%q)", result.Outcome, result.Detail)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-secret-value" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if body["model"] != "deepseek-v4-flash" {
		t.Errorf("model = %v", body["model"])
	}
	if body["max_tokens"] != float64(1) {
		t.Errorf("max_tokens = %v, want 1 so verification stays free", body["max_tokens"])
	}
}

func TestProbeAnthropicUsesKeyHeaderAndVersion(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	result := probeAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		w.WriteHeader(http.StatusOK)
	}, Config{Provider: "anthropic", API: "anthropic-messages", Model: "claude-sonnet-5"}, "sk-ant-1")

	if result.Outcome != OutcomeOK {
		t.Fatalf("outcome = %q", result.Outcome)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotKey != "sk-ant-1" {
		t.Errorf("x-api-key = %q", gotKey)
	}
	if gotVersion == "" {
		t.Error("anthropic-version header must be set")
	}
}

func TestProbeGoogleEncodesModelInPath(t *testing.T) {
	var gotPath, gotKey string
	result := probeAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		w.WriteHeader(http.StatusOK)
	}, Config{Provider: "google", API: "google-generative-ai", Model: "gemini-3.6-flash"}, "AIza-1")

	if result.Outcome != OutcomeOK {
		t.Fatalf("outcome = %q", result.Outcome)
	}
	if gotPath != "/models/gemini-3.6-flash:generateContent" {
		t.Errorf("path = %q", gotPath)
	}
	if gotKey != "AIza-1" {
		t.Errorf("x-goog-api-key = %q", gotKey)
	}
}

func TestProbeOpenAIResponsesRespectsMinimumCap(t *testing.T) {
	var body map[string]any
	probeAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusOK)
	}, Config{Provider: "openai", API: "openai-responses", Model: "gpt-5.6-sol"}, "sk-1")

	// The Responses API rejects a cap below 16; sending 1 would make every
	// probe fail as a bad request rather than verifying the key.
	if body["max_output_tokens"] != float64(16) {
		t.Errorf("max_output_tokens = %v, want 16", body["max_output_tokens"])
	}
}

func TestProbeClassifiesProviderFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   Outcome
	}{
		{"unauthorized", http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`, OutcomeInvalidKey},
		{"forbidden plain", http.StatusForbidden, `{"error":{"message":"not permitted"}}`, OutcomeInvalidKey},
		{"forbidden quota", http.StatusForbidden, `{"error":{"message":"Your credit balance is too low"}}`, OutcomeInsufficientQuota},
		{"payment required", http.StatusPaymentRequired, `{"error":{"message":"pay up"}}`, OutcomeInsufficientQuota},
		{"rate limited", http.StatusTooManyRequests, `{"error":{"message":"too many requests"}}`, OutcomeRateLimited},
		{"quota as 429", http.StatusTooManyRequests, `{"error":{"message":"insufficient_quota"}}`, OutcomeInsufficientQuota},
		{"missing model", http.StatusNotFound, `{"error":{"message":"model not found"}}`, OutcomeModelNotFound},
		{"missing endpoint", http.StatusNotFound, `{"error":{"message":"no such route"}}`, OutcomeEndpointNotFound},
		{"bad model", http.StatusBadRequest, `{"error":{"message":"unknown model id"}}`, OutcomeModelNotFound},
		{"server error", http.StatusBadGateway, `upstream exploded`, OutcomeProviderError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := probeAgainst(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}, Config{Provider: "p", API: "openai-completions", Model: "m"}, "sk-1")

			if result.Outcome != tc.want {
				t.Errorf("outcome = %q, want %q", result.Outcome, tc.want)
			}
			if result.Status != tc.status {
				t.Errorf("status = %d, want %d", result.Status, tc.status)
			}
			if result.Detail == "" {
				t.Error("detail must carry the provider message so the user can act on it")
			}
		})
	}
}

func TestProbeNeverEchoesTheSubmittedKey(t *testing.T) {
	const key = "sk-super-secret-value"
	result := probeAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		// Some providers echo the rejected credential straight back.
		_, _ = w.Write([]byte(`{"error":{"message":"key ` + key + ` is invalid"}}`))
	}, Config{Provider: "p", API: "openai-completions", Model: "m"}, key)

	if strings.Contains(result.Detail, key) {
		t.Fatalf("detail leaked the API key: %q", result.Detail)
	}
	if !strings.Contains(result.Detail, "***") {
		t.Errorf("detail = %q, want the key replaced with ***", result.Detail)
	}
}

func TestProbeRejectsUnsupportedAPI(t *testing.T) {
	prober := &Prober{Client: http.DefaultClient}
	result := prober.Probe(context.Background(), Config{
		Provider: "p", API: "made-up", BaseURL: "https://example.com", Model: "m",
	}, "sk-1")
	if result.Outcome != OutcomeProviderError {
		t.Fatalf("outcome = %q, want provider_error", result.Outcome)
	}
}

func TestIsPublicUnicastRejectsInternalRanges(t *testing.T) {
	blocked := []string{
		"127.0.0.1",
		"::1",
		"10.0.0.5",
		"192.168.1.10",
		"172.16.0.1",
		"169.254.169.254", // cloud metadata
		"100.64.0.1",      // CGNAT
		"0.0.0.0",
		"fd00::1",
		"224.0.0.1",
	}
	for _, raw := range blocked {
		if isPublicUnicast(net.ParseIP(raw)) {
			t.Errorf("%s must not be dialable from the server", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !isPublicUnicast(net.ParseIP(raw)) {
			t.Errorf("%s should be allowed", raw)
		}
	}
}

func TestValidateForRemoteProbeIsStricterThanValidate(t *testing.T) {
	loopback := Config{Provider: "local", API: "openai-completions", BaseURL: "http://127.0.0.1:8080/v1", Model: "m"}
	// The daemon legitimately allows this — Pi may talk to a local model server.
	if err := Validate(loopback); err != nil {
		t.Fatalf("Validate rejected a loopback config the daemon allows: %v", err)
	}
	// The server must never dial it on the user's behalf.
	if err := ValidateForRemoteProbe(loopback); err == nil {
		t.Error("ValidateForRemoteProbe must reject loopback HTTP")
	}

	privateHTTPS := Config{Provider: "internal", API: "openai-completions", BaseURL: "https://10.1.2.3/v1", Model: "m"}
	if err := ValidateForRemoteProbe(privateHTTPS); err == nil {
		t.Error("ValidateForRemoteProbe must reject a private IP literal")
	}

	// "localhost" parses as no IP at all, so the IP-literal branch misses it.
	for _, name := range []string{"https://localhost:8443/v1", "https://LocalHost/v1", "https://api.localhost/v1"} {
		byName := Config{Provider: "internal", API: "openai-completions", BaseURL: name, Model: "m"}
		if err := ValidateForRemoteProbe(byName); err == nil {
			t.Errorf("ValidateForRemoteProbe must reject %s by name", name)
		}
	}

	public := Config{Provider: "deepseek", API: "openai-completions", BaseURL: "https://api.deepseek.com", Model: "m"}
	if err := ValidateForRemoteProbe(public); err != nil {
		t.Errorf("ValidateForRemoteProbe rejected a normal endpoint: %v", err)
	}
}

func TestProbeReportsUnreachableEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := srv.Client()
	url := srv.URL
	srv.Close() // nothing is listening now

	prober := &Prober{Client: client}
	result := prober.Probe(context.Background(), Config{
		Provider: "p", API: "openai-completions", BaseURL: url, Model: "m",
	}, "sk-1")

	if result.Outcome != OutcomeNetworkUnreachable {
		t.Fatalf("outcome = %q, want network_unreachable", result.Outcome)
	}
}
