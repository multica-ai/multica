package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestCreateIssuePositionTopOfColumn verifies that a newly created issue is
// placed above all existing issues in the same status column (manual sort order).
//
// Before the fix, new issues were always assigned position=0. After drag-reorder
// activity, existing issues accumulate negative positions at the top of the
// column, so a fresh issue at 0 would land in the middle of a long list.
//
// The fix queries MIN(position) for the workspace+status pair and assigns
// newPosition = minPos - 1, so the new ticket always appears first.
func TestCreateIssuePositionTopOfColumn(t *testing.T) {
	// Create two issues via the API. The first lands at COALESCE(MIN,0)-1 = -1,
	// the second at -2, and so on — each successive issue ends up above the
	// previous one, which is exactly the desired behavior.
	createIssueAndGetPosition := func(t *testing.T, title string) (string, float64) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":    title,
			"status":   "todo",
			"priority": "low",
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
		var issue IssueResponse
		json.NewDecoder(w.Body).Decode(&issue)
		return issue.ID, issue.Position
	}

	id1, pos1 := createIssueAndGetPosition(t, "position-test first issue")
	t.Cleanup(func() { deleteTestIssue(t, id1) })

	id2, pos2 := createIssueAndGetPosition(t, "position-test second issue")
	t.Cleanup(func() { deleteTestIssue(t, id2) })

	id3, pos3 := createIssueAndGetPosition(t, "position-test third issue")
	t.Cleanup(func() { deleteTestIssue(t, id3) })

	// Each new issue must have a strictly lower position than the previous one,
	// ensuring it sorts to the top of the column in manual order.
	if pos2 >= pos1 {
		t.Errorf("second issue position (%v) should be less than first (%v)", pos2, pos1)
	}
	if pos3 >= pos2 {
		t.Errorf("third issue position (%v) should be less than second (%v)", pos3, pos2)
	}
}

// TestCreateIssuePositionBelowExplicitMinimum verifies the fix against a
// realistic drag-reordered column: after manually setting a low position
// directly in the DB (simulating drag-and-drop), a new issue created via the
// API should land below the explicit minimum, not at 0.
func TestCreateIssuePositionBelowExplicitMinimum(t *testing.T) {
	// Create a seed issue via the API.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "position-seed issue",
		"status":   "todo",
		"priority": "low",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var seed IssueResponse
	json.NewDecoder(w.Body).Decode(&seed)
	t.Cleanup(func() { deleteTestIssue(t, seed.ID) })

	// Simulate drag-and-drop: overwrite the seed's position to a large negative
	// value (-9999), as if the user dragged it to the very top of a long list.
	const simulatedMinPos = -9999.0
	if _, err := testPool.Exec(t.Context(),
		`UPDATE issue SET position = $1 WHERE id = $2`,
		simulatedMinPos, seed.ID,
	); err != nil {
		t.Fatalf("failed to set explicit position: %v", err)
	}

	// Now create a new issue. It must land below -9999, not at 0.
	w2 := httptest.NewRecorder()
	req2 := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "position-new issue",
		"status":   "todo",
		"priority": "low",
	})
	testHandler.CreateIssue(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("new CreateIssue: expected 201, got %d: %s", w2.Code, w2.Body.String())
	}
	var newIssue IssueResponse
	json.NewDecoder(w2.Body).Decode(&newIssue)
	t.Cleanup(func() { deleteTestIssue(t, newIssue.ID) })

	if newIssue.Position >= simulatedMinPos {
		t.Errorf("new issue position (%v) should be less than simulated min (%v); got position 0 (unfixed behavior)?",
			newIssue.Position, simulatedMinPos)
	}
}

func TestAutopilotCreateIssuePositionBelowCurrentMinimum(t *testing.T) {
	ctx := context.Background()
	seedTitle := fmt.Sprintf("position-autopilot seed %d", time.Now().UnixNano())
	autopilotIssueTitle := fmt.Sprintf("position-autopilot issue %d", time.Now().UnixNano())

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    seedTitle,
		"status":   "todo",
		"priority": "low",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var seed IssueResponse
	json.NewDecoder(w.Body).Decode(&seed)
	t.Cleanup(func() { deleteTestIssue(t, seed.ID) })

	const simulatedMinPos = -9999.0
	if _, err := testPool.Exec(ctx,
		`UPDATE issue SET position = $1 WHERE id = $2`,
		simulatedMinPos, seed.ID,
	); err != nil {
		t.Fatalf("failed to set explicit position: %v", err)
	}

	var minBefore float64
	if err := testPool.QueryRow(ctx,
		`SELECT MIN(position) FROM issue WHERE workspace_id = $1 AND status = 'todo'`,
		testWorkspaceID,
	).Scan(&minBefore); err != nil {
		t.Fatalf("load min position: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                "Position autopilot",
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"issue_title_template": autopilotIssueTitle,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilot.ID) })

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilot.ID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.IssueID.Valid {
		t.Fatalf("dispatch run = %+v, want linked issue", run)
	}
	issueID := uuidToString(run.IssueID)
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	var createdPos float64
	if err := testPool.QueryRow(ctx, `SELECT position FROM issue WHERE id = $1`, issueID).Scan(&createdPos); err != nil {
		t.Fatalf("load autopilot-created issue position: %v", err)
	}
	if createdPos >= minBefore {
		t.Errorf("autopilot-created issue position (%v) should be less than current min (%v); fixed position 0 would sort in the middle",
			createdPos, minBefore)
	}
}
