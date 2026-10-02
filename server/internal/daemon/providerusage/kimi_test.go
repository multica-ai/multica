package providerusage

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseKimiUsage(t *testing.T) {
	body := []byte(`{
		"user": {"membership": {"level": "LEVEL_ADVANCED"}},
		"usage": {"limit": "100", "used": "2", "remaining": "98", "resetTime": "2026-09-15T19:39:34.389610Z"},
		"limits": [{
			"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"},
			"detail": {"limit": "100", "used": "8", "remaining": "92", "resetTime": "2026-09-11T16:39:34.389610Z"}
		}]
	}`)
	windows, plan, ok := ParseKimiUsage(body)
	if !ok || plan != "Advanced" || len(windows) != 2 {
		t.Fatalf("windows=%+v plan=%q ok=%v", windows, plan, ok)
	}
	if windows[0].ID != "weekly" || windows[0].PercentUsed != 2 {
		t.Fatalf("weekly = %+v", windows[0])
	}
	if windows[1].ID != "rolling" || windows[1].PercentUsed != 8 {
		t.Fatalf("rolling = %+v", windows[1])
	}
}

func TestKimiExpiredTokenSkipsVendor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kimi-code.json")
	raw := []byte(`{"access_token":"kimi-access","expires_at":10}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c := KimiCollector{
		AuthPath: path,
		Now:      func() time.Time { return time.Unix(100, 0).UTC() },
		Do: func(*http.Request) (*http.Response, error) {
			t.Fatal("expired kimi token must not call the vendor")
			return nil, nil
		},
	}
	got := c.Collect(t.Context())
	if got.Upload {
		t.Fatalf("expired token uploaded %+v", got)
	}
}

func TestKimiCollectorUploadsPercentsNotToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kimi-code.json")
	const token = "kimi-access-token"
	raw := []byte(`{"access_token":"` + token + `","expires_at":1000}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c := KimiCollector{
		AuthPath: path,
		Now:      func() time.Time { return time.Unix(100, 0).UTC() },
		Do: func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "Bearer "+token {
				t.Fatalf("auth = %q", req.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{"usage":{"limit":"50","remaining":"25","resetTime":"2026-09-20T00:00:00Z"}}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || len(got.Snapshot.Windows) != 1 || got.Snapshot.Windows[0].PercentUsed != 50 {
		t.Fatalf("result = %+v", got)
	}
	if strings.Contains(got.Snapshot.PlanName+got.Snapshot.ReasonCode, token) {
		t.Fatal("snapshot kept the access token")
	}
}

func TestKimiNoPlanIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kimi-code.json")
	if err := os.WriteFile(path, []byte(`{"access_token":"kimi-access","expires_at":1000}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := KimiCollector{
		AuthPath: path,
		Now:      func() time.Time { return time.Unix(100, 0).UTC() },
		Do: func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound, `{"error":"missing"}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonUnsupported {
		t.Fatalf("result = %+v", got)
	}
}
