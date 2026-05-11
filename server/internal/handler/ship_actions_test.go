package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
)

// fakeShipGithub is the local mock used by the Phase 3 action tests.
// Each per-action test wires the field it cares about; the rest stay
// nil and the embedded defaults return success-shaped values.
type fakeShipGithub struct {
	listFn          func(ctx context.Context, owner, repo string, opts gh.ListOptions) ([]gh.PullRequest, error)
	mergeFn         func(ctx context.Context, owner, repo string, prNumber int, method, sha string) (*gh.MergeResult, error)
	updateBranchFn  func(ctx context.Context, owner, repo string, prNumber int, expectedSHA string) error
	createCommentFn func(ctx context.Context, owner, repo string, prNumber int, body string) (*gh.Comment, error)
	dismissReviewFn func(ctx context.Context, owner, repo string, prNumber int, reviewID int64, message string) error
	closePRFn       func(ctx context.Context, owner, repo string, prNumber int) error
	dispatchFn      func(ctx context.Context, owner, repo, workflowFile, ref string, inputs map[string]string) error
	submitReviewFn  func(ctx context.Context, owner, repo string, prNumber int, event gh.ReviewEvent, body string) (*gh.Review, error)
}

func (f *fakeShipGithub) ListPullRequests(ctx context.Context, owner, repo string, opts gh.ListOptions) ([]gh.PullRequest, error) {
	if f.listFn != nil {
		return f.listFn(ctx, owner, repo, opts)
	}
	return nil, nil
}
func (f *fakeShipGithub) MergePullRequest(ctx context.Context, owner, repo string, prNumber int, method, sha string) (*gh.MergeResult, error) {
	if f.mergeFn != nil {
		return f.mergeFn(ctx, owner, repo, prNumber, method, sha)
	}
	return &gh.MergeResult{SHA: "deadbeef", Merged: true, Message: "ok"}, nil
}
func (f *fakeShipGithub) UpdatePullRequestBranch(ctx context.Context, owner, repo string, prNumber int, expectedSHA string) error {
	if f.updateBranchFn != nil {
		return f.updateBranchFn(ctx, owner, repo, prNumber, expectedSHA)
	}
	return nil
}
func (f *fakeShipGithub) CreatePullRequestComment(ctx context.Context, owner, repo string, prNumber int, body string) (*gh.Comment, error) {
	if f.createCommentFn != nil {
		return f.createCommentFn(ctx, owner, repo, prNumber, body)
	}
	return &gh.Comment{ID: 99, HTMLURL: "https://example.com/c/99", Body: body}, nil
}
func (f *fakeShipGithub) DismissPullRequestReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, message string) error {
	if f.dismissReviewFn != nil {
		return f.dismissReviewFn(ctx, owner, repo, prNumber, reviewID, message)
	}
	return nil
}
func (f *fakeShipGithub) ClosePullRequest(ctx context.Context, owner, repo string, prNumber int) error {
	if f.closePRFn != nil {
		return f.closePRFn(ctx, owner, repo, prNumber)
	}
	return nil
}
func (f *fakeShipGithub) DispatchWorkflow(ctx context.Context, owner, repo, workflowFile, ref string, inputs map[string]string) error {
	if f.dispatchFn != nil {
		return f.dispatchFn(ctx, owner, repo, workflowFile, ref, inputs)
	}
	return nil
}
func (f *fakeShipGithub) ListPullRequestFiles(_ context.Context, _, _ string, _ int) ([]gh.PullRequestFile, error) {
	// Phase 5 — chip handler tests don't drive the risk classifier; return
	// nil so the classifier degrades to its title-only path.
	return nil, nil
}
func (f *fakeShipGithub) SubmitReview(ctx context.Context, owner, repo string, prNumber int, event gh.ReviewEvent, body string) (*gh.Review, error) {
	if f.submitReviewFn != nil {
		return f.submitReviewFn(ctx, owner, repo, prNumber, event, body)
	}
	return &gh.Review{ID: 700, HTMLURL: "https://example.com/r/700", State: string(event), Body: body}, nil
}

// fakeShipTaskEnqueuer captures spawn calls so tests can assert on the
// payload without standing up a real TaskService.
type fakeShipTaskEnqueuer struct {
	calls   []ship.ShipCardActionTaskRequest
	taskID  pgtype.UUID
	taskErr error
}

