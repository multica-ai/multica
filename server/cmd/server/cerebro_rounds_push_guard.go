package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// setRoundsPushGuard binds the notification listener's
// suppressPushForRoundIssue hook (declared in notification_listeners.go) to
// the real cerebro check: an issue that sits in one of the recipient's rounds
// notifies only inside the round — no mobile/desktop push, no in-app banner
// (FIR-3114). The inbox_item row itself is still created so the round list can
// render the unread state when a run surfaces it.
//
// Mirrors setChannelMentionGuard: a DB-less router (NewRouter(nil, …) in unit
// tests) must reset to the identity hook, because the hook is process-global
// and a nil-pool binding would panic inside the synchronous event bus.
func setRoundsPushGuard(pool *pgxpool.Pool) {
	if pool == nil {
		suppressPushForRoundIssue = func(_ context.Context, _ string, _, _ string) bool { return false }
		return
	}
	suppressPushForRoundIssue = func(ctx context.Context, recipientType string, recipientID, issueID string) bool {
		if recipientType != "member" || issueID == "" || recipientID == "" {
			return false
		}
		var inRound bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cerebro_round_member crm JOIN cerebro_round cr ON cr.id = crm.round_id WHERE crm.issue_id = $1 AND cr.owner_id = $2)`, issueID, recipientID).Scan(&inRound); err != nil {
			return false
		}
		return inRound
	}
}
