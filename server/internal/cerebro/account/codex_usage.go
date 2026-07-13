package account

// FIR-3118 (codex): parsing for the ChatGPT/Codex usage endpoint
// (GET https://chatgpt.com/backend-api/wham/usage). The Codex CLI's rate
// limits expose plan-dependent rolling windows. Their duration, rather than
// their primary/secondary position, determines which OAuthUsageSnapshot field
// the account row persists. All parsing lives here so the cerebro zone owns
// the logic and the daemon only carries HTTP plumbing.

import (
	"encoding/json"
	"strings"
	"time"
)

// codexUsageResponse mirrors the wire shape of /backend-api/wham/usage.
// Windows the plan doesn't include are absent.
type codexUsageResponse struct {
	RateLimit *struct {
		PrimaryWindow   *codexUsageWindow `json:"primary_window"`
		SecondaryWindow *codexUsageWindow `json:"secondary_window"`
	} `json:"rate_limit"`
}

type codexUsageWindow struct {
	UsedPercent *float64 `json:"used_percent"`
	// LimitWindowSeconds identifies the window duration. Some plans expose a
	// weekly-only primary window, so primary does not necessarily mean 5 hours.
	LimitWindowSeconds *int64 `json:"limit_window_seconds"`
	// ResetAt is unix seconds; ResetAfterSeconds is the relative fallback
	// some responses carry instead.
	ResetAt           *int64 `json:"reset_at"`
	ResetAfterSeconds *int64 `json:"reset_after_seconds"`
}

// ParseCodexUsageResponse decodes a /backend-api/wham/usage body into a
// snapshot. Windows lasting at least a day are weekly; shorter windows are
// session/5-hour windows. Position remains the fallback for older responses
// without limit_window_seconds. Utilization values are clamped to [0, 100].
func ParseCodexUsageResponse(body []byte, now time.Time) (OAuthUsageSnapshot, error) {
	var resp codexUsageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return OAuthUsageSnapshot{}, err
	}
	var snap OAuthUsageSnapshot
	if resp.RateLimit != nil {
		applyCodexWindow(&snap, resp.RateLimit.PrimaryWindow, false, now)
		applyCodexWindow(&snap, resp.RateLimit.SecondaryWindow, true, now)
	}
	return snap, nil
}

func applyCodexWindow(snap *OAuthUsageSnapshot, w *codexUsageWindow, weeklyFallback bool, now time.Time) {
	pct, resetsAt := codexWindowFields(w, now)
	if pct == nil {
		return
	}
	isWeekly := weeklyFallback
	if w.LimitWindowSeconds != nil && *w.LimitWindowSeconds > 0 {
		isWeekly = time.Duration(*w.LimitWindowSeconds)*time.Second >= 24*time.Hour
	}
	if isWeekly {
		snap.SevenDayPct, snap.SevenDayResetsAt = pct, resetsAt
		return
	}
	snap.FiveHourPct, snap.FiveHourResetsAt = pct, resetsAt
}

func codexWindowFields(w *codexUsageWindow, now time.Time) (*float32, *time.Time) {
	if w == nil || w.UsedPercent == nil {
		return nil, nil
	}
	pct := float32(*w.UsedPercent)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	var resetsAt *time.Time
	switch {
	case w.ResetAt != nil && *w.ResetAt > 0:
		t := time.Unix(*w.ResetAt, 0).UTC()
		resetsAt = &t
	case w.ResetAfterSeconds != nil && *w.ResetAfterSeconds > 0:
		t := now.Add(time.Duration(*w.ResetAfterSeconds) * time.Second).UTC()
		resetsAt = &t
	}
	return &pct, resetsAt
}

// codexAuthTokens mirrors the token block of the Codex CLI's auth.json
// (the same file detectCodexIdentity reads for the login identity).
type codexAuthTokens struct {
	Tokens struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

// ParseCodexCredentials extracts the ChatGPT access token (and the optional
// account id, sent as the ChatGPT-Account-Id header) from a Codex auth.json
// document. Returns ErrOAuthTokenUnavailable when no token is present. No
// expiry check: the Codex CLI refreshes the file itself, and the usage
// endpoint answers 401 on a stale token, which the caller treats as
// best-effort skip.
func ParseCodexCredentials(data []byte) (accessToken, accountID string, err error) {
	var auth codexAuthTokens
	if err := json.Unmarshal(data, &auth); err != nil {
		return "", "", err
	}
	token := strings.TrimSpace(auth.Tokens.AccessToken)
	if token == "" {
		return "", "", ErrOAuthTokenUnavailable
	}
	return token, strings.TrimSpace(auth.Tokens.AccountID), nil
}
