package providerusage

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseOpenCodeUsage(t *testing.T) {
	body := []byte(`{"usage":{
		"rolling":{"status":"ok","percent":12.5,"resetsAt":"2026-09-06T12:31:06.611Z"},
		"weekly":{"status":"ok","percent":3,"resetsAt":"2026-09-07T00:00:00.611Z"},
		"monthly":{"status":"ok","percent":1,"resetsAt":"2026-10-03T13:09:45.611Z"}
	}}`)
	windows, ok := ParseOpenCodeUsage(body)
	if !ok || len(windows) != 3 {
		t.Fatalf("windows=%+v ok=%v", windows, ok)
	}
	if windows[0].ID != "rolling" || windows[0].PercentUsed != 12.5 || windows[0].ResetsAt == nil {
		t.Fatalf("rolling = %+v", windows[0])
	}
	if windows[1].ID != "weekly" || windows[2].ID != "monthly" {
		t.Fatalf("order = %+v", windows)
	}
}

func TestOpenCodeIgnoresOtherVendorKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"openai":{"type":"api","key":"sk-other"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := OpenCodeCollector{
		AuthPath: path,
		Do: func(*http.Request) (*http.Response, error) {
			t.Fatal("a non-go key must not call OpenCode")
			return nil, nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonNotLoggedIn {
		t.Fatalf("result = %+v", got)
	}
}

func TestOpenCodeCollectorUploadsGoWindows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	const token = "opencode-go-key"
	if err := os.WriteFile(path, []byte(`{"opencode-go":{"type":"api","key":"`+token+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := OpenCodeCollector{
		AuthPath: path,
		Now:      func() time.Time { return time.Unix(10, 0).UTC() },
		Do: func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "Bearer "+token {
				t.Fatalf("auth = %q", req.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{"usage":{"rolling":{"percent":20,"resetsAt":"2026-09-06T12:31:06Z"}}}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.PlanName != "Go" || got.Snapshot.Windows[0].ID != "rolling" || got.Snapshot.Windows[0].PercentUsed != 20 {
		t.Fatalf("result = %+v", got)
	}
}

func TestOpenCodeForbiddenIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"opencode-go":"go-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := OpenCodeCollector{
		AuthPath: path,
		Do: func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden, `{}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonUnsupported {
		t.Fatalf("result = %+v", got)
	}
}