func (f *fakeShipTaskEnqueuer) EnqueueShipCardActionTask(ctx context.Context, p ship.ShipCardActionTaskRequest) (db.AgentTaskQueue, error) {
	f.calls = append(f.calls, p)
	if f.taskErr != nil {
		return db.AgentTaskQueue{}, f.taskErr
	}
	return db.AgentTaskQueue{ID: f.taskID, AgentID: p.AgentID}, nil
}

// shipPhase3MigrationApplied probes for the ship_card_action table so
// the harness can skip Phase 3 tests when running against a checkout
// that hasn't migrated yet.
func shipPhase3MigrationApplied(t *testing.T) bool {
	t.Helper()
	var exists bool
	err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'ship_card_action'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("probe ship_card_action: %v", err)
	}
	return exists
}

// seedPRForActions creates a project + PR row and returns both ids. The
// caller's t.Cleanup reaps via createShipProject's existing cleanup.
func seedPRForActions(t *testing.T, repoURL string, prNumber int) (projectID, prID string) {
	t.Helper()
	projectID = createShipProject(t, repoURL)
	mustSeedPR(t, projectID, repoURL, prNumber, "open")
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM pull_request WHERE workspace_id = $1 AND repo_url = $2 AND pr_number = $3`,
		testWorkspaceID, repoURL, prNumber).Scan(&prID); err != nil {
		t.Fatalf("look up seeded PR: %v", err)
	}
	return projectID, prID
}

// runAction invokes svc.ExecuteAction on a freshly-loaded PR row. Tests
// pass the per-action body and expect to assert on the ActionResult and
// the rows in ship_card_action.
func runAction(t *testing.T, svc *ship.Service, prID string, actorUserID pgtype.UUID, action string, body any, enqueuer ship.TaskEnqueuer, orchestrator pgtype.UUID, smokeWorkflow string) (*ship.ActionResult, error) {
	t.Helper()
	prUUID := parseUUID(prID)
	pr, err := svc.Q.GetPullRequest(context.Background(), prUUID)
	if err != nil {
		t.Fatalf("get pr: %v", err)
	}
	wsID := parseUUID(testWorkspaceID)
	var raw json.RawMessage
	if body != nil {
		b, _ := json.Marshal(body)
		raw = b
	}
	return svc.ExecuteAction(context.Background(), wsID, pr, actorUserID, action, raw, enqueuer, orchestrator, smokeWorkflow)
}

// rowCount returns the count of ship_card_action rows for the test PR.
// Lets each test prove the audit trail was written.
func actionRowCount(t *testing.T, prID, status string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM ship_card_action WHERE pull_request_id = $1 AND result_status = $2`,
		prID, status).Scan(&n); err != nil {
		t.Fatalf("count actions: %v", err)
	}
	return n
}

// TestActions_Merge_HappyPath — the full happy path: 200 response, a
// "succeeded" audit row, and the local PR row promoted to "merged".
func TestActions_Merge_HappyPath(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 101)

	gh := &fakeShipGithub{}
	svc := &ship.Service{Q: testHandler.Queries, Github: gh}

	res, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionMerge, map[string]string{"method": "squash"}, nil, pgtype.UUID{}, "")
	if err != nil {
		t.Fatalf("ExecuteAction: %v", err)
	}
	if res.Status != ship.StatusSucceeded || res.MergeSHA != "deadbeef" {
		t.Fatalf("result: %+v", res)
	}
	if actionRowCount(t, prID, ship.StatusSucceeded) != 1 {
		t.Fatalf("expected one succeeded audit row")
	}
	// PR promoted to merged.
	var state string
	testPool.QueryRow(context.Background(), `SELECT state FROM pull_request WHERE id = $1`, prID).Scan(&state)
	if state != "merged" {
		t.Fatalf("PR state should be merged, got %q", state)
	}
}

// TestActions_Merge_NotMergeable — 422 from GitHub bubbles up as
// ErrUnprocessable; the audit row is recorded as failed.
func TestActions_Merge_NotMergeable(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 102)

	ghClient := &fakeShipGithub{
		mergeFn: func(ctx context.Context, owner, repo string, prNumber int, method, sha string) (*gh.MergeResult, error) {
			return nil, gh.ErrUnprocessable
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionMerge, map[string]string{"method": "merge"}, nil, pgtype.UUID{}, "")
	if !errors.Is(err, gh.ErrUnprocessable) {
		t.Fatalf("expected ErrUnprocessable, got %v", err)
	}
	if actionRowCount(t, prID, ship.StatusFailed) != 1 {
		t.Fatalf("expected one failed audit row")
	}
}

