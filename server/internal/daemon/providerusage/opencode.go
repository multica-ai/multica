package providerusage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	openCodeGoUsageURL     = "https://opencode.ai/zen/go/v1/usage"
	openCodeOAuthUsageURL  = "https://opencode.ai/inference/go/v1/usage"
	openCodeDefaultConsole = "https://opencode.ai/console"
)

var openCodeWindows = []string{"rolling", "weekly", "monthly"}

// OpenCodeCollector reads OpenCode Go plan windows from the sign-in OpenCode
// stored locally. Lookup order matches OpenCode 1.18+: auth.json's opencode-go
// key, then the credential table in opencode.db, then auth.json's mirrored
// opencode OAuth entry. Other vendors' rows are ignored. The collector uploads
// derived windows only; the token never leaves this process.
type OpenCodeCollector struct {
	AuthPath string
	Do       HTTPDoer
	Now      func() time.Time
}

type openCodeCredential struct {
	token     string
	oauth     bool
	console   string
	org       string
	expires   time.Time
	hasExpiry bool
}

func (c openCodeCredential) expired(now time.Time) bool {
	return c.hasExpiry && !c.expires.After(now)
}

func (c openCodeCredential) usageURL() string {
	if c.oauth {
		return openCodeOAuthUsageURL
	}
	return openCodeGoUsageURL
}

func (c OpenCodeCollector) Collect(ctx context.Context) Result {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	cred, found := c.loadCredential(ctx)
	if !found {
		return emptyResult(ProviderOpenCode, ReasonNotLoggedIn, now)
	}
	// Expired OAuth copies are not refreshed. OpenCode renews the sign-in the
	// next time it runs; until then the snapshot stays empty so a stale token
	// is not reported as a missing login, and collection does not fail a task.
	if cred.expired(now) {
		return emptyResult(ProviderOpenCode, ReasonCredentialExpired, now)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cred.usageURL(), nil)
	if err != nil {
		return Result{Upload: false}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+cred.token)
	if cred.org != "" {
		req.Header.Set("x-opencode-org-id", cred.org)
	}
	body, status, retryAfter, err := performVendor(c.Do, req)
	if err != nil {
		return Result{Upload: false}
	}
	// zen/go refuses an OAuth token and inference/go refuses a Go key, each
	// with a 401 that is also what an account with no Go plan returns. The
	// console's /api/user answers 401 only to a bad token, so a 200 here means
	// the sign-in is fine and the account simply has no Go plan.
	if status == http.StatusUnauthorized && cred.oauth && openCodeConsoleSignedIn(ctx, c.Do, cred) {
		return emptyResult(ProviderOpenCode, ReasonUnsupported, now)
	}
	if status == http.StatusForbidden {
		return emptyResult(ProviderOpenCode, ReasonUnsupported, now)
	}
	reason, backoff, transient := classifyVendorStatus(status, retryAfter)
	if transient {
		return Result{Upload: false, Backoff: backoff}
	}
	if reason != "" {
		return emptyResult(ProviderOpenCode, reason, now)
	}
	windows, ok := ParseOpenCodeUsage(body)
	if !ok {
		return Result{Upload: false}
	}
	return Result{
		Upload: true,
		Snapshot: Snapshot{
			Provider:    ProviderOpenCode,
			PlanName:    "Go",
			CollectedAt: now,
			Windows:     windows,
		},
	}
}

func (c OpenCodeCollector) loadCredential(ctx context.Context) (openCodeCredential, bool) {
	if c.AuthPath != "" {
		return loadOpenCodeCredentialInDir(ctx, filepath.Dir(c.AuthPath), filepath.Base(c.AuthPath))
	}
	return loadOpenCodeFromDirs(ctx, openCodeDataDirs())
}

func loadOpenCodeFromDirs(ctx context.Context, dirs []string) (openCodeCredential, bool) {
	for _, dir := range dirs {
		if cred, ok := loadOpenCodeCredentialInDir(ctx, dir, "auth.json"); ok {
			return cred, true
		}
	}
	return openCodeCredential{}, false
}

