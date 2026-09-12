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

type conversationRoutingQuerySpy struct {
	db.DBTX
	queries, rows, columns int
	fail                   bool
}

func (s *conversationRoutingQuerySpy) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if !strings.Contains(query, "-- name: ListTasksByIssue :many") && !strings.Contains(query, "-- name: ListTaskRoutingByIssueAndTriggerComment :many") {
		return s.DBTX.Query(ctx, query, args...)
	}
	s.queries++
	if s.fail {
		return nil, errors.New("test routing query unavailable")
	}
	rows, err := s.DBTX.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	s.columns = len(rows.FieldDescriptions())
	return &conversationRoutingRows{Rows: rows, spy: s}, nil
}

type conversationRoutingRows struct {
	pgx.Rows
	spy *conversationRoutingQuerySpy
}

func (r *conversationRoutingRows) Next() bool {
	ok := r.Rows.Next()
	if ok {
		r.spy.rows++
	}
	return ok
}

func TestCommentConversationRoutingQuery(t *testing.T) {
	ctx := context.Background()
	issueID := dbfx.Issue(t, "Conversation routing")
	rootID := dbfx.Comment(t, issueID, "Continue this conversation")
	otherRootID := dbfx.Comment(t, issueID, "Another conversation")
	emptyRootID := dbfx.Comment(t, issueID, "No prior run")
	agentA := dbfx.Agent(t, "Routing agent A", testRuntimeID)
	agentB := dbfx.Agent(t, "Routing agent B", testRuntimeID)
	oldSquad := dbfx.Squad(t, "Old routing squad", agentA)
	newSquad := dbfx.Squad(t, "New routing squad", agentA)
	baseTime := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		agent string
		squad any
		ago   time.Duration
	}{
		{agentA, oldSquad, 3 * time.Hour},
		{agentA, newSquad, 2 * time.Hour},
		// The newest run has no squad: keep searching older runs for the
		// newest non-NULL squad, rather than choosing the newest row alone.
		{agentA, nil, time.Hour},
		{agentB, nil, 90 * time.Minute},
	} {
		dbfx.Task(t, row.agent, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issueID, "trigger_comment_id": rootID, "status": "completed", "squad_id": row.squad, "created_at": baseTime.Add(-row.ago)})
	}
	for i := range 128 {
		dbfx.Task(t, agentA, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issueID, "trigger_comment_id": otherRootID, "status": "completed", "trigger_summary": strings.Repeat("unrelated context ", 256), "created_at": baseTime.Add(time.Duration(i) * time.Second)})
	}
	dbfx.Task(t, agentA, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issueID, "status": "completed"})
	otherIssue := dbfx.Issue(t, "Different issue")
	// With no database FKs, independently enforce both IDs even for an
	// inconsistent historical task whose trigger belongs to another issue.
	dbfx.Task(t, agentA, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": otherIssue, "trigger_comment_id": rootID, "status": "completed"})
	foreignWS := dbfx.Workspace(t, "Foreign routing workspace", "foreign-routing-workspace")
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	root, err := testHandler.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: parseUUID(rootID), WorkspaceID: issue.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name          string
		queries, rows int
		found         bool
	}{
		{"matching history", 1, 4, true},
		{"exclude other comment", 1, 4, true},
		{"exclude root", 0, 0, false},
		{"empty history", 1, 0, false},
		{"wrong workspace", 1, 0, false},
		{"query error", 1, 0, false},
		{"explicit mention wins", 0, 0, true},
		{"archived agent still rejected", 1, 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy := &conversationRoutingQuerySpy{DBTX: testPool, fail: tc.name == "query error"}
			h := *testHandler
			h.Queries = db.New(spy)
			inputIssue, inputRoot := issue, root
			opts := commentTriggerComputeOptions{}
			switch tc.name {
			case "exclude other comment":
				opts.ExcludeTriggerCommentID = parseUUID(otherRootID)
			case "exclude root":
				opts.ExcludeTriggerCommentID = root.ID
			case "empty history":
				inputRoot.ID = parseUUID(emptyRootID)
			case "wrong workspace":
				inputIssue.WorkspaceID = parseUUID(foreignWS)
			case "explicit mention wins":
				inputRoot.Content = fmt.Sprintf("Ask [B](mention://agent/%s)", agentB)
			case "archived agent still rejected":
				dbfx.Exec(t, "UPDATE agent SET archived_at = now() WHERE id = $1", agentA)
				t.Cleanup(func() { dbfx.Exec(t, "UPDATE agent SET archived_at = NULL WHERE id = $1", agentA) })
			}
			triggers, found := h.routeConversationOwnersForRoot(ctx, inputIssue, inputRoot, testUserID, opts)
			if found != tc.found {
				t.Errorf("found = %t, want %t", found, tc.found)
			}
			if spy.queries != tc.queries || spy.rows != tc.rows {
				t.Errorf("history queries/rows = %d/%d, want %d/%d", spy.queries, spy.rows, tc.queries, tc.rows)
			}
			if tc.queries > 0 && !spy.fail && spy.columns != 2 {
				t.Errorf("history projection has %d columns, want agent_id and squad_id only", spy.columns)
			}
			want := map[string]string{}
			if tc.found {
				want[agentB] = ""
				if tc.name != "explicit mention wins" && tc.name != "archived agent still rejected" {
					want[agentA] = newSquad
				}
			}
			if len(triggers) != len(want) {
				t.Fatalf("triggers = %d, want %d", len(triggers), len(want))
			}
			for _, trigger := range triggers {
				id := uuidToString(trigger.Agent.ID)
				wantSquad, ok := want[id]
				if !ok {
					t.Fatalf("unexpected routed agent %s", id)
				}
				gotSquad := ""
				if trigger.Squad != nil {
					gotSquad = uuidToString(trigger.Squad.ID)
				}
				if gotSquad != wantSquad {
					t.Errorf("agent %s squad = %s, want %s", id, gotSquad, wantSquad)
				}
			}
		})
	}
}
