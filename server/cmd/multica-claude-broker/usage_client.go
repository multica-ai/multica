package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// usageEndpointDefault is Anthropic's OAuth usage endpoint. It returns the
// same server-side window state that powers Claude Code's `/usage` view:
// the five-hour session window and the rolling seven-day (weekly) windows,
// each as a 0-100 utilization percentage plus a reset timestamp.
//
// The endpoint is undocumented and aggressively rate-limited. The
// `User-Agent: claude-code/<version>` header is load-bearing: without it the
// request lands in a punitive bucket that returns persistent 429s. Poll no
// faster than every ~180s.
const usageEndpointDefault = "https://api.anthropic.com/api/oauth/usage"

// usageIntervalFloor is the minimum safe poll cadence. Below this the
// endpoint drops requests into a punitive bucket and returns persistent 429s.
const usageIntervalFloor = 180 * time.Second

// UsageWindow is a single rate-limit window: how much of the allowance has
// been consumed (0-100) and when the window rolls over.
type UsageWindow struct {
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// UsageSnapshot is the broker's view of the account's plan usage. Mirrors the
// Anthropic response shape; nullable per-model windows stay nil when the plan
// doesn't expose them. FetchedAt is stamped by the broker, not Anthropic, so
// clients can show staleness.
type UsageSnapshot struct {
	FiveHour       *UsageWindow `json:"five_hour"`
	SevenDay       *UsageWindow `json:"seven_day"`
	SevenDayOpus   *UsageWindow `json:"seven_day_opus"`
	SevenDaySonnet *UsageWindow `json:"seven_day_sonnet"`
	FetchedAt      time.Time    `json:"fetched_at"`
}

// ErrUsageRateLimited is returned when Anthropic answers the usage poll with
// 429. The poller treats it as transient and keeps serving the last good
// snapshot; it never counts as a hard failure.
var ErrUsageRateLimited = errors.New("usage endpoint rate-limited (429)")

// UsageClient fetches the OAuth usage snapshot. Public fields so tests can
// point Endpoint at an httptest server and assert on the headers.
type UsageClient struct {
	Endpoint   string
	BetaHeader string // anthropic-beta value (oauth-2025-04-20)
	UserAgent  string // MUST be claude-code/<version> or Anthropic 429s us

	HTTP *http.Client
}

// DefaultUsageClient wires the client to the embedded constants. The
// User-Agent deliberately impersonates the Claude Code CLI version the OAuth
// constants were extracted from — the usage endpoint requires it.
func DefaultUsageClient() *UsageClient {
	return &UsageClient{
		Endpoint:   usageEndpointDefault,
		BetaHeader: Constants.VersionHeader,
		UserAgent:  "claude-code/" + Constants.ClaudeVersion,
		HTTP:       &http.Client{Timeout: 15 * time.Second},
	}
}

// Fetch GETs the usage endpoint with the supplied bearer token and parses the
// snapshot. FetchedAt is stamped on success. A 429 returns ErrUsageRateLimited
// so the caller can back off without treating it as an outage; any other
// non-2xx or parse failure returns a generic error.
func (c *UsageClient) Fetch(ctx context.Context, accessToken string) (*UsageSnapshot, error) {
	if accessToken == "" {
		return nil, errors.New("usage fetch: empty access token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("usage fetch: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", c.BetaHeader)
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("usage fetch: http do: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrUsageRateLimited
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, fmt.Errorf("usage fetch: HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var snap UsageSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("usage fetch: decode body: %w (raw: %s)", err, string(raw))
	}
	snap.FetchedAt = time.Now().UTC()
	return &snap, nil
}
