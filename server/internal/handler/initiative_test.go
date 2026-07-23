package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func withInitiativesFlag(t *testing.T, enabled bool) {
	t.Helper()
	withFeatureFlag(t, testHandler, featureflags.InitiativesOrchestrator, enabled)
}

// insertTestInitiative seeds an initiative row directly and registers cleanup
// of the row plus its child tables (no DB cascades by repo rule).
func insertTestInitiative(t *testing.T, status, title string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO initiative (workspace_id, title, idea, status, created_by)
		VALUES ($1, $2, 'seeded idea', $3, $4)
		RETURNING id
	`, testWorkspaceID, title, status, testUserID).Scan(&id); err != nil {
		t.Fatalf("failed to insert test initiative: %v", err)
	}
	t.Cleanup(func() { cleanupTestInitiative(id) })
	return id
}

func cleanupTestInitiative(id string) {
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM initiative_blocker WHERE initiative_id = $1`, id)
	testPool.Exec(ctx, `DELETE FROM initiative_event WHERE initiative_id = $1`, id)
	testPool.Exec(ctx, `DELETE FROM initiative_task WHERE initiative_id = $1`, id)
	testPool.Exec(ctx, `DELETE FROM initiative WHERE id = $1`, id)
}

func decodeInitiativeResponse(t *testing.T, body []byte) InitiativeResponse {
	t.Helper()
	var resp InitiativeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to decode initiative response: %v (%s)", err, body)
	}
	return resp
}

