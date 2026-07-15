package wakeup

// FIR-2679 Spor 1b: a wakeup may pin its woken run to a cheaper model so a pure
// verification check ("is CI green?") never fires Opus. These tests prove the
// create path persists a valid model and rejects an unknown one. Dispatch-time
// application of the override onto the queued task is exercised through the
// autopilotmodel.SetOnTask path, which is covered by the autopilot model tests.
//
// Skips cleanly when no test DB is reachable, same pattern as service_db_test.go.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/autopilotmodel"
)

func TestCreatePersistsModelOverride(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	if _, err := wkPool.Exec(ctx, `DELETE FROM cerebro_agent_wakeup WHERE issue_id = $1 AND agent_id = $2`, wkIssueID, wkAgentID); err != nil {
		t.Fatalf("clear wakeups: %v", err)
	}

	row, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:       wkAgentID,
		IssueID:       wkIssueID,
		Prompt:        "is CI green?",
		TriggerType:   TriggerTime,
		FireAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ModelOverride: autopilotmodel.ModelHaiku,
	})
	if err != nil {
		t.Fatalf("create with model override: %v", err)
	}
	if row.ModelOverride != autopilotmodel.ModelHaiku {
		t.Fatalf("model_override = %q, want %q", row.ModelOverride, autopilotmodel.ModelHaiku)
	}

	// Round-trips through a fresh read, not just the RETURNING row.
	got, err := svc.Get(ctx, wkWorkspaceID, row.ID)
	if err != nil {
		t.Fatalf("get wakeup: %v", err)
	}
	if got.ModelOverride != autopilotmodel.ModelHaiku {
		t.Fatalf("reloaded model_override = %q, want %q", got.ModelOverride, autopilotmodel.ModelHaiku)
	}
}

func TestCreateRejectsUnknownModel(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	if _, err := wkPool.Exec(ctx, `DELETE FROM cerebro_agent_wakeup WHERE issue_id = $1 AND agent_id = $2`, wkIssueID, wkAgentID); err != nil {
		t.Fatalf("clear wakeups: %v", err)
	}

	_, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:       wkAgentID,
		IssueID:       wkIssueID,
		Prompt:        "bad model",
		TriggerType:   TriggerTime,
		FireAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ModelOverride: "gpt-4",
	})
	// The fixture agent runs on a 'claude' runtime, so Create checks against the
	// claude catalog and reports the sharper "not a claude model" — an unknown ID
	// and a real-but-wrong-provider ID both land here (FIR-3287).
	if !errors.Is(err, autopilotmodel.ErrModelNotOnProvider) {
		t.Fatalf("create with unknown model: err = %v, want ErrModelNotOnProvider", err)
	}
}

// FIR-3287: a wakeup pinned an Anthropic model on an agent whose runtime is
// Codex. Create accepted it (the registry knew only Anthropic IDs and never
// looked at the runtime), and the woken run then died on a 400 from the Codex
// CLI — with the wakeup being the only thing that would have retried it.
//
// Create must now reject it against the runtime that would run it.
func TestCreateRejectsModelFromAnotherProvider(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()

	// Dedicated codex runtime + agent so the shared claude fixture is untouched.
	var codexRuntime, codexAgent pgtype.UUID
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		 VALUES ($1, 'Codex Runtime', 'local', 'codex', 'online') RETURNING id`,
		wkWorkspaceID).Scan(&codexRuntime); err != nil {
		t.Fatalf("create codex runtime: %v", err)
	}
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		 VALUES ($1, 'Codex Agent', 'local', $2) RETURNING id`,
		wkWorkspaceID, codexRuntime).Scan(&codexAgent); err != nil {
		t.Fatalf("create codex agent: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		wkPool.Exec(bg, `DELETE FROM cerebro_agent_wakeup WHERE agent_id = $1`, codexAgent)
		wkPool.Exec(bg, `DELETE FROM agent WHERE id = $1`, codexAgent)
		wkPool.Exec(bg, `DELETE FROM agent_runtime WHERE id = $1`, codexRuntime)
	})

	_, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:       codexAgent,
		IssueID:       wkIssueID,
		Prompt:        "is CI green?",
		TriggerType:   TriggerTime,
		FireAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ModelOverride: autopilotmodel.ModelHaiku,
	})
	if !errors.Is(err, autopilotmodel.ErrModelNotOnProvider) {
		t.Fatalf("create anthropic model on codex runtime: err = %v, want ErrModelNotOnProvider", err)
	}
}

