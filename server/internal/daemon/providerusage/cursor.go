package providerusage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const cursorUsageURL = "https://cursor.com/api/usage-summary"

// CursorSession is the in-memory editor session used for one request.
// Callers must not persist it.
type CursorSession struct {
	AccessToken string
	AuthID      string
}

// CursorSessionSource reads the local editor or CLI session. Tests inject it.
type CursorSessionSource func(ctx context.Context) (CursorSession, error)

// HTTPDoer performs one vendor request. Tests inject a fake.
type HTTPDoer func(req *http.Request) (*http.Response, error)

// CursorCollector reads Cursor plan usage from the local editor session.
type CursorCollector struct {
	Session CursorSessionSource
	Do      HTTPDoer
	Now     func() time.Time
}

func (c CursorCollector) Collect(ctx context.Context) Result {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	source := c.Session
	if source == nil {
		source = readCursorSession
	}
	session, err := source(ctx)
	if err != nil || session.AccessToken == "" || session.AuthID == "" {
		return emptyResult(ProviderCursor, ReasonSessionUnavailable, now)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cursorUsageURL, nil)
	if err != nil {
		return Result{Upload: false}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", "WorkosCursorSessionToken="+session.AuthID+"::"+session.AccessToken)
	do := c.Do
	if do == nil {
		client := &http.Client{Timeout: vendorTimeout}
		do = client.Do
	}
	resp, err := do(req)
	if err != nil {
		return Result{Upload: false}
	}
	body, err := readVendorBody(resp)
	if err != nil {
		return Result{Upload: false}
	}
	reason, backoff, transient := classifyVendorStatus(resp.StatusCode, resp.Header.Get("Retry-After"))
	if transient {
		return Result{Upload: false, Backoff: backoff}
	}
	if reason != "" {
		return emptyResult(ProviderCursor, reason, now)
	}
	windows, plan, ok := ParseCursorUsage(body)
	if !ok {
		return Result{Upload: false}
	}
	return Result{
		Upload: true,
		Snapshot: Snapshot{
			Provider:    ProviderCursor,
			PlanName:    plan,
			CollectedAt: now,
			Windows:     windows,
		},
	}
}

type cursorSummary struct {
	BillingCycleEnd string `json:"billingCycleEnd"`
	MembershipType  string `json:"membershipType"`
	IndividualUsage struct {
		Plan struct {
			AutoPercentUsed *float64 `json:"autoPercentUsed"`
			APIPercentUsed  *float64 `json:"apiPercentUsed"`
		} `json:"plan"`
	} `json:"individualUsage"`
}

// ParseCursorUsage maps a usage-summary body onto auto and api windows.
// ok is false when the body is not the expected object.
func ParseCursorUsage(body []byte) (windows []Window, plan string, ok bool) {
	var summary cursorSummary
	if err := json.Unmarshal(body, &summary); err != nil {
		return nil, "", false
	}
	if summary.IndividualUsage.Plan.AutoPercentUsed == nil &&
		summary.IndividualUsage.Plan.APIPercentUsed == nil &&
		summary.MembershipType == "" &&
		summary.BillingCycleEnd == "" {
		return nil, "", false
	}
	var resets *time.Time
	if summary.BillingCycleEnd != "" {
		if t, err := time.Parse(time.RFC3339, summary.BillingCycleEnd); err == nil {
			resets = &t
		}
	}
	if summary.IndividualUsage.Plan.AutoPercentUsed != nil {
		windows = append(windows, Window{
			ID:          "auto",
			PercentUsed: *summary.IndividualUsage.Plan.AutoPercentUsed,
			ResetsAt:    resets,
		})
	}
	if pct := summary.IndividualUsage.Plan.APIPercentUsed; pct != nil && *pct > 0 {
		windows = append(windows, Window{
			ID:          "api",
			PercentUsed: *pct,
			ResetsAt:    resets,
		})
	}
	return windows, summary.MembershipType, true
}

// cursorStateDBPathsFor lists editor state databases for one OS. Linux uses
// XDG_CONFIG_HOME when set, then ~/.config/Cursor. The macOS Library path and
// the Windows APPDATA path are not used on Linux.
func cursorStateDBPathsFor(goos, home, xdgConfig, appData string) []string {
	var paths []string
	switch goos {
	case "linux":
		if xdgConfig != "" {
			paths = append(paths, filepath.Join(xdgConfig, "Cursor", "User", "globalStorage", "state.vscdb"))
		}
		if home != "" {
			paths = append(paths, filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"))
		}
	case "darwin":
		if home != "" {
			paths = append(paths, filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"))
		}
	case "windows":
		if appData != "" {
			paths = append(paths, filepath.Join(appData, "Cursor", "User", "globalStorage", "state.vscdb"))
		}
	default:
		// An unknown GOOS has no editor database path we can honestly read.
	}
	return paths
}

func cursorCLIConfigPath(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cursor", "cli-config.json")
}

func readCursorSession(ctx context.Context) (CursorSession, error) {
	home, _ := os.UserHomeDir()
	paths := cursorStateDBPathsFor(runtime.GOOS, home, os.Getenv("XDG_CONFIG_HOME"), os.Getenv("APPDATA"))
	for _, path := range paths {
		session, ok, err := readCursorStateDB(ctx, path)
		if err != nil {
			continue
		}
		if ok && session.AccessToken != "" {
			if session.AuthID == "" {
				session.AuthID = jwtSub(session.AccessToken)
			}
			if session.AuthID != "" {
				return session, nil
			}
		}
	}
	return readCursorCLIConfig(cursorCLIConfigPath(home))
}

func readCursorCLIConfig(path string) (CursorSession, error) {
	if path == "" {
		return CursorSession{}, fmt.Errorf("cursor cli config path missing")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return CursorSession{}, err
	}
	var doc struct {
		AuthInfo struct {
			AuthID      string `json:"authId"`
			AccessToken string `json:"accessToken"`
		} `json:"authInfo"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return CursorSession{}, err
	}
	session := CursorSession{AccessToken: doc.AuthInfo.AccessToken, AuthID: doc.AuthInfo.AuthID}
	if session.AccessToken != "" && session.AuthID == "" {
		session.AuthID = jwtSub(session.AccessToken)
	}
	if session.AccessToken == "" || session.AuthID == "" {
		return CursorSession{}, fmt.Errorf("cursor cli config has no session")
	}
	return session, nil
}

func jwtSub(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Sub
}

// cursorDBFileURL builds a read-only SQLite URI. mode=ro still sees WAL
// frames; immutable=1 would hide a token the editor just rotated.
func cursorDBFileURL(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro"}
	return u.String()
}