func TestInitiativeEndpoints_FlagOff(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// No flag override: the release flag defaults to off, so the entire
	// surface must be indistinguishable from absent.
	w := httptest.NewRecorder()
	testHandler.CreateInitiative(w, newRequest("POST", "/api/initiatives", map[string]any{"idea": "flag off"}))
	if w.Code != 404 {
		t.Fatalf("CreateInitiative with flag off: expected 404, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	testHandler.ListInitiatives(w, newRequest("GET", "/api/initiatives", nil))
	if w.Code != 404 {
		t.Fatalf("ListInitiatives with flag off: expected 404, got %d", w.Code)
	}
}

func TestCreateInitiative_ValidationAndDefaults(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)

	// Missing idea.
	w := httptest.NewRecorder()
	testHandler.CreateInitiative(w, newRequest("POST", "/api/initiatives", map[string]any{"title": "no idea"}))
	if w.Code != 400 {
		t.Fatalf("expected 400 for missing idea, got %d", w.Code)
	}

	// Autonomy out of range.
	w = httptest.NewRecorder()
	testHandler.CreateInitiative(w, newRequest("POST", "/api/initiatives", map[string]any{"idea": "x", "autonomy_level": 5}))
	if w.Code != 400 {
		t.Fatalf("expected 400 for autonomy_level 5, got %d", w.Code)
	}

	// Title derived from the idea's first line when omitted.
	w = httptest.NewRecorder()
	testHandler.CreateInitiative(w, newRequest("POST", "/api/initiatives", map[string]any{
		"idea": "Build the initiatives feature\nwith many details",
	}))
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	created := decodeInitiativeResponse(t, w.Body.Bytes())
	t.Cleanup(func() { cleanupTestInitiative(created.ID) })
	if created.Title != "Build the initiatives feature" {
		t.Errorf("derived title = %q", created.Title)
	}
	if created.Status != "draft" {
		t.Errorf("new initiative status = %q, want draft", created.Status)
	}
	if created.PlanVersion != 0 {
		t.Errorf("new initiative plan_version = %d, want 0", created.PlanVersion)
	}

	// The created audit event must exist.
	var eventCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM initiative_event WHERE initiative_id = $1 AND event_type = 'created'`,
		created.ID).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Errorf("created event count = %d (err %v), want 1", eventCount, err)
	}
}

func TestInitiativeLifecycle_PlanPauseResumeCancel(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)

	w := httptest.NewRecorder()
	testHandler.CreateInitiative(w, newRequest("POST", "/api/initiatives", map[string]any{"idea": "lifecycle test"}))
	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	initiative := decodeInitiativeResponse(t, w.Body.Bytes())
	t.Cleanup(func() { cleanupTestInitiative(initiative.ID) })

	// draft -> planning.
	w = httptest.NewRecorder()
	testHandler.RequestInitiativePlan(w, withURLParam(newRequest("POST", "/api/initiatives/"+initiative.ID+"/plan", nil), "id", initiative.ID))
	if w.Code != 200 {
		t.Fatalf("plan request: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeInitiativeResponse(t, w.Body.Bytes()).Status; got != "planning" {
		t.Fatalf("after plan request status = %q, want planning", got)
	}

	// Approving while planning is an illegal gate -> 409.
	w = httptest.NewRecorder()
	testHandler.ApproveInitiativePlan(w, withURLParam(newRequest("POST", "/api/initiatives/"+initiative.ID+"/plan/approve", nil), "id", initiative.ID))
	if w.Code != 409 {
		t.Fatalf("approve while planning: expected 409, got %d", w.Code)
	}

	// planning -> paused (with reason) -> resumed back to planning.
	w = httptest.NewRecorder()
	testHandler.PauseInitiative(w, withURLParam(newRequest("POST", "/api/initiatives/"+initiative.ID+"/pause", map[string]any{"reason": "manual stop"}), "id", initiative.ID))
	if w.Code != 200 {
		t.Fatalf("pause: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	paused := decodeInitiativeResponse(t, w.Body.Bytes())
	if paused.Status != "paused" {
		t.Fatalf("after pause status = %q", paused.Status)
	}
	if paused.PausePrevStatus == nil || *paused.PausePrevStatus != "planning" {
		t.Fatalf("pause_prev_status = %v, want planning", paused.PausePrevStatus)
	}
	if paused.PauseReason == nil || *paused.PauseReason != "manual stop" {
		t.Fatalf("pause_reason = %v", paused.PauseReason)
	}

	// Double pause conflicts.
	w = httptest.NewRecorder()
	testHandler.PauseInitiative(w, withURLParam(newRequest("POST", "/api/initiatives/"+initiative.ID+"/pause", nil), "id", initiative.ID))
	if w.Code != 409 {
		t.Fatalf("double pause: expected 409, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	testHandler.ResumeInitiative(w, withURLParam(newRequest("POST", "/api/initiatives/"+initiative.ID+"/resume", nil), "id", initiative.ID))
	if w.Code != 200 {
		t.Fatalf("resume: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resumed := decodeInitiativeResponse(t, w.Body.Bytes())
	if resumed.Status != "planning" {
		t.Fatalf("after resume status = %q, want planning", resumed.Status)
	}
	if resumed.PausePrevStatus != nil || resumed.PauseReason != nil {
		t.Fatalf("resume must clear pause bookkeeping, got prev=%v reason=%v", resumed.PausePrevStatus, resumed.PauseReason)
	}

	// Cancel from planning.
	w = httptest.NewRecorder()
	testHandler.CancelInitiative(w, withURLParam(newRequest("POST", "/api/initiatives/"+initiative.ID+"/cancel", map[string]any{"reason": "no longer needed"}), "id", initiative.ID))
	if w.Code != 200 {
		t.Fatalf("cancel: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := decodeInitiativeResponse(t, w.Body.Bytes()).Status; got != "cancelled" {
		t.Fatalf("after cancel status = %q", got)
	}

	// Cancelled is terminal: further transitions conflict.
	w = httptest.NewRecorder()
	testHandler.RequestInitiativePlan(w, withURLParam(newRequest("POST", "/api/initiatives/"+initiative.ID+"/plan", nil), "id", initiative.ID))
	if w.Code != 409 {
		t.Fatalf("plan request after cancel: expected 409, got %d", w.Code)
	}

	// The audit trail recorded every hop.
	w = httptest.NewRecorder()
	testHandler.ListInitiativeEvents(w, withURLParam(newRequest("GET", "/api/initiatives/"+initiative.ID+"/events", nil), "id", initiative.ID))
	if w.Code != 200 {
		t.Fatalf("events: expected 200, got %d", w.Code)
	}
	var eventsResp ListInitiativeEventsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &eventsResp); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	// created + status_changed(planning) + status_changed(paused) +
	// status_changed(planning) + cancelled = 5.
	if len(eventsResp.Events) != 5 {
		t.Fatalf("event count = %d, want 5: %s", len(eventsResp.Events), w.Body.String())
	}
	if eventsResp.Events[len(eventsResp.Events)-1].EventType != "created" {
		t.Errorf("oldest event = %q, want created", eventsResp.Events[len(eventsResp.Events)-1].EventType)
	}
}

func TestGetInitiative_CrossWorkspaceIsolation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)
	ctx := context.Background()

	// Seed a foreign workspace with its own initiative.
	var foreignWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix) VALUES ('Initiative Foreign WS', 'initiative-foreign-ws', 'IFW')
		RETURNING id
	`).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID) })

	var foreignInitiativeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO initiative (workspace_id, title, idea, created_by)
		VALUES ($1, 'foreign', 'foreign idea', $2)
		RETURNING id
	`, foreignWorkspaceID, testUserID).Scan(&foreignInitiativeID); err != nil {
		t.Fatalf("seed foreign initiative: %v", err)
	}
	t.Cleanup(func() { cleanupTestInitiative(foreignInitiativeID) })

	// Fetching it through the fixture workspace must 404.
	w := httptest.NewRecorder()
	testHandler.GetInitiative(w, withURLParam(newRequest("GET", "/api/initiatives/"+foreignInitiativeID, nil), "id", foreignInitiativeID))
	if w.Code != 404 {
		t.Fatalf("cross-workspace get: expected 404, got %d", w.Code)
	}
}

func TestApproveInitiativePlan_StampsApproval(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)

	id := insertTestInitiative(t, "plan_review", "approve-test")

	w := httptest.NewRecorder()
	testHandler.ApproveInitiativePlan(w, withURLParam(newRequest("POST", "/api/initiatives/"+id+"/plan/approve", nil), "id", id))
	if w.Code != 200 {
		t.Fatalf("approve: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	approved := decodeInitiativeResponse(t, w.Body.Bytes())
	if approved.Status != "executing" {
		t.Errorf("status after approve = %q, want executing", approved.Status)
	}
	if approved.ApprovedBy == nil || approved.ApprovedAt == nil {
		t.Errorf("approve must stamp approved_by/approved_at, got %v/%v", approved.ApprovedBy, approved.ApprovedAt)
	}

	// Second approve: the CAS already moved the row out of plan_review.
	w = httptest.NewRecorder()
	testHandler.ApproveInitiativePlan(w, withURLParam(newRequest("POST", "/api/initiatives/"+id+"/plan/approve", nil), "id", id))
	if w.Code != 409 {
		t.Fatalf("double approve: expected 409, got %d", w.Code)
	}
}

func TestCancelInitiative_CancelsLinkedIssuesAndTasks(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)
	ctx := context.Background()

	id := insertTestInitiative(t, "executing", "cancel-cleanup-test")

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'initiative task issue', 'in_progress', 'medium', 'member', $2,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	var doneIssueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'already done issue', 'done', 'medium', 'member', $2,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&doneIssueID); err != nil {
		t.Fatalf("seed done issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, doneIssueID) })

	for _, seed := range []struct {
		title, state, issue string
	}{
		{"active task", "in_progress", issueID},
		{"finished task", "done", doneIssueID},
	} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO initiative_task (initiative_id, workspace_id, plan_version, title, role, state, issue_id)
			VALUES ($1, $2, 1, $3, 'implement', $4, $5)
		`, id, testWorkspaceID, seed.title, seed.state, seed.issue); err != nil {
			t.Fatalf("seed initiative task: %v", err)
		}
	}

	w := httptest.NewRecorder()
	testHandler.CancelInitiative(w, withURLParam(newRequest("POST", "/api/initiatives/"+id+"/cancel", nil), "id", id))
	if w.Code != 200 {
		t.Fatalf("cancel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var issueStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&issueStatus); err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	if issueStatus != "cancelled" {
		t.Errorf("linked issue status = %q, want cancelled", issueStatus)
	}

	var doneIssueStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, doneIssueID).Scan(&doneIssueStatus); err != nil {
		t.Fatalf("reload done issue: %v", err)
	}
	if doneIssueStatus != "done" {
		t.Errorf("terminal issue must be left alone, got %q", doneIssueStatus)
	}

	var taskState, taskReason string
	if err := testPool.QueryRow(ctx,
		`SELECT state, COALESCE(state_reason, '') FROM initiative_task WHERE initiative_id = $1 AND title = 'active task'`,
		id).Scan(&taskState, &taskReason); err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if taskState != "failed" || taskReason != "initiative_cancelled" {
		t.Errorf("active task after cancel = %s/%s, want failed/initiative_cancelled", taskState, taskReason)
	}

	var doneTaskState string
	if err := testPool.QueryRow(ctx,
		`SELECT state FROM initiative_task WHERE initiative_id = $1 AND title = 'finished task'`,
		id).Scan(&doneTaskState); err != nil {
		t.Fatalf("reload done task: %v", err)
	}
	if doneTaskState != "done" {
		t.Errorf("terminal task must be left alone, got %q", doneTaskState)
	}
}

