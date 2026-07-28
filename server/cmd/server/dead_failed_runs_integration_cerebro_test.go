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

