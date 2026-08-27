package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Reclaiming a shared machine (public → private), the state half of #7571
// (MUL-6697): that PR stopped a private runtime from RUNNING another owner's
// agent, leaving bindings, queued work and Autopilots behind as a permanent
// "bound but never runs" state.
//
// Modelled on the runtime-delete cascade — snapshot, confirm, one transaction,
// one post-commit broadcast. Differences all follow from the runtime SURVIVING:
// only FOREIGN agents are affected (so the cancel passes agent ids with an EMPTY
// runtime_ids, or it would kill the owner's own work), task history stays pinned,
// and `kind='system'` carriers keep their binding instead of being deleted.
const (
	// Returned by the plain PATCH: flipping to private would affect agents that
	// are not the owner's, so the client must show the plan and confirm.
	runtimeVisibilityHasForeignAgentsCode = "runtime_visibility_has_foreign_agents"
	// Returned by the confirm endpoint when the affected set moved between
	// dialog-open and confirm. Zero writes.
	runtimeVisibilityPlanChangedCode = "runtime_visibility_plan_changed"
	// Mirrors runtime_delete_not_drained: the cancel left a non-terminal row, so
	// the transaction is abandoned rather than committing a half-revoked state.
	runtimeVisibilityNotDrainedCode = "runtime_visibility_not_drained"
)

// User-visible copy stored in agent_task_queue.error, next to the machine-readable
// failure_reason. RebindStrandedTaskError is exported because UpdateAgent writes it.
const (
	revokeUnboundTaskError  = "The runtime this agent was using was made private by its owner, so the agent was unbound. Bind the agent to a runtime you can use and retry."
	revokeRetainedTaskError = "The runtime this agent runs on was made private by its owner and no longer permits this agent. Ask the owner to share it again, or move the agent to another runtime."
	RebindStrandedTaskError = "The agent moved to another runtime before this task started, so it could no longer be claimed. Retry it to run on the agent's current runtime."
)

// runtimeRevokeAgentDTO is deliberately two fields. The plan is readable by
// whoever owns the MACHINE, who often does not own these agents and may have no
// right to read them — serialising them with agentToResponse handed a teammate's
// instructions, runtime_config, mcp_config and Composio allowlist to anyone who
// merely ATTEMPTED to make their own runtime private. Nothing to redact if
// nothing else is here.
type runtimeRevokeAgentDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// runtimeRevokePlan is what the revoke will do to the foreign agents bound here.
type runtimeRevokePlan struct {
	// UnboundAgents: active user agents losing their binding — the set the user
	// confirms via expected_active_agent_ids.
	UnboundAgents []db.Agent
	// ArchivedCount: foreign archived user agents. Unbound too (still the user's
	// data), but kept out of the confirmed set — invisible in the UI, and including
	// them would make every older client's snapshot mismatch.
	ArchivedCount int
	// RetainedSystemCount: foreign `kind='system'` carriers (Agent Builder). They
	// KEEP their binding — unbound they are unrepairable, deleted they take the
	// user's conversation — and admission refuses them instead.
	RetainedSystemCount int
	// MikaAffected: Mika is kind='user' and there is one per workspace, so
	// unbinding her stops her for everyone. The dialog must say so.
	MikaAffected bool
}

func (p runtimeRevokePlan) empty() bool {
	return len(p.UnboundAgents) == 0 && p.ArchivedCount == 0 && p.RetainedSystemCount == 0
}

// splitForeignAgents turns the locked foreign-agent set into the plan plus the id
// lists the teardown acts on. Shared by both entry points so they cannot disagree.
func splitForeignAgents(agents []db.Agent) (plan runtimeRevokePlan, unboundIDs, retainedIDs []pgtype.UUID) {
	for _, a := range agents {
		if a.Kind == "system" {
			plan.RetainedSystemCount++
			retainedIDs = append(retainedIDs, a.ID)
			continue
		}
		unboundIDs = append(unboundIDs, a.ID)
		if a.ArchivedAt.Valid {
			plan.ArchivedCount++
			continue
		}
		plan.UnboundAgents = append(plan.UnboundAgents, a)
		if a.SystemKey.Valid && a.SystemKey.String == service.MikaSystemKey {
			plan.MikaAffected = true
		}
	}
	return plan, unboundIDs, retainedIDs
}

