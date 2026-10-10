package providerusage

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseGrokUsageCredits(t *testing.T) {
	body := []byte(`{"config":{
		"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","end":"2026-09-12T08:21:18.802818+00:00"},
		"creditUsagePercent":8.0,
		"productUsage":[{"product":"GrokBuild","usagePercent":8.0}]
	}}`)
	windows, plan, ok := ParseGrokUsage(body)
	if !ok || plan != "Grok Build" || len(windows) != 1 {
		t.Fatalf("windows=%+v plan=%q ok=%v", windows, plan, ok)
	}
	if windows[0].ID != "credits" || windows[0].PercentUsed != 8 || windows[0].ResetsAt == nil {
		t.Fatalf("window = %+v", windows[0])
	}
}

func TestParseGrokFreshWeeklyIsZero(t *testing.T) {
	body := []byte(`{"config":{"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","end":"2026-09-12T00:00:00Z"}}}`)
	windows, _, ok := ParseGrokUsage(body)
	if !ok || len(windows) != 1 || windows[0].PercentUsed != 0 || windows[0].ID != "credits" {
		t.Fatalf("windows=%+v ok=%v", windows, ok)
	}
}

func TestGrokUntrustedIssuerDoesNotCallVendor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	raw := []byte(`{"https://customer.example/::client":{"key":"foreign-token","oidc_issuer":"https://customer.example"}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c := GrokCollector{
		AuthPath: path,
		Do: func(*http.Request) (*http.Response, error) {
			t.Fatal("untrusted grok issuer must not call the vendor")
			return nil, nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonNotLoggedIn {
		t.Fatalf("result = %+v", got)
	}
}

func TestGrokCollectorUploadsPercentNotToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	const token = "grok-access-token"
	raw := []byte(`{"https://auth.x.ai::client":{"key":"` + token + `","expires_at":"2026-10-01T00:00:00Z"}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c := GrokCollector{
		AuthPath: path,
		Now:      func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) },
		Do: func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "Bearer "+token {
				t.Fatalf("auth = %q", req.Header.Get("Authorization"))
			}
			if req.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" {
				t.Fatalf("token auth header = %q", req.Header.Get("X-XAI-Token-Auth"))
			}
			return jsonResponse(http.StatusOK, `{"config":{"creditUsagePercent":12.5,"productUsage":[{"product":"GrokBuild"}]}}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.PlanName != "Grok Build" || got.Snapshot.Windows[0].PercentUsed != 12.5 {
		t.Fatalf("result = %+v", got)
	}
	if strings.Contains(got.Snapshot.PlanName, token) {
		t.Fatal("snapshot kept the access token")
	}
}
