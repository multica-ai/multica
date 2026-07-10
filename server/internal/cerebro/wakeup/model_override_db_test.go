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
	if !errors.Is(err, autopilotmodel.ErrUnknownModel) {
		t.Fatalf("create with unknown model: err = %v, want ErrUnknownModel", err)
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
