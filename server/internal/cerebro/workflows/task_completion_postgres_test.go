package workflows

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FIR-4643: LoadTaskCompletionContext reports a run as silent only when the
// runtime reported execution messages and none of them was a tool call. The
// three cases below are the only ones the completion gate distinguishes.
func TestLoadTaskCompletionContextSilentRun(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	fixture := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, fixture, "Silent run", "in_progress", 1, pgtype.UUID{})

	var runtimeID, agentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		VALUES ($1, 'Silent run runtime', 'cloud', 'codex', 'online')
		RETURNING id
	`, fixture.workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, status)
		VALUES ($1, 'Silent run agent', 'cloud', $2, 'idle')
		RETURNING id
	`, fixture.workspaceID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	store := NewPostgresTaskCompletionStore(pool)

	for _, tc := range []struct {
		name        string
		messageType string
		commented   bool
		wantSilent  bool
	}{
		{name: "no reported messages", messageType: "", wantSilent: false},
		{name: "text only", messageType: "text", wantSilent: true},
		{name: "tool call", messageType: "tool_use", wantSilent: false},
		// Providers that stream plain text never emit tool_use even when the
		// agent ran tools (antigravity, firtal-gateway). A comment posted
		// during the run is the evidence that the run was not silent.
		{name: "text only but commented", messageType: "text", commented: true, wantSilent: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskID := insertSilentRunTask(t, pool, agentID, issueID, runtimeID)
			if tc.commented {
				if _, err := pool.Exec(ctx, `
					INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
					VALUES ($1, $2, 'agent', $3, 'status update')
				`, fixture.workspaceID, issueID, agentID); err != nil {
					t.Fatalf("insert comment: %v", err)
				}
			}
			if tc.messageType != "" {
				if _, err := pool.Exec(ctx, `
					INSERT INTO task_message (task_id, seq, type, content)
					VALUES ($1, 1, $2, 'hello')
				`, taskID, tc.messageType); err != nil {
					t.Fatalf("insert task_message: %v", err)
				}
			}
			got, err := store.LoadTaskCompletionContext(ctx, taskID)
			if err != nil {
				t.Fatalf("load completion context: %v", err)
			}
			if got.SilentRun != tc.wantSilent {
				t.Fatalf("SilentRun = %v, want %v", got.SilentRun, tc.wantSilent)
			}
		})
	}
}

func insertSilentRunTask(t *testing.T, pool *pgxpool.Pool, agentID, issueID, runtimeID pgtype.UUID) pgtype.UUID {
	t.Helper()
	var taskID pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at)
		VALUES ($1, $2, $3, 'running', now() - interval '1 minute')
		RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	return taskID
}
