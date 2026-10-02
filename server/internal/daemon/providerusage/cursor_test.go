package providerusage

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestParseCursorUsageSummary(t *testing.T) {
	body := []byte(`{
		"billingCycleStart": "2026-09-01T00:00:00Z",
		"billingCycleEnd": "2026-10-01T00:00:00Z",
		"membershipType": "pro",
		"individualUsage": {"plan": {"autoPercentUsed": 42.5, "apiPercentUsed": 0, "used": 0, "limit": 0}}
	}`)
	windows, plan, ok := ParseCursorUsage(body)
	if !ok || plan != "pro" || len(windows) != 1 {
		t.Fatalf("windows=%+v plan=%q ok=%v", windows, plan, ok)
	}
	if windows[0].ID != "auto" || windows[0].PercentUsed != 42.5 {
		t.Fatalf("window = %+v", windows[0])
	}
	if windows[0].ResetsAt == nil || !windows[0].ResetsAt.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("reset = %v", windows[0].ResetsAt)
	}
}

func TestParseCursorUsageIncludesAPIWhenUsed(t *testing.T) {
	body := []byte(`{"membershipType":"pro","individualUsage":{"plan":{"autoPercentUsed":1,"apiPercentUsed":12.5}}}`)
	windows, _, ok := ParseCursorUsage(body)
	if !ok || len(windows) != 2 || windows[1].ID != "api" || windows[1].PercentUsed != 12.5 {
		t.Fatalf("windows = %+v", windows)
	}
}

func TestParseCursorUsageMalformed(t *testing.T) {
	if _, _, ok := ParseCursorUsage([]byte(`not-json`)); ok {
		t.Fatal("non-json parsed")
	}
	if _, _, ok := ParseCursorUsage([]byte(`{}`)); ok {
		t.Fatal("empty object parsed")
	}
	if _, _, ok := ParseCursorUsage([]byte(`[]`)); ok {
		t.Fatal("array parsed")
	}
}

func TestCursorStateDBPathsLinux(t *testing.T) {
	paths := cursorStateDBPathsFor("linux", "/home/dev", "/xdg", "")
	if len(paths) != 2 {
		t.Fatalf("paths = %#v", paths)
	}
	if !strings.HasPrefix(paths[0], "/xdg/Cursor/") || !strings.Contains(paths[1], "/home/dev/.config/Cursor/") {
		t.Fatalf("linux paths = %#v", paths)
	}
	mac := cursorStateDBPathsFor("darwin", "/Users/dev", "", "")
	if len(mac) != 1 || !strings.Contains(mac[0], "Library/Application Support/Cursor/") {
		t.Fatalf("darwin paths = %#v", mac)
	}
	if got := cursorStateDBPathsFor("linux", "/home/dev", "", ""); len(got) != 1 || strings.Contains(got[0], "Library") {
		t.Fatalf("linux must not use the macOS library path: %#v", got)
	}
}

func TestCursorCollectorUploadsPercentsNotCookie(t *testing.T) {
	var gotCookie string
	c := CursorCollector{
		Session: func(context.Context) (CursorSession, error) {
			return CursorSession{AccessToken: "test-access-token", AuthID: "user_123"}, nil
		},
		Do: func(req *http.Request) (*http.Response, error) {
			gotCookie = req.Header.Get("Cookie")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"membershipType":"pro","billingCycleEnd":"2026-10-01T00:00:00Z","individualUsage":{"plan":{"autoPercentUsed":10,"apiPercentUsed":0}}}`)),
			}, nil
		},
	}
	got := c.Collect(context.Background())
	if !got.Upload || len(got.Snapshot.Windows) != 1 || got.Snapshot.Windows[0].PercentUsed != 10 {
		t.Fatalf("result = %+v", got)
	}
	if !strings.Contains(gotCookie, "WorkosCursorSessionToken=user_123::test-access-token") {
		t.Fatalf("cookie = %q", gotCookie)
	}
	encoded, _ := json.Marshal(got.Snapshot)
	if strings.Contains(string(encoded), "test-access-token") || strings.Contains(string(encoded), "WorkosCursorSessionToken") {
		t.Fatalf("snapshot leaked a secret: %s", encoded)
	}
}

func TestCursorCollectorUnauthorizedIsEmpty(t *testing.T) {
	c := CursorCollector{
		Session: func(context.Context) (CursorSession, error) {
			return CursorSession{AccessToken: "t", AuthID: "a"}, nil
		},
		Do: func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       http.NoBody,
				Header:     http.Header{},
			}, nil
		},
	}
	got := c.Collect(context.Background())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonUnauthorized {
		t.Fatalf("result = %+v", got)
	}
}

func TestCursorCollectorRateLimitKeepsLastGood(t *testing.T) {
	c := CursorCollector{
		Session: func(context.Context) (CursorSession, error) {
			return CursorSession{AccessToken: "t", AuthID: "a"}, nil
		},
		Do: func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       http.NoBody,
				Header:     http.Header{"Retry-After": []string{"120"}},
			}, nil
		},
	}
	got := c.Collect(context.Background())
	if got.Upload || got.Backoff != 120*time.Second {
		t.Fatalf("result = %+v", got)
	}
}

func TestReadCursorStateDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.vscdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES (?, ?), (?, ?)`,
		"cursorAuth/accessToken", "db-token",
		"cursorAuth/stripeMembershipAuthId", "auth-id",
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	session, ok, err := readCursorStateDB(context.Background(), path)
	if err != nil || !ok {
		t.Fatalf("session err=%v ok=%v", err, ok)
	}
	if session.AccessToken != "db-token" || session.AuthID != "auth-id" {
		t.Fatalf("session = %+v", session)
	}
}

func TestCursorCLIConfigFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cli-config.json")
	body := []byte(`{"authInfo":{"authId":"user_9","accessToken":"cli-token","email":"a@example.com"}}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := readCursorCLIConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if session.AuthID != "user_9" || session.AccessToken != "cli-token" {
		t.Fatalf("session = %+v", session)
	}
}

func TestCursorMissingSessionIsEmpty(t *testing.T) {
	c := CursorCollector{
		Session: func(context.Context) (CursorSession, error) {
			return CursorSession{}, errString("no session")
		},
	}
	got := c.Collect(context.Background())
	if !got.Upload || got.Snapshot.ReasonCode != ReasonSessionUnavailable {
		t.Fatalf("result = %+v", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
