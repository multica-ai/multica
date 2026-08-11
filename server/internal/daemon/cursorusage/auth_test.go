package cursorusage

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStateVscdbCandidatesIncludesOSPath(t *testing.T) {
	t.Parallel()
	paths := StateVscdbCandidates("/home/test")
	if len(paths) == 0 {
		t.Fatal("expected at least one candidate")
	}
	joined := filepath.ToSlash(paths[0])
	switch runtime.GOOS {
	case "darwin":
		if filepath.Base(filepath.Dir(joined)) != "globalStorage" {
			t.Fatalf("unexpected mac path: %s", paths[0])
		}
	case "windows":
		// APPDATA may or may not be set in CI; home fallback must exist.
		found := false
		for _, p := range paths {
			if filepath.Base(p) == "state.vscdb" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no state.vscdb candidate: %#v", paths)
		}
	default:
		if filepath.ToSlash(paths[0]) != "/home/test/.config/Cursor/User/globalStorage/state.vscdb" {
			t.Fatalf("unexpected linux path: %s", paths[0])
		}
	}
}

func TestReadAccessTokenAndSessionToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	payload, err := json.Marshal(map[string]any{
		"sub":  "github|user_01ABCDEF",
		"aud":  "https://cursor.com",
		"type": "session",
	})
	if err != nil {
		t.Fatal(err)
	}
	access := "hdr." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES (?, ?)`, accessTokenKey, access); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := ReadAccessTokenFromStateVscdb(dbPath)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if got != access {
		t.Fatalf("token = %q, want %q", got, access)
	}

	session, err := SessionTokenFromAccessToken(access)
	if err != nil {
		t.Fatalf("session token: %v", err)
	}
	want := "user_01ABCDEF%3A%3A" + access
	if session != want {
		t.Fatalf("session = %q, want %q", session, want)
	}
}

func TestFindStateVscdb(t *testing.T) {
	home := t.TempDir()
	var target string
	switch runtime.GOOS {
	case "darwin":
		target = filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb")
	case "windows":
		// Prefer the APPDATA candidate — pin it to the temp tree so a real
		// desktop install on the machine cannot shadow the fixture.
		appdata := filepath.Join(home, "AppData", "Roaming")
		t.Setenv("APPDATA", appdata)
		target = filepath.Join(appdata, "Cursor", "User", "globalStorage", "state.vscdb")
	default:
		target = filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("not-a-real-db"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := FindStateVscdb(home)
	if !ok {
		t.Fatal("expected to find state.vscdb")
	}
	if got != target {
		t.Fatalf("got %q want %q", got, target)
	}
}

func TestSessionTokenErrorDoesNotExposeJWTSubject(t *testing.T) {
	subject := "github|account-123@example.com"
	payload, err := json.Marshal(map[string]string{"sub": subject})
	if err != nil {
		t.Fatal(err)
	}
	access := "hdr." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
	_, err = SessionTokenFromAccessToken(access)
	if err == nil {
		t.Fatal("expected unsupported subject error")
	}
	if strings.Contains(err.Error(), subject) {
		t.Fatalf("error exposed JWT subject: %v", err)
	}
}