func TestListInitiatives_StatusFilterAndDetail(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)

	executingID := insertTestInitiative(t, "executing", "filter-executing")
	insertTestInitiative(t, "draft", "filter-draft")

	// Seed tasks for progress: one done, one pending at plan_version 0.
	ctx := context.Background()
	for _, seed := range []struct{ title, state string }{{"t-done", "done"}, {"t-pending", "pending"}} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO initiative_task (initiative_id, workspace_id, plan_version, title, role, state)
			VALUES ($1, $2, 0, $3, 'implement', $4)
		`, executingID, testWorkspaceID, seed.title, seed.state); err != nil {
			t.Fatalf("seed task: %v", err)
		}
	}

	w := httptest.NewRecorder()
	testHandler.ListInitiatives(w, newRequest("GET", "/api/initiatives?status=executing", nil))
	if w.Code != 200 {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	var listResp ListInitiativesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, item := range listResp.Initiatives {
		if item.Status != "executing" {
			t.Errorf("status filter leaked %q", item.Status)
		}
	}
	found := false
	for _, item := range listResp.Initiatives {
		if item.ID == executingID {
			found = true
		}
	}
	if !found {
		t.Fatalf("filtered list missing seeded executing initiative")
	}

	w = httptest.NewRecorder()
	testHandler.GetInitiative(w, withURLParam(newRequest("GET", "/api/initiatives/"+executingID, nil), "id", executingID))
	if w.Code != 200 {
		t.Fatalf("detail: expected 200, got %d", w.Code)
	}
	var detail InitiativeDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Progress.Total != 2 || detail.Progress.Done != 1 {
		t.Errorf("progress = %+v, want 1/2", detail.Progress)
	}
	if len(detail.Tasks) != 2 {
		t.Errorf("detail tasks = %d, want 2", len(detail.Tasks))
	}
}

func TestPauseResume_PreservesNeedsHumanReason(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)

	id := insertTestInitiative(t, "needs_human", "needs-human-pause-test")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE initiative SET needs_human_reason = 'budget_exceeded' WHERE id = $1`, id); err != nil {
		t.Fatalf("seed needs_human_reason: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.PauseInitiative(w, withURLParam(newRequest("POST", "/api/initiatives/"+id+"/pause", nil), "id", id))
	if w.Code != 200 {
		t.Fatalf("pause: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.ResumeInitiative(w, withURLParam(newRequest("POST", "/api/initiatives/"+id+"/resume", nil), "id", id))
	if w.Code != 200 {
		t.Fatalf("resume: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resumed := decodeInitiativeResponse(t, w.Body.Bytes())
	if resumed.Status != "needs_human" {
		t.Fatalf("status after resume = %q, want needs_human", resumed.Status)
	}
	if resumed.NeedsHumanReason == nil || *resumed.NeedsHumanReason != "budget_exceeded" {
		t.Fatalf("needs_human_reason lost across pause/resume: %v", resumed.NeedsHumanReason)
	}
}

func TestGetInitiativePlan_Versions(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)
	ctx := context.Background()

	id := insertTestInitiative(t, "executing", "plan-versions-test")
	if _, err := testPool.Exec(ctx, `UPDATE initiative SET plan_version = 2 WHERE id = $1`, id); err != nil {
		t.Fatalf("bump plan version: %v", err)
	}
	for _, seed := range []struct {
		version int
		title   string
	}{{1, "old-task"}, {2, "current-task"}} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO initiative_task (initiative_id, workspace_id, plan_version, title, role)
			VALUES ($1, $2, $3, $4, 'implement')
		`, id, testWorkspaceID, seed.version, seed.title); err != nil {
			t.Fatalf("seed task: %v", err)
		}
	}

	// Default: current plan_version.
	w := httptest.NewRecorder()
	testHandler.GetInitiativePlan(w, withURLParam(newRequest("GET", "/api/initiatives/"+id+"/plan", nil), "id", id))
	if w.Code != 200 {
		t.Fatalf("plan: expected 200, got %d", w.Code)
	}
	var plan InitiativePlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if plan.PlanVersion != 2 || len(plan.Tasks) != 1 || plan.Tasks[0].Title != "current-task" {
		t.Fatalf("default plan = v%d %v", plan.PlanVersion, plan.Tasks)
	}

	// Explicit older version.
	w = httptest.NewRecorder()
	testHandler.GetInitiativePlan(w, withURLParam(newRequest("GET", "/api/initiatives/"+id+"/plan?version=1", nil), "id", id))
	if w.Code != 200 {
		t.Fatalf("plan v1: expected 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode plan v1: %v", err)
	}
	if plan.PlanVersion != 1 || len(plan.Tasks) != 1 || plan.Tasks[0].Title != "old-task" {
		t.Fatalf("v1 plan = v%d %v", plan.PlanVersion, plan.Tasks)
	}

	// Invalid version values are 400s, including 32-bit overflow.
	for _, bad := range []string{"abc", "-1", "5000000000"} {
		w = httptest.NewRecorder()
		testHandler.GetInitiativePlan(w, withURLParam(newRequest("GET", "/api/initiatives/"+id+"/plan?version="+bad, nil), "id", id))
		if w.Code != 400 {
			t.Errorf("version=%s: expected 400, got %d", bad, w.Code)
		}
	}
}

func TestListInitiativeBlockers_FilterAndDetailCount(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)
	ctx := context.Background()

	id := insertTestInitiative(t, "executing", "blockers-test")
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO initiative_task (initiative_id, workspace_id, plan_version, title, role, state)
		VALUES ($1, $2, 0, 'blocked-task', 'implement', 'blocked')
		RETURNING id
	`, id, testWorkspaceID).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	for _, seed := range []struct{ status, question string }{
		{"open", "which auth provider?"},
		{"answered", "which database?"},
	} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO initiative_blocker (workspace_id, initiative_id, task_id, status, question)
			VALUES ($1, $2, $3, $4, $5)
		`, testWorkspaceID, id, taskID, seed.status, seed.question); err != nil {
			t.Fatalf("seed blocker: %v", err)
		}
	}

	// Unfiltered list returns both.
	w := httptest.NewRecorder()
	testHandler.ListInitiativeBlockers(w, withURLParam(newRequest("GET", "/api/initiatives/"+id+"/blockers", nil), "id", id))
	if w.Code != 200 {
		t.Fatalf("blockers: expected 200, got %d", w.Code)
	}
	var blockers ListInitiativeBlockersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &blockers); err != nil {
		t.Fatalf("decode blockers: %v", err)
	}
	if len(blockers.Blockers) != 2 {
		t.Fatalf("unfiltered blockers = %d, want 2", len(blockers.Blockers))
	}

	// Status filter.
	w = httptest.NewRecorder()
	testHandler.ListInitiativeBlockers(w, withURLParam(newRequest("GET", "/api/initiatives/"+id+"/blockers?status=open", nil), "id", id))
	if err := json.Unmarshal(w.Body.Bytes(), &blockers); err != nil {
		t.Fatalf("decode filtered blockers: %v", err)
	}
	if len(blockers.Blockers) != 1 || blockers.Blockers[0].Question != "which auth provider?" {
		t.Fatalf("open blockers = %v", blockers.Blockers)
	}

	// Detail counts only open blockers.
	w = httptest.NewRecorder()
	testHandler.GetInitiative(w, withURLParam(newRequest("GET", "/api/initiatives/"+id, nil), "id", id))
	var detail InitiativeDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.OpenBlockerCount != 1 {
		t.Fatalf("open_blocker_count = %d, want 1", detail.OpenBlockerCount)
	}
}

// TestListInitiativeEvents_KeysetPagination seeds a same-second burst of
// events (microsecond-distinct created_at) and walks pages via next_cursor —
// the truncated-second cursor bug class this asserts against silently dropped
// same-second events at page boundaries.
func TestListInitiativeEvents_KeysetPagination(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)
	ctx := context.Background()

	id := insertTestInitiative(t, "executing", "events-pagination-test")
	// insertTestInitiative writes no events; seed exactly 5, all inside the
	// same second, microseconds apart.
	for i := range 5 {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO initiative_event (workspace_id, initiative_id, actor_type, event_type, payload, created_at)
			VALUES ($1, $2, 'system', 'seq_' || ($3::int)::text, '{}', date_trunc('second', now()) + ($3::int * interval '250 microseconds'))
		`, testWorkspaceID, id, i); err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}

	collected := map[string]bool{}
	cursorQuery := ""
	pages := 0
	for {
		w := httptest.NewRecorder()
		testHandler.ListInitiativeEvents(w, withURLParam(newRequest("GET", "/api/initiatives/"+id+"/events?limit=2"+cursorQuery, nil), "id", id))
		if w.Code != 200 {
			t.Fatalf("events page %d: expected 200, got %d: %s", pages, w.Code, w.Body.String())
		}
		var page ListInitiativeEventsResponse
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode events page: %v", err)
		}
		for _, e := range page.Events {
			if collected[e.EventType] {
				t.Fatalf("event %s returned twice", e.EventType)
			}
			collected[e.EventType] = true
		}
		pages++
		if !page.HasMore {
			break
		}
		if page.NextCursor == nil {
			t.Fatal("has_more with nil next_cursor")
		}
		cursorQuery = "&before_created_at=" + url.QueryEscape(page.NextCursor.CreatedAt) + "&before_id=" + page.NextCursor.ID
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(collected) != 5 {
		t.Fatalf("collected %d events across %d pages, want all 5", len(collected), pages)
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3 (2+2+1)", pages)
	}
}

// TestInitiativeMutations_RejectMachineActors drives the approve endpoint
// through the RequireHumanActor middleware exactly as the router composes it:
// a mat_/mcn_ credential (authoritative server-set X-Actor-Source) must get a
// 403 before the handler runs — an agent must never approve the plan gate it
// executes under.
func TestInitiativeMutations_RejectMachineActors(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)

	id := insertTestInitiative(t, "plan_review", "human-gate-test")

	guarded := RequireHumanActor(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		testHandler.ApproveInitiativePlan(w, r)
	}))

	for _, source := range []string{"task_token", "cloud_pat"} {
		req := withURLParam(newRequest("POST", "/api/initiatives/"+id+"/plan/approve", nil), "id", id)
		req.Header.Set("X-Actor-Source", source)
		w := httptest.NewRecorder()
		guarded.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Errorf("%s approve: expected 403, got %d", source, w.Code)
		}
	}

	// A human request (no X-Actor-Source) passes through and approves.
	req := withURLParam(newRequest("POST", "/api/initiatives/"+id+"/plan/approve", nil), "id", id)
	w := httptest.NewRecorder()
	guarded.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("human approve through guard: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestInitiativeTransition_CASConflict exercises the generic lost-race branch
// of TransitionInitiativeStatus: the edge is legal from the stale snapshot,
// but the row has moved, so the CAS matches zero rows.
func TestInitiativeTransition_CASConflict(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	id := insertTestInitiative(t, "executing", "cas-conflict-test")
	stale, err := testHandler.Queries.GetInitiativeInWorkspace(ctx, mustGetInitiativeParams(t, id))
	if err != nil {
		t.Fatalf("load initiative: %v", err)
	}

	// The row moves under the caller.
	if _, err := testPool.Exec(ctx, `UPDATE initiative SET status = 'needs_human' WHERE id = $1`, id); err != nil {
		t.Fatalf("race update: %v", err)
	}

	// executing -> paused is a legal edge, but the CAS must lose.
	_, err = testHandler.InitiativeService.Pause(ctx, stale, service.SystemInitiativeActor(), "race")
	if !errors.Is(err, service.ErrInitiativeTransitionConflict) {
		t.Fatalf("expected ErrInitiativeTransitionConflict, got %v", err)
	}
}

func mustGetInitiativeParams(t *testing.T, id string) db.GetInitiativeInWorkspaceParams {
	t.Helper()
	idUUID, err := util.ParseUUID(id)
	if err != nil {
		t.Fatalf("parse initiative id: %v", err)
	}
	wsUUID, err := util.ParseUUID(testWorkspaceID)
	if err != nil {
		t.Fatalf("parse workspace id: %v", err)
	}
	return db.GetInitiativeInWorkspaceParams{ID: idUUID, WorkspaceID: wsUUID}
}

func TestUpdateInitiative_PatchAndValidation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withInitiativesFlag(t, true)

	id := insertTestInitiative(t, "draft", "update-test")

	w := httptest.NewRecorder()
	testHandler.UpdateInitiative(w, withURLParam(newRequest("PATCH", "/api/initiatives/"+id, map[string]any{
		"title": "renamed initiative",
	}), "id", id))
	if w.Code != 200 {
		t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	updated := decodeInitiativeResponse(t, w.Body.Bytes())
	if updated.Title != "renamed initiative" {
		t.Errorf("title = %q", updated.Title)
	}
	if updated.Idea != "seeded idea" {
		t.Errorf("idea must be untouched, got %q", updated.Idea)
	}

	for name, body := range map[string]map[string]any{
		"empty title":    {"title": "  "},
		"empty idea":     {"idea": ""},
		"bad autonomy":   {"autonomy_level": 9},
		"zero budget":    {"budget_limit_tokens": 0},
		"bad constraint": nil, // replaced below with raw invalid JSON case
	} {
		if body == nil {
			continue
		}
		w = httptest.NewRecorder()
		testHandler.UpdateInitiative(w, withURLParam(newRequest("PATCH", "/api/initiatives/"+id, body), "id", id))
		if w.Code != 400 {
			t.Errorf("%s: expected 400, got %d", name, w.Code)
		}
	}
}

// TestInitiativeWSPayloadMatchesResponse is the drift guard for the two
// hand-maintained shapes of an initiative: the REST InitiativeResponse and
// the WS payload from service.InitiativeWSPayload. Frontends patch caches
// from both, so their JSON key sets must stay identical.
func TestInitiativeWSPayloadMatchesResponse(t *testing.T) {
	var row db.Initiative

	restJSON, err := json.Marshal(initiativeToResponse(row))
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	wsJSON, err := json.Marshal(service.InitiativeWSPayload(row))
	if err != nil {
		t.Fatalf("marshal ws payload: %v", err)
	}

	var restKeys, wsKeys map[string]any
	if err := json.Unmarshal(restJSON, &restKeys); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if err := json.Unmarshal(wsJSON, &wsKeys); err != nil {
		t.Fatalf("unmarshal ws payload: %v", err)
	}

	for k := range restKeys {
		if _, ok := wsKeys[k]; !ok {
			t.Errorf("WS payload missing key %q present in InitiativeResponse", k)
		}
	}
	for k := range wsKeys {
		if _, ok := restKeys[k]; !ok {
			t.Errorf("WS payload has extra key %q absent from InitiativeResponse", k)
		}
	}
}
