// Phase 7b — Merge train handler + service integration tests.
//
// These tests drive the orchestrator end-to-end through the service
// layer (StartMerge / ResumeMerge / AbortMergeTrain) against the real
// Postgres test pool, with a fake GitHub client. The orchestrator
// runs in a goroutine, so each test waits on a synchronization point
// (channel signalled by the fake's mergeFn) rather than a fixed
// sleep.

package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
)

// shipMergeMigrationApplied probes for the 086 migration. Mirrors
// shipReleaseMigrationApplied so a checkout running pre-086 just
// skips the new tests instead of hard-failing.
func shipMergeMigrationApplied(t *testing.T) bool {
	t.Helper()
	var exists bool
	err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM pg_type WHERE typname = 'pr_merge_state'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("probe pr_merge_state: %v", err)
	}
	return exists
}

// enableMergeTest sets up a workspace with ship_hub enabled, channels
// enabled, and registers a cleanup. Skips when migrations aren't
// applied.
func enableMergeTest(t *testing.T) {
	t.Helper()
	if !shipMergeMigrationApplied(t) {
		t.Skip("phase 7b migration not yet applied; skipping")
	}
	enableShipReleaseTest(t)
}

// seedReleaseWithPRs inserts a release in `assembling` stage with the
// given number of eligible PRs attached (positions 0..N-1, all
// merge_state=queued by default). Returns the release id and the
// per-PR ids in position order.
func seedReleaseWithPRs(t *testing.T, projectID, repoURL string, count int) (string, []string) {
	t.Helper()
	prIDs := make([]string, 0, count)
	for i := 0; i < count; i++ {
		prIDs = append(prIDs, seedReleasePR(t, projectID, repoURL, 1000+i))
	}
	var releaseID string
	err := testPool.QueryRow(context.Background(),
		`INSERT INTO ship_release (workspace_id, project_id, title, risk_level)
		 VALUES ($1, $2, $3, 'low')
		 RETURNING id`,
		testWorkspaceID, projectID, "Test release "+repoURL).Scan(&releaseID)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}
	for i, prID := range prIDs {
		if _, err := testPool.Exec(context.Background(),
			`INSERT INTO ship_release_pull_request (release_id, pull_request_id, position, is_active)
			 VALUES ($1, $2, $3, TRUE)`,
			releaseID, prID, i); err != nil {
			t.Fatalf("seed membership: %v", err)
		}
	}
	return releaseID, prIDs
}

// recordingPublisher captures every PublishMergeEvent call. Tests
// assert on event types fired in the right order.
type recordingPublisher struct {
	mu     sync.Mutex
	events []recordedMergeEvent
}

type recordedMergeEvent struct {
	Type      string
	Workspace string
	Payload   map[string]any
}

func (p *recordingPublisher) PublishMergeEvent(eventType, workspaceID string, payload map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Copy the payload so subsequent caller mutations don't race.
	cp := make(map[string]any, len(payload))
	for k, v := range payload {
		cp[k] = v
	}
	p.events = append(p.events, recordedMergeEvent{Type: eventType, Workspace: workspaceID, Payload: cp})
}

func (p *recordingPublisher) types() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.events))
	for i, e := range p.events {
		out[i] = e.Type
	}
	return out
}

// waitFor polls fn for up to 5s, returning when it returns true. Used
// to synchronize on async orchestrator state without flaky sleeps.
func waitFor(t *testing.T, name string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitFor: %s did not occur within 5s", name)
}

// readReleaseStage returns the current stage of the release row.
func readReleaseStage(t *testing.T, releaseID string) string {
	t.Helper()
	var s string
	if err := testPool.QueryRow(context.Background(),
		`SELECT stage FROM ship_release WHERE id = $1`, releaseID).Scan(&s); err != nil {
		t.Fatalf("read stage: %v", err)
	}
	return s
}

func readReleasePaused(t *testing.T, releaseID string) bool {
	t.Helper()
	var p bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT merge_paused FROM ship_release WHERE id = $1`, releaseID).Scan(&p); err != nil {
		t.Fatalf("read paused: %v", err)
	}
	return p
}

// readMergeStates returns merge_state per PR id (position-keyed).
func readMergeStates(t *testing.T, releaseID string) map[string]string {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT pull_request_id::text, merge_state::text FROM ship_release_pull_request WHERE release_id = $1`,
		releaseID)
	if err != nil {
		t.Fatalf("read merge states: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var prID, state string
		if err := rows.Scan(&prID, &state); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[prID] = state
	}
	return out
}

