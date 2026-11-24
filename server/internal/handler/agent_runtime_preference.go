package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentRuntimePreferenceResponse contains only the authenticated user's choice.
type AgentRuntimePreferenceResponse struct {
	RuntimeID          *string `json:"runtime_id"`
	Provider           *string `json:"provider"`
	ModelMode          string  `json:"model_mode,omitempty"`
	Model              string  `json:"model,omitempty"`
	MaxConcurrentTasks *int32  `json:"max_concurrent_tasks,omitempty"`
}

func (h *Handler) authorizeAgentRuntimePreference(w http.ResponseWriter, r *http.Request) (db.Agent, db.Member, bool) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Agent{}, db.Member{}, false
	}
	if agent.Kind == "system" {
		writeError(w, http.StatusForbidden, "system agents do not support personal runtime settings")
		return db.Agent{}, db.Member{}, false
	}
	workspaceID := uuidToString(agent.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "agent not found")
	if !ok {
		return db.Agent{}, db.Member{}, false
	}
	userID := uuidToString(member.UserID)
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType != "member" || !h.canInvokeAgent(r.Context(), agent, "member", userID, userID, workspaceID) {
		writeError(w, http.StatusForbidden, "agent invocation permission required")
		return db.Agent{}, db.Member{}, false
	}
	return agent, member, true
}

func (h *Handler) GetAgentRuntimePreference(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.authorizeAgentRuntimePreference(w, r)
	if !ok {
		return
	}
	preference, err := h.Queries.GetAgentRuntimePreference(r.Context(), db.GetAgentRuntimePreferenceParams{
		WorkspaceID: agent.WorkspaceID, UserID: member.UserID, AgentID: agent.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, h.runtimePreferenceResponse(r, agent, nil))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load runtime preference")
		return
	}
	id := uuidToString(preference.RuntimeID)
	writeJSON(w, http.StatusOK, withExecutionPreference(h.runtimePreferenceResponse(r, agent, &id), preference))
}

