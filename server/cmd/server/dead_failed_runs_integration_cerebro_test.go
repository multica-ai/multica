package main

// FIR-3901 — end-to-end cover for the dead failed-run endpoints against a real
// database. The predicate is hand-written SQL, so a unit test on the Go side
// would prove nothing: these seed the four cases that decide whether the red
// bar appears and whether Resume is offered.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

)

type deadFailedRunPayload struct {
	Runs []struct {
		TaskID         string `json:"task_id"`
		IssueID        string `json:"issue_id"`
		FailureReason  string `json:"failure_reason"`
		ResumePossible bool   `json:"resume_possible"`
		BlockedReason  string `json:"blocked_reason"`
	} `json:"runs"`
}

// seedDeadFailedIssue creates an issue owned by a fixture agent and returns its id.
func seedDeadFailedIssue(t *testing.T, title string) string {
	t.Helper()
	var issueID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, $2, 'todo', 'medium', 'member', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id::text`,
		testWorkspaceID, title, testUserID).Scan(&issueID)
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1::uuid`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1::uuid`, issueID)
	})
	return issueID
}

// seedFailedTask inserts one failed run. completedAgo controls whether it has
// settled past the 60-second grace window.
func seedFailedTask(t *testing.T, issueID, agentID string, runtimeID any, sessionID any, completedAgo time.Duration) string {
	t.Helper()
	var taskID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, error, failure_reason,
		    session_id, work_dir, attempt, max_attempts, dispatched_at, started_at, completed_at, created_at)
		VALUES ($1::uuid, $2::uuid, $3, 'failed', 'claude execution failed', 'agent_error.unknown',
		        $4, '/tmp/fir3901', 1, 2, NOW() - $5::interval, NOW() - $5::interval, NOW() - $5::interval, NOW() - $5::interval)
		RETURNING id::text`,
		agentID, issueID, runtimeID, sessionID, fmt.Sprintf("%d seconds", int(completedAgo.Seconds()))).Scan(&taskID)
	if err != nil {
		t.Fatalf("seed failed task: %v", err)
	}
	return taskID
}

func fixtureAgentID(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id::text FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&id); err != nil {
		t.Skipf("no fixture agent in workspace: %v", err)
	}
	return id
}

// seedRuntime creates a runtime with the given status so resume_possible can be
// exercised in both directions.
func seedRuntime(t *testing.T, status string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, owner_id)
		VALUES ($1::uuid, $2, 'local', 'claude', $3, '', $4::uuid)
		RETURNING id::text`,
		testWorkspaceID, "FIR-3901 "+status, status, testUserID).Scan(&id)
	if err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1::uuid`, id)
	})
	return id
}

func fetchIssueFailedRuns(t *testing.T, issueID string) deadFailedRunPayload {
	t.Helper()
	resp := authRequest(t, http.MethodGet, "/api/issues/"+issueID+"/failed-runs", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET failed-runs: status %d", resp.StatusCode)
	}
	var out deadFailedRunPayload
	readJSON(t, resp, &out)
	return out
}

// A settled failure with a session on an online runtime is the case the whole
// feature exists for: it shows, and Resume is offered.
func TestDeadFailedRun_SettledFailureIsOfferedForResume(t *testing.T) {
	issueID := seedDeadFailedIssue(t, "FIR-3901 resumable")
	runtimeID := seedRuntime(t, "online")
	taskID := seedFailedTask(t, issueID, fixtureAgentID(t), runtimeID, "sess-fir3901", 5*time.Minute)

	got := fetchIssueFailedRuns(t, issueID)
	if len(got.Runs) != 1 {
		t.Fatalf("expected exactly 1 dead failed run, got %d", len(got.Runs))
	}
	if got.Runs[0].TaskID != taskID {
		t.Errorf("task id = %q, want %q", got.Runs[0].TaskID, taskID)
	}
	if !got.Runs[0].ResumePossible {
		t.Errorf("resume_possible = false, want true (session present, runtime online); blocked_reason = %q", got.Runs[0].BlockedReason)
	}
}

// The auto-retry row lands milliseconds after the failure. Without the grace
// window every auto-retried failure would flash red for one poll cycle.
func TestDeadFailedRun_FreshFailureIsHeldBackByTheGraceWindow(t *testing.T) {
	issueID := seedDeadFailedIssue(t, "FIR-3901 grace")
	seedFailedTask(t, issueID, fixtureAgentID(t), seedRuntime(t, "online"), "sess-grace", 5*time.Second)

	if got := fetchIssueFailedRuns(t, issueID); len(got.Runs) != 0 {
		t.Fatalf("a failure that settled 5s ago must not show yet, got %d runs", len(got.Runs))
	}
}

