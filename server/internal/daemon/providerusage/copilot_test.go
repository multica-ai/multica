package providerusage

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCopilotUsage(t *testing.T) {
	body := []byte(`{
		"copilot_plan": "individual",
		"quota_reset_date": "2026-10-01T00:00:00Z",
		"quota_snapshots": {
			"completions": {"entitlement": 2000, "remaining": 1500},
			"chat": {"unlimited": true, "entitlement": 1},
			"premium_interactions": {"entitlement": 100, "used": 25, "reset_date": "2026-10-01T00:00:00Z"}
		}
	}`)
	windows, plan, ok := ParseCopilotUsage(body)
	if !ok || plan != "individual" || len(windows) != 2 {
		t.Fatalf("windows=%+v plan=%q ok=%v", windows, plan, ok)
	}
	if windows[0].ID != "premium_interactions" || windows[0].PercentUsed != 25 {
		t.Fatalf("premium = %+v", windows[0])
	}
	if windows[1].ID != "completions" || windows[1].PercentUsed != 25 {
		t.Fatalf("completions = %+v", windows[1])
	}
	if windows[0].ResetsAt == nil || !windows[0].ResetsAt.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("reset = %v", windows[0].ResetsAt)
	}
}

func TestCopilotCollectorUsesHostsTokenAndDropsIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.yml")
	const token = "gho_local_session_token"
	body := "github.com:\n    user: octocat\n    oauth_token: " + token + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var sawAuth bool
	c := CopilotCollector{
		HostsPath: path,
		Token: func(ctx context.Context) (string, error) {
			t.Fatal("hosts.yml token must not fall through to gh")
			return "", nil
		},
		Do: func(req *http.Request) (*http.Response, error) {
			sawAuth = req.Header.Get("Authorization") == "Bearer "+token
			if strings.Contains(req.URL.String(), token) {
				t.Fatal("token leaked into the URL")
			}
			return jsonResponse(http.StatusOK, `{"copilot_plan":"business","quota_snapshots":{"premium_interactions":{"entitlement":10,"used":4}}}`), nil
		},
		Now: func() time.Time { return time.Unix(10, 0).UTC() },
	}
	got := c.Collect(t.Context())
	if !sawAuth || !got.Upload || got.Snapshot.PlanName != "business" || len(got.Snapshot.Windows) != 1 {
		t.Fatalf("result = %+v sawAuth=%v", got, sawAuth)
	}
	if got.Snapshot.Windows[0].PercentUsed != 40 {
		t.Fatalf("percent = %v", got.Snapshot.Windows[0].PercentUsed)
	}
	encoded := got.Snapshot.Provider + got.Snapshot.PlanName + got.Snapshot.ReasonCode
	if strings.Contains(encoded, token) {
		t.Fatal("snapshot kept the github token")
	}
}

func TestCopilotMissingSessionDoesNotCallVendor(t *testing.T) {
	c := CopilotCollector{
		HostsPath: filepath.Join(t.TempDir(), "missing.yml"),
		Token: func(ctx context.Context) (string, error) {
			return "", os.ErrNotExist
		},
		Do: func(*http.Request) (*http.Response, error) {
			t.Fatal("missing session must not call GitHub")
			return nil, nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonNotLoggedIn || len(got.Snapshot.Windows) != 0 {
		t.Fatalf("result = %+v", got)
	}
}
