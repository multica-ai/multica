package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// addAutopilotSuccessorViaAPI is the test helper for POST
// /api/autopilots/{id}/successors as the workspace owner.
func addAutopilotSuccessorViaAPI(t *testing.T, apID, successorID, onStatus string, wantStatus int) {
	t.Helper()
	body := map[string]any{
		"successor_autopilot_id": successorID,
	}
	if onStatus != "" {
		body["on_status"] = onStatus
	}
	w := httptest.NewRecorder()
	path := "/api/autopilots/" + apID + "/successors?workspace_id=" + testWorkspaceID
	r := withURLParam(newRequest("POST", path, body), "id", apID)
	testHandler.AddAutopilotSuccessor(w, r)
	if w.Code != wantStatus {
		t.Fatalf("AddAutopilotSuccessor: expected %d, got %d: %s", wantStatus, w.Code, w.Body.String())
	}
}

// listAutopilotSuccessorsViaAPI returns the decoded successors list as the
// workspace owner.
func listAutopilotSuccessorsViaAPI(t *testing.T, apID string) []AutopilotSuccessorEntry {
	t.Helper()
	w := httptest.NewRecorder()
	path := "/api/autopilots/" + apID + "/successors?workspace_id=" + testWorkspaceID
	r := withURLParam(newRequest("GET", path, nil), "id", apID)
	testHandler.ListAutopilotSuccessors(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAutopilotSuccessors: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Successors []AutopilotSuccessorEntry `json:"successors"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode successors: %v", err)
	}
	return resp.Successors
}

func TestAutopilotSuccessorAddListDelete(t *testing.T) {
	ctx := context.Background()
	apA := createAutopilotAs(t, "", "successor-add-list-A")
	apB := createAutopilotAs(t, "", "successor-add-list-B")
	defer func() {
		testPool.Exec(ctx, `DELETE FROM autopilot_successor WHERE autopilot_id = $1`, apA)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id IN ($1, $2)`, apA, apB)
	}()

	// Add A -> B with on_status=failed.
	addAutopilotSuccessorViaAPI(t, apA, apB, "failed", http.StatusCreated)

	succs := listAutopilotSuccessorsViaAPI(t, apA)
	if len(succs) != 1 {
		t.Fatalf("expected 1 successor, got %d", len(succs))
	}
	if succs[0].SuccessorAutopilotID != apB {
		t.Fatalf("expected successor %s, got %s", apB, succs[0].SuccessorAutopilotID)
	}
	if succs[0].OnStatus != "failed" {
		t.Fatalf("expected on_status failed, got %s", succs[0].OnStatus)
	}

	// Re-adding the same edge should upsert on_status (idempotent).
	addAutopilotSuccessorViaAPI(t, apA, apB, "both", http.StatusCreated)
	succs = listAutopilotSuccessorsViaAPI(t, apA)
	if len(succs) != 1 {
		t.Fatalf("expected 1 successor after upsert, got %d", len(succs))
	}
	if succs[0].OnStatus != "both" {
		t.Fatalf("expected on_status both after upsert, got %s", succs[0].OnStatus)
	}

	// Delete the edge.
	w := httptest.NewRecorder()
	path := "/api/autopilots/" + apA + "/successors/" + apB + "?workspace_id=" + testWorkspaceID
	r := withURLParams(newRequest("DELETE", path, nil), "id", apA, "successorId", apB)
	testHandler.DeleteAutopilotSuccessor(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteAutopilotSuccessor: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	succs = listAutopilotSuccessorsViaAPI(t, apA)
	if len(succs) != 0 {
		t.Fatalf("expected 0 successors after delete, got %d", len(succs))
	}
}

