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
//
// DispatchRevision is the parallel seam for a GateRevise decision: instead of
// running one check command, it re-dispatches the worker's build skill so the
// agent can fix whatever failed. Optional the same way — a nil dispatcher (or
// a config missing an agent/skill) leaves the caps tracker updated but does
// not re-dispatch anything.
//
// DispatchJudge is the seam for a judge-type check: instead of a runnable
// command, it sends the judge agent the rubric and asks for a structured
// pass/revise verdict. It is sent to cfg.JudgeAgentID (or cfg.AgentID as a
// self-graded fallback), never assumed to be the same session as the worker
// — the gate does not enforce blindness, but the caller should route a real
// judge agent here for the verdict to mean anything.
type CheckDispatcher interface {
	DispatchCheck(ctx context.Context, d CheckDispatch) error
	DispatchRevision(ctx context.Context, d RevisionDispatch) error
	DispatchJudge(ctx context.Context, d JudgeDispatch) error
}

// JudgeDispatch is one judge check the engine asks a judge agent to score.
type JudgeDispatch struct {
	AgentID   string // judge agent whose runtime scores the check
	IssueID   string // issue the loop is building
	Gate      string // stable per-gate key (the workflow id)
	Round     int32  // loop round the check belongs to
	CheckID   string // the verification id from loop.yaml, echoed back on report
	Rubric    string // what the judge scores the delivered work against
	SkillName string // the skill dispatched to run the judgment
}

// RevisionDispatch is one round's worth of failed checks the engine asks the
// worker agent to fix by re-running its build skill.
type RevisionDispatch struct {
	AgentID   string         // worker agent whose runtime re-runs the build
	IssueID   string         // issue the loop is building
	Gate      string         // stable per-gate key (the workflow id)
	Round     int32          // the new round this revision starts
	SkillName string         // the skill to re-dispatch (Compile sets the build skill)
	Failures  []CheckOutcome // the outcomes that triggered this revision
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

// DispatchRevision enqueues a quick_create task asking the worker agent to
// re-run its build skill and fix the checks that failed, starting a new
// round. It resolves the agent's runtime the same way DispatchCheck does and
// fails loudly rather than silently dropping a revision the caps tracker has
// already counted.
func (t *TaskDispatcher) DispatchRevision(ctx context.Context, d RevisionDispatch) error {
	if d.SkillName == "" {
		return fmt.Errorf("dispatch revision: empty skill name")
	}
	agentID, err := util.ParseUUID(d.AgentID)
	if err != nil {
		return fmt.Errorf("dispatch revision: parse agent id: %w", err)
	}
	agent, err := t.queries.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("dispatch revision: load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("dispatch revision: agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return fmt.Errorf("dispatch revision: agent has no runtime")
	}

	contextJSON, err := json.Marshal(map[string]any{
		"type":         "quick_create",
		"prompt":       buildRevisionPrompt(d),
		"workspace_id": util.UUIDToString(agent.WorkspaceID),
		// Structured loop_revision fields, mirroring loop_check: bookkeeping
		// the daemon ignores today, pinned for a later deterministic handler.
		"loop_revision": map[string]any{
			"issue_id": d.IssueID,
			"gate":     d.Gate,
			"round":    d.Round,
		},
	})
	if err != nil {
		return fmt.Errorf("dispatch revision: marshal context: %w", err)
	}

	if _, err := t.queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   agentID,
		RuntimeID: agent.RuntimeID,
		Priority:  0,
		Context:   contextJSON,
	}); err != nil {
		return fmt.Errorf("dispatch revision: enqueue task: %w", err)
	}
	return nil
}

// DispatchJudge enqueues a quick_create task asking the judge agent to score
// the delivered work against the check's rubric and report a structured
// verdict back. It resolves the agent's runtime the same way DispatchCheck
// does and fails loudly if the judge agent is archived or has no runtime — a
// judge check with nowhere to run must not be silently dropped, or the gate
// would wait forever.
func (t *TaskDispatcher) DispatchJudge(ctx context.Context, d JudgeDispatch) error {
	if d.Rubric == "" {
		return fmt.Errorf("dispatch judge: empty rubric")
	}
	agentID, err := util.ParseUUID(d.AgentID)
	if err != nil {
		return fmt.Errorf("dispatch judge: parse agent id: %w", err)
	}
	agent, err := t.queries.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("dispatch judge: load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("dispatch judge: agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return fmt.Errorf("dispatch judge: agent has no runtime")
	}

	contextJSON, err := json.Marshal(map[string]any{
		"type":         "quick_create",
		"prompt":       buildJudgePrompt(d),
		"workspace_id": util.UUIDToString(agent.WorkspaceID),
		// Structured loop_judge fields, mirroring loop_check: bookkeeping the
		// daemon ignores today, but the ingress matches a reported verdict
		// back to this check on (issue_id, gate, round, check_id).
		"loop_judge": map[string]any{
			"issue_id": d.IssueID,
			"gate":     d.Gate,
			"round":    d.Round,
			"check_id": d.CheckID,
		},
	})
	if err != nil {
		return fmt.Errorf("dispatch judge: marshal context: %w", err)
	}

	if _, err := t.queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   agentID,
		RuntimeID: agent.RuntimeID,
		Priority:  0,
		Context:   contextJSON,
	}); err != nil {
		return fmt.Errorf("dispatch judge: enqueue task: %w", err)
	}
	return nil
}

// buildJudgePrompt renders the instruction the judge agent's model sees. The
// gate decides on the reported structured verdict alone, so the prompt makes
// the rubric the only thing that matters and tells the judge to read the
// issue's own delivered work (not to trust a worker's summary of it) before
// scoring — the same "blind, evidence-based" standard the delivery gate
// programmatic checks get from a raw exit code.
func buildJudgePrompt(d JudgeDispatch) string {
	return fmt.Sprintf(
		"Score this loop delivery against the rubric below (gate %s, round %d, check %q). "+
			"Read the actual delivered work on the issue yourself — do not take the builder's own summary as proof. "+
			"Then report your verdict back to the loop gate via the report_loop_judge tool with issue_id, gate, round, check_id=%q, "+
			"pass (true only if the rubric is fully met), and blocking_issues (the specific, concrete reasons if pass is false; empty if true).\n\n"+
			"Rubric:\n%s",
		d.Gate, d.Round, d.CheckID, d.CheckID, d.Rubric,
	)
}

// buildRevisionPrompt renders the instruction the worker agent's model sees
// on a revision: which checks failed and with what exit code, so the agent
// does not have to re-discover the failure before it can fix it.
func buildRevisionPrompt(d RevisionDispatch) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Loop gate %s failed and needs round %d: the previous round's verification checks did not all pass. Fix the issue, then let the loop re-verify.\n\nFailed checks:\n", d.Gate, d.Round)
	for _, o := range d.Failures {
		if o.Ran && o.ExitCode != 0 {
			fmt.Fprintf(&b, "  - exit %d: %s\n", o.ExitCode, strings.Join(o.Argv, " "))
		}
	}
	return b.String()
}