// A newer run for the same agent means the thread moved on — the old failure is
// no longer what the user has to act on.
func TestDeadFailedRun_NewerRunClearsTheFailure(t *testing.T) {
	issueID := seedDeadFailedIssue(t, "FIR-3901 superseded")
	agentID := fixtureAgentID(t)
	runtimeID := seedRuntime(t, "online")
	seedFailedTask(t, issueID, agentID, runtimeID, "sess-old", 10*time.Minute)

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, attempt, max_attempts, created_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'queued', 1, 2, NOW())`,
		agentID, issueID, runtimeID); err != nil {
		t.Fatalf("seed successor: %v", err)
	}

	if got := fetchIssueFailedRuns(t, issueID); len(got.Runs) != 0 {
		t.Fatalf("a superseded failure must not show, got %d runs", len(got.Runs))
	}
}

// The conversation lives on the machine that ran it. Offering Resume when that
// machine is gone would silently start a blank run — worse than no button.
func TestDeadFailedRun_OfflineRuntimeBlocksResumeWithAnExplanation(t *testing.T) {
	issueID := seedDeadFailedIssue(t, "FIR-3901 offline")
	seedFailedTask(t, issueID, fixtureAgentID(t), seedRuntime(t, "offline"), "sess-offline", 5*time.Minute)

	got := fetchIssueFailedRuns(t, issueID)
	if len(got.Runs) != 1 {
		t.Fatalf("expected 1 dead failed run, got %d", len(got.Runs))
	}
	if got.Runs[0].ResumePossible {
		t.Error("resume_possible = true, want false — the runtime is offline")
	}
	if got.Runs[0].BlockedReason == "" {
		t.Error("blocked_reason is empty; the UI needs a plain-words explanation to show")
	}
}

// A run that never established a session has nothing to continue.
func TestDeadFailedRun_NoSessionBlocksResume(t *testing.T) {
	issueID := seedDeadFailedIssue(t, "FIR-3901 no session")
	seedFailedTask(t, issueID, fixtureAgentID(t), seedRuntime(t, "online"), nil, 5*time.Minute)

	got := fetchIssueFailedRuns(t, issueID)
	if len(got.Runs) != 1 {
		t.Fatalf("expected 1 dead failed run, got %d", len(got.Runs))
	}
	if got.Runs[0].ResumePossible {
		t.Error("resume_possible = true, want false — the run has no session to resume")
	}
}

// The workspace-wide endpoint backs the inbox pip and must surface the same run.
func TestDeadFailedRun_WorkspaceEndpointSurfacesTheIssue(t *testing.T) {
	issueID := seedDeadFailedIssue(t, "FIR-3901 workspace")
	seedFailedTask(t, issueID, fixtureAgentID(t), seedRuntime(t, "online"), "sess-ws", 5*time.Minute)

	resp := authRequest(t, http.MethodGet, "/api/inbox/failed-issue-tasks", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET failed-issue-tasks: status %d", resp.StatusCode)
	}
	var out deadFailedRunPayload
	readJSON(t, resp, &out)

	for _, run := range out.Runs {
		if run.IssueID == issueID {
			return
		}
	}
	t.Fatalf("workspace endpoint did not surface issue %s (%d runs returned)", issueID, len(out.Runs))
}


// ─── Access control ──────────────────────────────────────────────────────────
//
// The failure reason, the raw error text and the machine name are issue
// content. Both endpoints originally answered on a bare "is there a user?"
// check, so knowing an issue's UUID was enough to read its failure text from
// anywhere. These four cases pin the gate shut.

// seedForeignWorkspaceIssue creates an issue in a DIFFERENT workspace and
// returns its id, so a caller scoped to the fixture workspace must not see it.
func seedForeignWorkspaceIssue(t *testing.T, title string) (issueID, agentID, runtimeID string) {
	t.Helper()
	ctx := context.Background()
	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, $3) RETURNING id::text`,
		"FIR-3901 Foreign", fmt.Sprintf("fir3901-foreign-%d", time.Now().UnixNano()),
		"Temporary workspace for the FIR-3901 access test").Scan(&wsID); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info)
		VALUES ($1::uuid, 'FIR-3901 foreign runtime', 'local', 'claude', 'online', '')
		RETURNING id::text`, wsID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed foreign runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks)
		VALUES ($1::uuid, 'FIR-3901 foreign agent', 'local', '{}'::jsonb, $2::uuid, 'workspace', 1)
		RETURNING id::text`, wsID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("seed foreign agent: %v", err)
	}
	// Creator is the fixture user on purpose: even the person who filed it
	// cannot read it from a workspace they are not a member of.
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1::uuid, $2, 'todo', 'medium', 'member', $3::uuid,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1::uuid))
		RETURNING id::text`, wsID, title, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed foreign issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1::uuid`, issueID)
		_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1::uuid`, issueID)
		_, _ = testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1::uuid`, agentID)
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1::uuid`, runtimeID)
		_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1::uuid`, wsID)
	})
	return issueID, agentID, runtimeID
}