// errRuntimeRevokeNeedsConfirmation: the flip affects agents that are not the
// owner's, so the plain PATCH must refuse and hand the plan to the user.
var errRuntimeRevokeNeedsConfirmation = errors.New("runtime visibility change needs confirmation")

// errRuntimeVisibilityOwnerChanged: the runtime changed hands while this request
// waited for the lock, so the caller's owner-only permission no longer holds.
var errRuntimeVisibilityOwnerChanged = errors.New("runtime owner changed before the visibility write")

// makeRuntimePrivateIfUnaffected is the "nothing to tear down" flip, and it is
// transactional even though it writes one column.
//
// Read-then-update is racy in a way the lock modes hide: a bind holds FOR KEY
// SHARE on the runtime (FK validation) and a plain non-key UPDATE takes FOR NO KEY
// UPDATE, which do NOT conflict — so a bind passing its check against the `public`
// snapshot and this PATCH could both commit, leaving a private runtime with a
// foreign agent and no teardown. LockAgentRuntime (FOR UPDATE) conflicts with that
// KEY SHARE, and revalidateRuntimeForBind re-reads after the wait, so both
// orderings end safely: bind-first → we recount and see its agent (409);
// revoke-first → the bind wakes, re-reads `private`, and is refused.
//
// member is re-checked against the locked row for the same reason the confirm path
// does it: owner_id is rewritten by daemon registration, so the caller's owner-only
// permission has to be re-established after the wait, not assumed from before it.
func (h *Handler) makeRuntimePrivateIfUnaffected(ctx context.Context, member db.Member, rt db.AgentRuntime) (db.AgentRuntime, runtimeRevokePlan, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return rt, runtimeRevokePlan{}, fmt.Errorf("begin visibility tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	locked, err := qtx.LockAgentRuntime(ctx, rt.ID)
	if err != nil {
		return rt, runtimeRevokePlan{}, fmt.Errorf("lock runtime: %w", err)
	}
	if !canSetRuntimeVisibility(member, locked) {
		return rt, runtimeRevokePlan{}, errRuntimeVisibilityOwnerChanged
	}
	if locked.Visibility == "private" {
		// Someone else got there first; the requested end state holds.
		if err := tx.Commit(ctx); err != nil {
			return rt, runtimeRevokePlan{}, fmt.Errorf("commit visibility tx: %w", err)
		}
		return locked, runtimeRevokePlan{}, nil
	}

	foreign, err := qtx.LockForeignAgentsByRuntime(ctx, db.LockForeignAgentsByRuntimeParams{
		RuntimeID: locked.ID,
		OwnerID:   locked.OwnerID,
	})
	if err != nil {
		return rt, runtimeRevokePlan{}, fmt.Errorf("lock foreign agents: %w", err)
	}
	if plan, _, _ := splitForeignAgents(foreign); !plan.empty() {
		return rt, plan, errRuntimeRevokeNeedsConfirmation // zero writes
	}

	updated, err := qtx.UpdateAgentRuntimeVisibility(ctx, db.UpdateAgentRuntimeVisibilityParams{
		ID:         locked.ID,
		Visibility: "private",
	})
	if err != nil {
		return rt, runtimeRevokePlan{}, fmt.Errorf("update visibility: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return rt, runtimeRevokePlan{}, fmt.Errorf("commit visibility tx: %w", err)
	}
	return updated, runtimeRevokePlan{}, nil
}

// runtimeRevokePlanResponse is the 409 body for both codes. Same `error` + `code`
// + agent-list shape as runtimeHasActiveAgentsResponse so clients keep one 409
// pattern, with the minimal agent DTO.
func (h *Handler) runtimeRevokePlanResponse(plan runtimeRevokePlan, code string) map[string]any {
	agents := make([]runtimeRevokeAgentDTO, len(plan.UnboundAgents))
	for i, a := range plan.UnboundAgents {
		agents[i] = runtimeRevokeAgentDTO{ID: uuidToString(a.ID), Name: a.Name}
	}
	message := "making this runtime private affects agents that are not yours. Review and confirm the impact first."
	if code == runtimeVisibilityPlanChangedCode {
		message = "the affected agent set changed; please review and confirm again."
	}
	return map[string]any{
		"error":                message,
		"code":                 code,
		"active_agents":        agents,
		"archived_agent_count": plan.ArchivedCount,
		"retained_agent_count": plan.RetainedSystemCount,
		"mika_affected":        plan.MikaAffected,
	}
}

// revokeAndMakePrivateRequest is the confirmed snapshot. It carries EVERY
// category the dialog put in front of the user, not just the named agents:
// archived agents and retained system carriers are affected too, and comparing
// only the active ids let an archived agent appearing (or a builder session moving
// in) while the dialog was open sail through — the user would have approved a
// smaller impact than the one that ran.
//
// The counts are plain ints, so a client that omits them submits zeros; that
// matches only when there is genuinely nothing in those categories, and otherwise
// produces a plan-changed 409 rather than a silent extra teardown. This endpoint
// ships with the dialog, so there is no older client to strand.
type revokeAndMakePrivateRequest struct {
	ExpectedActiveAgentIDs     []string `json:"expected_active_agent_ids"`
	ExpectedArchivedAgentCount int      `json:"expected_archived_agent_count"`
	ExpectedRetainedAgentCount int      `json:"expected_retained_agent_count"`
}

// matches reports whether the live plan is still the one the user confirmed.
func (req revokeAndMakePrivateRequest) matches(plan runtimeRevokePlan, expectedIDs map[string]struct{}) bool {
	return activeAgentSetMatches(plan.UnboundAgents, expectedIDs) &&
		plan.ArchivedCount == req.ExpectedArchivedAgentCount &&
		plan.RetainedSystemCount == req.ExpectedRetainedAgentCount
}

// RevokeAndMakePrivateRuntime is the confirmed revoke:
// POST /api/runtimes/:id/revoke-and-make-private. Owner-only like the PATCH it
// completes — lending the machine out was the owner's call, so taking it back is
// too (the override MUL-6126 removed).
func (h *Handler) RevokeAndMakePrivateRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	var req revokeAndMakePrivateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	expected, ok := parseExpectedActiveAgentIDs(req.ExpectedActiveAgentIDs)
	if !ok {
		writeError(w, http.StatusBadRequest, "expected_active_agent_ids must be a list of valid UUIDs")
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	wsID := uuidToString(rt.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "runtime not found")
	if !ok {
		return
	}
	if !canSetRuntimeVisibility(member, rt) {
		writeError(w, http.StatusForbidden, "only the runtime owner can change its visibility")
		return
	}
	userID := uuidToString(member.UserID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Lock order runtime → agents (by id), identical to DeleteAgentRuntime and
	// revokeAndRemoveMember; diverging would let the two deadlock.
	locked, err := qtx.LockAgentRuntime(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock runtime")
		return
	}
	rt = locked
	// Re-authorize against the LOCKED row. agent_runtime.owner_id is not
	// immutable — daemon registration rewrites it — so the owner can change while
	// this request queues for the lock. Without this recheck the previous owner
	// could still unbind the new owner's teammates' agents, cancel their tasks and
	// pause their Autopilots, using consent that no longer exists.
	if !canSetRuntimeVisibility(member, rt) {
		writeError(w, http.StatusForbidden, "only the runtime owner can change its visibility")
		return
	}
	if rt.Visibility == "private" {
		// Another confirm already landed: idempotent success, nothing left to do.
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit transaction")
			return
		}
		writeJSON(w, http.StatusOK, revokeResultBody(runtimeTeardownResult{}, 0))
		return
	}

	foreign, err := qtx.LockForeignAgentsByRuntime(r.Context(), db.LockForeignAgentsByRuntimeParams{
		RuntimeID: rt.ID,
		OwnerID:   rt.OwnerID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock runtime dependencies")
		return
	}
	plan, unboundIDs, retainedIDs := splitForeignAgents(foreign)
	if !req.matches(plan, expected) {
		writeJSON(w, http.StatusConflict, h.runtimeRevokePlanResponse(plan, runtimeVisibilityPlanChangedCode))
		return
	}

	teardown, err := revokeRuntimeVisibility(r.Context(), qtx, rt, unboundIDs, retainedIDs)
	if err != nil {
		if errors.Is(err, errRuntimeNotDrained) {
			slog.Error("runtime visibility revoke aborted: tasks not drained",
				"runtime_id", uuidToString(rt.ID), "error", err)
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "the runtime still has tasks in flight; retry in a moment.",
				"code":  runtimeVisibilityNotDrainedCode,
			})
			return
		}
		slog.Error("runtime visibility revoke failed", "runtime_id", uuidToString(rt.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to make runtime private")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	slog.Info("runtime made private, foreign agents revoked",
		"runtime_id", uuidToString(rt.ID),
		"revoked_by", userID,
		"agents_unbound", len(teardown.UnboundAgents),
		"agents_retained", len(retainedIDs),
		"tasks_cancelled", len(teardown.CancelledTasks),
		"autopilots_paused", len(teardown.PausedAutopilots),
	)

	// The runtime row survives, so the trailing runtime event is an update.
	// task:cancelled goes first and revokes each task's token.
	h.publishRuntimeTeardown(r.Context(), teardown, wsID, userID, "update")
	writeJSON(w, http.StatusOK, revokeResultBody(teardown, len(retainedIDs)))
}

func revokeResultBody(teardown runtimeTeardownResult, retained int) map[string]any {
	return map[string]any{
		"status":            "ok",
		"agents_unbound":    len(teardown.UnboundAgents),
		"tasks_cancelled":   len(teardown.CancelledTasks),
		"autopilots_paused": len(teardown.PausedAutopilots),
		"agents_retained":   retained,
	}
}

// revokeRuntimeVisibility is the teardown, inside the caller's transaction. Order
// follows unbindRuntimeForDelete so both share its race-safety reasoning: flip the
// visibility first (so whatever blocked on our lock re-reads `private` on commit),
// unbind the foreign user agents, pause their Autopilots, cancel their non-terminal
// tasks with a reason per group, then assert drained — a missed status means the
// cancel query and the drain predicate disagree, so abort rather than commit a
// partial revoke.
func revokeRuntimeVisibility(ctx context.Context, qtx *db.Queries, rt db.AgentRuntime, unboundIDs, retainedIDs []pgtype.UUID) (runtimeTeardownResult, error) {
	var out runtimeTeardownResult

	if _, err := qtx.UpdateAgentRuntimeVisibility(ctx, db.UpdateAgentRuntimeVisibilityParams{
		ID:         rt.ID,
		Visibility: "private",
	}); err != nil {
		return out, fmt.Errorf("update visibility: %w", err)
	}

	unbound, err := qtx.UnbindForeignUserAgentsFromRuntime(ctx, db.UnbindForeignUserAgentsFromRuntimeParams{
		RuntimeID: rt.ID,
		OwnerID:   rt.OwnerID,
	})
	if err != nil {
		return out, fmt.Errorf("unbind foreign agents: %w", err)
	}
	out.UnboundAgents = unbound

	paused, err := qtx.PauseAutopilotsByUnboundAgents(ctx, unboundIDs)
	if err != nil {
		return out, fmt.Errorf("pause autopilots: %w", err)
	}
	out.PausedAutopilots = paused

	// runtime_ids stays empty in both calls: the runtime survives, so matching on
	// it would cancel the owner's own work on their own machine.
	for _, group := range []struct {
		ids    []pgtype.UUID
		errMsg string
		reason taskfailure.Reason
		what   string
	}{
		{unboundIDs, revokeUnboundTaskError, taskfailure.ReasonAgentRuntimeRequired, "unbound"},
		{retainedIDs, revokeRetainedTaskError, taskfailure.ReasonRuntimeAccessRevoked, "retained"},
	} {
		if len(group.ids) == 0 {
			continue
		}
		cancelled, err := qtx.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{
			RuntimeIds:    []pgtype.UUID{},
			AgentIds:      group.ids,
			Error:         pgtype.Text{String: group.errMsg, Valid: true},
			FailureReason: pgtype.Text{String: string(group.reason), Valid: true},
		})
		if err != nil {
			return out, fmt.Errorf("cancel %s agent tasks: %w", group.what, err)
		}
		out.CancelledTasks = append(out.CancelledTasks, cancelled...)
	}

	undrained, err := qtx.CountUndrainedTasksByRuntimeOrAgent(ctx, db.CountUndrainedTasksByRuntimeOrAgentParams{
		RuntimeIds: []pgtype.UUID{},
		AgentIds:   append(append([]pgtype.UUID{}, unboundIDs...), retainedIDs...),
	})
	if err != nil {
		return out, fmt.Errorf("count undrained tasks: %w", err)
	}
	if undrained > 0 {
		return out, fmt.Errorf("%w: %d", errRuntimeNotDrained, undrained)
	}
	return out, nil
}