// loadOpenCodeCredentialInDir tries one data directory in CodeNotch's order:
// auth.json opencode-go, then opencode.db, then auth.json's opencode OAuth entry.
func loadOpenCodeCredentialInDir(ctx context.Context, dir, authName string) (openCodeCredential, bool) {
	auth := filepath.Join(dir, authName)
	if cred, ok := loadOpenCodeGoKey(auth); ok {
		return cred, true
	}
	if cred, ok := loadOpenCodeDBCredential(ctx, filepath.Join(dir, "opencode.db")); ok {
		return cred, true
	}
	if cred, ok := loadOpenCodeOAuthFile(auth); ok {
		return cred, true
	}
	return openCodeCredential{}, false
}

func openCodeDataDirs() []string {
	home, _ := os.UserHomeDir()
	config, _ := os.UserConfigDir()
	return openCodeDataDirsFor(runtime.GOOS, home, os.Getenv("XDG_DATA_HOME"), config)
}

// openCodeDataDirsFor is OpenCode's own data dir, then the platform paths this
// collector already searched. XDG_DATA_HOME replaces ~/.local/share when set.
func openCodeDataDirsFor(goos, home, xdg, configDir string) []string {
	var dirs []string
	seen := map[string]struct{}{}
	add := func(dir string) {
		if dir == "" {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	if strings.TrimSpace(xdg) != "" {
		add(filepath.Join(xdg, "opencode"))
	} else if home != "" {
		add(filepath.Join(home, ".local", "share", "opencode"))
	}
	if goos == "darwin" && home != "" {
		add(filepath.Join(home, "Library", "Application Support", "opencode"))
	}
	if goos == "windows" && configDir != "" {
		add(filepath.Join(configDir, "opencode"))
	}
	return dirs
}

func loadOpenCodeGoKey(path string) (openCodeCredential, bool) {
	root, ok := readOpenCodeAuth(path)
	if !ok {
		return openCodeCredential{}, false
	}
	entry, exists := root["opencode-go"]
	if !exists {
		return openCodeCredential{}, false
	}
	return openCodeCredentialFrom(entry)
}

func loadOpenCodeOAuthFile(path string) (openCodeCredential, bool) {
	root, ok := readOpenCodeAuth(path)
	if !ok {
		return openCodeCredential{}, false
	}
	entry := asMap(root["opencode"])
	if stringField(entry, "type") != "oauth" {
		return openCodeCredential{}, false
	}
	return openCodeCredentialFrom(entry)
}

func readOpenCodeAuth(path string) (map[string]any, bool) {
	body, err := readRegularFile(path)
	if err != nil {
		return nil, false
	}
	return decodeObject(body)
}

func openCodeCredentialFrom(entry any) (openCodeCredential, bool) {
	if token, isString := entry.(string); isString {
		token = strings.TrimSpace(token)
		if token == "" {
			return openCodeCredential{}, false
		}
		return openCodeCredential{token: token}, true
	}
	object := asMap(entry)
	if object == nil {
		return openCodeCredential{}, false
	}
	if stringField(object, "type") == "oauth" {
		token := stringField(object, "access")
		if token == "" {
			return openCodeCredential{}, false
		}
		meta := asMap(object["metadata"])
		expires, hasExpiry := openCodeExpiry(object["expires"])
		return openCodeCredential{
			token:     token,
			oauth:     true,
			console:   stringField(meta, "server"),
			org:       stringField(meta, "orgID"),
			expires:   expires,
			hasExpiry: hasExpiry,
		}, true
	}
	for _, key := range []string{"key", "apiKey", "api_key", "token", "accessToken"} {
		token := stringField(object, key)
		if token != "" {
			return openCodeCredential{token: token}, true
		}
	}
	return openCodeCredential{}, false
}

// openCodeExpiry reads expires as epoch milliseconds. auth.json stores a
// number; opencode.db stores the same instant as a string.
func openCodeExpiry(v any) (time.Time, bool) {
	var ms float64
	switch value := v.(type) {
	case float64:
		ms = value
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return time.Time{}, false
		}
		ms = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return time.Time{}, false
		}
		ms = parsed
	default:
		return time.Time{}, false
	}
	if ms <= 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(ms)).UTC(), true
}