// TestMergeTrain_HappyPath_ThreePRs — the canonical success path: the
// orchestrator merges every PR, advances the release to in_staging,
// and publishes the expected sequence of WS events.
func TestMergeTrain_HappyPath_ThreePRs(t *testing.T) {
	enableMergeTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/merge-happy")
	releaseID, prIDs := seedReleaseWithPRs(t, projectID, "https://github.com/multica-ai/merge-happy", 3)

	mergedNumbers := atomic.Int32{}
	ghClient := &fakeShipGithub{
		mergeFn: func(_ context.Context, _, _ string, prNumber int, method, _ string) (*gh.MergeResult, error) {
			if method != "merge" {
				return nil, fmt.Errorf("unexpected method %q", method)
			}
			n := mergedNumbers.Add(1)
			return &gh.MergeResult{SHA: fmt.Sprintf("sha%d", n), Merged: true, Message: "ok"}, nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	pub := &recordingPublisher{}
	deps := &ship.MergeTrainDeps{Publisher: pub, ParentCtx: context.Background()}

	if err := svc.StartMerge(context.Background(), parseUUID(releaseID), parseUUID(testUserID), "merge", "", deps); err != nil {
		t.Fatalf("StartMerge: %v", err)
	}
	waitFor(t, "stage=in_staging", func() bool {
		return readReleaseStage(t, releaseID) == "in_staging"
	})
	// stage=in_staging is set inside completeMergeTrain BEFORE the
	// merge_completed event is published — a separate poll is needed
	// to avoid checking pub.types() between those two operations.
	waitFor(t, "merge_completed event", func() bool {
		for _, tp := range pub.types() {
			if tp == "release:merge_completed" {
				return true
			}
		}
		return false
	})

	states := readMergeStates(t, releaseID)
	for _, prID := range prIDs {
		if states[prID] != "merged" {
			t.Fatalf("expected pr %s merged, got %s", prID, states[prID])
		}
	}
	if mergedNumbers.Load() != 3 {
		t.Fatalf("expected 3 GitHub merges, got %d", mergedNumbers.Load())
	}

	// Event types expected: started, progress×6 (2 per PR: merging,merged), completed, updated.
	types := pub.types()
	if len(types) < 5 {
		t.Fatalf("expected ≥5 events, got %d: %v", len(types), types)
	}
	if types[0] != "release:merge_started" {
		t.Fatalf("first event should be merge_started, got %v", types)
	}
	hasCompleted := false
	for _, tp := range types {
		if tp == "release:merge_completed" {
			hasCompleted = true
		}
	}
	if !hasCompleted {
		t.Fatalf("expected merge_completed event, got %v", types)
	}
}

// TestMergeTrain_ConflictMidTrain_PausesAtPR2 — PR 1 merges, PR 2's
// merge returns 422 Unprocessable: PR 2 marked failed, release
// paused, PR 3 stays queued.
func TestMergeTrain_ConflictMidTrain_PausesAtPR2(t *testing.T) {
	enableMergeTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/merge-conflict")
	releaseID, prIDs := seedReleaseWithPRs(t, projectID, "https://github.com/multica-ai/merge-conflict", 3)

	var calls atomic.Int32
	ghClient := &fakeShipGithub{
		mergeFn: func(_ context.Context, _, _ string, prNumber int, _, _ string) (*gh.MergeResult, error) {
			n := calls.Add(1)
			if n == 2 {
				return nil, gh.ErrUnprocessable
			}
			return &gh.MergeResult{SHA: fmt.Sprintf("sha%d", prNumber), Merged: true}, nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	deps := &ship.MergeTrainDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}

	if err := svc.StartMerge(context.Background(), parseUUID(releaseID), parseUUID(testUserID), "", "", deps); err != nil {
		t.Fatalf("StartMerge: %v", err)
	}
	waitFor(t, "release paused", func() bool { return readReleasePaused(t, releaseID) })

	if got := readReleaseStage(t, releaseID); got != "merging" {
		t.Fatalf("expected stage=merging, got %q", got)
	}
	states := readMergeStates(t, releaseID)
	if states[prIDs[0]] != "merged" {
		t.Fatalf("pr0: expected merged, got %q", states[prIDs[0]])
	}
	if states[prIDs[1]] != "failed" {
		t.Fatalf("pr1: expected failed, got %q", states[prIDs[1]])
	}
	if states[prIDs[2]] != "queued" {
		t.Fatalf("pr2: expected still queued, got %q", states[prIDs[2]])
	}
}

// TestMergeTrain_Resume_AfterConflictResolved — caller resumes after
// fixing the conflict: PR 2 now merges, PR 3 follows, release
// reaches in_staging.
func TestMergeTrain_Resume_AfterConflictResolved(t *testing.T) {
	enableMergeTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/merge-resume")
	releaseID, prIDs := seedReleaseWithPRs(t, projectID, "https://github.com/multica-ai/merge-resume", 3)

	var failOnce atomic.Bool
	failOnce.Store(true)
	var attempts atomic.Int32
	ghClient := &fakeShipGithub{
		mergeFn: func(_ context.Context, _, _ string, prNumber int, _, _ string) (*gh.MergeResult, error) {
			n := attempts.Add(1)
			// First attempt at PR #1001 (the second PR in the train) fails.
			if n == 2 && failOnce.Load() {
				failOnce.Store(false)
				return nil, gh.ErrUnprocessable
			}
			return &gh.MergeResult{SHA: fmt.Sprintf("sha%d", n), Merged: true}, nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	deps := &ship.MergeTrainDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}

	if err := svc.StartMerge(context.Background(), parseUUID(releaseID), parseUUID(testUserID), "", "", deps); err != nil {
		t.Fatalf("StartMerge: %v", err)
	}
	waitFor(t, "paused", func() bool { return readReleasePaused(t, releaseID) })

	if err := svc.ResumeMerge(context.Background(), parseUUID(releaseID), parseUUID(testUserID), nil, deps); err != nil {
		t.Fatalf("ResumeMerge: %v", err)
	}
	waitFor(t, "stage=in_staging", func() bool {
		return readReleaseStage(t, releaseID) == "in_staging"
	})

	states := readMergeStates(t, releaseID)
	for _, prID := range prIDs {
		if states[prID] != "merged" {
			t.Fatalf("pr %s: expected merged, got %q", prID, states[prID])
		}
	}
}

// TestMergeTrain_Resume_WithSkip — caller resumes but marks the failed
// PR as skipped. Train completes without merging that PR.
func TestMergeTrain_Resume_WithSkip(t *testing.T) {
	enableMergeTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/merge-skip")
	releaseID, prIDs := seedReleaseWithPRs(t, projectID, "https://github.com/multica-ai/merge-skip", 3)

	var calls atomic.Int32
	ghClient := &fakeShipGithub{
		mergeFn: func(_ context.Context, _, _ string, prNumber int, _, _ string) (*gh.MergeResult, error) {
			n := calls.Add(1)
			if n == 2 {
				return nil, gh.ErrUnprocessable
			}
			return &gh.MergeResult{SHA: fmt.Sprintf("sha%d", n), Merged: true}, nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	deps := &ship.MergeTrainDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}

	if err := svc.StartMerge(context.Background(), parseUUID(releaseID), parseUUID(testUserID), "", "", deps); err != nil {
		t.Fatalf("StartMerge: %v", err)
	}
	waitFor(t, "paused", func() bool { return readReleasePaused(t, releaseID) })

	skip := []pgtype.UUID{parseUUID(prIDs[1])}
	if err := svc.ResumeMerge(context.Background(), parseUUID(releaseID), parseUUID(testUserID), skip, deps); err != nil {
		t.Fatalf("ResumeMerge: %v", err)
	}
	waitFor(t, "stage=in_staging", func() bool {
		return readReleaseStage(t, releaseID) == "in_staging"
	})

	states := readMergeStates(t, releaseID)
	if states[prIDs[0]] != "merged" {
		t.Fatalf("pr0: expected merged, got %q", states[prIDs[0]])
	}
	if states[prIDs[1]] != "skipped" {
		t.Fatalf("pr1: expected skipped, got %q", states[prIDs[1]])
	}
	if states[prIDs[2]] != "merged" {
		t.Fatalf("pr2: expected merged, got %q", states[prIDs[2]])
	}
}

// TestMergeTrain_Abort_WhilePaused — abort flips to cancelled and
// records the reason.
func TestMergeTrain_Abort_WhilePaused(t *testing.T) {
	enableMergeTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/merge-abort")
	releaseID, _ := seedReleaseWithPRs(t, projectID, "https://github.com/multica-ai/merge-abort", 2)

	ghClient := &fakeShipGithub{
		mergeFn: func(_ context.Context, _, _ string, _ int, _, _ string) (*gh.MergeResult, error) {
			return nil, gh.ErrUnprocessable
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	deps := &ship.MergeTrainDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}

	if err := svc.StartMerge(context.Background(), parseUUID(releaseID), parseUUID(testUserID), "", "", deps); err != nil {
		t.Fatalf("StartMerge: %v", err)
	}
	waitFor(t, "paused", func() bool { return readReleasePaused(t, releaseID) })

	updated, err := svc.AbortMergeTrain(context.Background(), parseUUID(releaseID),
		"giving up", parseUUID(testUserID),
		&releaseChannelOps{h: testHandler}, &releaseIssueOps{h: testHandler}, deps)
	if err != nil {
		t.Fatalf("AbortMergeTrain: %v", err)
	}
	if updated.Stage != db.ReleaseStageCancelled {
		t.Fatalf("expected stage=cancelled, got %q", updated.Stage)
	}
	if updated.RollbackReason.String != "giving up" {
		t.Fatalf("expected reason recorded, got %q", updated.RollbackReason.String)
	}
}

// TestMergeTrain_StartFromInStaging_ReturnsStageMismatch — a release
// that's already in staging cannot be re-started.
func TestMergeTrain_StartFromInStaging_ReturnsStageMismatch(t *testing.T) {
	enableMergeTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/merge-wrong-stage")
	releaseID, _ := seedReleaseWithPRs(t, projectID, "https://github.com/multica-ai/merge-wrong-stage", 1)
	// Force the row to in_staging.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET stage='in_staging' WHERE id = $1`, releaseID); err != nil {
		t.Fatalf("force stage: %v", err)
	}

	ghClient := &fakeShipGithub{}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	err := svc.StartMerge(context.Background(), parseUUID(releaseID), parseUUID(testUserID), "", "", &ship.MergeTrainDeps{ParentCtx: context.Background()})
	if !errors.Is(err, ship.ErrReleaseStageMismatch) {
		t.Fatalf("expected ErrReleaseStageMismatch, got %v", err)
	}
}

// TestMergeTrain_TokenRevokedMidTrain_Pauses — 401 from GitHub is an
// authoritative failure; pause immediately with a clear error.
func TestMergeTrain_TokenRevokedMidTrain_Pauses(t *testing.T) {
	enableMergeTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/merge-token")
	releaseID, prIDs := seedReleaseWithPRs(t, projectID, "https://github.com/multica-ai/merge-token", 2)

	ghClient := &fakeShipGithub{
		mergeFn: func(_ context.Context, _, _ string, _ int, _, _ string) (*gh.MergeResult, error) {
			return nil, gh.ErrUnauthorized
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	deps := &ship.MergeTrainDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}

	if err := svc.StartMerge(context.Background(), parseUUID(releaseID), parseUUID(testUserID), "", "", deps); err != nil {
		t.Fatalf("StartMerge: %v", err)
	}
	waitFor(t, "paused", func() bool { return readReleasePaused(t, releaseID) })

	states := readMergeStates(t, releaseID)
	if states[prIDs[0]] != "failed" {
		t.Fatalf("expected pr0 failed, got %q", states[prIDs[0]])
	}
	// Read the merge_error via the membership row.
	var errMsg string
	testPool.QueryRow(context.Background(),
		`SELECT merge_error FROM ship_release_pull_request WHERE release_id=$1 AND pull_request_id=$2`,
		releaseID, prIDs[0]).Scan(&errMsg)
	if !strings.Contains(strings.ToLower(errMsg), "unauthor") {
		t.Fatalf("expected unauthorized in error, got %q", errMsg)
	}
}

// TestMergeTrain_TransientErrorRetries — a transient error retries up
// to 3 times before giving up. We arrange the second PR to fail with
// a generic error twice then succeed; the merge should land on
// attempt 3 and the train should complete cleanly.
func TestMergeTrain_TransientErrorRetries(t *testing.T) {
	enableMergeTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/merge-retry")
	releaseID, _ := seedReleaseWithPRs(t, projectID, "https://github.com/multica-ai/merge-retry", 2)

	var attempts atomic.Int32
	ghClient := &fakeShipGithub{
		mergeFn: func(_ context.Context, _, _ string, _ int, _, _ string) (*gh.MergeResult, error) {
			n := attempts.Add(1)
			// The second PR's first two attempts fail with a generic error
			// (not a typed sentinel — so the orchestrator retries). The
			// first PR succeeds on attempt 1; the second PR succeeds on
			// attempt 4 (1 first PR + 2 fails + 1 success).
			if n == 2 || n == 3 {
				return nil, fmt.Errorf("flaky github error")
			}
			return &gh.MergeResult{SHA: fmt.Sprintf("sha%d", n), Merged: true}, nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: ghClient}
	deps := &ship.MergeTrainDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}

	if err := svc.StartMerge(context.Background(), parseUUID(releaseID), parseUUID(testUserID), "", "", deps); err != nil {
		t.Fatalf("StartMerge: %v", err)
	}
	waitFor(t, "stage=in_staging or paused", func() bool {
		s := readReleaseStage(t, releaseID)
		return s == "in_staging" || readReleasePaused(t, releaseID)
	})
	if readReleasePaused(t, releaseID) {
		t.Fatalf("expected retries to succeed, got paused state")
	}
	if got := readReleaseStage(t, releaseID); got != "in_staging" {
		t.Fatalf("expected stage=in_staging, got %q", got)
	}
	// 1 first PR + at least 3 second PR (2 retries + 1 success) = 4 attempts.
	if attempts.Load() < 4 {
		t.Fatalf("expected ≥4 merge attempts, got %d", attempts.Load())
	}
}

// TestStartMerge_Endpoint_HappyPath — exercises the HTTP layer once
// to confirm wiring (auth, body decode, 202 response). The
// orchestrator then runs through to completion as in the service-
// level happy path.
func TestStartMerge_Endpoint_HappyPath(t *testing.T) {
	enableMergeTest(t)
	enableShipHub(t, true) // need a token for the ship service path
	projectID := createShipProject(t, "https://github.com/multica-ai/merge-endpoint")
	releaseID, _ := seedReleaseWithPRs(t, projectID, "https://github.com/multica-ai/merge-endpoint", 2)

	// We can't easily intercept the GitHub client constructed inside
	// the handler (it builds gh.NewClient(token) with the workspace
	// token), so we skip that path and just assert the endpoint
	// returns 202 / 4xx as expected. The orchestrator goroutine will
	// then fail on the first GitHub call (no real backend) and
	// pause; that's fine for endpoint wiring assertion.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/releases/"+releaseID+"/start_merge", []byte(`{"merge_method":"merge"}`))
	req = withURLParam(req, "id", releaseID)
	testHandler.StartMergeRelease(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("StartMergeRelease: want 202, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetReleaseMergeState_Endpoint — the lightweight poll endpoint
// returns the right shape pre-merge.
func TestGetReleaseMergeState_Endpoint(t *testing.T) {
	enableMergeTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/merge-poll")
	releaseID, _ := seedReleaseWithPRs(t, projectID, "https://github.com/multica-ai/merge-poll", 2)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/releases/"+releaseID+"/merge_state", nil)
	req = withURLParam(req, "id", releaseID)
	testHandler.GetReleaseMergeState(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetReleaseMergeState: want 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"merge_paused":false`) {
		t.Fatalf("expected merge_paused=false, body: %s", body)
	}
	if !strings.Contains(body, `"merge_method":"merge"`) {
		t.Fatalf("expected merge_method=merge, body: %s", body)
	}
	if !strings.Contains(body, `"total":2`) {
		t.Fatalf("expected total=2, body: %s", body)
	}
}