// TestActions_Merge_TokenRevoked — 401 from GitHub maps to ErrUnauthorized.
// The handler-layer error mapping (mapActionError) translates this to 401
// to the client.
func TestActions_Merge_TokenRevoked(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 103)

	ghClient := &fakeShipGithub{
		mergeFn: func(ctx context.Context, owner, repo string, prNumber int, method, sha string) (*gh.MergeResult, error) {
			return nil, gh.ErrUnauthorized
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionMerge, map[string]string{"method": "merge"}, nil, pgtype.UUID{}, "")
	if !errors.Is(err, gh.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if mapActionError(err) != http.StatusUnauthorized {
		t.Fatalf("expected 401 from mapper, got %d", mapActionError(err))
	}
}

// TestActions_Comment_RequiresBody — empty / missing body yields
// ErrInvalidPayload (which the handler maps to 400).
func TestActions_Comment_RequiresBody(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 104)

	svc := &ship.Service{Q: testHandler.Queries, Github: &fakeShipGithub{}}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionComment, map[string]string{"body": "  "}, nil, pgtype.UUID{}, "")
	if !errors.Is(err, ship.ErrInvalidPayload) {
		t.Fatalf("expected ErrInvalidPayload, got %v", err)
	}
}

// TestActions_Comment_HappyPath — the comment chip returns the GitHub
// comment payload so the frontend can render an inline preview.
func TestActions_Comment_HappyPath(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 105)

	var bodySeen string
	ghClient := &fakeShipGithub{
		createCommentFn: func(ctx context.Context, owner, repo string, prNumber int, body string) (*gh.Comment, error) {
			bodySeen = body
			return &gh.Comment{ID: 555, HTMLURL: "https://github.com/c/555", Body: body}, nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	res, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionComment, map[string]string{"body": "looks good"}, nil, pgtype.UUID{}, "")
	if err != nil {
		t.Fatalf("ExecuteAction: %v", err)
	}
	if res.Comment == nil || res.Comment.ID != 555 || res.Comment.Body != "looks good" {
		t.Fatalf("comment: %+v", res.Comment)
	}
	if bodySeen != "looks good" {
		t.Fatalf("body forwarded: got %q", bodySeen)
	}
}

// TestActions_DismissReview_RequiresFields — review_id and message are
// both required.
func TestActions_DismissReview_RequiresFields(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 106)
	svc := &ship.Service{Q: testHandler.Queries, Github: &fakeShipGithub{}}

	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionDismissReview, map[string]any{"message": "stale"}, nil, pgtype.UUID{}, "")
	if !errors.Is(err, ship.ErrInvalidPayload) {
		t.Fatalf("missing review_id: expected ErrInvalidPayload, got %v", err)
	}
	_, err = runAction(t, svc, prID, parseUUID(testUserID), ship.ActionDismissReview, map[string]any{"review_id": 1, "message": " "}, nil, pgtype.UUID{}, "")
	if !errors.Is(err, ship.ErrInvalidPayload) {
		t.Fatalf("missing message: expected ErrInvalidPayload, got %v", err)
	}
}

// TestActions_RebaseOnMain_HappyPath — Update Branch is the available
// primitive; success records "succeeded" with strategy=update-branch.
func TestActions_RebaseOnMain_HappyPath(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 107)
	svc := &ship.Service{Q: testHandler.Queries, Github: &fakeShipGithub{}}

	res, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionRebaseOnMain, nil, nil, pgtype.UUID{}, "")
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if res.Status != ship.StatusSucceeded {
		t.Fatalf("status: %s", res.Status)
	}
}

// TestActions_RebaseOnMain_AlreadyUpToDate — 409 from GitHub becomes
// "succeeded" + a hint message rather than a failure.
func TestActions_RebaseOnMain_AlreadyUpToDate(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 108)
	ghClient := &fakeShipGithub{
		updateBranchFn: func(ctx context.Context, owner, repo string, prNumber int, expectedSHA string) error {
			return gh.ErrConflict
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}

	res, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionRebaseOnMain, nil, nil, pgtype.UUID{}, "")
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if res.Status != ship.StatusSucceeded {
		t.Fatalf("status: %s", res.Status)
	}
	if !strings.Contains(res.Error, "up to date") {
		t.Fatalf("expected up-to-date hint, got %q", res.Error)
	}
}

