// CEREBRO-PATCH(daemon-client-account): JEH-998 daemon-side hook that POSTs
// account-usage events (429 retry-after + usage-window-percent) parsed from
// adapter output to /api/daemon/accounts/{id}/usage. Net-new fork file in
// the upstream daemon package; the parser itself lives in
// server/internal/cerebro/account so the cerebro zone owns all account
// logic and the upstream daemon package only carries the HTTP plumbing.
package daemon

import (
	"context"
	"fmt"
	"time"

	cerebroaccount "github.com/multica-ai/multica/server/internal/cerebro/account"
)

// ReportAccountUsage posts a partial usage update for the given account.
// The signal parameters are pointer-typed:
//
//   - A nil pointer means "leave that column alone" on the server.
//   - A non-nil pointer wrapping a value writes the value.
//   - A non-nil pointer wrapping the zero value (time.Time{} / NaN) is
//     transmitted as JSON null, which the server interprets as
//     "explicitly clear this column".
//
// The endpoint is daemon-auth-scoped: the server validates that the
// account belongs to the workspace this daemon authenticated against.
func (c *Client) ReportAccountUsage(ctx context.Context, accountID string, throttledUntil *time.Time, usageWindowPct *float32, tokens int64) error { // CEREBRO-PATCH(daemon-client-account-token-usage): include exact task token total.
	if accountID == "" {
		return fmt.Errorf("ReportAccountUsage: accountID is empty")
	}
	if throttledUntil == nil && usageWindowPct == nil && tokens <= 0 {
		// Nothing to report — avoid an empty round-trip.
		return nil
	}
	body := map[string]any{}
	if throttledUntil != nil {
		if throttledUntil.IsZero() {
			body["throttled_until"] = nil
		} else {
			body["throttled_until"] = throttledUntil.UTC().Format(time.RFC3339)
		}
	}
	if usageWindowPct != nil {
		body["usage_window_pct"] = *usageWindowPct
	}
	if tokens > 0 { // CEREBRO-PATCH(daemon-client-account-token-usage): omit absent/zero token reports.
		body["tokens"] = tokens
	}
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/accounts/%s/usage", accountID), body, nil)
}

// ReportAccountUsageWindows posts the exact rolling-window utilization
// fetched from Claude's OAuth usage endpoint (FIR-3118). The snapshot is the
// full truth about the plan's windows, so a window the provider did NOT report
// is sent as an explicit null to clear the column: omitting it would leave a
// stale value (e.g. a weekly-only Codex plan keeping an old 5h percentage
// forever and rendering it as "Next 5h").
func (c *Client) ReportAccountUsageWindows(ctx context.Context, accountID string, snap cerebroaccount.OAuthUsageSnapshot) error {
	if accountID == "" {
		return fmt.Errorf("ReportAccountUsageWindows: accountID is empty")
	}
	if !snap.HasSignal() {
		return nil
	}
	body := map[string]any{
		"usage_5h_pct":       nil,
		"usage_5h_resets_at": nil,
		"usage_7d_pct":       nil,
		"usage_7d_resets_at": nil,
	}
	if snap.FiveHourPct != nil {
		body["usage_5h_pct"] = *snap.FiveHourPct
		if snap.FiveHourResetsAt != nil {
			body["usage_5h_resets_at"] = snap.FiveHourResetsAt.UTC().Format(time.RFC3339)
		}
	}
	if snap.SevenDayPct != nil {
		body["usage_7d_pct"] = *snap.SevenDayPct
		if snap.SevenDayResetsAt != nil {
			body["usage_7d_resets_at"] = snap.SevenDayResetsAt.UTC().Format(time.RFC3339)
		}
	}
	// Mirror the tightest window the plan actually has into the legacy
	// usage_window_pct column so consumers of the old field (coordinator
	// agents, runtime cards) see the exact value instead of the log-scraped
	// approximation. Weekly-only plans (Codex Pro) mirror the week.
	if snap.FiveHourPct != nil {
		body["usage_window_pct"] = *snap.FiveHourPct
	} else {
		body["usage_window_pct"] = *snap.SevenDayPct
	}
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/accounts/%s/usage", accountID), body, nil)
}
