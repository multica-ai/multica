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
// signals and POSTs them to the server when found. Always best-effort:
// errors are logged but never block the task-completion path.
//
// Signal sources:
//   - 429 retry-after in result.Comment → ThrottledUntil on the account row.
//   - Usage-window-percent in result.Comment → UsageWindowPct on the account row.
//
// The account_id is resolved from the identity cache populated on each
// heartbeat ack. If no account has been registered for this runtime yet the
// call is a no-op.
func (d *Daemon) maybeReportAccountUsage(ctx context.Context, runtimeID, comment string, log *slog.Logger) {
	if d.accountIdentities == nil {
		return
	}
	accountID := d.accountIdentities.getAccountID(runtimeID)
	if accountID == "" {
		return
	}
	ev := cerebroaccount.ParseAdapterOutput(comment, time.Now())
	if !ev.HasSignal() {
		return
	}
	if err := d.client.ReportAccountUsage(ctx, accountID, ev.ThrottledUntil, ev.UsageWindowPct); err != nil {
		log.Warn("report account usage failed",
			"account_id", accountID,
			"runtime_id", runtimeID,
			"error", err)
	}
}