// TestActions_RunSmokeTests_RequiresWorkflow — workspaces without a
// configured smoke workflow get ErrSmokeWorkflowNotConfigured (mapped
// to 400 by the handler).
func TestActions_RunSmokeTests_RequiresWorkflow(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 109)
	svc := &ship.Service{Q: testHandler.Queries, Github: &fakeShipGithub{}}

	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionRunSmokeTests, map[string]string{"environment_id": "env-1"}, nil, pgtype.UUID{}, "")
	if !errors.Is(err, ship.ErrSmokeWorkflowNotConfigured) {
		t.Fatalf("expected ErrSmokeWorkflowNotConfigured, got %v", err)
	}
}

// TestActions_RunSmokeTests_HappyPath — workflow_dispatch fires with
// the PR's head_ref + head_sha + environment_id.
func TestActions_RunSmokeTests_HappyPath(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 110)

	var dispatched struct {
		ref      string
		workflow string
		envID    string
	}
	ghClient := &fakeShipGithub{
		dispatchFn: func(ctx context.Context, owner, repo, workflowFile, ref string, inputs map[string]string) error {
			dispatched.ref = ref
			dispatched.workflow = workflowFile
			dispatched.envID = inputs["environment_id"]
			return nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionRunSmokeTests, map[string]string{"environment_id": "env-1"}, nil, pgtype.UUID{}, "smoke.yml")
	if err != nil {
		t.Fatalf("smoke: %v", err)
	}
	if dispatched.workflow != "smoke.yml" || dispatched.envID != "env-1" {
		t.Fatalf("dispatched: %+v", dispatched)
	}
}

// TestActions_CloseAsStale — comment + close in sequence; PR row gets
// state=closed.
func TestActions_CloseAsStale(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 111)

	commented := false
	closed := false
	ghClient := &fakeShipGithub{
		createCommentFn: func(ctx context.Context, owner, repo string, prNumber int, body string) (*gh.Comment, error) {
			commented = true
			if !strings.Contains(body, "stale") {
				t.Errorf("default reason missing 'stale': %q", body)
			}
			return &gh.Comment{ID: 1, Body: body}, nil
		},
		closePRFn: func(ctx context.Context, owner, repo string, prNumber int) error {
			closed = true
			return nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionCloseAsStale, nil, nil, pgtype.UUID{}, "")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if !commented || !closed {
		t.Fatalf("commented=%v closed=%v", commented, closed)
	}
	var state string
	testPool.QueryRow(context.Background(), `SELECT state FROM pull_request WHERE id = $1`, prID).Scan(&state)
	if state != "closed" {
		t.Fatalf("expected state=closed, got %q", state)
	}
}

// TestActions_NudgeAuthor_DefaultMessage — no message in body uses the
// default polite nudge.
func TestActions_NudgeAuthor_DefaultMessage(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 112)

	var sent string
	ghClient := &fakeShipGithub{
		createCommentFn: func(ctx context.Context, owner, repo string, prNumber int, body string) (*gh.Comment, error) {
			sent = body
			return &gh.Comment{ID: 1, Body: body}, nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionNudgeAuthor, nil, nil, pgtype.UUID{}, "")
	if err != nil {
		t.Fatalf("nudge: %v", err)
	}
	if !strings.Contains(sent, "@alice") {
		t.Fatalf("default nudge should mention @author: got %q", sent)
	}
}

