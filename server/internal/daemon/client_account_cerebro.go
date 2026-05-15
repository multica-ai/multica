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