func loadOpenCodeDBCredential(ctx context.Context, path string) (openCodeCredential, bool) {
	db, err := openOpenCodeDB(ctx, path)
	if err != nil {
		return openCodeCredential{}, false
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, `
		SELECT integration_id, value FROM credential
		WHERE integration_id IN ('opencode-go', 'opencode')
		  AND COALESCE(active, 1) != 0
		ORDER BY time_updated DESC`)
	if err != nil {
		return openCodeCredential{}, false
	}
	defer rows.Close()

	type stored struct {
		id    string
		value string
	}
	var got []stored
	for rows.Next() {
		var row stored
		if err := rows.Scan(&row.id, &row.value); err != nil {
			return openCodeCredential{}, false
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		return openCodeCredential{}, false
	}
	// Go key first, then OAuth, so a machine holding both reads the same
	// account as the auth.json order. Rows are already newest-first.
	for _, wanted := range []string{"opencode-go", "opencode"} {
		for _, row := range got {
			if row.id != wanted {
				continue
			}
			parsed, ok := decodeJSONValue([]byte(row.value))
			if !ok {
				continue
			}
			if cred, ok := openCodeCredentialFrom(parsed); ok {
				return cred, true
			}
		}
	}
	return openCodeCredential{}, false
}

func decodeJSONValue(body []byte) (any, bool) {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, false
	}
	return value, true
}

// openOpenCodeDB opens another process's database. mode=ro sees the
// write-ahead log, so a token OpenCode just rotated is visible. immutable=1
// is the fallback for a checkpointed file whose -shm sidecar is gone; it
// ignores the log, which is safe only because that is when mode=ro failed.
func openOpenCodeDB(ctx context.Context, path string) (*sql.DB, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = os.ErrInvalid
		}
		return nil, err
	}
	var last error
	for _, query := range []string{"mode=ro", "immutable=1"} {
		db, err := sql.Open("sqlite", openCodeDBFileURL(path, query))
		if err != nil {
			last = err
			continue
		}
		db.SetMaxOpenConns(1)
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = probeOpenCodeCredentialTable(probeCtx, db)
		cancel()
		if err == nil {
			return db, nil
		}
		last = err
		_ = db.Close()
	}
	if last == nil {
		last = os.ErrInvalid
	}
	return nil, last
}

func probeOpenCodeCredentialTable(ctx context.Context, db *sql.DB) error {
	var one int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM credential LIMIT 1`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func openCodeDBFileURL(path, query string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: query}
	return u.String()
}

func openCodeConsoleSignedIn(ctx context.Context, do HTTPDoer, cred openCodeCredential) bool {
	endpoint, ok := openCodeUserURL(cred.console)
	if !ok {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+cred.token)
	if cred.org != "" {
		req.Header.Set("x-opencode-org-id", cred.org)
	}
	_, status, _, err := performVendor(do, req)
	return err == nil && status == http.StatusOK
}

func openCodeUserURL(console string) (string, bool) {
	raw := strings.TrimSpace(console)
	if raw == "" {
		raw = openCodeDefaultConsole
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		if strings.TrimSpace(console) == "" || raw == openCodeDefaultConsole {
			return "", false
		}
		return openCodeUserURL("")
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/") + "/api/user"
	return u.String(), true
}

// ParseOpenCodeUsage maps Go plan windows. percent is already "percent used".
func ParseOpenCodeUsage(body []byte) ([]Window, bool) {
	root, ok := decodeObject(body)
	if !ok {
		return nil, false
	}
	usage := asMap(root["usage"])
	if usage == nil {
		return nil, false
	}
	var windows []Window
	for _, id := range openCodeWindows {
		entry := asMap(usage[id])
		percent, hasPercent := asFloat(entry["percent"])
		if !hasPercent {
			continue
		}
		value, usable := usablePercent(percent)
		if !usable {
			continue
		}
		windows = append(windows, Window{
			ID:          id,
			PercentUsed: value,
			ResetsAt:    parseResetValue(entry["resetsAt"]),
		})
	}
	return windows, len(windows) > 0
}
