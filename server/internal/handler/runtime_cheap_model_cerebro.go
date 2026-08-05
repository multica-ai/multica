package handler

// CEREBRO-PATCH(runtime-cheap-model-handler): FIR-4492 per-runtime cheap model API.
//
// PATCH /api/runtimes/{runtimeId}/cheap-model sets the model this runtime uses
// for the provider-independent "cheap" tier. Pass `{"cheap_model": ""}` to clear.
//
// Why it lives on the runtime and not in a server-side table: the server only has
// an authoritative model catalog for claude, codex, openai-eu and firtal-local.
// For hermes, opencode, cursor, kimi, kiro, pi, openclaw, antigravity and
// firtal-gateway the model list is a file on the machine, so autopilotmodel had no
// cheap model to name and "cheap" resolved to no override — the wakeup asked for a
// cheap run and got the agent's own model. Nor can the server derive one: the
// discovered list carries no price, so "cheapest" is not computable. It has to be
// chosen from the runtime's own list, which is exactly what the runtime page can
// enumerate (POST /api/runtimes/{runtimeId}/models).
//
// A wrong or stale value cannot fail a run: the daemon checks every model against
// the runtime's live list before spawning and degrades to the agent's own model
// (daemon.runnableTaskModel).
//
// Auth: workspace owner/admin, same as the other runtime-level settings — this
// decides what every agent on the runtime bills for a cheap run.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/autopilotmodel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// curatedCheapModel returns the cheap model Multica curates for provider, or ""
// when it curates none — which is exactly the case a per-runtime setting exists
// to cover.
func curatedCheapModel(provider string) string {
	model, _ := autopilotmodel.CheapForProvider(provider)
	return model
}

// UpdateRuntimeCheapModelRequest carries the new cheap model ID. "" clears it.
type UpdateRuntimeCheapModelRequest struct {
	CheapModel *string `json:"cheap_model"`
}

// UpdateAgentRuntimeCheapModel sets or clears the runtime's cheap model.
func (h *Handler) UpdateAgentRuntimeCheapModel(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
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
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can change the cheap model")
		return
	}

	var req UpdateRuntimeCheapModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CheapModel == nil {
		writeError(w, http.StatusBadRequest, "cheap_model is required (use an empty string to clear)")
		return
	}

	model := strings.TrimSpace(*req.CheapModel)
	if model == autopilotmodel.TierCheap {
		writeError(w, http.StatusBadRequest, "cheap_model must be a model ID, not \""+autopilotmodel.TierCheap+"\"")
		return
	}
	// Only catches what the server can prove wrong. For a provider with a static
	// catalog this rejects a typo now; for one without, SupportedByProvider is
	// permissive by design — the machine's list is the only truth, and the daemon
	// checks against it at spawn time.
	if model != "" && !autopilotmodel.SupportedByProvider(rt.Provider, model) {
		allowed, _ := autopilotmodel.AllowedForProvider(rt.Provider)
		writeError(w, http.StatusBadRequest, "cheap_model "+model+" is not a "+rt.Provider+" model (allowed: "+strings.Join(allowed, ", ")+")")
		return
	}

	updated, err := h.Queries.UpdateAgentRuntimeCheapModel(r.Context(), db.UpdateAgentRuntimeCheapModelParams{
		ID:         rt.ID,
		CheapModel: model,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update cheap model")
		return
	}

	slog.Info("runtime cheap model updated",
		"runtime_id", runtimeID,
		"updated_by", uuidToString(member.UserID),
		"provider", rt.Provider,
		"cheap_model", model,
	)

	auditDetails, _ := json.Marshal(map[string]any{
		"runtime_id":   runtimeID,
		"runtime_name": rt.Name,
		"cheap_model":  model,
	})
	_, _ = h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: rt.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     member.UserID,
		Action:      "runtime_cheap_model_changed",
		Details:     auditDetails,
	})

	h.publish(protocol.EventDaemonRegister, wsID, "member", uuidToString(member.UserID), map[string]any{
		"action":     "update",
		"runtime_id": runtimeID,
	})

	writeJSON(w, http.StatusOK, runtimeToResponse(updated))
}