// TestActions_DiagnoseCIFailure_SpawnsTask — async chip enqueues a task
// on the orchestrator, returns in_progress + agent_task_id, and leaves
// the audit row in_progress (completed_at NULL).
func TestActions_DiagnoseCIFailure_SpawnsTask(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 113)

	taskID := pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true}
	enqueuer := &fakeShipTaskEnqueuer{taskID: taskID}
	orchestratorAgent := pgtype.UUID{Bytes: [16]byte{9, 9, 9}, Valid: true}

	svc := &ship.Service{Q: testHandler.Queries, Github: &fakeShipGithub{}}
	res, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionDiagnoseCIFailure, nil, enqueuer, orchestratorAgent, "")
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if res.Status != ship.StatusInProgress {
		t.Fatalf("expected in_progress, got %s", res.Status)
	}
	if res.AgentTaskID == nil || *res.AgentTaskID == "" {
		t.Fatalf("expected agent_task_id, got %+v", res)
	}
	if len(enqueuer.calls) != 1 {
		t.Fatalf("expected 1 enqueue, got %d", len(enqueuer.calls))
	}
	call := enqueuer.calls[0]
	if call.Action != ship.ActionDiagnoseCIFailure || call.PRNumber != 113 {
		t.Fatalf("enqueue call: %+v", call)
	}
	// completed_at should be NULL for in_progress rows.
	var completedAt *string
	testPool.QueryRow(context.Background(),
		`SELECT completed_at::text FROM ship_card_action WHERE pull_request_id = $1`, prID).Scan(&completedAt)
	if completedAt != nil {
		t.Fatalf("completed_at should be NULL for in_progress row, got %v", completedAt)
	}
}

// TestActions_DiagnoseCIFailure_NoOrchestrator — workspaces without an
// orchestrator agent get a structured failure (mapped to 500 by the
// handler since there's no canonical 4xx for "configuration missing").
func TestActions_DiagnoseCIFailure_NoOrchestrator(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 114)

	enqueuer := &fakeShipTaskEnqueuer{}
	svc := &ship.Service{Q: testHandler.Queries, Github: &fakeShipGithub{}}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionDiagnoseCIFailure, nil, enqueuer, pgtype.UUID{}, "")
	if err == nil {
		t.Fatalf("expected error when orchestrator agent missing")
	}
}

// TestActions_UnknownAction — bogus action name returns ErrActionUnknown.
func TestActions_UnknownAction(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 115)
	svc := &ship.Service{Q: testHandler.Queries, Github: &fakeShipGithub{}}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), "delete_universe", nil, nil, pgtype.UUID{}, "")
	if !errors.Is(err, ship.ErrActionUnknown) {
		t.Fatalf("expected ErrActionUnknown, got %v", err)
	}
	if mapActionError(err) != http.StatusBadRequest {
		t.Fatalf("expected 400 from mapper, got %d", mapActionError(err))
	}
}

// TestShip_MergeEndpoint_RequiresShipHubEnabled — without the workspace
// flag the chip endpoint returns 404 (matching every other Ship Hub
// endpoint).
func TestShip_MergeEndpoint_RequiresShipHubEnabled(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/pull_requests/abc/merge", map[string]string{"method": "merge"})
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
	testHandler.MergePullRequest(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (ship hub off), got %d", w.Code)
	}
}

// TestShip_MergeEndpoint_BadID — invalid UUID returns 400.
func TestShip_MergeEndpoint_BadID(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/pull_requests/garbage/merge", map[string]string{"method": "merge"})
	req = withURLParam(req, "id", "garbage")
	testHandler.MergePullRequest(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (bad uuid), got %d", w.Code)
	}
}

// TestActions_SubmitReview_Approve_HappyPath — APPROVE with empty body
// succeeds; the review is recorded and the result carries the review URL.
func TestActions_SubmitReview_Approve_HappyPath(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 200)

	var sawEvent gh.ReviewEvent
	var sawBody string
	ghClient := &fakeShipGithub{
		submitReviewFn: func(ctx context.Context, owner, repo string, prNumber int, event gh.ReviewEvent, body string) (*gh.Review, error) {
			sawEvent = event
			sawBody = body
			return &gh.Review{ID: 9001, HTMLURL: "https://github.com/.../#review-9001", State: "APPROVED"}, nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	res, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionSubmitReview, map[string]string{"event": "APPROVE"}, nil, pgtype.UUID{}, "")
	if err != nil {
		t.Fatalf("submit_review: %v", err)
	}
	if res.Status != ship.StatusSucceeded {
		t.Fatalf("status: %s", res.Status)
	}
	if res.Review == nil || res.Review.ID != 9001 {
		t.Fatalf("review missing/wrong: %+v", res.Review)
	}
	if sawEvent != gh.ReviewEventApprove || sawBody != "" {
		t.Fatalf("upstream call: event=%s body=%q", sawEvent, sawBody)
	}
	if actionRowCount(t, prID, ship.StatusSucceeded) != 1 {
		t.Fatalf("expected one succeeded audit row")
	}
}

