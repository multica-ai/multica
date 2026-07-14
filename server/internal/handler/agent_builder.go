package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/featureflags"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const agentBuilderInstructions = `You are Multica Agent Builder. Help the user design one practical AI agent through a short conversation.

Your job is to propose and refine configuration, never to create resources yourself. Ask only questions that materially change behavior. Prefer making a reasonable draft immediately, then ask at most two focused questions per turn.

Every response MUST end with exactly one <agent_draft> JSON block using this shape:
<agent_draft>{"name":"","description":"","instructions":"","model":"","skill_ids":[],"permission_scope":"private","member_ids":[]}</agent_draft>

Rules:
- The JSON must be valid, compact JSON on one physical line. Do not wrap it in Markdown fences.
- Escape every line break inside instructions as \n. Never place a literal newline inside a JSON string.
- Preserve good existing draft fields supplied in the user's message unless the user asks to change them.
- name is concise and suitable for a workspace list.
- description is one sentence, at most 200 characters.
- instructions are a complete Markdown system prompt describing role, workflow, output, and constraints.
- model must be empty, preserve current_draft.model, or exactly match an id explicitly listed in AVAILABLE RUNTIME MODELS. Never use a model label as the id.
- When AVAILABLE RUNTIME MODELS is null or empty, preserve current_draft.model and never invent a model id.
- skill_ids may only contain IDs explicitly listed in AVAILABLE WORKSPACE SKILLS.
- permission_scope must be private, workspace, or members. Default to private unless the user explicitly requests sharing.
- member_ids may only contain IDs explicitly listed in AVAILABLE WORKSPACE MEMBERS, and only when permission_scope is members.
- Never request, expose, or place secrets, tokens, passwords, or environment-variable values in the draft.
- Do not claim that the agent has been created. The user must review and confirm the draft in the UI.`

type CreateAgentBuilderSessionRequest struct {
	RuntimeID string `json:"runtime_id"`
	Model     string `json:"model,omitempty"`
}

type CreateAgentBuilderSessionResponse struct {
	SessionID      string `json:"session_id"`
	BuilderAgentID string `json:"builder_agent_id"`
	RuntimeID      string `json:"runtime_id"`
}

// CreateAgentBuilderSession starts a private configuration conversation on an
// existing runtime. A hidden system agent is the execution carrier because the
// chat/task pipeline is intentionally agent-backed; it never appears in normal
// agent lists and cannot be selected as an assignee.
func (h *Handler) CreateAgentBuilderSession(w http.ResponseWriter, r *http.Request) {
	if !featureflags.AgentBuilderEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "agent builder is not enabled")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateAgentBuilderSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runtimeID := strings.TrimSpace(req.RuntimeID)
	if runtimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can use it")
		return
	}
	if runtime.Status != "online" {
		writeError(w, http.StatusConflict, "runtime must be online to start an agent builder session")
		return
	}

	flowID := uuid.NewString()
	ownerUUID := parseUUID(userID)
	model := strings.TrimSpace(req.Model)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start agent builder session")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	builder, err := qtx.CreateAgentBuilder(r.Context(), db.CreateAgentBuilderParams{
		WorkspaceID:  workspaceUUID,
		Name:         fmt.Sprintf(".multica-agent-builder-%s", flowID),
		RuntimeMode:  runtime.RuntimeMode,
		RuntimeID:    runtime.ID,
		OwnerID:      ownerUUID,
		Instructions: agentBuilderInstructions,
		Model:        pgtype.Text{String: model, Valid: model != ""},
		SystemKey: pgtype.Text{
			String: fmt.Sprintf("agent_builder:%s", flowID),
			Valid:  true,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare agent builder")
		return
	}

	session, err := qtx.CreateChatSession(r.Context(), db.CreateChatSessionParams{
		WorkspaceID: workspaceUUID,
		AgentID:     builder.ID,
		CreatorID:   ownerUUID,
		Title:       "Create an agent",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent builder session")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit agent builder session")
		return
	}

	writeJSON(w, http.StatusCreated, CreateAgentBuilderSessionResponse{
		SessionID:      uuidToString(session.ID),
		BuilderAgentID: uuidToString(builder.ID),
		RuntimeID:      runtimeID,
	})
}
