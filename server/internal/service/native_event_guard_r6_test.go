package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Native Issue membership is independent of execution qualification.
func TestNativeEventR6PreservesOriginalIssueEligibility(t *testing.T) {
	agent := db.Agent{ID: util.MustParseUUID("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")}
	issue := db.Issue{AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: agent.ID}
	for _, category := range []string{"todo", "in_progress"} {
		if !nativeEventMayRun(issue, category, agent) {
			t.Errorf("eligible original assignment rejected at %q", category)
		}
	}
	for _, category := range []string{"backlog", "blocked", "in_review", "done", "cancelled"} {
		if nativeEventMayRun(issue, category, agent) {
			t.Errorf("unsafe automatic Task for %q", category)
		}
	}
	unassigned := issue
	unassigned.AssigneeID = pgtype.UUID{}
	if nativeEventMayRun(unassigned, "todo", agent) {
		t.Fatal("unassigned Issue was auto-routed to Oracle")
	}
	member := issue
	member.AssigneeType = pgtype.Text{String: "member", Valid: true}
	if nativeEventMayRun(member, "in_progress", agent) {
		t.Fatal("human-owned Issue was hijacked")
	}
	different := db.Agent{ID: util.MustParseUUID("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")}
	if nativeEventMayRun(issue, "todo", different) {
		t.Fatal("Issue was dispatched to a different agent")
	}
	triage := issue
	triage.TriageState = pgtype.Text{String: "pending", Valid: true}
	if nativeEventMayRun(triage, "todo", agent) {
		t.Fatal("triage Issue bypassed native gate")
	}
}
