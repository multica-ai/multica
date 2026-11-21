package inbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Publisher receives the event announcing each delivered row: the event bus,
// or a Deferred when the row is written inside a transaction.
type Publisher interface {
	Publish(events.Event)
}

// Notification is one inbox row to deliver.
type Notification struct {
	Params db.CreateInboxItemParams
	// IssueStatus, when set, rides on the announced item as issue_status.
	IssueStatus *string
	// Preferences are the recipients' preferences a fanout loaded once with
	// LoadPreferences. Nil makes Deliver read this recipient's own.
	Preferences Preferences
	// IgnorePreferences delivers a mutable type even when the recipient
	// muted its group.
	IgnorePreferences bool
}

// Deliver is the one way an inbox row is written. It refuses an unregistered
// type, drops a member notification whose group the member muted, inserts the
// row only when the recipient is eligible to see it, and announces the new row
// on pub: inbox:new, or inbox:snoozed when it joined a snooze on its issue.
// A row that needs the recipient ends that snooze instead (see
// createIfEligible). inserted is false, without an error, for a
// muted or ineligible recipient.
//
// Inside a transaction, pass a Deferred and Flush it after commit, so no
// client hears about a row that may roll back.
func Deliver(ctx context.Context, queries *db.Queries, pub Publisher, n Notification) (item db.InboxItem, inserted bool, err error) {
	group, ok := typeGroups[n.Params.Type]
	if !ok {
		return db.InboxItem{}, false, fmt.Errorf("inbox: unregistered notification type %q", n.Params.Type)
	}
	if group != "" && !n.IgnorePreferences && n.Params.RecipientType == "member" {
		prefs := n.Preferences
		if prefs == nil {
			prefs = LoadPreferences(ctx, queries, n.Params.WorkspaceID, []pgtype.UUID{n.Params.RecipientID})
		}
		if Muted(prefs[util.UUIDToString(n.Params.RecipientID)], n.Params.Type) {
			return db.InboxItem{}, false, nil
		}
	}
	item, inserted, err = createIfEligible(ctx, queries, n.Params)
	if err != nil || !inserted {
		return item, inserted, err
	}
	publish(pub, newEvent(item, n.IssueStatus))
	return item, true, nil
}

// Deferred holds announcing events until the transaction that wrote their rows
// commits. The zero value is ready to use.
type Deferred struct {
	events []events.Event
}

// Publish holds e for Flush.
func (d *Deferred) Publish(e events.Event) {
	d.events = append(d.events, e)
}

// Flush publishes the held events in delivery order and empties d.
func (d *Deferred) Flush(pub Publisher) {
	held := d.events
	d.events = nil
	for _, e := range held {
		publish(pub, e)
	}
}

func publish(pub Publisher, e events.Event) {
	// A service built without a bus holds a nil *events.Bus, which arrives
	// here as a non-nil interface.
	if pub == nil {
		return
	}
	if bus, ok := pub.(*events.Bus); ok && bus == nil {
		return
	}
	pub.Publish(e)
}

// newEvent builds the event that announces a written row: inbox:new, or
// inbox:snoozed for a row that joined a snooze. Its actor is the row's actor;
// listeners route it on the nested item's recipient_id.
func newEvent(item db.InboxItem, issueStatus *string) events.Event {
	payload := map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"issue_id":       util.UUIDToPtr(item.IssueID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
	}
	if issueStatus != nil {
		payload["issue_status"] = *issueStatus
	}
	return events.Event{
		Type:        NewItemEventType(item),
		WorkspaceID: util.UUIDToString(item.WorkspaceID),
		ActorType:   item.ActorType.String,
		ActorID:     util.UUIDToString(item.ActorID),
		Payload:     map[string]any{"item": payload},
	}
}