// FIR-3287 second half: Create validated against the runtime the agent had at
// the time, but an agent can be re-pointed at another provider before the wakeup
// fires. Dispatch must not stamp a model that runtime cannot run — the override
// is only a cost optimisation, so the run proceeds on the agent's own model.
func TestDispatchDropsModelOverrideForeignToRuntime(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	if _, err := wkPool.Exec(ctx, `DELETE FROM cerebro_agent_wakeup WHERE issue_id = $1 AND agent_id = $2`, wkIssueID, wkAgentID); err != nil {
		t.Fatalf("clear wakeups: %v", err)
	}

	// Created while the agent is on its claude runtime — a valid pin today.
	row, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:       wkAgentID,
		IssueID:       wkIssueID,
		Prompt:        "is CI green?",
		TriggerType:   TriggerTime,
		FireAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ModelOverride: autopilotmodel.ModelHaiku,
	})
	if err != nil {
		t.Fatalf("create with model override: %v", err)
	}

	// The agent is moved to a Codex runtime before the wakeup fires.
	var codexRuntime pgtype.UUID
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		 VALUES ($1, 'Codex Runtime (repoint)', 'local', 'codex', 'online') RETURNING id`,
		wkWorkspaceID).Scan(&codexRuntime); err != nil {
		t.Fatalf("create codex runtime: %v", err)
	}
	if _, err := wkPool.Exec(ctx, `UPDATE agent SET runtime_id = $1 WHERE id = $2`, codexRuntime, wkAgentID); err != nil {
		t.Fatalf("repoint agent at codex runtime: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		wkPool.Exec(bg, `UPDATE agent SET runtime_id = $1 WHERE id = $2`, wkRuntimeID, wkAgentID)
		wkPool.Exec(bg, `DELETE FROM agent_task_queue WHERE agent_id = $1`, wkAgentID)
		wkPool.Exec(bg, `DELETE FROM agent_runtime WHERE id = $1`, codexRuntime)
	})

	if err := svc.dispatch(ctx, row); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	var taskModel pgtype.Text
	if err := wkPool.QueryRow(ctx, `
		SELECT model_override
		FROM agent_task_queue
		WHERE agent_id = $1 AND issue_id = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, wkAgentID, wkIssueID).Scan(&taskModel); err != nil {
		t.Fatalf("load task: %v", err)
	}
	if taskModel.Valid && taskModel.String != "" {
		t.Fatalf("task model_override = %q, want empty (the codex runtime cannot run %q)", taskModel.String, autopilotmodel.ModelHaiku)
	}
}

func TestCreateEmptyModelOverrideStaysEmpty(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	if _, err := wkPool.Exec(ctx, `DELETE FROM cerebro_agent_wakeup WHERE issue_id = $1 AND agent_id = $2`, wkIssueID, wkAgentID); err != nil {
		t.Fatalf("clear wakeups: %v", err)
	}

	row, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:     wkAgentID,
		IssueID:     wkIssueID,
		Prompt:      "no model override",
		TriggerType: TriggerTime,
		FireAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatalf("create without model override: %v", err)
	}
	if row.ModelOverride != "" {
		t.Fatalf("model_override = %q, want empty", row.ModelOverride)
	}
}

// TestDispatchAppliesModelOverrideToTask proves the end-to-end goal of Spor 1b:
// a wakeup carrying a cheaper model dispatches a run whose task is pinned to that
// model, so a pure verification wakeup never fires Opus.
func TestDispatchAppliesModelOverrideToTask(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	if _, err := wkPool.Exec(ctx, `DELETE FROM cerebro_agent_wakeup WHERE issue_id = $1 AND agent_id = $2`, wkIssueID, wkAgentID); err != nil {
		t.Fatalf("clear wakeups: %v", err)
	}

	row, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:       wkAgentID,
		IssueID:       wkIssueID,
		Prompt:        "is CI green?",
		TriggerType:   TriggerTime,
		FireAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ModelOverride: autopilotmodel.ModelHaiku,
	})
	if err != nil {
		t.Fatalf("create wakeup: %v", err)
	}
	if err := svc.dispatch(ctx, row); err != nil {
		t.Fatalf("dispatch wakeup: %v", err)
	}
	t.Cleanup(func() {
		wkPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1 AND context->>'type' = 'wakeup'`, wkIssueID)
	})

	var taskModel pgtype.Text
	if err := wkPool.QueryRow(ctx, `
		SELECT model_override
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND context->>'type' = 'wakeup'
		ORDER BY created_at DESC
		LIMIT 1
	`, wkIssueID, wkAgentID).Scan(&taskModel); err != nil {
		t.Fatalf("load wakeup task: %v", err)
	}
	if !taskModel.Valid || taskModel.String != autopilotmodel.ModelHaiku {
		t.Fatalf("task model_override = %v, want %q", taskModel, autopilotmodel.ModelHaiku)
	}
}
