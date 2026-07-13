package account

// FIR-3118: parsing for Claude's OAuth usage endpoint
// (GET https://api.anthropic.com/api/oauth/usage). The daemon calls the
// endpoint after each task run with the Claude Code OAuth access token and
// reports the exact 5-hour / 7-day window utilization to the server, which
// persists it on the cerebro_account row. All parsing lives here so the
// cerebro zone owns the logic and the daemon only carries HTTP plumbing.

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// OAuthUsageSnapshot is the subset of the OAuth usage response the account
// row persists. Pointer fields are nil when the provider omitted the window
// (e.g. seven_day is absent on some plans).
type OAuthUsageSnapshot struct {
	FiveHourPct      *float32
	FiveHourResetsAt *time.Time
	SevenDayPct      *float32
	SevenDayResetsAt *time.Time
}

// HasSignal reports whether the snapshot carries at least one window worth
// reporting.
func (s OAuthUsageSnapshot) HasSignal() bool {
	return s.FiveHourPct != nil || s.SevenDayPct != nil
}

// oauthUsageResponse mirrors the wire shape of /api/oauth/usage. Field names
// match the JSON exactly. Windows the plan doesn't include are absent.
type oauthUsageResponse struct {
	FiveHour *oauthUsageWindow `json:"five_hour"`
	SevenDay *oauthUsageWindow `json:"seven_day"`
}

type oauthUsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

// ParseOAuthUsageResponse decodes an /api/oauth/usage body into a snapshot.
// Utilization values are clamped to [0, 100]; unparseable resets_at
// timestamps are dropped (the pct is still kept).
func ParseOAuthUsageResponse(body []byte) (OAuthUsageSnapshot, error) {
	var resp oauthUsageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return OAuthUsageSnapshot{}, err
	}
	var snap OAuthUsageSnapshot
	snap.FiveHourPct, snap.FiveHourResetsAt = windowFields(resp.FiveHour)
	snap.SevenDayPct, snap.SevenDayResetsAt = windowFields(resp.SevenDay)
	return snap, nil
}

func windowFields(w *oauthUsageWindow) (*float32, *time.Time) {
	if w == nil || w.Utilization == nil {
		return nil, nil
	}
	pct := float32(*w.Utilization)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	var resetsAt *time.Time
	if w.ResetsAt != nil {
		if t, err := time.Parse(time.RFC3339, *w.ResetsAt); err == nil {
			utc := t.UTC()
			resetsAt = &utc
		}
	}
	return &pct, resetsAt
}

// ErrOAuthTokenUnavailable is returned when no usable Claude Code OAuth
// access token could be extracted from the credential source.
var ErrOAuthTokenUnavailable = errors.New("claude oauth access token unavailable")

// claudeCredentials mirrors the Claude Code credential store
// (~/.claude/.credentials.json, or the "Claude Code-credentials" macOS
// keychain item — both carry the same JSON document).
type claudeCredentials struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"` // unix milliseconds
	} `json:"claudeAiOauth"`
}

// ParseClaudeCredentials extracts the OAuth access token from a Claude Code
// credential JSON document. Returns ErrOAuthTokenUnavailable when the token
// is missing or already expired at `now` (we never call the usage endpoint
// with a stale token — Claude Code itself owns the refresh flow).
func ParseClaudeCredentials(data []byte, now time.Time) (string, error) {
	var creds claudeCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", err
	}
	token := strings.TrimSpace(creds.ClaudeAiOauth.AccessToken)
	if token == "" {
		return "", ErrOAuthTokenUnavailable
	}
	if creds.ClaudeAiOauth.ExpiresAt > 0 {
		expires := time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
		if !expires.After(now) {
			return "", ErrOAuthTokenUnavailable
		}
	}
	return token, nil
}
