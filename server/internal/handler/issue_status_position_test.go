package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// position ranks an issue inside its own (workspace, status) column, so the
// value is meaningless once the status moves the issue to a different column.
// Carrying it over put agent-completed work below every hand-dragged issue in
// Done — the column an agent writes to and never drags in. These tests pin the
// re-ranking to the two queries that can change a status.

// destinationMin reads the current top of a column so assertions stay valid
// against a shared test workspace other suites have left rows in.
func destinationMin(t *testing.T, status string) float64 {
	t.Helper()
	var min float64
	dbfx.QueryRow(t,
		`SELECT COALESCE(MIN(position), 0) FROM issue WHERE workspace_id = $1 AND status = $2`,
		testWorkspaceID, status).Scan(&min)
	return min
}

func issuePosition(t *testing.T, issueID string) float64 {
	t.Helper()
	var pos float64
	dbfx.QueryRow(t, `SELECT position FROM issue WHERE id = $1`, issueID).Scan(&pos)
	return pos
}

// TestUpdateIssueStatusRanksAboveDestinationColumn reproduces the reported
// board state: a Done column ordered by hand, and an issue arriving from Todo
// carrying the -1 that made it the top of Todo. -1 is above nothing in Done.
func TestUpdateIssueStatusRanksAboveDestinationColumn(t *testing.T) {
	for _, pos := range []float64{-57.75, -20, -7.25} {
		dbfx.Issue(t, "status-position hand-dragged done issue", testutil.Cols{
			"status":   "done",
			"position": pos,
		})
	}
	moving := dbfx.Issue(t, "status-position moving issue", testutil.Cols{
		"status":   "todo",
		"position": -1.0,
	})

	before := destinationMin(t, "done")

	var updated IssueResponse
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+moving, map[string]any{"status": "done"}),
		"id", moving,
	)).Want(http.StatusOK).JSON(&updated)

	if updated.Position >= before {
		t.Errorf("position after move = %v, want above the Done column top (%v)", updated.Position, before)
	}
	if updated.Position >= -57.75 {
		t.Errorf("position after move = %v, want above the hand-dragged issues (-57.75 and lower)", updated.Position)
	}
}

// TestUpdateIssueStatusKeepsExplicitPosition covers cross-column drag-and-drop,
// which sends status and position together and means the slot it dropped on.
func TestUpdateIssueStatusKeepsExplicitPosition(t *testing.T) {
	dbfx.Issue(t, "status-position explicit destination issue", testutil.Cols{
		"status":   "done",
		"position": -1000.0,
	})
	moving := dbfx.Issue(t, "status-position explicit moving issue", testutil.Cols{
		"status":   "todo",
		"position": -1.0,
	})

	const dropped = -12.5
	var updated IssueResponse
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+moving, map[string]any{
			"status":   "done",
			"position": dropped,
		}),
		"id", moving,
	)).Want(http.StatusOK).JSON(&updated)

	if updated.Position != dropped {
		t.Errorf("position after drag = %v, want the dropped slot %v", updated.Position, dropped)
	}
}

// TestUpdateIssueWithoutStatusChangeKeepsPosition guards the other direction:
// editing an issue in place must not shuffle the column it is already in.
func TestUpdateIssueWithoutStatusChangeKeepsPosition(t *testing.T) {
	issueID := dbfx.Issue(t, "status-position untouched issue", testutil.Cols{
		"status":   "todo",
		"position": -3.5,
	})

	var updated IssueResponse
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
			"title":  "status-position untouched issue, retitled",
			"status": "todo",
		}),
		"id", issueID,
	)).Want(http.StatusOK).JSON(&updated)

	if updated.Position != -3.5 {
		t.Errorf("position after in-place edit = %v, want it left at -3.5", updated.Position)
	}
}

// TestUpdateIssueStatusQueryRanksAboveDestinationColumn covers the query GitHub
// sync and agent task completion call directly, bypassing the UpdateIssue
// handler. Agents close issues through this path and never drag anything, so
// leaving it out would leave the reported bug in place for its main victim.
func TestUpdateIssueStatusQueryRanksAboveDestinationColumn(t *testing.T) {
	dbfx.Issue(t, "direct status-position destination issue", testutil.Cols{
		"status":   "done",
		"position": -1000.0,
	})
	moving := dbfx.Issue(t, "direct status-position moving issue", testutil.Cols{
		"status":   "todo",
		"position": -1.0,
	})

	before := destinationMin(t, "done")

	updated, err := testHandler.Queries.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{
		ID:          parseUUID(moving),
		Status:      "done",
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("UpdateIssueStatus: %v", err)
	}
	if updated.Position >= before {
		t.Errorf("position after move = %v, want above the Done column top (%v)", updated.Position, before)
	}
}

// TestUpdateIssueStatusQueryNoopKeepsPosition pins the same-status write: a
// re-delivered GitHub webhook or a repeated completion must not walk the issue
// further up its own column on every call.
func TestUpdateIssueStatusQueryNoopKeepsPosition(t *testing.T) {
	issueID := dbfx.Issue(t, "direct status-position noop issue", testutil.Cols{
		"status":   "done",
		"position": -4.25,
	})

	if _, err := testHandler.Queries.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{
		ID:          parseUUID(issueID),
		Status:      "done",
		WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("UpdateIssueStatus: %v", err)
	}

	if got := issuePosition(t, issueID); got != -4.25 {
		t.Errorf("position after same-status write = %v, want it left at -4.25", got)
	}
}
