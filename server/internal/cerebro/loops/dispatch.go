package loops

// dispatch.go is the egress half of the loop check transport: it sends an
// enqueued check out to the worker agent's runtime as a task so the runtime can
// run the argv in the checkout where the build happened and (a later slice)
// report the exit code back. It mirrors the workflow engine's
// actionRunSkill -> CreateQuickCreateTask pattern: a quick_create task whose
// context carries a prompt plus the structured loop_check fields, so no
// daemon-side change is needed to run it today while the structured fields pin
// the contract a deterministic daemon handler can target later.
//
// Dispatch never weakens the gate. The gate still decides only on the exit code
// the runtime reports — never the agent's opinion — and each check is sent
// exactly once (Store.PendingDispatch / MarkDispatched), so a gate that is
// re-evaluated while checks are in flight does not re-send them.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CheckDispatch is one check the engine asks a runtime to run.
type CheckDispatch struct {
	AgentID string   // worker agent whose runtime runs the check
	IssueID string   // issue the loop is building
	Gate    string   // stable per-gate key (the workflow id)
	Round   int32    // loop round the check belongs to
	Argv    []string // the check command, argv form (never a shell string)
}

// CheckDispatcher sends a single enqueued check out to a runtime. It is the
// seam GateEvaluator calls when a gate enqueues new checks; the concrete
// TaskDispatcher enqueues an agent task. Optional and nil-safe on the
// evaluator: without it the gate only records pending checks (the pre-dispatch
// behavior).
type CheckDispatcher interface {
	DispatchCheck(ctx context.Context, d CheckDispatch) error
}

// DispatchQueries is the narrow slice of the upstream issue queries the task
// dispatcher needs: resolve the agent (for its runtime + workspace) and enqueue
// the task. Keeping it an interface mirrors workflows.IssueActions and lets the
// dispatcher be unit-tested without a live DB.
type DispatchQueries interface {
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	CreateQuickCreateTask(ctx context.Context, arg db.CreateQuickCreateTaskParams) (db.AgentTaskQueue, error)
}

// TaskDispatcher dispatches a check by enqueuing an agent task on the worker
// agent's runtime, reusing the quick_create task shape the daemon already runs.
type TaskDispatcher struct {
	queries DispatchQueries
}

// NewTaskDispatcher builds a TaskDispatcher over the given issue queries.
func NewTaskDispatcher(queries DispatchQueries) *TaskDispatcher {
	return &TaskDispatcher{queries: queries}
}

// DispatchCheck enqueues a quick_create task asking the worker agent to run the
// check argv in its checkout and report the exit code back. It resolves the
// agent's runtime the same way actionRunSkill does and fails loudly if the
// agent is archived or has no runtime — a check with nowhere to run must not be
// silently dropped, or the gate would wait forever.
func (t *TaskDispatcher) DispatchCheck(ctx context.Context, d CheckDispatch) error {
	if len(d.Argv) == 0 {
		return fmt.Errorf("dispatch check: empty argv")
	}
	agentID, err := util.ParseUUID(d.AgentID)
	if err != nil {
		return fmt.Errorf("dispatch check: parse agent id: %w", err)
	}
	agent, err := t.queries.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("dispatch check: load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("dispatch check: agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return fmt.Errorf("dispatch check: agent has no runtime")
	}

	contextJSON, err := json.Marshal(map[string]any{
		"type":         "quick_create",
		"prompt":       buildCheckPrompt(d),
		"workspace_id": util.UUIDToString(agent.WorkspaceID),
		// Structured loop_check fields: bookkeeping the daemon ignores today,
		// but the ingress matches a reported exit code back to this check on
		// (issue_id, gate, round, argv).
		"loop_check": map[string]any{
			"issue_id": d.IssueID,
			"gate":     d.Gate,
			"round":    d.Round,
			"argv":     d.Argv,
		},
	})
	if err != nil {
		return fmt.Errorf("dispatch check: marshal context: %w", err)
	}

	if _, err := t.queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   agentID,
		RuntimeID: agent.RuntimeID,
		Priority:  0,
		Context:   contextJSON,
	}); err != nil {
		return fmt.Errorf("dispatch check: enqueue task: %w", err)
	}
	return nil
}

// buildCheckPrompt renders the instruction the worker agent's model sees. The
// gate decides on the reported exit code alone, so the prompt's only job is to
// get the exact argv run and its exit code reported back unchanged.
func buildCheckPrompt(d CheckDispatch) string {
	return fmt.Sprintf(
		"Run this loop verification check in the repository checkout, exactly as given, and report its exit code back to the loop gate (gate %s, round %d) without judging the result yourself:\n\n    %s",
		d.Gate, d.Round, strings.Join(d.Argv, " "),
	)
}
