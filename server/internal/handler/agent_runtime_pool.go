package handler

import (
	"context"
	"fmt"
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

// AgentRuntimeBindingDTO is one entry of an agent's ordered runtime pool
// (SE-37711 / SE-37664). Priority 0 is the highest-priority runtime and mirrors
// the legacy agent.runtime_id projection; the dispatcher walks the list in
// ascending priority and routes work to the first live, non-circuit-held
// runtime. The pool is the multi-runtime binding surface; the flat runtime_id /
// runtime_bound fields stay on the response for installed clients.
type AgentRuntimeBindingDTO struct {
	RuntimeID string `json:"runtime_id"`
	Priority  int32  `json:"priority"`
}

// runtimePoolIntent captures how an update request touches the runtime pool.
// The legacy runtime_id pointer (omitted / "" clear / value set) and the
// ordered runtime_ids array are mutually exclusive and both collapse into one
// of these so the rest of UpdateAgent reasons about a single notion of the
// runtime the agent lands on.
type runtimePoolIntent int

const (
	runtimePoolUntouched runtimePoolIntent = iota
	runtimePoolSet
	runtimePoolClear
)

// resolveAgentRuntimePool validates an ordered list of runtime id strings into
// the runtimes they name, in the given order, applying every binding invariant
// (SE-37711 §2.6). On any failure it writes the HTTP error and returns ok=false.
//
//   - each id must be a well-formed UUID that names a runtime in this workspace;
//   - a private runtime is bindable only by its owner (same gate as a single
//     runtime_id — an update must not be an end-run onto someone else's runtime);
//   - the list may not repeat a runtime;
//   - a pool of more than one runtime must span at least two provider families,
//     because a same-provider pool cannot survive a provider-wide quota outage —
//     the whole point of the fallback (mix of providers is mandatory).
func (h *Handler) resolveAgentRuntimePool(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, member db.Member, ids []string) ([]db.AgentRuntime, bool) {
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "runtime_ids must contain at least one runtime")
		return nil, false
	}

	seen := make(map[string]struct{}, len(ids))
	providers := make(map[string]struct{}, len(ids))
	runtimes := make([]db.AgentRuntime, 0, len(ids))
	for i, raw := range ids {
		rid, ok := parseUUIDOrBadRequest(w, raw, fmt.Sprintf("runtime_ids[%d]", i))
		if !ok {
			return nil, false
		}
		key := uuidToString(rid)
		if _, dup := seen[key]; dup {
			writeError(w, http.StatusBadRequest, "runtime_ids must not repeat a runtime")
			return nil, false
		}
		seen[key] = struct{}{}

		rt, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
			ID:          rid,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid runtime_id")
			return nil, false
		}
		if !canUseRuntimeForAgent(member, rt) {
			writeError(w, http.StatusForbidden, "this runtime is private; only its owner can bind agents to it")
			return nil, false
		}
		runtimes = append(runtimes, rt)
		providers[rt.Provider] = struct{}{}
	}

	if len(runtimes) > 1 && len(providers) < 2 {
		writeError(w, http.StatusBadRequest, "a runtime pool with more than one runtime must span at least two provider families")
		return nil, false
	}

	return runtimes, true
}

// writeAgentRuntimePool replaces an agent's binding rows with the given ordered
// pool inside the caller's transaction. The FOR UPDATE lock serializes the swap
// against runtime teardown and a concurrent pool edit; the delete + re-insert is
// therefore never observed half-applied. It does NOT touch agent.runtime_id —
// the caller keeps that priority-0 projection in step through its own
// CreateAgent/UpdateAgent parameters (or ClearAgentRuntimeID on a clear).
func writeAgentRuntimePool(ctx context.Context, qtx *db.Queries, wsUUID, agentID pgtype.UUID, runtimes []db.AgentRuntime) error {
	if _, err := qtx.ListAgentRuntimeBindingsForAgentForUpdate(ctx, agentID); err != nil {
		return err
	}
	if err := qtx.DeleteAgentRuntimeBindingsForAgent(ctx, agentID); err != nil {
		return err
	}
	for i, rt := range runtimes {
		if _, err := qtx.CreateAgentRuntimeBinding(ctx, db.CreateAgentRuntimeBindingParams{
			WorkspaceID: wsUUID,
			AgentID:     agentID,
			RuntimeID:   rt.ID,
			Priority:    int32(i),
		}); err != nil {
			return err
		}
	}
	return nil
}

// clearAgentRuntimePoolWithQueries empties an agent's runtime pool and nulls the
// legacy runtime_id projection inside the caller's transaction, leaving the
// agent unbound: it keeps its configuration and history and needs a new runtime
// before it can run again (MUL-5559, invariant I14). An unbound agent cannot
// run, so its autopilots are paused in the same transaction — the same
// consequence runtime teardown applies when a pool empties, so a user-initiated
// clear and an automatic one converge. The FOR UPDATE lock serializes the swap
// against runtime teardown and a concurrent edit. Returns the refreshed agent
// row. The caller commits, so the pool clear, the projection null, and any
// sibling writes (e.g. UpdateAgent) are never observed half-applied (F2).
func clearAgentRuntimePoolWithQueries(ctx context.Context, qtx *db.Queries, agentID pgtype.UUID) (db.Agent, error) {
	if _, err := qtx.ListAgentRuntimeBindingsForAgentForUpdate(ctx, agentID); err != nil {
		return db.Agent{}, err
	}
	if err := qtx.DeleteAgentRuntimeBindingsForAgent(ctx, agentID); err != nil {
		return db.Agent{}, err
	}
	cleared, err := qtx.ClearAgentRuntimeID(ctx, agentID)
	if err != nil {
		return db.Agent{}, err
	}
	if _, err := qtx.PauseAutopilotsByUnboundAgents(ctx, []pgtype.UUID{agentID}); err != nil {
		return db.Agent{}, err
	}
	return cleared, nil
}

// enrichAgentResponseWithRuntimeBindings loads an agent's ordered runtime pool
// onto the response. When no binding rows exist yet (a legacy agent created
// before pools, or one whose backfill has not run) the flat runtime_id is
// surfaced as a synthesized singleton so the ordered view always agrees with
// what the dispatcher resolves; an unbound agent stays an empty pool.
func (h *Handler) enrichAgentResponseWithRuntimeBindings(ctx context.Context, resp *AgentResponse, agentID pgtype.UUID) error {
	rows, err := h.Queries.ListAgentRuntimeBindings(ctx, agentID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		if resp.RuntimeBound && resp.RuntimeID != "" {
			resp.RuntimeBindings = []AgentRuntimeBindingDTO{{RuntimeID: resp.RuntimeID, Priority: 0}}
		} else {
			resp.RuntimeBindings = []AgentRuntimeBindingDTO{}
		}
		return nil
	}
	out := make([]AgentRuntimeBindingDTO, len(rows))
	for i, b := range rows {
		out[i] = AgentRuntimeBindingDTO{
			RuntimeID: uuidToString(b.RuntimeID),
			Priority:  b.Priority,
		}
	}
	resp.RuntimeBindings = out
	return nil
}
