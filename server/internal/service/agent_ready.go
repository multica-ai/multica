package service

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentReadiness reports whether an agent can accept new work right now.
// "Ready" means archived_at IS NULL, runtime_id IS NOT NULL, and the bound
// runtime's status is 'online'. When not ready, reason describes which gate
// failed in language suitable for autopilot_run.failure_reason.
//
// err is non-nil only on DB lookup failure for the runtime row. Callers that
// treat a transient DB error as "do not skip" (the autopilot admission gate)
// should swallow it; callers that need a hard yes/no (the squad-leader
// pre-enqueue check in the handler) should fail closed.
//
// This is the single source of truth shared by:
//   - service.shouldSkipDispatch (autopilot admission gate)
//   - service.dispatchRunOnly    (squad-leader runtime check, MUL-2429)
//   - handler.isSquadLeaderReady (issue-assign / comment-trigger path)
//
// Keeping these aligned matters because the three paths can otherwise drift
// — e.g. one starts allowing "starting" runtimes while another doesn't, and
// the bug only surfaces when a user assigns the same squad through two
// different entry points. Touch this function, all three paths move together.
func AgentReadiness(ctx context.Context, q *db.Queries, agent db.Agent) (ready bool, reason string, err error) {
	if agent.ArchivedAt.Valid {
		return false, "agent is archived", nil
	}
	if !agent.RuntimeID.Valid {
		return false, "agent has no runtime bound", nil
	}
	rt, err := q.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return false, "", err
	}
	if rt.Status != "online" {
		return false, "agent runtime is " + rt.Status, nil
	}
	return true, "", nil
}
