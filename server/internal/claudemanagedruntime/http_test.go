package claudemanagedruntime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestBridge(t *testing.T, anthropicURL string) *Bridge {
	t.Helper()
	b, err := New(Config{MulticaURL: "http://127.0.0.1:1", MulticaToken: "multica-secret", WorkspaceID: workspaceID, AnthropicURL: anthropicURL, AnthropicToken: "anthropic-secret", EnvironmentID: "env_1", StateDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAnthropicRequestsCarryManagedAgentsHeaders(t *testing.T) {
	var got *http.Request
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		io.WriteString(w, `{"id":"env_1"}`)
	}))
	defer remote.Close()
	b := newTestBridge(t, remote.URL+"/v1")
	if err := b.anthropic.call(context.Background(), http.MethodGet, "/environments/env_1", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got.URL.Path != "/v1/environments/env_1" {
		t.Errorf("path = %q, want /v1/environments/env_1", got.URL.Path)
	}
	want := map[string]string{
		"Authorization":     "Bearer anthropic-secret",
		"anthropic-version": "2023-06-01",
		"anthropic-beta":    "managed-agents-2026-04-01",
	}
	for name, value := range want {
		if got.Header.Get(name) != value {
			t.Errorf("%s = %q, want %q", name, got.Header.Get(name), value)
		}
	}
	if got.Header.Get("X-Workspace-ID") != "" {
		t.Error("X-Workspace-ID sent to Anthropic")
	}
}

func TestRateLimitedCreationHonoursRetryAfter(t *testing.T) {
	h := setup(t)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer remote.Close()
	h.b.anthropic.base = remote.URL
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := h.b.state.first()
	if r == nil || r.Phase != "prepared" {
		t.Fatal("rate-limited run not left for retry")
	}
	if wait := time.Until(r.RetryAt); wait < 110*time.Second || wait > 120*time.Second {
		t.Fatalf("retry in %v, want about 120s", wait)
	}
}

func TestWorkspaceHeaderRequirementNamesWorkspaceScopedKey(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"The anthropic-workspace-id header is required for this API key (sensitive-secret)"}}`)
	}))
	defer remote.Close()
	b := newTestBridge(t, remote.URL)
	err := b.anthropic.call(context.Background(), http.MethodGet, "/environments/env_1", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "workspace-scoped Anthropic API key") {
		t.Fatalf("error does not name the requirement: %v", err)
	}
	if strings.Contains(err.Error(), "sensitive-secret") {
		t.Fatalf("remote body leaked: %v", err)
	}
	if !definitelyRejected(err) {
		t.Fatal("workspace requirement not a definite rejection")
	}
}

func TestCheckReadsClaudeEnvironment(t *testing.T) {
	envID := "env_1"
	var paths []string
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("anthropic-beta") != "managed-agents-2026-04-01" {
			t.Error("Check bypassed the Anthropic client")
		}
		io.WriteString(w, `{"id":"`+envID+`"}`)
	}))
	defer anthropic.Close()
	multica := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"`+workspaceID+`"}`)
	}))
	defer multica.Close()
	b, err := New(Config{MulticaURL: multica.URL, MulticaToken: "multica-secret", WorkspaceID: workspaceID, AnthropicURL: anthropic.URL + "/v1", AnthropicToken: "anthropic-secret", EnvironmentID: "env_1", StateDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/v1/environments/env_1" {
		t.Fatalf("environment paths = %v", paths)
	}
	envID = "env_other"
	if err := b.Check(context.Background()); err == nil {
		t.Fatal("mismatched environment accepted")
	}
}

func TestNewDefaultsToAnthropicAPI(t *testing.T) {
	if b := newTestBridge(t, ""); b.anthropic.base != "https://api.anthropic.com/v1" {
		t.Fatalf("base = %q", b.anthropic.base)
	}
}

func TestAnthropicErrorClassification(t *testing.T) {
	cases := []struct {
		name               string
		status             int
		retryAfter         string
		rateLimited        bool
		definitelyRejected bool
		delay              time.Duration
	}{
		{"rate limited with retry-after", 429, "7", true, false, 7 * time.Second},
		{"rate limited without retry-after", 429, "", true, false, 30 * time.Second},
		{"not found", 404, "", false, true, 30 * time.Second},
		{"bad request", 400, "", false, true, 30 * time.Second},
		{"request timeout", 408, "", false, false, 30 * time.Second},
		{"server error", 500, "", false, false, 30 * time.Second},
		{"overloaded", 529, "3", false, false, 3 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, `{"type":"error","error":{"type":"x","message":"sensitive-secret"}}`)
			}))
			defer remote.Close()
			b := newTestBridge(t, remote.URL)
			err := b.anthropic.call(context.Background(), http.MethodGet, "/sessions/s", nil, nil)
			if err == nil || strings.Contains(err.Error(), "sensitive-secret") {
				t.Fatalf("unsafe or missing error: %v", err)
			}
			if rateLimited(err) != tc.rateLimited || definitelyRejected(err) != tc.definitelyRejected {
				t.Fatalf("rateLimited=%v definitelyRejected=%v", rateLimited(err), definitelyRejected(err))
			}
			if got := retryDelay(err); got != tc.delay {
				t.Fatalf("retryDelay = %v, want %v", got, tc.delay)
			}
		})
	}
}

func TestStandaloneDaemonIDAvoidsTheServerManagedNamespace(t *testing.T) {
	if strings.HasPrefix(StandaloneDaemonPrefix, "claude-managed-") {
		t.Fatalf("standalone daemon prefix %q is inside the namespace reserved for the server-run bridge", StandaloneDaemonPrefix)
	}
}
