// CEREBRO-PATCH(daemon-task-account-usage): JEH-881 post-task account usage
// reporting. After each task run the daemon parses the adapter output for
// account-level signals (429 retry-after + usage-window-percent) and reports
// them to /api/daemon/accounts/{id}/usage so the server can persist the exact
// quota state on the cerebro_account row.
//
// This file is net-new in the upstream daemon package — no upstream symbols
// are modified; all cerebro-specific account logic stays in
// server/internal/cerebro/account/ and is imported here for the parser only.
package daemon

import (
	"context"
	"log/slog"
	"time"

	cerebroaccount "github.com/multica-ai/multica/server/internal/cerebro/account"
)

// maybeReportAccountUsage inspects the task result for account-level usage
// CEREBRO-PATCH(daemon-task-account-token-usage): It also reports exact
// input+output tokens from the task result so the server can expose rolling
// account load even when the provider emitted no quota warning. Always
// best-effort: errors are logged but never block the task-completion path.
//
// Signal sources (JEH-1365: now scans both final output and verbose run logs):
//   - 429 retry-after in comment or logs → ThrottledUntil on the account row.
//   - Usage-window-percent in comment or logs → UsageWindowPct on the account row.
//   - Anthropic x-ratelimit-limit/remaining headers in logs → computed pct.
//   - Task usage entries → rolling 5h/7d token totals for the account.
//
// The account_id is resolved from the identity cache populated on each
// heartbeat ack. If no account has been registered for this runtime yet the
// call is a no-op.
func (d *Daemon) maybeReportAccountUsage(ctx context.Context, runtimeID, comment, logs string, usage []TaskUsageEntry, log *slog.Logger) { // CEREBRO-PATCH(daemon-task-account-token-usage): include task token usage in account telemetry.
	if d.accountIdentities == nil {
		return
	}
	accountID := d.accountIdentities.getAccountID(runtimeID)
	if accountID == "" {
		return
	}
	// Merge comment + verbose logs so the parser can pick up signals that
	// appear in Claude Code's debug log stream (e.g. x-ratelimit headers)
	// rather than only in the agent's final text output.
	combined := comment
	if logs != "" {
		combined = comment + "\n" + logs
	}
	ev := cerebroaccount.ParseAdapterOutput(combined, time.Now())
	tokens := accountUsageTokens(usage)
	if !ev.HasSignal() && tokens <= 0 {
		return
	}
	if err := d.client.ReportAccountUsage(ctx, accountID, ev.ThrottledUntil, ev.UsageWindowPct, tokens); err != nil {
		log.Warn("report account usage failed",
			"account_id", accountID,
			"runtime_id", runtimeID,
			"error", err)
	}
}

func accountUsageTokens(usage []TaskUsageEntry) int64 { // CEREBRO-PATCH(daemon-task-account-token-usage): sum tokens for rolling account windows.
	var total int64
	for _, u := range usage {
		total += u.InputTokens + u.OutputTokens
	}
	return total
}
