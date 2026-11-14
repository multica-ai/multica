package inbox

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CreateIfEligible returns inserted=false without an error when the recipient
// does not belong to the workspace. Other database errors remain failures.
// Eligibility is checked by the insert's snapshot, without additional locks.
//
// Every row is written with its Inbox section. A caller that fans an event out
// to subscribers sets it per recipient (SubscriberSection); one that writes
// straight to the person concerned leaves it unset and gets DefaultSection.
func CreateIfEligible(ctx context.Context, queries *db.Queries, params db.CreateInboxItemParams) (item db.InboxItem, inserted bool, err error) {
	if !params.Section.Valid {
		params.Section = pgtype.Text{String: DefaultSection(params.Type), Valid: true}
	}
	item, err = queries.CreateInboxItem(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.InboxItem{}, false, nil
	}
	if err != nil {
		return db.InboxItem{}, false, err
	}
	return item, true, nil
}
