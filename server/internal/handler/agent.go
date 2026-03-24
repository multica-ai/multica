package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type AgentResponse struct {
	ID                 string  `json:"id"`
	WorkspaceID        string  `json:"workspace_id"`
	RuntimeID          string  `json:"runtime_id"`
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	AvatarURL          *string `json:"avatar_url"`
	RuntimeMode        string  `json:"runtime_mode"`
	RuntimeConfig      any     `json:"runtime_config"`
	Visibility         string  `json:"visibility"`
	Status             string  `json:"status"`
	MaxConcurrentTasks int32   `json:"max_concurrent_tasks"`
	OwnerID            *string `json:"owner_id"`
	Skills             string  `json:"skills"`
	Tools              any     `json:"tools"`
	Triggers           any     `json:"triggers"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

func agentToResponse(a db.Agent) AgentResponse {
	var rc any
	if a.RuntimeConfig != nil {
		json.Unmarshal(a.RuntimeConfig, &rc)
	}
	if rc == nil {
		rc = map[string]any{}
	}

	var tools any
	if a.Tools != nil {
		json.Unmarshal(a.Tools, &tools)
	}
	if tools == nil {
		tools = []any{}
	}

	var triggers any
	if a.Triggers != nil {
		json.Unmarshal(a.Triggers, &triggers)
	}
	if triggers == nil {
		triggers = []any{}
	}

	return AgentResponse{
		ID:                 uuidToString(a.ID),
		WorkspaceID:        uuidToString(a.WorkspaceID),
		RuntimeID:          uuidToString(a.RuntimeID),
		Name:               a.Name,
		Description:        a.Description,
		AvatarURL:          textToPtr(a.AvatarUrl),
		RuntimeMode:        a.RuntimeMode,
		RuntimeConfig:      rc,
		Visibility:         a.Visibility,
		Status:             a.Status,
		MaxConcurrentTasks: a.MaxConcurrentTasks,
		OwnerID:            uuidToPtr(a.OwnerID),
		Skills:             a.Skills,
		Tools:              tools,
		Triggers:           triggers,
		CreatedAt:          timestampToString(a.CreatedAt),
		UpdatedAt:          timestampToString(a.UpdatedAt),
	}
}

type AgentTaskResponse struct {
	ID           string  `json:"id"`
	AgentID      string  `json:"agent_id"`
	RuntimeID    string  `json:"runtime_id"`
	IssueID      string  `json:"issue_id"`
	Status       string  `json:"status"`
	Priority     int32   `json:"priority"`
	DispatchedAt *string `json:"dispatched_at"`
	StartedAt    *string `json:"started_at"`
	CompletedAt  *string `json:"completed_at"`
	Result       any     `json:"result"`
	Error        *string `json:"error"`
	Context      any     `json:"context,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

func taskToResponse(t db.AgentTaskQueue) AgentTaskResponse {
	var result any
	if t.Result != nil {
		json.Unmarshal(t.Result, &result)
	}
	var ctx any
	if t.Context != nil {
		json.Unmarshal(t.Context, &ctx)
	}
	return AgentTaskResponse{
		ID:           uuidToString(t.ID),
		AgentID:      uuidToString(t.AgentID),
		RuntimeID:    uuidToString(t.RuntimeID),
		IssueID:      uuidToString(t.IssueID),
		Status:       t.Status,
		Priority:     t.Priority,
		DispatchedAt: timestampToPtr(t.DispatchedAt),
		StartedAt:    timestampToPtr(t.StartedAt),
		CompletedAt:  timestampToPtr(t.CompletedAt),
		Result:       result,
		Error:        textToPtr(t.Error),
		Context:      ctx,
		CreatedAt:    timestampToString(t.CreatedAt),
	}
}

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}

	agents, err := h.Queries.ListAgents(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}

	resp := make([]AgentResponse, len(agents))
	for i, a := range agents {
		resp[i] = agentToResponse(a)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, agentToResponse(agent))
}

type CreateAgentRequest struct {
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	AvatarURL          *string `json:"avatar_url"`
	RuntimeID          string  `json:"runtime_id"`
	RuntimeConfig      any     `json:"runtime_config"`
	Visibility         string  `json:"visibility"`
	MaxConcurrentTasks int32   `json:"max_concurrent_tasks"`
	Skills             string  `json:"skills"`
	Tools              any     `json:"tools"`
	Triggers           any     `json:"triggers"`
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ownerID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "workspace"
	}
	if req.MaxConcurrentTasks == 0 {
		req.MaxConcurrentTasks = 1
	}

	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(req.RuntimeID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}

	rc, _ := json.Marshal(req.RuntimeConfig)
	if req.RuntimeConfig == nil {
		rc = []byte("{}")
	}

	tools, _ := json.Marshal(req.Tools)
	if req.Tools == nil {
		tools = []byte("[]")
	}

	triggers, _ := json.Marshal(req.Triggers)
	if req.Triggers == nil {
		triggers = []byte("[]")
	}

	agent, err := h.Queries.CreateAgent(r.Context(), db.CreateAgentParams{
		WorkspaceID:        parseUUID(workspaceID),
		Name:               req.Name,
		Description:        req.Description,
		AvatarUrl:          ptrToText(req.AvatarURL),
		RuntimeMode:        runtime.RuntimeMode,
		RuntimeConfig:      rc,
		RuntimeID:          runtime.ID,
		Visibility:         req.Visibility,
		MaxConcurrentTasks: req.MaxConcurrentTasks,
		OwnerID:            parseUUID(ownerID),
		Skills:             req.Skills,
		Tools:              tools,
		Triggers:           triggers,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent: "+err.Error())
		return
	}

	if runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(r.Context(), agent.ID)
		agent, _ = h.Queries.GetAgent(r.Context(), agent.ID)
	}

	// Best-effort: create an initialization issue assigned to the new agent.
	h.createAgentInitIssue(r.Context(), agent, parseUUID(ownerID))

	writeJSON(w, http.StatusCreated, agentToResponse(agent))
}

