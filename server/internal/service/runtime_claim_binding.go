package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RuntimeBindingAllowsClaim reports whether runtimeID may deliver work for the
// agent. The agent's own binding always counts. Smart agents also admit a live
// personal binding or the task originator's own machine (CLI match / default
// user runtime). Fixed agents admit only a retained grant whose runtime and
// task provenance still match the exact row marked by a batch migration.
func RuntimeBindingAllowsClaim(ctx context.Context, q *db.Queries, agent db.Agent, task db.AgentTaskQueue) (bool, error) {
	runtimeID := task.RuntimeID
	if !runtimeID.Valid {
		return false, nil
	}
	if task.AgentID != agent.ID {
		return false, nil
	}
	inherited, inheritErr := stickyRuntimeInheritanceAllowed(ctx, q, task)
	if inheritErr != nil {
		return false, inheritErr
	}
	if inherited {
		return true, nil
	}
	if agent.ShareRuntime {
		return agent.RuntimeID == runtimeID || taskHasFixedRuntimeRetention(task), nil
	}
	// Any member's saved personal configuration binding this agent to the
	// machine authorizes delivery, exactly like the claim SQL's smart clause.
	bound, err := q.AgentHasUserRuntimeBinding(ctx, db.AgentHasUserRuntimeBindingParams{
		AgentID:   agent.ID,
		RuntimeID: runtimeID,
	})
	if err != nil {
		return false, err
	}
	if bound {
		return true, nil
	}
	runtime, err := q.GetAgentRuntime(ctx, runtimeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if runtime.WorkspaceID != agent.WorkspaceID {
		return false, nil
	}
	// The template owner's own machine stays a current binding for smart
	// agents, matching the claim SQL: a sticky follow-up keeps the run's
	// machine even though the joining member triggered the enqueue.
	return agent.OwnerID.Valid && runtime.OwnerID == agent.OwnerID ||
		taskOriginatorOwnsRuntime(runtime, task), nil
}

// PrivateRuntimePermitsAgent is the Go-side copy of ClaimAgentTask's private
// runtime owner fence. The template owner's own machine (or one explicitly
// shared with them) always drives their agent, whoever triggered the run —
// this is what keeps a sticky "current task" follow-up deliverable on the
// run's machine after another member joins the thread. A smart agent also
// admits a personal configuration saved by the runtime's owner and the task
// originator's own machine, matching the SQL originator-owned clause; a
// retained fixed task is authorized only by a grant bound to that task and
// runtime, never by another task on the same agent/runtime.
func PrivateRuntimePermitsAgent(ctx context.Context, q *db.Queries, agent db.Agent, task db.AgentTaskQueue, runtime db.AgentRuntime) (bool, error) {
	if task.AgentID != agent.ID || runtime.ID != task.RuntimeID || runtime.WorkspaceID != agent.WorkspaceID {
		return false, nil
	}
	inherited, inheritErr := stickyRuntimeInheritanceAllowed(ctx, q, task)
	if inheritErr != nil {
		return false, inheritErr
	}
	if inherited {
		return true, nil
	}
	if runtime.Visibility != "private" || !runtime.OwnerID.Valid {
		return true, nil
	}
	if !agent.OwnerID.Valid {
		return false, nil
	}
	if agent.OwnerID == runtime.OwnerID || slices.Contains(runtime.SharedWith, agent.OwnerID) {
		return true, nil
	}
	if !agent.ShareRuntime {
		bound, err := q.AgentHasUserRuntimeBindingForUser(ctx, db.AgentHasUserRuntimeBindingForUserParams{
			AgentID:   agent.ID,
			RuntimeID: runtime.ID,
			UserID:    runtime.OwnerID,
		})
		if err != nil {
			return false, err
		}
		if bound {
			return true, nil
		}
		return taskOriginatorOwnsRuntime(runtime, task), nil
	}
	// A fixed agent on a foreign private runtime stays a denial the handler
	// settles before delivery; the claim SQL's retention marker handles the
	// batch-migration shape.
	return false, nil
}

// taskOriginatorOwnsRuntime is the Go copy of ClaimAgentTask's originator-owned
// smart-runtime clause. A queued task already routed to the trigger user's
// machine remains deliverable even when that user never saved a personal
// agent_user_runtime_config row.
func taskOriginatorOwnsRuntime(runtime db.AgentRuntime, task db.AgentTaskQueue) bool {
	if !runtime.OwnerID.Valid {
		return false
	}
	return task.OriginatorUserID.Valid && runtime.OwnerID == task.OriginatorUserID
}

// stickyRuntimeInheritanceAllowed reports whether the task carries the
// completion-replay marker vouching that the run it continues claimed and
// delivered on this machine. The SQL fence keeps a copy of this check; the
// source task is re-read here so a stale or forged marker cannot outlive the
// run's own authorization.
func stickyRuntimeInheritanceAllowed(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (bool, error) {
	if len(task.Context) == 0 || !task.RuntimeID.Valid {
		return false, nil
	}
	var marker struct {
		Inherited bool   `json:"sticky_runtime_inherited"`
		RuntimeID string `json:"sticky_runtime_inherited_runtime_id"`
		TaskID    string `json:"sticky_runtime_inherited_task_id"`
		SourceID  string `json:"sticky_runtime_source_task_id"`
	}
	if json.Unmarshal(task.Context, &marker) != nil || !marker.Inherited {
		return false, nil
	}
	if marker.RuntimeID != util.UUIDToString(task.RuntimeID) || marker.TaskID != util.UUIDToString(task.ID) {
		return false, nil
	}
	sourceID, err := util.ParseUUID(marker.SourceID)
	if err != nil {
		return false, nil
	}
	source, err := q.GetAgentTask(ctx, sourceID)
	if err != nil {
		return false, nil
	}
	return source.RuntimeID == task.RuntimeID && source.DispatchedAt.Valid, nil
}

func taskHasFixedRuntimeRetention(task db.AgentTaskQueue) bool {
	if len(task.Context) == 0 {
		return false
	}
	var marker struct {
		Retained  bool   `json:"fixed_runtime_batch_retained"`
		RuntimeID string `json:"fixed_runtime_batch_retained_runtime_id"`
		TaskID    string `json:"fixed_runtime_batch_retained_task_id"`
	}
	if json.Unmarshal(task.Context, &marker) != nil || !marker.Retained {
		return false
	}
	runtimeID, err := util.ParseUUID(marker.RuntimeID)
	if err != nil || runtimeID != task.RuntimeID {
		return false
	}
	taskID, err := util.ParseUUID(marker.TaskID)
	return err == nil && taskID == task.ID
}
