package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// mul8188Agent returns the seeded workspace agent and its runtime.
func mul8188Agent(t *testing.T) (agentID, runtimeID string) {
	t.Helper()
	dbfx.QueryRow(t, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)
	return agentID, runtimeID
}

func ackCancelled(t *testing.T, taskID string, body map[string]any) {
	t.Helper()
	req := newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/cancel-ack", body)
	req = testutil.WithURLParams(req, "taskId", taskID)
	testutil.Call(t, testHandler.AckTaskCancelled, req).Want(http.StatusOK)
}

func lastIssueSession(t *testing.T, agentID, issueID string) (string, bool) {
	t.Helper()
	got, err := testHandler.Queries.GetLastTaskSession(context.Background(), db.GetLastTaskSessionParams{
		AgentID: parseUUID(agentID),
		IssueID: parseUUID(issueID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("GetLastTaskSession: %v", err)
	}
	return got.SessionID.String, true
}

func retiredSessionOf(t *testing.T, taskID string) string {
	t.Helper()
	var retired string
	dbfx.QueryRow(t, `
		SELECT COALESCE(retired_session_id, '') FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&retired)
	return retired
}

// TestCancelAck_RetiresSilentResumedSession is the server half of MUL-8188's
// "keeps resuming the broken session" loop: an earlier provider 5xx (resume
// safe) and then a hand-cancelled run, both naming the same session. Every row
// was a valid resume source, so each trigger resumed it again.
func TestCancelAck_RetiresSilentResumedSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID := mul8188Agent(t)
	issueID := dbfx.Issue(t, "mul-8188 retire on cancel-ack")

	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":     runtimeID,
		"issue_id":       issueID,
		"status":         "failed",
		"session_id":     "ses_broken",
		"failure_reason": "agent_error.provider_server_error",
		"error":          "kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=dispatch failure)",
		"started_at":     testutil.Raw("now() - interval '10 minutes'"),
		"completed_at":   testutil.Raw("now() - interval '9 minutes'"),
	})
	cancelled := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   runtimeID,
		"issue_id":     issueID,
		"status":       "cancelled",
		"session_id":   "ses_broken",
		"started_at":   testutil.Raw("now() - interval '8 minutes'"),
		"completed_at": testutil.Raw("now() - interval '1 minute'"),
	})

	if got, ok := lastIssueSession(t, agentID, issueID); !ok || got != "ses_broken" {
		t.Fatalf("precondition: resume pointer = %q (%v), want ses_broken", got, ok)
	}

	ackCancelled(t, cancelled, map[string]any{"retired_session_id": "ses_broken"})

	if got := retiredSessionOf(t, cancelled); got != "ses_broken" {
		t.Fatalf("retired_session_id = %q, want ses_broken", got)
	}
	if got, ok := lastIssueSession(t, agentID, issueID); ok && got == "ses_broken" {
		t.Fatal("the undrivable session is still offered as the resume pointer")
	}

	// Replay is idempotent and never overwrites.
	ackCancelled(t, cancelled, map[string]any{"retired_session_id": "ses_other"})
	if got := retiredSessionOf(t, cancelled); got != "ses_broken" {
		t.Fatalf("replay overwrote retired_session_id: %q", got)
	}
}

// TestCancelAck_RetiredSessionIgnoredOnNonCancelledRow: the status CAS keeps a
// stale ack from retiring the session of a completed run.
func TestCancelAck_RetiredSessionIgnoredOnNonCancelledRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID := mul8188Agent(t)
	issueID := dbfx.Issue(t, "mul-8188 cas on completed row")
	completed := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   runtimeID,
		"issue_id":     issueID,
		"status":       "completed",
		"session_id":   "ses_healthy",
		"started_at":   testutil.Raw("now()"),
		"completed_at": testutil.Raw("now()"),
	})

	ackCancelled(t, completed, map[string]any{"retired_session_id": "ses_healthy"})

	if got := retiredSessionOf(t, completed); got != "" {
		t.Fatalf("retired_session_id = %q on a completed row, want empty", got)
	}
	if got, ok := lastIssueSession(t, agentID, issueID); !ok || got != "ses_healthy" {
		t.Fatalf("resume pointer = %q (%v), want ses_healthy to stay resumable", got, ok)
	}
}

// TestCancelAck_RetiredSessionClearsChatPointer: a chat turn resumes from
// chat_session.session_id first, so the pointer must be cleared too — but
// only while it still names the retired session.
func TestCancelAck_RetiredSessionClearsChatPointer(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID := mul8188Agent(t)
	chatSessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"session_id": "ses_broken",
		"runtime_id": runtimeID,
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":      runtimeID,
		"chat_session_id": chatSessionID,
		"status":          "cancelled",
		"session_id":      "ses_broken",
		"started_at":      testutil.Raw("now()"),
		"completed_at":    testutil.Raw("now()"),
	})

	ackCancelled(t, taskID, map[string]any{"retired_session_id": "ses_broken"})

	var pointer *string
	dbfx.QueryRow(t, `SELECT session_id FROM chat_session WHERE id = $1`, chatSessionID).Scan(&pointer)
	if pointer != nil && *pointer == "ses_broken" {
		t.Fatal("chat resume pointer still names the retired session")
	}
}

// TestGetLastTaskSession_ExcludesInvalidConversationHistory pins the
// server-side text guard for rows an older daemon classified as resume-safe:
// Kiro's "invalid conversation history" panic must never be resumed again.
func TestGetLastTaskSession_ExcludesInvalidConversationHistory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID, runtimeID := mul8188Agent(t)
	issueID := dbfx.Issue(t, "mul-8188 invalid history text guard")
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":     runtimeID,
		"issue_id":       issueID,
		"status":         "failed",
		"session_id":     "ses_broken",
		"failure_reason": "agent_error.unknown",
		"error":          `kiro runtime panicked: The application panicked (crashed). | Message:  first agent loop request should never fail: AgentLoopResponse(Custom("invalid conversation history received"))`,
		"started_at":     testutil.Raw("now()"),
		"completed_at":   testutil.Raw("now()"),
	})

	if got, ok := lastIssueSession(t, agentID, issueID); ok && got == "ses_broken" {
		t.Fatal("a session Kiro reported as invalid history is still offered as the resume pointer")
	}
}

// TestClaimRerun_DoesNotReviveRetiredSession is the review's P1 regression
// (adapted from the reviewer's attachment). A manual retry resumes from its
// exact source task instead of GetLastTaskSession, so it used to bypass the
// retired set: retrying the original provider-5xx failure handed back the
// very session a later cancelled run had retired. The workdir must still be
// reused, and the agent told that continuity is unavailable.
func TestClaimRerun_DoesNotReviveRetiredSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	issueID := dbfx.Issue(t, "mul-8188 rerun retired session")
	sourceID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":     runtimeID,
		"issue_id":       issueID,
		"status":         "failed",
		"session_id":     "ses_broken",
		"failure_reason": "agent_error.provider_server_error",
		"error":          "dispatch failure",
		"work_dir":       "/tmp/mul8188-existing-workdir",
		"started_at":     testutil.Raw("now() - interval '10 minutes'"),
		"completed_at":   testutil.Raw("now() - interval '9 minutes'"),
	})
	cancelledID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   runtimeID,
		"issue_id":     issueID,
		"status":       "cancelled",
		"started_at":   testutil.Raw("now() - interval '8 minutes'"),
		"completed_at": testutil.Raw("now() - interval '1 minute'"),
	})
	ackCancelled(t, cancelledID, map[string]any{"retired_session_id": "ses_broken"})
	if got, ok := lastIssueSession(t, agentID, issueID); ok && got == "ses_broken" {
		t.Fatal("precondition: retirement did not take effect")
	}

	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":          runtimeID,
		"issue_id":            issueID,
		"rerun_of_task_id":    sourceID,
		"force_fresh_session": true,
	})
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	claimForRuntimeExpecting(t, testHandler, runtimeID, daemonID, http.StatusOK).JSON(&response)
	if response.Task == nil {
		t.Fatal("no task claimed")
	}
	if response.Task.PriorSessionID != "" {
		t.Fatalf("manual retry revived retired session: PriorSessionID=%q", response.Task.PriorSessionID)
	}
	if response.Task.PriorWorkDir != "/tmp/mul8188-existing-workdir" {
		t.Fatalf("PriorWorkDir = %q, want the source workdir reused", response.Task.PriorWorkDir)
	}
	if !response.Task.PriorSessionResumeUnavailable {
		t.Fatal("PriorSessionResumeUnavailable = false; the agent must be told continuity is gone")
	}
}

// TestClaimRerun_ResumesHealthySourceSession is the control: with nothing
// retired, a manual retry of a resume-safe failure still resumes its session.
func TestClaimRerun_ResumesHealthySourceSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	issueID := dbfx.Issue(t, "mul-8188 rerun healthy session")
	sourceID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":     runtimeID,
		"issue_id":       issueID,
		"status":         "failed",
		"session_id":     "ses_healthy",
		"failure_reason": "agent_error.provider_server_error",
		"error":          "dispatch failure",
		"started_at":     testutil.Raw("now() - interval '10 minutes'"),
		"completed_at":   testutil.Raw("now() - interval '9 minutes'"),
	})
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":          runtimeID,
		"issue_id":            issueID,
		"rerun_of_task_id":    sourceID,
		"force_fresh_session": true,
	})
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	claimForRuntimeExpecting(t, testHandler, runtimeID, daemonID, http.StatusOK).JSON(&response)
	if response.Task == nil {
		t.Fatal("no task claimed")
	}
	if response.Task.PriorSessionID != "ses_healthy" {
		t.Fatalf("PriorSessionID = %q, want ses_healthy", response.Task.PriorSessionID)
	}
}
