package inbox

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createIfEligible returns inserted=false without an error when the recipient
// does not belong to the workspace. Other database errors remain failures.
// Eligibility is checked by the insert's snapshot, without additional locks.
//
// A row written on an issue the recipient has snoozed joins the snooze (see
// CreateInboxItem). When the row is one that needs the recipient
// (WakesSnooze), the snooze ends instead and the issue is back in the inbox
// with this row on top. Announce the returned row with NewItemEventType.
func createIfEligible(ctx context.Context, queries *db.Queries, params db.CreateInboxItemParams) (item db.InboxItem, inserted bool, err error) {
	item, err = queries.CreateInboxItem(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.InboxItem{}, false, nil
	}
	if err != nil {
		return db.InboxItem{}, false, err
	}
	if item.SnoozedUntil.Valid && WakesSnooze(item.Type, item.Details) {
		if _, err := queries.UnsnoozeInboxGroup(ctx, db.UnsnoozeInboxGroupParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
		}); err != nil {
			// The row is written; it stays snoozed with the rest of the issue.
			slog.Warn("inbox: failed to end snooze for a waking notification",
				"type", item.Type, "error", err)
			return item, true, nil
		}
		item.SnoozedUntil = pgtype.Timestamptz{}
		item.SnoozedAt = pgtype.Timestamptz{}
	}
	return item, true, nil
}
