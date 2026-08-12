package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// TestCompleteTask_ContextExhaustionFromOlderDaemonIsRecordedAsFailed is the
// mixed-version regression for GH #6402.
//
// A daemon whose backend does not read the provider's structured terminal
// reason reports a context-exhausted run through /complete, with the CLI's
// "your context window is full" notice as the answer. Installed daemons upgrade
// on their own cadence, so without a server-side rule an un-upgraded host keeps
// publishing that notice as the agent's reply AND keeps the dead session as the
// resume pointer — every later trigger on the issue resumes it and reproduces
// the same non-answer. That is a permanently stuck (agent, issue) pair, not a
// mislabelled row, which is why the normalization lives at the boundary rather
// than only in the daemon.
//
// Asserts the whole chain: the task lands failed with the reason the resume
// blacklist covers, and the saturated session stops being handed to the next
// task.
func TestCompleteTask_ContextExhaustionFromOlderDaemonIsRecordedAsFailed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'gh-6402 context exhaustion fixture', 'in_progress', 'none', $2, 'member', 6402, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now())
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	// Exactly what an older daemon posts: a successful completion whose output
	// is the provider's notice, carrying the saturated session id.
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{
			"output": "Prompt is too long · the request is ~274931 tokens (limit 200000) but this conversation is only ~1597 tokens — " +
				"the rest is system prompt, tool definitions, and attachment content. A single-exchange conversation cannot be " +
				"compacted; reduce attached files/tools or start with less context.",
			"session_id": "sess-saturated-6402",
			"work_dir":   "/tmp/gh6402",
		},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status, failureReason string
	if err := testPool.QueryRow(ctx, `
		SELECT status, COALESCE(failure_reason, '') FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&status, &failureReason); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("task status = %q, want failed — a context-exhausted run produced no work", status)
	}
	if failureReason != string(taskfailure.ReasonAgentContextOverflow) {
		t.Fatalf("failure_reason = %q, want %q", failureReason, taskfailure.ReasonAgentContextOverflow)
	}

	// The payoff: the next task on this (agent, issue) must not be handed the
	// session that just proved it cannot answer. This is the automated form of
	// the reporter's manual workaround — archiving the transcript on disk.
	got, err := testHandler.Queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: parseUUID(agentID),
		IssueID: parseUUID(issueID),
	})
	if err == nil && got.SessionID.Valid && got.SessionID.String == "sess-saturated-6402" {
		t.Fatal("the saturated session is still offered as the resume pointer")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetLastTaskSession: %v", err)
	}

	// And nothing was published as the agent's answer: the run failed, so the
	// completion path's fallback comment never ran.
	var agentComments int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2 AND type = 'comment'
	`, issueID, agentID).Scan(&agentComments); err != nil {
		t.Fatalf("count agent comments: %v", err)
	}
	if agentComments != 0 {
		t.Fatalf("expected the provider notice NOT to be posted as an answer, got %d agent comment(s)", agentComments)
	}
}

// TestCompleteTask_RealAnswerStillCompletes is the companion guard: outputs a
// real agent can legitimately produce go through the completion path untouched.
//
// The second case is the one that shaped the classifier. "Prompt is too long"
// IS a wording the CLI emits, but it is also a complete, ordinary English
// answer to "is my prompt too long?" — so the success-path predicate must not
// claim it. Matching it would fail a task that succeeded and retire a healthy
// session, which is strictly worse than the miss: the real provider frame
// carries is_error, so that shape reaches the server through /fail, where
// Classify routes it to context_overflow anyway.
func TestCompleteTask_RealAnswerStillCompletes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	tests := []struct {
		name   string
		number int32
		output string
	}{
		{
			name:   "an answer that mentions compacting",
			number: 6403,
			output: "Ran /compact first because the session was getting long, then fixed the redirect and pushed.",
		},
		{
			name:   "an answer that IS the provider's bare sentence",
			number: 6404,
			output: "Prompt is too long",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var issueID string
			if err := testPool.QueryRow(ctx, `
				INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
				VALUES ($1, 'gh-6402 control fixture', 'in_progress', 'none', $2, 'member', $3, 0)
				RETURNING id
			`, testWorkspaceID, testUserID, tc.number).Scan(&issueID); err != nil {
				t.Fatalf("setup: create issue: %v", err)
			}
			t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

			var taskID string
			if err := testPool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
				VALUES ($1, $2, $3, 'running', 0, now())
				RETURNING id
			`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
				t.Fatalf("setup: create task: %v", err)
			}
			t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

			w := httptest.NewRecorder()
			req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
				map[string]any{"output": tc.output, "session_id": "sess-healthy"},
				testWorkspaceID, "legit-daemon")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("taskId", taskID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			testHandler.CompleteTask(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
			}

			var status string
			if err := testPool.QueryRow(ctx, `
				SELECT status FROM agent_task_queue WHERE id = $1
			`, taskID).Scan(&status); err != nil {
				t.Fatalf("read task: %v", err)
			}
			if status != "completed" {
				t.Fatalf("task status = %q, want completed", status)
			}

			// And the healthy session stays available to the next task.
			got, err := testHandler.Queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
				AgentID: parseUUID(agentID),
				IssueID: parseUUID(issueID),
			})
			if err != nil {
				t.Fatalf("GetLastTaskSession: %v", err)
			}
			if got.SessionID.String != "sess-healthy" {
				t.Fatalf("expected the healthy session to remain resumable, got %q", got.SessionID.String)
			}
		})
	}
}