// TestActions_SubmitReview_Comment_RequiresBody — COMMENT with empty body
// is rejected at the service layer (mapped to 400 by the handler) so the
// user sees a clean error rather than GitHub's 422 forwarded raw.
func TestActions_SubmitReview_Comment_RequiresBody(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 201)

	svc := &ship.Service{Q: testHandler.Queries, Github: &fakeShipGithub{}}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionSubmitReview, map[string]string{"event": "COMMENT", "body": "  "}, nil, pgtype.UUID{}, "")
	if !errors.Is(err, ship.ErrInvalidPayload) {
		t.Fatalf("expected ErrInvalidPayload, got %v", err)
	}
	if mapActionError(err) != http.StatusBadRequest {
		t.Fatalf("expected 400 from mapper, got %d", mapActionError(err))
	}
}

// TestActions_SubmitReview_RequestChanges_RequiresBody — REQUEST_CHANGES
// with empty body is rejected at the service layer.
func TestActions_SubmitReview_RequestChanges_RequiresBody(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 202)

	svc := &ship.Service{Q: testHandler.Queries, Github: &fakeShipGithub{}}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionSubmitReview, map[string]string{"event": "REQUEST_CHANGES", "body": ""}, nil, pgtype.UUID{}, "")
	if !errors.Is(err, ship.ErrInvalidPayload) {
		t.Fatalf("expected ErrInvalidPayload, got %v", err)
	}
}

// TestActions_SubmitReview_InvalidEvent — anything outside the three-value
// enum is a 400 (drift from a future GitHub event must not crash).
func TestActions_SubmitReview_InvalidEvent(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 203)

	svc := &ship.Service{Q: testHandler.Queries, Github: &fakeShipGithub{}}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionSubmitReview, map[string]string{"event": "DELETE", "body": "x"}, nil, pgtype.UUID{}, "")
	if !errors.Is(err, ship.ErrInvalidPayload) {
		t.Fatalf("expected ErrInvalidPayload, got %v", err)
	}
}

// TestActions_SubmitReview_PostsToConversationChannel — when the PR has a
// conversation_channel_id set AND the service has PostToPRChannel wired,
// the review submission triggers a status post in the channel. The
// channel post is best-effort; a failure must not fail the review.
func TestActions_SubmitReview_PostsToConversationChannel(t *testing.T) {
	if !shipPhase3MigrationApplied(t) {
		t.Skip("phase 3 migration not applied")
	}
	enableShipHub(t, true)
	_, prID := seedPRForActions(t, "https://github.com/multica-ai/multica", 204)

	// Attach a real channel id to the PR row so the service hook
	// fires. Real flow uses GetOrCreatePRConversationChannel; we
	// shortcut by inserting a minimal channel row, since the
	// pull_request.conversation_channel_id column has an FK on
	// channel.id and rejects synthetic UUIDs.
	var chanID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO channel (
			workspace_id, name, kind, visibility,
			created_by_type, created_by_id
		)
		VALUES ($1, $2, 'channel', 'public', 'system', gen_random_uuid())
		RETURNING id`,
		testWorkspaceID, "review-channel-"+t.Name()).Scan(&chanID); err != nil {
		t.Fatalf("insert conversation channel: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`UPDATE pull_request SET conversation_channel_id = $1 WHERE id = $2`,
		chanID, prID); err != nil {
		t.Fatalf("attach channel id: %v", err)
	}

	var posted []string
	svc := &ship.Service{
		Q:      testHandler.Queries,
		Github: &fakeShipGithub{},
		PostToPRChannel: func(ctx context.Context, channelID pgtype.UUID, content string) error {
			posted = append(posted, content)
			return nil
		},
	}
	_, err := runAction(t, svc, prID, parseUUID(testUserID), ship.ActionSubmitReview, map[string]string{"event": "APPROVE", "body": "LGTM"}, nil, pgtype.UUID{}, "")
	if err != nil {
		t.Fatalf("submit_review: %v", err)
	}
	if len(posted) != 1 {
		t.Fatalf("expected 1 channel post, got %d", len(posted))
	}
	if !strings.Contains(posted[0], "Approved") || !strings.Contains(posted[0], "LGTM") {
		t.Fatalf("channel post content: %q", posted[0])
	}
}
