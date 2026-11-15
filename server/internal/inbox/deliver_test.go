package inbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestDeliverRefusesUnregisteredType(t *testing.T) {
	_, inserted, err := Deliver(context.Background(), nil, nil, Notification{
		Params: db.CreateInboxItemParams{RecipientType: "member", Type: "not_a_type"},
	})
	if err == nil || !strings.Contains(err.Error(), "not_a_type") {
		t.Fatalf("Deliver error = %v, want unregistered type error", err)
	}
	if inserted {
		t.Fatal("Deliver inserted an unregistered type")
	}
}

// A muted member is dropped before anything is written, from the preferences
// a fanout loaded.
func TestDeliverDropsMutedMember(t *testing.T) {
	recipient := util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	prefs := Preferences{util.UUIDToString(recipient): {GroupComments: "muted"}}
	bus := events.New()
	published := 0
	bus.Subscribe(protocol.EventInboxNew, func(events.Event) { published++ })

	_, inserted, err := Deliver(context.Background(), nil, bus, Notification{
		Params:      db.CreateInboxItemParams{RecipientType: "member", RecipientID: recipient, Type: "new_comment"},
		Preferences: prefs,
	})
	if err != nil || inserted || published != 0 {
		t.Fatalf("Deliver = inserted %v, err %v, published %d; want a silent drop", inserted, err, published)
	}
}

func TestDeferredPublishesOnlyOnFlush(t *testing.T) {
	bus := events.New()
	var got []string
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) { got = append(got, e.ActorID) })

	var pending Deferred
	pending.Publish(events.Event{Type: protocol.EventInboxNew, ActorID: "a"})
	pending.Publish(events.Event{Type: protocol.EventInboxNew, ActorID: "b"})
	if len(got) != 0 {
		t.Fatalf("published before Flush: %v", got)
	}
	pending.Flush(bus)
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("Flush published %v, want [a b] in order", got)
	}
	pending.Flush(bus)
	if len(got) != 2 {
		t.Fatalf("second Flush republished: %v", got)
	}
}

func TestFlushWithoutBus(t *testing.T) {
	var nilBus *events.Bus
	for _, pub := range []Publisher{nil, nilBus} {
		var pending Deferred
		pending.Publish(events.Event{Type: protocol.EventInboxNew})
		pending.Flush(pub)
	}
}

func TestNewEventShape(t *testing.T) {
	agentID := util.MustParseUUID("22222222-2222-2222-2222-222222222222")
	workspaceID := util.MustParseUUID("33333333-3333-3333-3333-333333333333")
	item := db.InboxItem{
		ID:            util.MustParseUUID("44444444-4444-4444-4444-444444444444"),
		WorkspaceID:   workspaceID,
		RecipientType: "member",
		RecipientID:   util.MustParseUUID("55555555-5555-5555-5555-555555555555"),
		Type:          "quick_create_done",
		ActorType:     pgtype.Text{String: "agent", Valid: true},
		ActorID:       agentID,
		Details:       []byte("{}"),
	}

	status := "todo"
	e := newEvent(item, &status)
	if e.Type != protocol.EventInboxNew || e.WorkspaceID != util.UUIDToString(workspaceID) {
		t.Fatalf("event = %s in %s", e.Type, e.WorkspaceID)
	}
	if e.ActorType != "agent" || e.ActorID != util.UUIDToString(agentID) {
		t.Fatalf("event actor = %s/%s, want the row's actor", e.ActorType, e.ActorID)
	}
	payload := e.Payload.(map[string]any)["item"].(map[string]any)
	if payload["recipient_id"] != "55555555-5555-5555-5555-555555555555" {
		t.Fatalf("recipient_id = %v", payload["recipient_id"])
	}
	if payload["issue_status"] != "todo" {
		t.Fatalf("issue_status = %v, want todo", payload["issue_status"])
	}

	system := newEvent(db.InboxItem{ActorType: pgtype.Text{String: "system", Valid: true}}, nil)
	if _, ok := system.Payload.(map[string]any)["item"].(map[string]any)["issue_status"]; ok {
		t.Fatal("issue_status present without an issue status")
	}
	if system.ActorType != "system" || system.ActorID != "" {
		t.Fatalf("system event actor = %s/%q", system.ActorType, system.ActorID)
	}
}

// A row that joined a snooze must not banner or push.
func TestNewEventAnnouncesSnoozedRowAsSnoozed(t *testing.T) {
	item := db.InboxItem{SnoozedUntil: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}}
	if got := newEvent(item, nil).Type; got != protocol.EventInboxSnoozed {
		t.Fatalf("snoozed row announced as %s, want %s", got, protocol.EventInboxSnoozed)
	}
}