// seedChannelIssue creates a channel in the fixture workspace with NO
// subscriber rows. Channels are subscriber-gated and workspace owners get no
// implicit read access, so this is unreadable even for the owner fixture user.
func seedChannelIssue(t *testing.T, title string) string {
	t.Helper()
	ctx := context.Background()
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, kind, number)
		VALUES ($1, $2, 'todo', 'medium', 'member', $3, 'channel',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id::text`, testWorkspaceID, title, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed channel issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1::uuid`, issueID)
		_, _ = testPool.Exec(ctx, `DELETE FROM issue_subscriber WHERE issue_id = $1::uuid`, issueID)
		_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1::uuid`, issueID)
	})
	return issueID
}

// The leak this fix closes: an issue in another workspace answered on its UUID
// alone, handing over the failure reason, the error text and the machine name.
func TestDeadFailedRun_ForeignWorkspaceIssueIsNotReadable(t *testing.T) {
	issueID, agentID, runtimeID := seedForeignWorkspaceIssue(t, "FIR-3901 foreign")
	seedFailedTask(t, issueID, agentID, runtimeID, "sess-foreign", 5*time.Minute)

	resp := authRequest(t, http.MethodGet, "/api/issues/"+issueID+"/failed-runs", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET failed-runs on a foreign workspace's issue: status %d, want 404", resp.StatusCode)
	}
}

// Channels are subscriber-gated, and a workspace owner gets no implicit read
// access. This is the in-workspace half of the same gate.
func TestDeadFailedRun_ChannelYouAreNotInIsNotReadable(t *testing.T) {
	issueID := seedChannelIssue(t, "FIR-3901 private channel")
	seedFailedTask(t, issueID, fixtureAgentID(t), seedRuntime(t, "online"), "sess-channel", 5*time.Minute)

	resp := authRequest(t, http.MethodGet, "/api/issues/"+issueID+"/failed-runs", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET failed-runs on a channel the caller is not in: status %d, want 404", resp.StatusCode)
	}
}

// The inbox feed is workspace-scoped, which is not the same as visible-scoped.
// A channel the caller is not in must not reach the pip either.
func TestDeadFailedRun_WorkspaceEndpointHidesAChannelYouAreNotIn(t *testing.T) {
	issueID := seedChannelIssue(t, "FIR-3901 hidden channel")
	seedFailedTask(t, issueID, fixtureAgentID(t), seedRuntime(t, "online"), "sess-hidden", 5*time.Minute)

	resp := authRequest(t, http.MethodGet, "/api/inbox/failed-issue-tasks", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET failed-issue-tasks: status %d", resp.StatusCode)
	}
	var out deadFailedRunPayload
	readJSON(t, resp, &out)

	for _, run := range out.Runs {
		if run.IssueID == issueID {
			t.Fatalf("the inbox feed leaked a channel the caller is not subscribed to (%s)", issueID)
		}
	}
}

// The gate must not swallow the ordinary case: an issue the caller can see
// still returns its dead run after the access check runs.
func TestDeadFailedRun_OwnIssueStillReadableThroughTheGate(t *testing.T) {
	issueID := seedDeadFailedIssue(t, "FIR-3901 gate passthrough")
	taskID := seedFailedTask(t, issueID, fixtureAgentID(t), seedRuntime(t, "online"), "sess-gate", 5*time.Minute)

	got := fetchIssueFailedRuns(t, issueID)
	if len(got.Runs) != 1 || got.Runs[0].TaskID != taskID {
		t.Fatalf("the gate hid a run the caller may read: %+v", got.Runs)
	}
}