func (h *Handler) UpdateAgentRuntimePreference(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.authorizeAgentRuntimePreference(w, r)
	if !ok {
		return
	}
	// RawMessage distinguishes an explicit reset from an omitted field.
	var req struct {
		RuntimeID          json.RawMessage `json:"runtime_id"`
		ModelMode          *string         `json:"model_mode"`
		Model              *string         `json:"model"`
		MaxConcurrentTasks json.RawMessage `json:"max_concurrent_tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.RuntimeID) == 0 {
		writeError(w, http.StatusBadRequest, "runtime_id is required (use null to reset)")
		return
	}
	var runtimeID *string
	if err := json.Unmarshal(req.RuntimeID, &runtimeID); err != nil {
		writeError(w, http.StatusBadRequest, "runtime_id must be a UUID or null")
		return
	}
	if runtimeID == nil {
		if err := h.Queries.DeleteAgentRuntimePreference(r.Context(), db.DeleteAgentRuntimePreferenceParams{
			WorkspaceID: agent.WorkspaceID, UserID: member.UserID, AgentID: agent.ID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reset runtime preference")
			return
		}
		writeJSON(w, http.StatusOK, h.runtimePreferenceResponse(r, agent, nil))
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, *runtimeID, "runtime_id")
	if !ok {
		return
	}
	runtime, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil || runtime.WorkspaceID != agent.WorkspaceID {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	if runtime.OwnerID != member.UserID {
		writeError(w, http.StatusForbidden, "personal runtime must be owned by you")
		return
	}
	// Runtime-only requests retain compatible settings and the personal limit.
	mode, model := "runtime_default", ""
	var limit pgtype.Int4
	previous, previousErr := h.Queries.GetAgentRuntimePreference(r.Context(), db.GetAgentRuntimePreferenceParams{WorkspaceID: agent.WorkspaceID, UserID: member.UserID, AgentID: agent.ID})
	if previousErr == nil {
		mode, model, limit = previous.ModelMode, previous.Model, previous.MaxConcurrentTasks
	} else if !errors.Is(previousErr, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load execution preference")
		return
	}
	if previousErr == nil && previous.RuntimeID != runtime.ID {
		previousRuntime, err := h.Queries.GetAgentRuntime(r.Context(), previous.RuntimeID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to load previous runtime")
			return
		}
		if err != nil || previousRuntime.Provider != runtime.Provider {
			mode, model = "runtime_default", ""
		}
	}
	if req.ModelMode != nil {
		mode = *req.ModelMode
	}
	if req.Model != nil {
		model = strings.TrimSpace(*req.Model)
	}
	if mode != "inherit" && mode != "runtime_default" && mode != "custom" {
		writeError(w, http.StatusBadRequest, "invalid model_mode")
		return
	}
	// Normalize older clients without inheriting shared execution settings.
	if mode == "inherit" {
		mode = "runtime_default"
	}
	if mode != "custom" {
		model = ""
	} else if len(model) == 0 || len(model) > 256 || strings.ContainsAny(model, "\r\n\x00") {
		writeError(w, http.StatusBadRequest, "custom model must contain 1 to 256 characters")
		return
	}
	if len(req.MaxConcurrentTasks) > 0 {
		var value *int32
		if err := json.Unmarshal(req.MaxConcurrentTasks, &value); err != nil || (value != nil && (*value < 1 || *value > 100)) {
			writeError(w, http.StatusBadRequest, "max_concurrent_tasks must be null or an integer from 1 to 100")
			return
		}
		limit = pgtype.Int4{}
		if value != nil {
			limit = pgtype.Int4{Int32: *value, Valid: true}
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save runtime preference")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), agent.WorkspaceID); err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if err := qtx.LockSubscriberWrites(r.Context(), db.LockSubscriberWritesParams{WorkspaceID: agent.WorkspaceID, UserID: member.UserID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save runtime preference")
		return
	}
	if _, err := qtx.LockActiveMember(r.Context(), db.LockActiveMemberParams{WorkspaceID: agent.WorkspaceID, UserID: member.UserID}); err != nil {
		writeError(w, http.StatusForbidden, "workspace membership required")
		return
	}
	preference, err := qtx.UpsertAgentRuntimePreference(r.Context(), db.UpsertAgentRuntimePreferenceParams{
		WorkspaceID: agent.WorkspaceID, UserID: member.UserID, AgentID: agent.ID, RuntimeID: runtime.ID, ModelMode: mode, Model: model, MaxConcurrentTasks: limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save runtime preference")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save runtime preference")
		return
	}
	id := uuidToString(preference.RuntimeID)
	writeJSON(w, http.StatusOK, withExecutionPreference(h.runtimePreferenceResponse(r, agent, &id), preference))
}

// Provider is non-secret agent capability metadata. Invocable agents may have
// a hidden default runtime, so selectors cannot derive it from runtime lists.
func (h *Handler) runtimePreferenceResponse(r *http.Request, agent db.Agent, runtimeID *string) AgentRuntimePreferenceResponse {
	response := AgentRuntimePreferenceResponse{RuntimeID: runtimeID}
	if runtime, err := h.Queries.GetAgentRuntime(r.Context(), agent.RuntimeID); err == nil && runtime.WorkspaceID == agent.WorkspaceID {
		response.Provider = &runtime.Provider
	}
	return response
}

func withExecutionPreference(response AgentRuntimePreferenceResponse, preference db.AgentRuntimePreference) AgentRuntimePreferenceResponse {
	response.ModelMode, response.Model = preference.ModelMode, preference.Model
	if response.ModelMode == "inherit" {
		response.ModelMode, response.Model = "runtime_default", ""
	}
	if preference.MaxConcurrentTasks.Valid {
		value := preference.MaxConcurrentTasks.Int32
		response.MaxConcurrentTasks = &value
	}
	return response
}
