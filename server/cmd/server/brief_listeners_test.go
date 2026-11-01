package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type briefInboxRow struct {
	Type, Severity, Title string
	Body                  *string
	IssueID               *string
	Details               map[string]string
}

func briefInboxFor(t *testing.T, workspaceID, userID string) []briefInboxRow {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT type, severity, title, body, issue_id::text, details
		FROM inbox_item
		WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND type = 'brief_ready'`,
		workspaceID, userID)
	if err != nil {
		t.Fatalf("query inbox: %v", err)
	}
	defer rows.Close()
	var out []briefInboxRow
	for rows.Next() {
		var r briefInboxRow
		var details []byte
		if err := rows.Scan(&r.Type, &r.Severity, &r.Title, &r.Body, &r.IssueID, &details); err != nil {
			t.Fatalf("scan: %v", err)
		}
		_ = json.Unmarshal(details, &r.Details)
		out = append(out, r)
	}
	return out
}

func TestNotification_BriefCreated_OneItemPerOwner(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	fx := workspaceFixture(t)

	ws := fx.Workspace(t, "Brief inbox", "brief-inbox-"+uuid.NewString())
	stamp := time.Now().UnixNano()
	owner1 := fx.User(t, "Owner One", fmt.Sprintf("brief-owner1-%d@example.com", stamp))
	owner2 := fx.User(t, "Owner Two", fmt.Sprintf("brief-owner2-%d@example.com", stamp))
	mutedOwner := fx.User(t, "Owner Muted", fmt.Sprintf("brief-owner3-%d@example.com", stamp))
	admin := fx.User(t, "Admin", fmt.Sprintf("brief-admin-%d@example.com", stamp))
	member := fx.User(t, "Member", fmt.Sprintf("brief-member-%d@example.com", stamp))
	for user, role := range map[string]string{owner1: "owner", owner2: "owner", mutedOwner: "owner", admin: "admin", member: "member"} {
		fx.Member(t, ws, user, role)
	}
	fx.Insert(t, "notification_preference", map[string]any{
		"workspace_id": ws, "user_id": mutedOwner, "preferences": `{"agent_activity":"muted"}`,
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE workspace_id = $1`, ws)
	})

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) { inboxEvents = append(inboxEvents, e) })

	brief := handler.BriefResponse{
		ID: uuid.NewString(), WorkspaceID: ws, Kind: "daily", Title: "Daily brief",
		Body: "## Headline\nTwo decisions need you.\n\n## Needs your decision\nSee below.\n",
	}
	bus.Publish(events.Event{
		Type: protocol.EventBriefCreated, WorkspaceID: ws, ActorType: "member", ActorID: owner1,
		Payload: map[string]any{"brief": brief},
	})

	for _, owner := range []string{owner1, owner2} {
		items := briefInboxFor(t, ws, owner)
		if len(items) != 1 {
			t.Fatalf("owner %s got %d brief_ready items, want 1", owner, len(items))
		}
		it := items[0]
		if it.Severity != "attention" || it.Title != "Daily brief" || it.IssueID != nil {
			t.Fatalf("item wrong: %+v", it)
		}
		if it.Body == nil || *it.Body != "Two decisions need you." {
			t.Fatalf("body should be the headline, got %v", it.Body)
		}
		if it.Details["brief_id"] != brief.ID || it.Details["kind"] != "daily" {
			t.Fatalf("details wrong: %+v", it.Details)
		}
	}
	for name, user := range map[string]string{"admin": admin, "member": member, "muted owner": mutedOwner} {
		if items := briefInboxFor(t, ws, user); len(items) != 0 {
			t.Fatalf("%s must not get a brief_ready item, got %d", name, len(items))
		}
	}
	if len(inboxEvents) != 2 {
		t.Fatalf("inbox:new events = %d, want one per notified owner (2)", len(inboxEvents))
	}
}

func TestBriefHeadlineBody(t *testing.T) {
	if got := briefHeadlineBody("## Progress\nx\n\n## Headline\nLine one\nLine two\n## FYI\nz"); got != "Line one Line two" {
		t.Fatalf("headline = %q", got)
	}
	if got := briefHeadlineBody("## Progress\nx"); got != "" {
		t.Fatalf("no headline should give empty, got %q", got)
	}
}
