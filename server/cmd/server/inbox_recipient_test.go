package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestCreateInboxItem_RecipientWorkspace(t *testing.T) {
	fx := workspaceFixture(t)
	otherWorkspace := fx.Workspace(t, "Other workspace", "inbox-recipient-other")
	member := fx.User(t, "Member", "inbox-query-member@example.test")
	fx.Member(t, testWorkspaceID, member, "member")
	otherMember := fx.User(t, "Other member", "inbox-query-other@example.test")
	fx.Member(t, otherWorkspace, otherMember, "member")
	agent := fx.Agent(t, "Current agent", "")
	otherAgent := fx.Agent(t, "Other agent", "", testutil.Cols{"workspace_id": otherWorkspace})
	deletedAgent := fx.Agent(t, "Deleted agent", "")
	fx.Exec(t, `DELETE FROM agent WHERE id = $1`, deletedAgent)
	issue := fx.Issue(t, "Recipient guard")
	fx.Cleanup(t, `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	queries := db.New(testPool)
	for _, tc := range []struct {
		name, recipientType, recipientID string
		allowed                          bool
	}{
		{"current member", "member", member, true},
		{"other workspace member", "member", otherMember, false},
		{"current agent", "agent", agent, true},
		{"other workspace agent", "agent", otherAgent, false},
		{"deleted agent", "agent", deletedAgent, false},
		{"member ID as agent", "agent", member, false},
		{"agent ID as member", "member", agent, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := db.CreateInboxItemParams{ID: dbid.NewV7(), WorkspaceID: util.MustParseUUID(testWorkspaceID), RecipientType: tc.recipientType, RecipientID: util.MustParseUUID(tc.recipientID), IssueID: util.MustParseUUID(issue), Type: "reaction_added", Severity: "info", Title: "Recipient guard", Details: emptyDetails}
			item, err := queries.CreateInboxItem(context.Background(), params)
			if tc.allowed {
				if err != nil {
					t.Fatal(err)
				}
				if item.ID != params.ID || item.RecipientType != tc.recipientType || item.RecipientID != params.RecipientID {
					t.Fatalf("wrong inserted item: %+v", item)
				}
				fx.Exec(t, `DELETE FROM inbox_item WHERE id = $1`, item.ID)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("CreateInboxItem error = %v, want pgx.ErrNoRows", err)
			}
			bus := events.New()
			count := 0
			bus.Subscribe(protocol.EventInboxNew, func(events.Event) { count++ })
			notifyDirect(context.Background(), queries, bus, tc.recipientType, tc.recipientID, testWorkspaceID,
				events.Event{ActorType: "member", ActorID: testUserID}, issue, "todo", "reaction_added", "info", "Recipient guard", "", emptyDetails)
			want := 0
			if tc.allowed {
				want = 1
			}
			if count != want {
				t.Errorf("inbox:new events = %d, want %d", count, want)
			}
			if n := fx.Count(t, `SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND recipient_type = $2 AND recipient_id = $3`, issue, tc.recipientType, tc.recipientID); n != want {
				t.Errorf("inbox rows = %d, want %d", n, want)
			}
		})
	}
}
