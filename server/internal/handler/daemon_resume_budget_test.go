package handler

import (
	"context"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestResumeBudgetExceeded pins the policy itself, which is the half that must
// hold on every deployment whether or not usage was ever recorded.
//
// The disabled case is the one with teeth: an operator who sets the budget to 0
// is asking for the pre-#4754 behaviour back, and the gate must be off even for
// a session whose billed input is enormous.
func TestResumeBudgetExceeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		billed int64
		budget int64
		want   bool
	}{
		{"ordinary session is far under budget", 330_000, 8_000_000, false},
		{"just under the ceiling still resumes", 7_999_999, 8_000_000, false},
		{"exactly at the ceiling does not", 8_000_000, 8_000_000, true},
		{"a runaway session does not", 11_500_000, 8_000_000, true},
		{"budget 0 disables the gate entirely", 11_500_000, 0, false},
		{"a session with no recorded usage resumes", 0, 8_000_000, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resumeBudgetExceeded(tc.billed, tc.budget); got != tc.want {
				t.Errorf("resumeBudgetExceeded(%d, %d) = %v, want %v", tc.billed, tc.budget, got, tc.want)
			}
		})
	}
}

// TestResumeContextBudgetTokensIsTriState covers why the config field is a
// pointer: nil and 0 are different answers, and collapsing them would make an
// explicit "never gate a resume" silently re-enable the gate.
func TestResumeContextBudgetTokensIsTriState(t *testing.T) {
	t.Parallel()

	disabled := int64(0)
	tightened := int64(500_000)
	tests := []struct {
		name string
		cfg  *int64
		want int64
	}{
		{"unset takes the default", nil, defaultResumeContextBudgetTokens},
		{"an explicit 0 disables", &disabled, 0},
		{"an explicit value is honoured", &tightened, 500_000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{cfg: Config{ResumeContextBudgetTokens: tc.cfg}}
			if got := h.resumeContextBudgetTokens(); got != tc.want {
				t.Errorf("resumeContextBudgetTokens() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestResumeExceedsContextBudgetReadsWholeSession is the database half: the
// budget is spent by the SESSION, not by one task, so a conversation that got
// expensive over several turns has to be seen as expensive on the next one.
//
// The fixture is the #4754 shape in miniature — one heavy task followed by a
// cheap one, both on the same provider session. Neither exceeds the budget
// alone; together they do, and that is the run that would otherwise inherit
// the whole transcript to answer a one-line comment.
func TestResumeExceedsContextBudgetReadsWholeSession(t *testing.T) {
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
		VALUES ($1, 'gh-4754 resume budget fixture', 'in_progress', 'none', $2, 'member', 4754, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	const sessionID = "sess-gh4754"
	addTask := func(input, cacheRead int64) {
		t.Helper()
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id)
			VALUES ($1, $2, $3, 'completed', 0, now(), now(), $4)
			RETURNING id
		`, agentID, runtimeID, issueID, sessionID).Scan(&taskID); err != nil {
			t.Fatalf("setup: create task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, cache_read_tokens)
			VALUES ($1, 'anthropic', 'claude-test', $2, $3)
		`, taskID, input, cacheRead); err != nil {
			t.Fatalf("setup: record usage: %v", err)
		}
	}

	task := &db.AgentTaskQueue{
		ID:      parseUUID("00000000-0000-4000-8000-000000004754"),
		AgentID: parseUUID(agentID),
		IssueID: parseUUID(issueID),
	}
	budget := int64(5_000_000)
	h := *testHandler
	h.cfg.ResumeContextBudgetTokens = &budget

	// A session nobody has spent anything on resumes.
	if h.resumeExceedsContextBudget(ctx, task, sessionID) {
		t.Fatal("an unused session must stay resumable")
	}

	// One heavy coding turn. Cache reads count — a cached prefix is cheaper,
	// not smaller.
	addTask(1_000_000, 2_000_000)
	if h.resumeExceedsContextBudget(ctx, task, sessionID) {
		t.Fatal("3M of 5M must still resume")
	}

	// A second turn on the same session pushes it over. This is the assertion
	// that would fail if the query scoped to a single task.
	addTask(500_000, 1_600_000)
	if !h.resumeExceedsContextBudget(ctx, task, sessionID) {
		t.Fatal("5.1M against a 5M budget must not resume")
	}

	// Same rows, gate disabled: the operator's 0 wins over any amount of spend.
	disabled := int64(0)
	off := *testHandler
	off.cfg.ResumeContextBudgetTokens = &disabled
	if off.resumeExceedsContextBudget(ctx, task, sessionID) {
		t.Fatal("budget 0 must never gate a resume")
	}

	// A different session on the same issue is a different budget.
	if h.resumeExceedsContextBudget(ctx, task, "sess-unrelated") {
		t.Fatal("the budget must be scoped to the session being resumed")
	}
}
