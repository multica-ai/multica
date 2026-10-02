package providerusage

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAntigravityGroupedQuota(t *testing.T) {
	body := []byte(`{
		"currentTier": {"name": "Pro"},
		"groups": [
			{"displayName": "Gemini Models", "buckets": [
				{"displayName": "5-hour Limit", "remainingFraction": 0.8, "resetTime": "2026-09-23T18:00:00Z"},
				{"displayName": "Weekly Limit", "remainingFraction": 0.5, "resetTime": "2026-09-28T00:00:00Z"}
			]},
			{"displayName": "Claude and GPT models", "buckets": [
				{"bucketId": "session", "remainingFraction": 0.9, "resetTime": "2026-09-23T18:00:00Z"},
				{"displayName": "Weekly Limit", "remaining": {"case": "remainingFraction", "value": 0.25}}
			]}
		]
	}`)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	windows, plan, ok := ParseAntigravityQuota(body, now)
	if !ok || plan != "Pro" || len(windows) != 4 {
		t.Fatalf("windows=%+v plan=%q ok=%v", windows, plan, ok)
	}
	want := []struct {
		id      string
		percent float64
	}{
		{"gemini_hourly", 20},
		{"gemini_weekly", 50},
		{"third_party_hourly", 10},
		{"third_party_weekly", 75},
	}
	for i, item := range want {
		if windows[i].ID != item.id || windows[i].PercentUsed != item.percent {
			t.Fatalf("window %d = %+v", i, windows[i])
		}
	}
}

func TestAntigravityMissingSessionDoesNotCallVendor(t *testing.T) {
	c := AntigravityCollector{
		AuthPath: filepath.Join(t.TempDir(), "oauth_creds.json"),
		Do: func(*http.Request) (*http.Response, error) {
			t.Fatal("missing antigravity session must not call Google")
			return nil, nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonSessionUnavailable {
		t.Fatalf("result = %+v", got)
	}
}

func TestAntigravityExpiredTokenSkipsVendor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oauth_creds.json")
	raw := []byte(`{"access_token":"ya29.expired","expiry_date":1000}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c := AntigravityCollector{
		AuthPath: path,
		Now:      func() time.Time { return time.UnixMilli(5000) },
		Do: func(*http.Request) (*http.Response, error) {
			t.Fatal("expired antigravity token must not call Google")
			return nil, nil
		},
	}
	got := c.Collect(t.Context())
	if got.Upload {
		t.Fatalf("expired token uploaded %+v", got)
	}
}

func TestAntigravityCollectorUploadsPercentsNotToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oauth_creds.json")
	const token = "ya29.local-session"
	raw := []byte(`{"access_token":"` + token + `","expiry_date":5000,"projectId":"proj-1"}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c := AntigravityCollector{
		AuthPath: path,
		Now:      func() time.Time { return time.UnixMilli(1000) },
		Do: func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Fatalf("method = %s", req.Method)
			}
			if req.Header.Get("Authorization") != "Bearer "+token {
				t.Fatalf("auth = %q", req.Header.Get("Authorization"))
			}
			payload, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(payload), `"project":"proj-1"`) || strings.Contains(string(payload), token) {
				t.Fatalf("body = %s", payload)
			}
			return jsonResponse(http.StatusOK, `{"groups":[{"displayName":"Gemini Models","buckets":[{"displayName":"5-hour Limit","remainingFraction":0.6}]}]}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || len(got.Snapshot.Windows) != 1 || got.Snapshot.Windows[0].ID != "gemini_hourly" || got.Snapshot.Windows[0].PercentUsed != 40 {
		t.Fatalf("result = %+v", got)
	}
}