// createAgentInitIssue creates an initialization issue assigned to a newly created agent.
// It incorporates workspace context so the agent can set up its environment.
// Failures are silently ignored — the agent creation itself has already succeeded.
func (h *Handler) createAgentInitIssue(ctx context.Context, agent db.Agent, creatorID pgtype.UUID) {
	ws, err := h.Queries.GetWorkspace(ctx, agent.WorkspaceID)
	if err != nil {
		return
	}

	var desc string
	if ws.Context.Valid && ws.Context.String != "" {
		desc = fmt.Sprintf("Initialize the development environment for agent **%s**.\n\n## Workspace Context\n\n%s\n\n## Instructions\n\n- Set up the local development environment based on the workspace context above\n- Clone and configure any referenced repositories\n- Verify access to the codebase and tools\n- Report back on what was set up and any issues encountered", agent.Name, ws.Context.String)
	} else {
		desc = fmt.Sprintf("Initialize the development environment for agent **%s**.\n\n## Instructions\n\n- Explore the local working directory and understand the project structure\n- Verify access to the codebase and tools\n- Report back on what was found and any issues encountered", agent.Name)
	}

	issue, err := h.Queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:        agent.WorkspaceID,
		Title:              "Initialize environment for " + agent.Name,
		Description:        strToText(desc),
		Status:             "todo",
		Priority:           "medium",
		AssigneeType:       pgtype.Text{String: "agent", Valid: true},
		AssigneeID:         agent.ID,
		CreatorType:        "member",
		CreatorID:          creatorID,
		AcceptanceCriteria: []byte("[]"),
		ContextRefs:        []byte("[]"),
		Position:           0,
	})
	if err != nil {
		return
	}

	h.broadcast("issue:created", map[string]any{"issue": issueToResponse(issue)})

	// Enqueue the task directly — we know the agent is assigned and status is "todo".
	if _, err := h.TaskService.EnqueueTaskForIssue(ctx, issue); err != nil {
		log.Printf("createAgentInitIssue: enqueue task failed for issue %s: %v", issue.Title, err)
	}
}

type UpdateAgentRequest struct {
	Name               *string `json:"name"`
	Description        *string `json:"description"`
	AvatarURL          *string `json:"avatar_url"`
	RuntimeID          *string `json:"runtime_id"`
	RuntimeConfig      any     `json:"runtime_config"`
	Visibility         *string `json:"visibility"`
	Status             *string `json:"status"`
	MaxConcurrentTasks *int32  `json:"max_concurrent_tasks"`
	Skills             *string `json:"skills"`
	Tools              any     `json:"tools"`
	Triggers           any     `json:"triggers"`
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, uuidToString(agent.WorkspaceID), "agent not found", "owner", "admin"); !ok {
		return
	}

	var req UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateAgentParams{
		ID: parseUUID(id),
	}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.AvatarURL != nil {
		params.AvatarUrl = pgtype.Text{String: *req.AvatarURL, Valid: true}
	}
	if req.RuntimeConfig != nil {
		rc, _ := json.Marshal(req.RuntimeConfig)
		params.RuntimeConfig = rc
	}
	if req.RuntimeID != nil {
		runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
			ID:          parseUUID(*req.RuntimeID),
			WorkspaceID: agent.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid runtime_id")
			return
		}
		params.RuntimeID = runtime.ID
		params.RuntimeMode = pgtype.Text{String: runtime.RuntimeMode, Valid: true}
	}
	if req.Visibility != nil {
		params.Visibility = pgtype.Text{String: *req.Visibility, Valid: true}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.MaxConcurrentTasks != nil {
		params.MaxConcurrentTasks = pgtype.Int4{Int32: *req.MaxConcurrentTasks, Valid: true}
	}
	if req.Skills != nil {
		params.Skills = pgtype.Text{String: *req.Skills, Valid: true}
	}
	if req.Tools != nil {
		tools, _ := json.Marshal(req.Tools)
		params.Tools = tools
	}
	if req.Triggers != nil {
		triggers, _ := json.Marshal(req.Triggers)
		params.Triggers = triggers
	}

	agent, err := h.Queries.UpdateAgent(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agent: "+err.Error())
		return
	}

	resp := agentToResponse(agent)
	h.broadcast("agent:status", map[string]any{"agent": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := h.Queries.DeleteAgent(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete agent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListAgentTasks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tasks, err := h.Queries.ListAgentTasks(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent tasks")
		return
	}

	resp := make([]AgentTaskResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = taskToResponse(t)
	}

	writeJSON(w, http.StatusOK, resp)
}