func TestAutopilotSuccessorDefaultOnStatus(t *testing.T) {
	ctx := context.Background()
	apA := createAutopilotAs(t, "", "successor-default-A")
	apB := createAutopilotAs(t, "", "successor-default-B")
	defer func() {
		testPool.Exec(ctx, `DELETE FROM autopilot_successor WHERE autopilot_id = $1`, apA)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id IN ($1, $2)`, apA, apB)
	}()

	// Omit on_status; expect default "completed".
	body := map[string]any{"successor_autopilot_id": apB}
	w := httptest.NewRecorder()
	path := "/api/autopilots/" + apA + "/successors?workspace_id=" + testWorkspaceID
	r := withURLParam(newRequest("POST", path, body), "id", apA)
	testHandler.AddAutopilotSuccessor(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	succs := listAutopilotSuccessorsViaAPI(t, apA)
	if len(succs) != 1 || succs[0].OnStatus != "completed" {
		t.Fatalf("expected default on_status=completed, got %+v", succs)
	}
}

func TestAutopilotSuccessorSelfLoopRejected(t *testing.T) {
	ctx := context.Background()
	apA := createAutopilotAs(t, "", "successor-self-A")
	defer func() {
		testPool.Exec(ctx, `DELETE FROM autopilot_successor WHERE autopilot_id = $1`, apA)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, apA)
	}()

	addAutopilotSuccessorViaAPI(t, apA, apA, "completed", http.StatusBadRequest)
}

func TestAutopilotSuccessorCycleRejected(t *testing.T) {
	ctx := context.Background()
	apA := createAutopilotAs(t, "", "successor-cycle-A")
	apB := createAutopilotAs(t, "", "successor-cycle-B")
	apC := createAutopilotAs(t, "", "successor-cycle-C")
	defer func() {
		testPool.Exec(ctx, `DELETE FROM autopilot_successor WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id IN ($1, $2, $3)`, apA, apB, apC)
	}()

	// Build a chain A -> B -> C.
	addAutopilotSuccessorViaAPI(t, apA, apB, "completed", http.StatusCreated)
	addAutopilotSuccessorViaAPI(t, apB, apC, "completed", http.StatusCreated)

	// Adding C -> A would close the loop A -> B -> C -> A; expect 409.
	addAutopilotSuccessorViaAPI(t, apC, apA, "completed", http.StatusConflict)

	// Adding A -> C (forward edge, no cycle) should succeed.
	addAutopilotSuccessorViaAPI(t, apA, apC, "completed", http.StatusCreated)
}

func TestAutopilotSuccessorInvalidOnStatus(t *testing.T) {
	ctx := context.Background()
	apA := createAutopilotAs(t, "", "successor-invalid-A")
	apB := createAutopilotAs(t, "", "successor-invalid-B")
	defer func() {
		testPool.Exec(ctx, `DELETE FROM autopilot_successor WHERE autopilot_id = $1`, apA)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id IN ($1, $2)`, apA, apB)
	}()

	addAutopilotSuccessorViaAPI(t, apA, apB, "sometimes", http.StatusBadRequest)
}

func TestAutopilotPredecessorsList(t *testing.T) {
	ctx := context.Background()
	apA := createAutopilotAs(t, "", "successor-pred-A")
	apB := createAutopilotAs(t, "", "successor-pred-B")
	defer func() {
		testPool.Exec(ctx, `DELETE FROM autopilot_successor WHERE autopilot_id = $1`, apA)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id IN ($1, $2)`, apA, apB)
	}()

	addAutopilotSuccessorViaAPI(t, apA, apB, "completed", http.StatusCreated)

	// B should see A as a predecessor.
	w := httptest.NewRecorder()
	path := "/api/autopilots/" + apB + "/predecessors?workspace_id=" + testWorkspaceID
	r := withURLParam(newRequest("GET", path, nil), "id", apB)
	testHandler.ListAutopilotPredecessors(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAutopilotPredecessors: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Predecessors []AutopilotSuccessorEntry `json:"predecessors"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode predecessors: %v", err)
	}
	if len(resp.Predecessors) != 1 || resp.Predecessors[0].AutopilotID != apA {
		t.Fatalf("expected predecessor %s, got %+v", apA, resp.Predecessors)
	}
}

func TestAutopilotSuccessorNotFound(t *testing.T) {
	ctx := context.Background()
	apA := createAutopilotAs(t, "", "successor-nf-A")
	defer func() {
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, apA)
	}()

	// Non-existent successor UUID -> 404.
	bogusID := "00000000-0000-0000-0000-000000000000"
	addAutopilotSuccessorViaAPI(t, apA, bogusID, "completed", http.StatusNotFound)
}

func TestGetAutopilotIncludesSuccessors(t *testing.T) {
	ctx := context.Background()
	apA := createAutopilotAs(t, "", "successor-get-A")
	apB := createAutopilotAs(t, "", "successor-get-B")
	defer func() {
		testPool.Exec(ctx, `DELETE FROM autopilot_successor WHERE autopilot_id = $1`, apA)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id IN ($1, $2)`, apA, apB)
	}()

	addAutopilotSuccessorViaAPI(t, apA, apB, "both", http.StatusCreated)

	w := httptest.NewRecorder()
	path := "/api/autopilots/" + apA + "?workspace_id=" + testWorkspaceID
	r := withURLParam(newRequest("GET", path, nil), "id", apA)
	testHandler.GetAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Successors []AutopilotSuccessorEntry `json:"successors"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode get autopilot: %v", err)
	}
	if len(resp.Successors) != 1 || resp.Successors[0].SuccessorAutopilotID != apB {
		t.Fatalf("expected successors to include %s, got %+v", apB, resp.Successors)
	}
}
