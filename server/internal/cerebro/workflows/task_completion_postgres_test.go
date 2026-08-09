package workflows

import (
	"context"
	"encoding/json"
	"strings"
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

func TestNotifyCompletionWarningCreatesOneInboxItemForIssueCreator(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	fixture := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, fixture, "Hook warning", "in_progress", 2, pgtype.UUID{})

	var runtimeID, agentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		VALUES ($1, 'Hook warning runtime', 'cloud', 'codex', 'online') RETURNING id
	`, fixture.workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, status)
		VALUES ($1, 'Hook warning agent', 'cloud', $2, 'idle') RETURNING id
	`, fixture.workspaceID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	taskID := insertSilentRunTask(t, pool, agentID, issueID, runtimeID)
	store := NewPostgresTaskCompletionStore(pool)
	completion, err := store.LoadTaskCompletionContext(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	warning := TaskCompletionGuidance{
		Code: "workflow_gate_rejected", Requirement: "Require evidence before an agent run stops: Create a wakeup", Attempt: 2,
		HookID: "11111111-1111-1111-1111-111111111111", HookName: "Require evidence before an agent run stops",
	}
	if err := store.NotifyCompletionWarning(ctx, completion, warning); err != nil {
		t.Fatal(err)
	}
	if err := store.NotifyCompletionWarning(ctx, completion, warning); err != nil {
		t.Fatal(err)
	}

	var count int
	var title, body string
	var details []byte
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM inbox_item
		WHERE issue_id=$1 AND type='workflow_gate_rejected'
	`, issueID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT title, body, details FROM inbox_item
		WHERE issue_id=$1 AND type='workflow_gate_rejected' LIMIT 1
	`, issueID).Scan(&title, &body, &details); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(details, &decoded); err != nil {
		t.Fatal(err)
	}
	if count != 1 || title != "Workflow hook warning" || body != "The run completed with a warning from Require evidence before an agent run stops: Create a wakeup" || strings.Count(body, warning.HookName) != 1 || decoded["task_id"] != completion.TaskID || decoded["hook_name"] != warning.HookName {
		t.Fatalf("count=%d title=%q body=%q details=%#v", count, title, body, decoded)
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
