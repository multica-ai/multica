package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Observe actual result columns/rows at the database boundary. A query that
// filters later in Go still transfers the unwanted task payloads and fails.
type rootOwnerReadDB struct {
	db.DBTX
	rows, reads int
	payload     bool
	fail        bool
}
type rootOwnerRows struct {
	pgx.Rows
	observer *rootOwnerReadDB
}

func (r *rootOwnerRows) Next() bool {
	ok := r.Rows.Next()
	if ok {
		r.observer.rows++
	}
	return ok
}
func (d *rootOwnerReadDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !strings.Contains(sql, "FROM agent_task_queue") {
		return d.DBTX.Query(ctx, sql, args...)
	}
	d.reads++
	if d.fail {
		return nil, errors.New("injected owner read failure")
	}
	rows, err := d.DBTX.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	for _, field := range rows.FieldDescriptions() {
		if field.Name == "context" || field.Name == "result" {
			d.payload = true
		}
	}
	return &rootOwnerRows{Rows: rows, observer: d}, nil
}

func TestConversationRootOwnersReadOnlyRoutingData(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentA := createHandlerTestAgent(t, "Root owner A", nil)
	agentB := createHandlerTestAgent(t, "Root owner B", nil)
	otherAgent := createHandlerTestAgent(t, "Other root owner", nil)
	issueID := dbfx.Issue(t, "Routing projection")
	otherIssueID := dbfx.Issue(t, "Other issue")
	rootID := dbfx.Comment(t, issueID, "No explicit owner")
	otherRootID := dbfx.Comment(t, issueID, "Another root")
	oldSquad := dbfx.Squad(t, "Old root squad", agentA)
	newSquad := dbfx.Squad(t, "New root squad", agentA)
	for i := 0; i < 96; i++ {
		squad := any(nil)
		if i == 0 {
			squad = oldSquad
		}
		if i == 1 {
			squad = newSquad
		}
		dbfx.Task(t, agentA, testutil.Cols{
			"issue_id": issueID, "trigger_comment_id": rootID, "runtime_id": handlerTestRuntimeID(t), "status": "completed", "squad_id": squad,
			"created_at": time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC),
			"context":    testutil.Raw(`jsonb_build_object('large',repeat('x',65536))`),
			"result":     testutil.Raw(`jsonb_build_object('large',repeat('y',65536))`),
		})
	}
	dbfx.Task(t, agentB, testutil.Cols{"issue_id": issueID, "trigger_comment_id": rootID, "runtime_id": handlerTestRuntimeID(t), "status": "failed"})
	dbfx.Task(t, otherAgent, testutil.Cols{"issue_id": issueID, "trigger_comment_id": otherRootID, "runtime_id": handlerTestRuntimeID(t), "status": "completed"})
	dbfx.Task(t, otherAgent, testutil.Cols{"issue_id": otherIssueID, "trigger_comment_id": rootID, "runtime_id": handlerTestRuntimeID(t), "status": "completed"})
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	root, err := testHandler.Queries.GetComment(ctx, parseUUID(rootID))
	if err != nil {
		t.Fatal(err)
	}
	observer := &rootOwnerReadDB{DBTX: testPool}
	h := *testHandler
	h.Queries = db.New(observer)
	triggers, owned := h.routeConversationOwnersForRoot(ctx, issue, root, testUserID, commentTriggerComputeOptions{})
	if !owned || len(triggers) != 2 {
		t.Fatalf("owned=%t triggers=%d, want two root owners", owned, len(triggers))
	}
	for _, trigger := range triggers {
		switch uuidToString(trigger.Agent.ID) {
		case agentA:
			if trigger.Squad == nil || uuidToString(trigger.Squad.ID) != newSquad {
				t.Fatal("lost newest non-null squad")
			}
		case agentB:
			if trigger.Squad != nil {
				t.Fatal("invented squad")
			}
		default:
			t.Fatal("routed unrelated task owner")
		}
	}
	if observer.reads != 1 || observer.rows != 2 || observer.payload {
		t.Fatalf("routing read: reads=%d rows=%d payload=%t; want one narrow row per owner", observer.reads, observer.rows, observer.payload)
	}

	for _, excluded := range []string{rootID, otherRootID} {
		got, owned := h.routeConversationOwnersForRoot(ctx, issue, root, testUserID, commentTriggerComputeOptions{ExcludeTriggerCommentID: parseUUID(excluded)})
		if excluded == rootID {
			if owned || len(got) != 0 {
				t.Fatal("excluded root routed")
			}
		} else if !owned || len(got) != 2 {
			t.Fatal("unrelated exclusion removed owners")
		}
	}
	emptyID := dbfx.Comment(t, issueID, "No routed task")
	empty, err := testHandler.Queries.GetComment(ctx, parseUUID(emptyID))
	if err != nil {
		t.Fatal(err)
	}
	if got, owned := h.routeConversationOwnersForRoot(ctx, issue, empty, testUserID, commentTriggerComputeOptions{}); owned || len(got) != 0 {
		t.Fatal("invented empty root owner")
	}
	observer.fail = true
	if got, owned := h.routeConversationOwnersForRoot(ctx, issue, root, testUserID, commentTriggerComputeOptions{}); owned || len(got) != 0 {
		t.Fatal("read failure routed an owner")
	}
	// An explicit mention remains authoritative, even if historical lookup fails.
	root.Content = fmt.Sprintf("[@Other](mention://agent/%s)", otherAgent)
	if got, owned := h.routeConversationOwnersForRoot(ctx, issue, root, testUserID, commentTriggerComputeOptions{}); !owned || len(got) != 1 || uuidToString(got[0].Agent.ID) != otherAgent {
		t.Fatal("explicit owner lost precedence")
	}
	observer.fail = false
	root.Content = "No explicit owner"
	dbfx.Exec(t, "UPDATE agent SET archived_at=now() WHERE id=$1", agentB)
	if got, owned := h.routeConversationOwnersForRoot(ctx, issue, root, testUserID, commentTriggerComputeOptions{}); !owned || len(got) != 1 || uuidToString(got[0].Agent.ID) != agentA {
		t.Fatal("archived owner remained invokable")
	}
	dbfx.Exec(t, "UPDATE agent SET archived_at=now() WHERE id=$1", agentA)
	if got, owned := h.routeConversationOwnersForRoot(ctx, issue, root, testUserID, commentTriggerComputeOptions{}); !owned || len(got) != 0 {
		t.Fatal("unavailable historical owners must still suppress reassignment fallback")
	}

}
