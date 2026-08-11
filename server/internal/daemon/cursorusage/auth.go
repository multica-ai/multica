// Package cursorusage talks to Cursor's local desktop session and undocumented
// Dashboard usage APIs so Multica can attach authoritative spend to
// cursor-agent tasks.
//
// Auth follows Tokscale's desktop auto-login path: read
// cursorAuth/accessToken from Cursor's state.vscdb and build the
// WorkosCursorSessionToken cookie. Credentials never leave the machine —
// only reconciled cost figures and opaque SHA-256 claim keys are reported
// upstream. Raw Cursor user ids and event fingerprints stay local.
package cursorusage

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite"
)

const accessTokenKey = "cursorAuth/accessToken"

// StateVscdbCandidates returns likely paths for Cursor's globalStorage DB.
// Mirrors Tokscale's layout (and Cursor Usage Agent on Windows).
func StateVscdbCandidates(homeDir string) []string {
	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = append(paths, filepath.Join(homeDir, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"))
	case "windows":
		if appdata := strings.TrimSpace(os.Getenv("APPDATA")); appdata != "" {
			paths = append(paths, filepath.Join(appdata, "Cursor", "User", "globalStorage", "state.vscdb"))
		}
		paths = append(paths, filepath.Join(homeDir, "AppData", "Roaming", "Cursor", "User", "globalStorage", "state.vscdb"))
	default:
		paths = append(paths, filepath.Join(homeDir, ".config", "Cursor", "User", "globalStorage", "state.vscdb"))
	}
	return paths
}

// FindStateVscdb returns the first existing Cursor state DB under homeDir
// (and APPDATA on Windows).
func FindStateVscdb(homeDir string) (string, bool) {
	for _, p := range StateVscdbCandidates(homeDir) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// ReadAccessTokenFromStateVscdb reads cursorAuth/accessToken from a Cursor
// state.vscdb SQLite DB. Opens read-only so a running Cursor IDE holding a
// write lock does not block us (WAL readers are allowed).
func ReadAccessTokenFromStateVscdb(dbPath string) (string, error) {
	// modernc accepts a file: URI; mode=ro keeps us from taking a write lock.
	uri := "file:" + filepath.ToSlash(dbPath) + "?mode=ro"
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return "", fmt.Errorf("open Cursor state DB %s: %w", dbPath, err)
	}
	defer db.Close()

	var token string
	err = db.QueryRow(`SELECT value FROM ItemTable WHERE key = ?`, accessTokenKey).Scan(&token)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("cursorAuth/accessToken not found in Cursor state DB (is Cursor logged in?)")
	}
	if err != nil {
		return "", fmt.Errorf("read cursorAuth/accessToken from %s: %w", dbPath, err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("cursorAuth/accessToken is empty")
	}
	return token, nil
}

// SessionTokenFromAccessToken builds the WorkosCursorSessionToken cookie value
// from a desktop access token JWT. Format matches Tokscale / browser cookies:
// `{user_id}%3A%3A{access_token}` (`%3A%3A` is URL-encoded `::`).
func SessionTokenFromAccessToken(accessToken string) (string, error) {
	userID, err := userIDFromAccessTokenJWT(accessToken)
	if err != nil {
		return "", err
	}
	return userID + "%3A%3A" + accessToken, nil
}

// AccountKeyFromSessionToken extracts the raw Cursor user id from a session
// cookie. Accepts Tokscale's `user_%3A%3Ajwt` form and the raw `user_::jwt`
// value. Callers that upload or persist claim keys must wrap this with
// OpaqueClaimKey — the plaintext id must not leave the machine.
func AccountKeyFromSessionToken(sessionToken string) string {
	token := strings.TrimSpace(sessionToken)
	if token == "" {
		return ""
	}
	token = strings.ReplaceAll(token, "%3A%3A", "::")
	token = strings.ReplaceAll(token, "%3a%3a", "::")
	if before, _, ok := strings.Cut(token, "::"); ok {
		return strings.TrimSpace(before)
	}
	return ""
}

// ReadLocalSessionToken reads the local Cursor desktop login and returns a
// WorkosCursorSessionToken cookie value. No network I/O.
func ReadLocalSessionToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	dbPath, ok := FindStateVscdb(home)
	if !ok {
		return "", fmt.Errorf("Cursor desktop state.vscdb not found (install Cursor and sign in first)")
	}
	accessToken, err := ReadAccessTokenFromStateVscdb(dbPath)
	if err != nil {
		return "", err
	}
	return SessionTokenFromAccessToken(accessToken)
}

func userIDFromAccessTokenJWT(accessToken string) (string, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid Cursor access token JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some tokens include padding; retry with standard URL encoding.
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return "", fmt.Errorf("decode Cursor access token JWT payload: %w", err)
		}
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parse Cursor access token JWT: %w", err)
	}
	sub := strings.TrimSpace(claims.Sub)
	if sub == "" {
		return "", fmt.Errorf("Cursor access token JWT missing sub claim")
	}

	// Tokscale extracts the trailing `user_…` segment from subs like
	// `github|user_01…` / `auth0|user_…`.
	idx := strings.Index(sub, "user_")
	if idx < 0 {
		return "", fmt.Errorf("Cursor access token JWT sub does not contain a user id")
	}
	rest := sub[idx:]
	end := len(rest)
	for i, r := range rest {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			end = i
			break
		}
	}
	userID := rest[:end]
	if len(userID) <= len("user_") {
		return "", fmt.Errorf("Cursor access token JWT sub does not contain a user id")
	}
	return userID, nil
}
