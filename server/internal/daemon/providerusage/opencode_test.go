package providerusage

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestOpenCodeEmptyKeyAndOAuthWithoutAccessAreMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"opencode-go":{"type":"api","key":"  "},"opencode":{"type":"oauth","access":""}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := OpenCodeCollector{AuthPath: path, Do: failVendor(t)}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonNotLoggedIn || len(got.Snapshot.Windows) != 0 {
		t.Fatalf("result = %+v", got)
	}
}

func TestOpenCodeOAuthUsesInferenceEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	const access = "st-oauth-access"
	const refresh = "rt-must-not-leak"
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour).UnixMilli()
	body := fmt.Sprintf(`{"opencode":{"type":"oauth","refresh":"%s","access":"%s","expires":%d,"metadata":{"server":"https://opencode.ai/console","orgID":"org_1"}}}`, refresh, access, expires)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c := OpenCodeCollector{
		AuthPath: path,
		Now:      func() time.Time { return now },
		Do: func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != openCodeOAuthUsageURL {
				t.Fatalf("url = %s", req.URL.String())
			}
			if req.Header.Get("Authorization") != "Bearer "+access {
				t.Fatalf("auth = %q", req.Header.Get("Authorization"))
			}
			if req.Header.Get("x-opencode-org-id") != "org_1" {
				t.Fatalf("org = %q", req.Header.Get("x-opencode-org-id"))
			}
			if strings.Contains(req.Header.Get("Authorization"), refresh) {
				t.Fatal("refresh token was sent")
			}
			return jsonResponse(http.StatusOK, `{"usage":{"weekly":{"percent":4,"resetsAt":"2026-09-07T00:00:00Z"}}}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.PlanName != "Go" || got.Snapshot.Windows[0].ID != "weekly" {
		t.Fatalf("result = %+v", got)
	}
	assertSnapshotOmits(t, got, access, refresh)
}

func TestOpenCodeExpiredTokenIsEmptySnapshot(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	past := strconv.FormatInt(now.Add(-time.Minute).UnixMilli(), 10)
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	const access = "st-expired-access"
	raw := fmt.Sprintf(`{"opencode":{"type":"oauth","access":"%s","expires":"%s"}}`, access, past)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c := OpenCodeCollector{
		AuthPath: path,
		Now:      func() time.Time { return now },
		Do:       failVendor(t),
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonCredentialExpired || len(got.Snapshot.Windows) != 0 || got.Snapshot.PlanName != "" {
		t.Fatalf("result = %+v", got)
	}
	assertSnapshotOmits(t, got, access)
}

func TestOpenCodeDatabaseCredentialWithoutAuthFile(t *testing.T) {
	dir := t.TempDir()
	const access = "st-from-db"
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	expires := strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)
	writeOpenCodeDB(t, filepath.Join(dir, "opencode.db"), []openCodeDBRow{
		{integration: "openrouter", value: `{"type":"api","key":"sk-or"}`, updated: 9},
		{integration: "opencode", value: fmt.Sprintf(`{"type":"oauth","access":"%s","expires":"%s","metadata":{"orgID":"org_db","server":"https://opencode.ai/console"}}`, access, expires), active: intPtr(1), updated: 3},
	})
	c := OpenCodeCollector{
		AuthPath: filepath.Join(dir, "auth.json"),
		Now:      func() time.Time { return now },
		Do: func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != openCodeOAuthUsageURL {
				t.Fatalf("url = %s", req.URL.String())
			}
			if req.Header.Get("Authorization") != "Bearer "+access {
				t.Fatalf("auth = %q", req.Header.Get("Authorization"))
			}
			if req.Header.Get("x-opencode-org-id") != "org_db" {
				t.Fatalf("org = %q", req.Header.Get("x-opencode-org-id"))
			}
			return jsonResponse(http.StatusOK, `{"usage":{"rolling":{"percent":45,"resetsAt":"2026-09-29T11:16:16Z"}}}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.Windows[0].PercentUsed != 45 {
		t.Fatalf("result = %+v", got)
	}
	assertSnapshotOmits(t, got, access, "sk-or")
}

func TestOpenCodeDatabaseIgnoresOtherVendors(t *testing.T) {
	dir := t.TempDir()
	writeOpenCodeDB(t, filepath.Join(dir, "opencode.db"), []openCodeDBRow{
		{integration: "openrouter", value: `{"type":"api","key":"sk-or"}`, updated: 5},
		{integration: "openai", value: `{"type":"api","key":"sk-openai"}`, updated: 8},
	})
	c := OpenCodeCollector{AuthPath: filepath.Join(dir, "auth.json"), Do: failVendor(t)}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonNotLoggedIn {
		t.Fatalf("result = %+v", got)
	}
}

func TestOpenCodeDatabasePrefersGoKeyAndSkipsInactive(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	writeOpenCodeDB(t, filepath.Join(dir, "opencode.db"), []openCodeDBRow{
		{integration: "opencode", value: `{"type":"oauth","access":"st-newer"}`, active: intPtr(1), updated: 9},
		{integration: "opencode-go", value: `{"type":"api","key":"sk-go-db"}`, updated: 1},
	})
	c := OpenCodeCollector{
		AuthPath: filepath.Join(dir, "auth.json"),
		Now:      func() time.Time { return now },
		Do: func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != openCodeGoUsageURL {
				t.Fatalf("url = %s", req.URL.String())
			}
			if req.Header.Get("Authorization") != "Bearer sk-go-db" {
				t.Fatalf("auth = %q", req.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{"usage":{"monthly":{"percent":2}}}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.Windows[0].ID != "monthly" {
		t.Fatalf("result = %+v", got)
	}

	dir = t.TempDir()
	writeOpenCodeDB(t, filepath.Join(dir, "opencode.db"), []openCodeDBRow{
		{integration: "opencode", value: `{"type":"oauth","access":"st-old"}`, active: intPtr(0), updated: 9},
		{integration: "opencode", value: `{"type":"oauth","access":"st-live"}`, active: intPtr(1), updated: 1},
	})
	c = OpenCodeCollector{
		AuthPath: filepath.Join(dir, "auth.json"),
		Now:      func() time.Time { return now },
		Do: func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "Bearer st-live" {
				t.Fatalf("auth = %q", req.Header.Get("Authorization"))
			}
			if req.URL.String() != openCodeOAuthUsageURL {
				t.Fatalf("url = %s", req.URL.String())
			}
			return jsonResponse(http.StatusOK, `{"usage":{"rolling":{"percent":1}}}`), nil
		},
	}
	got = c.Collect(t.Context())
	if !got.Upload || got.Snapshot.Windows[0].PercentUsed != 1 {
		t.Fatalf("result = %+v", got)
	}
}

func TestOpenCodeGoKeyBeatsDatabase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"opencode-go":"file-go-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeDB(t, filepath.Join(dir, "opencode.db"), []openCodeDBRow{
		{integration: "opencode", value: `{"type":"oauth","access":"st-db"}`, active: intPtr(1), updated: 20},
	})
	c := OpenCodeCollector{
		AuthPath: filepath.Join(dir, "auth.json"),
		Do: func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != openCodeGoUsageURL || req.Header.Get("Authorization") != "Bearer file-go-key" {
				t.Fatalf("req %s %q", req.URL, req.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{"usage":{"rolling":{"percent":9}}}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.Windows[0].PercentUsed != 9 {
		t.Fatalf("result = %+v", got)
	}
}

func TestOpenCodeOAuthFileIsFallbackWhenDatabaseUnreadable(t *testing.T) {
	dir := t.TempDir()
	const access = "st-file-oauth"
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	raw := fmt.Sprintf(`{"opencode":{"type":"oauth","access":"%s","expires":%d}}`, access, now.Add(time.Hour).UnixMilli())
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.db"), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := OpenCodeCollector{
		AuthPath: filepath.Join(dir, "auth.json"),
		Now:      func() time.Time { return now },
		Do: func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != openCodeOAuthUsageURL || req.Header.Get("Authorization") != "Bearer "+access {
				t.Fatalf("req %s %q", req.URL, req.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{"usage":{"rolling":{"percent":6}}}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.Windows[0].PercentUsed != 6 {
		t.Fatalf("result = %+v", got)
	}
}

func TestOpenCodeOAuth401SignedInIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	const access = "st-no-plan"
	raw := fmt.Sprintf(`{"opencode":{"type":"oauth","access":"%s","expires":%d,"metadata":{"server":"https://opencode.ai/console?ignored=1","orgID":"org_9"}}}`, access, now.Add(time.Hour).UnixMilli())
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c := OpenCodeCollector{
		AuthPath: filepath.Join(dir, "auth.json"),
		Now:      func() time.Time { return now },
		Do: func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case openCodeOAuthUsageURL:
				return jsonResponse(http.StatusUnauthorized, `{}`), nil
			case "https://opencode.ai/console/api/user":
				if req.Header.Get("Authorization") != "Bearer "+access || req.Header.Get("x-opencode-org-id") != "org_9" {
					t.Fatalf("console headers = %v", req.Header)
				}
				return jsonResponse(http.StatusOK, `{"id":"user"}`), nil
			default:
				t.Fatalf("unexpected url %s", req.URL.String())
				return nil, nil
			}
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonUnsupported || len(got.Snapshot.Windows) != 0 {
		t.Fatalf("result = %+v", got)
	}
	assertSnapshotOmits(t, got, access)
}

func TestOpenCodeOAuth401ProbeFailureIsUnauthorized(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	raw := fmt.Sprintf(`{"opencode":{"type":"oauth","access":"st-bad","expires":%d}}`, now.Add(time.Hour).UnixMilli())
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c := OpenCodeCollector{
		AuthPath: filepath.Join(dir, "auth.json"),
		Now:      func() time.Time { return now },
		Do: func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusUnauthorized, `{}`), nil
		},
	}
	got := c.Collect(t.Context())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonUnauthorized || len(got.Snapshot.Windows) != 0 {
		t.Fatalf("result = %+v", got)
	}
}

func TestOpenCodeGoKey401DoesNotProbeConsole(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"opencode-go":"go-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c := OpenCodeCollector{
		AuthPath: filepath.Join(dir, "auth.json"),
		Do: func(req *http.Request) (*http.Response, error) {
			calls++
			if req.URL.String() != openCodeGoUsageURL {
				t.Fatalf("url = %s", req.URL.String())
			}
			return jsonResponse(http.StatusUnauthorized, `{}`), nil
		},
	}
	got := c.Collect(t.Context())
	if calls != 1 || !got.Upload || got.Snapshot.ReasonCode != ReasonUnauthorized {
		t.Fatalf("calls=%d result=%+v", calls, got)
	}
}

func TestOpenCodeDataDirsAndPrimaryDirectoryWins(t *testing.T) {
	linux := openCodeDataDirsFor("linux", "/home/dev", "/xdg", "")
	if len(linux) != 1 || linux[0] != filepath.Join("/xdg", "opencode") {
		t.Fatalf("xdg dirs = %#v", linux)
	}
	fallback := openCodeDataDirsFor("linux", "/home/dev", "  ", "")
	if len(fallback) != 1 || fallback[0] != filepath.Join("/home/dev", ".local", "share", "opencode") {
		t.Fatalf("fallback dirs = %#v", fallback)
	}
	mac := openCodeDataDirsFor("darwin", "/Users/dev", "", "")
	if len(mac) != 2 || !strings.Contains(mac[1], "Library/Application Support/opencode") {
		t.Fatalf("darwin dirs = %#v", mac)
	}
	win := openCodeDataDirsFor("windows", `C:\Users\dev`, "", `C:\Users\dev\AppData\Roaming`)
	if len(win) != 2 || !strings.Contains(win[1], "AppData") {
		t.Fatalf("windows dirs = %#v", win)
	}

	primary := t.TempDir()
	other := t.TempDir()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	expires := strconv.FormatInt(now.Add(time.Hour).UnixMilli(), 10)
	writeOpenCodeDB(t, filepath.Join(primary, "opencode.db"), []openCodeDBRow{
		{integration: "opencode", value: fmt.Sprintf(`{"type":"oauth","access":"st-primary","expires":"%s"}`, expires), updated: 1},
	})
	if err := os.WriteFile(filepath.Join(other, "auth.json"), []byte(`{"opencode-go":"stale-go-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cred, ok := loadOpenCodeFromDirs(context.Background(), []string{primary, other})
	if !ok || cred.token != "st-primary" || !cred.oauth {
		t.Fatalf("cred = %+v ok=%v", cred, ok)
	}
}

func TestOpenCodeExpiryNumberAndString(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	number, ok := openCodeExpiry(float64(now.Add(time.Second).UnixMilli()))
	if !ok || !number.Equal(now.Add(time.Second)) {
		t.Fatalf("number expiry = %v ok=%v", number, ok)
	}
	text, ok := openCodeExpiry(strconv.FormatInt(now.UnixMilli(), 10))
	if !ok || !text.Equal(now) {
		t.Fatalf("string expiry = %v ok=%v", text, ok)
	}
	if _, ok := openCodeExpiry("not-a-time"); ok {
		t.Fatal("non-numeric expires parsed")
	}
	if _, ok := openCodeExpiry(float64(0)); ok {
		t.Fatal("zero expires parsed")
	}
	cred := openCodeCredential{hasExpiry: true, expires: now}
	if !cred.expired(now) {
		t.Fatal("expiry equal to now must be expired")
	}
}

type openCodeDBRow struct {
	integration string
	value       string
	active      *int
	updated     int64
}

func writeOpenCodeDB(t *testing.T, path string, rows []openCodeDBRow) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE credential (
		id TEXT PRIMARY KEY,
		integration_id TEXT,
		label TEXT NOT NULL,
		value TEXT NOT NULL,
		connector_id TEXT,
		method_id TEXT,
		active INTEGER,
		time_created INTEGER NOT NULL,
		time_updated INTEGER NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		var active any
		if row.active != nil {
			active = *row.active
		}
		if _, err := db.Exec(
			`INSERT INTO credential (id, integration_id, label, value, active, time_created, time_updated)
			 VALUES (?, ?, 'x', ?, ?, 0, ?)`,
			fmt.Sprintf("cred_%d", i), row.integration, row.value, active, row.updated,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func intPtr(v int) *int { return &v }

func failVendor(t *testing.T) HTTPDoer {
	t.Helper()
	return func(*http.Request) (*http.Response, error) {
		t.Fatal("vendor must not be called")
		return nil, nil
	}
}

func assertSnapshotOmits(t *testing.T, got Result, secrets ...string) {
	t.Helper()
	blob := got.Snapshot.Provider + got.Snapshot.PlanName + got.Snapshot.ReasonCode
	for _, window := range got.Snapshot.Windows {
		blob += window.ID
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(blob, secret) {
			t.Fatalf("snapshot included credential material")
		}
	}
}
