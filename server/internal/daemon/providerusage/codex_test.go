package providerusage

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCodexUsageWindows(t *testing.T) {
	body := []byte(`{
		"plan_type": "plus",
		"rate_limit": {
			"primary_window": {"used_percent": 20, "limit_window_seconds": 18000, "reset_at": 1750000000},
			"secondary_window": {"used_percent": 5, "reset_after_seconds": 3600}
		}
	}`)
	now := time.Unix(1_700_000_000, 0).UTC()
	windows, plan, ok := ParseCodexUsage(body, now)
	if !ok || plan != "plus" || len(windows) != 2 {
		t.Fatalf("windows=%+v plan=%q ok=%v", windows, plan, ok)
	}
	if windows[0].ID != "primary" || windows[0].PercentUsed != 20 || windows[0].ResetsAt == nil {
		t.Fatalf("primary = %+v", windows[0])
	}
	if !windows[0].ResetsAt.Equal(time.Unix(1750000000, 0).UTC()) {
		t.Fatalf("primary reset = %v", windows[0].ResetsAt)
	}
	if windows[1].ID != "secondary" || windows[1].PercentUsed != 5 || windows[1].ResetsAt == nil {
		t.Fatalf("secondary = %+v", windows[1])
	}
	if !windows[1].ResetsAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("secondary reset = %v", windows[1].ResetsAt)
	}
}

func TestParseCodexUsageMalformed(t *testing.T) {
	if _, _, ok := ParseCodexUsage([]byte(`not-json`), time.Now()); ok {
		t.Fatal("non-json parsed")
	}
	if _, _, ok := ParseCodexUsage([]byte(`{}`), time.Now()); ok {
		t.Fatal("empty object parsed")
	}
	if _, _, ok := ParseCodexUsage([]byte(`{"rate_limit":{}}`), time.Now()); ok {
		t.Fatal("empty rate_limit parsed as success without a plan")
	}
}

func TestCodexAPIKeyLoginIsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"OPENAI_API_KEY":"sk-test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := CodexCollector{
		AuthPath: path,
		Do: func(*http.Request) (*http.Response, error) {
			t.Fatal("api-key login must not call the vendor")
			return nil, nil
		},
		Now: func() time.Time { return time.Unix(10, 0).UTC() },
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonAPIKeyOnly {
		t.Fatalf("result = %+v", got)
	}
}

func TestCodexCollectorUploadsPercentsNotToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	raw := []byte(`{"tokens":{"access_token":"test-access-token","account_id":"acct_1"}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var authz, account string
	c := CodexCollector{
		AuthPath: path,
		Do: func(req *http.Request) (*http.Response, error) {
			authz = req.Header.Get("Authorization")
			account = req.Header.Get("ChatGPT-Account-Id")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":7,"reset_at":1750000000}}}`)),
			}, nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.PlanName != "plus" || len(got.Snapshot.Windows) != 1 || got.Snapshot.Windows[0].PercentUsed != 7 {
		t.Fatalf("result = %+v", got)
	}
	if authz != "Bearer test-access-token" || account != "acct_1" {
		t.Fatalf("authz=%q account=%q", authz, account)
	}
	encoded, _ := json.Marshal(got.Snapshot)
	if strings.Contains(string(encoded), "test-access-token") || strings.Contains(string(encoded), "acct_1") {
		t.Fatalf("snapshot leaked a secret: %s", encoded)
	}
}

func TestCodexMissingFileIsNotLoggedIn(t *testing.T) {
	c := CodexCollector{AuthPath: filepath.Join(t.TempDir(), "missing-auth.json")}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonNotLoggedIn {
		t.Fatalf("result = %+v", got)
	}
}

func TestCodexUnauthorizedIsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"tokens":{"access_token":"t","account_id":"a"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := CodexCollector{
		AuthPath: path,
		Do: func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusUnauthorized, Body: http.NoBody, Header: http.Header{}}, nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonUnauthorized {
		t.Fatalf("result = %+v", got)
	}
}
