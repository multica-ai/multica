package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type AgentRuntimeRef struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	RuntimeMode string  `json:"runtime_mode"`
	Provider    string  `json:"provider"`
	DeviceInfo  string  `json:"device_info"`
	OwnerID     *string `json:"owner_id"`
	LastUsedAt  *string `json:"last_used_at"`
}

type AgentRuntimeGroupRefOverride struct {
	RuntimeID   string `json:"runtime_id"`
	RuntimeName string `json:"runtime_name"`
	EndsAt      string `json:"ends_at"`
}

type AgentRuntimeGroupRef struct {
	ID             string                        `json:"id"`
	Name           string                        `json:"name"`
	ActiveOverride *AgentRuntimeGroupRefOverride `json:"active_override"`
}

type AgentResponse struct {
	ID                 string                 `json:"id"`
	WorkspaceID        string                 `json:"workspace_id"`
	Name               string                 `json:"name"`
	Description        string                 `json:"description"`
	Instructions       string                 `json:"instructions"`
	AvatarURL          *string                `json:"avatar_url"`
	RuntimeMode        string                 `json:"runtime_mode"`
	RuntimeConfig      any                    `json:"runtime_config"`
	CustomEnv          map[string]string      `json:"custom_env"`
	CustomArgs         []string               `json:"custom_args"`
	CustomEnvRedacted  bool                   `json:"custom_env_redacted"`
	Visibility         string                 `json:"visibility"`
	Status             string                 `json:"status"`
	MaxConcurrentTasks int32                  `json:"max_concurrent_tasks"`
	OwnerID            *string                `json:"owner_id"`
	Skills             []SkillResponse        `json:"skills"`
	RuntimeIDs         []string               `json:"runtime_ids"`
	Runtimes           []AgentRuntimeRef      `json:"runtimes"`
	Groups             []AgentRuntimeGroupRef `json:"groups"`
	CreatedAt          string                 `json:"created_at"`
	UpdatedAt          string                 `json:"updated_at"`
	ArchivedAt         *string                `json:"archived_at"`
	ArchivedBy         *string                `json:"archived_by"`
}

func agentToResponse(a db.Agent) AgentResponse {
	var rc any
	if a.RuntimeConfig != nil {
		json.Unmarshal(a.RuntimeConfig, &rc)
	}
	if rc == nil {
		rc = map[string]any{}
	}

	var customEnv map[string]string
	if a.CustomEnv != nil {
		if err := json.Unmarshal(a.CustomEnv, &customEnv); err != nil {
			slog.Warn("failed to unmarshal agent custom_env", "agent_id", uuidToString(a.ID), "error", err)
		}
	}
	if customEnv == nil {
		customEnv = map[string]string{}
	}

	var customArgs []string
	if a.CustomArgs != nil {
		if err := json.Unmarshal(a.CustomArgs, &customArgs); err != nil {
			slog.Warn("failed to unmarshal agent custom_args", "agent_id", uuidToString(a.ID), "error", err)
		}
	}
	if customArgs == nil {
		customArgs = []string{}
	}

	return AgentResponse{
		ID:                 uuidToString(a.ID),
		WorkspaceID:        uuidToString(a.WorkspaceID),
		Name:               a.Name,
		Description:        a.Description,
		Instructions:       a.Instructions,
		AvatarURL:          textToPtr(a.AvatarUrl),
		RuntimeMode:        a.RuntimeMode,
		RuntimeConfig:      rc,
		CustomEnv:          customEnv,
		CustomArgs:         customArgs,
		Visibility:         a.Visibility,
		Status:             a.Status,
		MaxConcurrentTasks: a.MaxConcurrentTasks,
		OwnerID:            uuidToPtr(a.OwnerID),
		Skills:             []SkillResponse{},
		RuntimeIDs:         []string{},
		Runtimes:           []AgentRuntimeRef{},
		Groups:             []AgentRuntimeGroupRef{},
		CreatedAt:          timestampToString(a.CreatedAt),
		UpdatedAt:          timestampToString(a.UpdatedAt),
		ArchivedAt:         timestampToPtr(a.ArchivedAt),
		ArchivedBy:         uuidToPtr(a.ArchivedBy),
	}
}

// buildAgentResponse enriches an AgentResponse with skills and runtime
// assignments. Call sites that used to call agentToResponse + inline skill
// loading should call this instead.
//
// NOTE: This performs N+1 queries when called from ListAgents (one per agent).
// Acceptable at current team sizes; a batch path can be added later if needed.
func (h *Handler) buildAgentResponse(ctx context.Context, a db.Agent) (AgentResponse, error) {
	resp := agentToResponse(a)

	skills, err := h.Queries.ListAgentSkills(ctx, a.ID)
	if err != nil {
		return resp, fmt.Errorf("list skills: %w", err)
	}
	if len(skills) > 0 {
		resp.Skills = make([]SkillResponse, len(skills))
		for i, s := range skills {
			resp.Skills[i] = skillToResponse(s)
		}
	}

	assignments, err := h.Queries.ListAgentRuntimeAssignments(ctx, a.ID)
	if err != nil {
		return resp, fmt.Errorf("list assignments: %w", err)
	}
	resp.Runtimes = make([]AgentRuntimeRef, len(assignments))
	resp.RuntimeIDs = make([]string, len(assignments))
	for i, asn := range assignments {
		resp.RuntimeIDs[i] = uuidToString(asn.RuntimeID)
		resp.Runtimes[i] = AgentRuntimeRef{
			ID:          uuidToString(asn.RuntimeID),
			Name:        asn.RuntimeName,
			Status:      asn.RuntimeStatus,
			RuntimeMode: asn.RuntimeMode,
			Provider:    asn.RuntimeProvider,
			DeviceInfo:  asn.RuntimeDeviceInfo,
			OwnerID:     uuidToPtr(asn.RuntimeOwnerID),
			LastUsedAt:  timestampToPtr(asn.LastUsedAt),
		}
	}

	groups, err := h.Queries.ListAgentRuntimeGroupsByAgent(ctx, a.ID)
	if err != nil {
		return resp, fmt.Errorf("list agent groups: %w", err)
	}
	resp.Groups = make([]AgentRuntimeGroupRef, 0, len(groups))
	for _, g := range groups {
		ref := AgentRuntimeGroupRef{
			ID:   uuidToString(g.GroupID),
			Name: g.GroupName,
		}
		active, err := h.Queries.GetActiveRuntimeGroupOverride(ctx, g.GroupID)
		if err == nil {
			ref.ActiveOverride = &AgentRuntimeGroupRefOverride{
				RuntimeID:   uuidToString(active.RuntimeID),
				RuntimeName: active.RuntimeName,
				EndsAt:      timestampToString(active.EndsAt),
			}
		}
		resp.Groups = append(resp.Groups, ref)
	}

	return resp, nil
}

// RepoData holds repository information included in claim responses so the
// daemon can set up worktrees for each workspace repo.
type RepoData struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

type AgentTaskResponse struct {
	ID                    string         `json:"id"`
	AgentID               string         `json:"agent_id"`
	RuntimeID             string         `json:"runtime_id"`
	IssueID               string         `json:"issue_id"`
	WorkspaceID           string         `json:"workspace_id"`
	Status                string         `json:"status"`
	Priority              int32          `json:"priority"`
	DispatchedAt          *string        `json:"dispatched_at"`
	StartedAt             *string        `json:"started_at"`
	CompletedAt           *string        `json:"completed_at"`
	Result                any            `json:"result"`
	Error                 *string        `json:"error"`
	Agent                 *TaskAgentData `json:"agent,omitempty"`
	Repos                 []RepoData     `json:"repos,omitempty"`
	CreatedAt             string         `json:"created_at"`
	PriorSessionID        string         `json:"prior_session_id,omitempty"`
	PriorWorkDir          string         `json:"prior_work_dir,omitempty"`
	TriggerCommentID      *string        `json:"trigger_comment_id,omitempty"`
	TriggerCommentContent string         `json:"trigger_comment_content,omitempty"`
	ChatSessionID         string         `json:"chat_session_id,omitempty"`
	ChatMessage           string         `json:"chat_message,omitempty"`
}

// TaskAgentData holds agent info included in claim responses so the daemon
// can set up the execution environment (branch naming, skill files, instructions).
type TaskAgentData struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Instructions string                   `json:"instructions"`
	Skills       []service.AgentSkillData `json:"skills,omitempty"`
	CustomEnv    map[string]string        `json:"custom_env,omitempty"`
	CustomArgs   []string                 `json:"custom_args,omitempty"`
}

func taskToResponse(t db.AgentTaskQueue) AgentTaskResponse {
	var result any
	if t.Result != nil {
		json.Unmarshal(t.Result, &result)
	}
	return AgentTaskResponse{
		ID:               uuidToString(t.ID),
		AgentID:          uuidToString(t.AgentID),
		RuntimeID:        uuidToString(t.RuntimeID),
		IssueID:          uuidToString(t.IssueID),
		Status:           t.Status,
		Priority:         t.Priority,
		DispatchedAt:     timestampToPtr(t.DispatchedAt),
		StartedAt:        timestampToPtr(t.StartedAt),
		CompletedAt:      timestampToPtr(t.CompletedAt),
		Result:           result,
		Error:            textToPtr(t.Error),
		CreatedAt:        timestampToString(t.CreatedAt),
		TriggerCommentID: uuidToPtr(t.TriggerCommentID),
	}
}

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID := requestUserID(r)

	var agents []db.Agent
	var err error
	if r.URL.Query().Get("include_archived") == "true" {
		agents, err = h.Queries.ListAllAgents(r.Context(), parseUUID(workspaceID))
	} else {
		agents, err = h.Queries.ListAgents(r.Context(), parseUUID(workspaceID))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}

	// Batch-fetch skills and runtime assignments once, then assemble per-agent
	// responses inline. Avoids the N+1 that buildAgentResponse would produce
	// for the list endpoint.
	skillRows, err := h.Queries.ListAgentSkillsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	skillsByAgent := map[string][]SkillResponse{}
	for _, row := range skillRows {
		aid := uuidToString(row.AgentID)
		skillsByAgent[aid] = append(skillsByAgent[aid], SkillResponse{
			ID:          uuidToString(row.ID),
			Name:        row.Name,
			Description: row.Description,
		})
	}

	assignmentRows, err := h.Queries.ListAgentRuntimeAssignmentsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent runtime assignments")
		return
	}
	runtimesByAgent := map[string][]AgentRuntimeRef{}
	for _, row := range assignmentRows {
		aid := uuidToString(row.AgentID)
		runtimesByAgent[aid] = append(runtimesByAgent[aid], AgentRuntimeRef{
			ID:          uuidToString(row.RuntimeID),
			Name:        row.RuntimeName,
			Status:      row.RuntimeStatus,
			RuntimeMode: row.RuntimeMode,
			Provider:    row.RuntimeProvider,
			DeviceInfo:  row.RuntimeDeviceInfo,
			OwnerID:     uuidToPtr(row.RuntimeOwnerID),
			LastUsedAt:  timestampToPtr(row.LastUsedAt),
		})
	}

	groupRows, err := h.Queries.ListAgentRuntimeGroupsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent groups")
		return
	}
	overrideRows, err := h.Queries.ListActiveRuntimeGroupOverridesByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load active overrides")
		return
	}
	overridesByGroup := map[string]AgentRuntimeGroupRefOverride{}
	for _, o := range overrideRows {
		overridesByGroup[uuidToString(o.GroupID)] = AgentRuntimeGroupRefOverride{
			RuntimeID:   uuidToString(o.RuntimeID),
			RuntimeName: o.RuntimeName,
			EndsAt:      timestampToString(o.EndsAt),
		}
	}
	groupsByAgent := map[string][]AgentRuntimeGroupRef{}
	for _, row := range groupRows {
		aid := uuidToString(row.AgentID)
		ref := AgentRuntimeGroupRef{
			ID:   uuidToString(row.GroupID),
			Name: row.GroupName,
		}
		if o, ok := overridesByGroup[ref.ID]; ok {
			oCopy := o
			ref.ActiveOverride = &oCopy
		}
		groupsByAgent[aid] = append(groupsByAgent[aid], ref)
	}

	// All agents (including private) are visible to workspace members.
	visible := make([]AgentResponse, 0, len(agents))
	for _, a := range agents {
		resp := agentToResponse(a)
		aid := uuidToString(a.ID)
		if skills := skillsByAgent[aid]; skills != nil {
			resp.Skills = skills
		}
		if rts := runtimesByAgent[aid]; rts != nil {
			resp.Runtimes = rts
			ids := make([]string, len(rts))
			for i, rt := range rts {
				ids[i] = rt.ID
			}
			resp.RuntimeIDs = ids
		}
		if grps := groupsByAgent[aid]; grps != nil {
			resp.Groups = grps
		}
		// Redact custom_env for users who are not the agent owner or workspace owner/admin.
		if !canViewAgentEnv(a, userID, member.Role) {
			redactEnv(&resp)
		}
		visible = append(visible, resp)
	}

	writeJSON(w, http.StatusOK, visible)
}

func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	resp, err := h.buildAgentResponse(r.Context(), agent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}

	// Redact custom_env for users who are not the agent owner or workspace owner/admin.
	userID := requestUserID(r)
	if member, ok := ctxMember(r.Context()); ok {
		if !canViewAgentEnv(agent, userID, member.Role) {
			redactEnv(&resp)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type CreateAgentRequest struct {
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Instructions       string            `json:"instructions"`
	AvatarURL          *string           `json:"avatar_url"`
	RuntimeIDs         []string          `json:"runtime_ids"`
	GroupIDs           []string          `json:"group_ids"`
	RuntimeConfig      any               `json:"runtime_config"`
	CustomEnv          map[string]string `json:"custom_env"`
	CustomArgs         []string          `json:"custom_args"`
	Visibility         string            `json:"visibility"`
	MaxConcurrentTasks int32             `json:"max_concurrent_tasks"`
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

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
	if len(req.RuntimeIDs) == 0 && len(req.GroupIDs) == 0 {
		writeError(w, http.StatusBadRequest, "at least one runtime_id or group_id is required")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	if req.MaxConcurrentTasks == 0 {
		req.MaxConcurrentTasks = 6
	}

	// Validate all runtimes belong to this workspace; capture the first for the
	// legacy agent.runtime_mode column. That column is a denormalised snapshot
	// left over from the single-runtime design; when runtimes have mixed modes
	// (e.g. local + cloud) we record only the first, and downstream UI should
	// read the real mode off the individual runtime. Replace this field with a
	// computed "mixed" value or drop it entirely once every consumer has moved
	// off it.
	runtimeUUIDs := make([]pgtype.UUID, 0, len(req.RuntimeIDs))
	var primaryMode string
	for i, rid := range req.RuntimeIDs {
		rt, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
			ID:          parseUUID(rid),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid runtime_id: %s", rid))
			return
		}
		runtimeUUIDs = append(runtimeUUIDs, rt.ID)
		if i == 0 {
			primaryMode = rt.RuntimeMode
		}
	}
	if primaryMode == "" {
		primaryMode = "local"
	}

	groupUUIDs := make([]pgtype.UUID, 0, len(req.GroupIDs))
	for _, gid := range req.GroupIDs {
		if _, err := h.Queries.GetRuntimeGroupInWorkspace(r.Context(), db.GetRuntimeGroupInWorkspaceParams{
			ID:          parseUUID(gid),
			WorkspaceID: parseUUID(workspaceID),
		}); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid group_id: %s", gid))
			return
		}
		groupUUIDs = append(groupUUIDs, parseUUID(gid))
	}

	rc, _ := json.Marshal(req.RuntimeConfig)
	if req.RuntimeConfig == nil {
		rc = []byte("{}")
	}

	ce, _ := json.Marshal(req.CustomEnv)
	if req.CustomEnv == nil {
		ce = []byte("{}")
	}

	ca, _ := json.Marshal(req.CustomArgs)
	if req.CustomArgs == nil {
		ca = []byte("[]")
	}

	// Create agent + assignments in a single transaction.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	agent, err := qtx.CreateAgent(r.Context(), db.CreateAgentParams{
		WorkspaceID:        parseUUID(workspaceID),
		Name:               req.Name,
		Description:        req.Description,
		Instructions:       req.Instructions,
		AvatarUrl:          ptrToText(req.AvatarURL),
		RuntimeMode:        primaryMode,
		RuntimeConfig:      rc,
		Visibility:         req.Visibility,
		MaxConcurrentTasks: req.MaxConcurrentTasks,
		OwnerID:            parseUUID(ownerID),
		CustomEnv:          ce,
		CustomArgs:         ca,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "agent_workspace_name_unique" {
			writeError(w, http.StatusConflict, fmt.Sprintf("an agent named %q already exists in this workspace", req.Name))
			return
		}
		slog.Warn("create agent failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create agent: "+err.Error())
		return
	}

	for _, rtID := range runtimeUUIDs {
		if err := qtx.AddAgentRuntimeAssignment(r.Context(), db.AddAgentRuntimeAssignmentParams{
			AgentID:   agent.ID,
			RuntimeID: rtID,
		}); err != nil {
			// FK violation means the runtime was deleted between validation
			// and insert — treat as a clean 400 rather than 500.
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				writeError(w, http.StatusBadRequest, "invalid runtime_id")
				return
			}
			slog.Warn("add agent runtime assignment failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
			writeError(w, http.StatusInternalServerError, "failed to assign runtime")
			return
		}
	}

	for _, gid := range groupUUIDs {
		if err := qtx.AddAgentRuntimeGroup(r.Context(), db.AddAgentRuntimeGroupParams{
			AgentID: agent.ID,
			GroupID: gid,
		}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				writeError(w, http.StatusBadRequest, "invalid group_id")
				return
			}
			slog.Warn("add agent runtime group failed", "agent_id", uuidToString(agent.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to assign group")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	slog.Info("agent created", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "name", agent.Name, "workspace_id", workspaceID)...)

	// Reload agent after reconcile so status is up to date.
	h.TaskService.ReconcileAgentStatus(r.Context(), agent.ID)
	if reloaded, err := h.Queries.GetAgent(r.Context(), agent.ID); err == nil {
		agent = reloaded
	}

	resp, err := h.buildAgentResponse(r.Context(), agent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}
	actorType, actorID := h.resolveActor(r, ownerID, workspaceID)
	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": resp})
	writeJSON(w, http.StatusCreated, resp)
}

type UpdateAgentRequest struct {
	Name               *string            `json:"name"`
	Description        *string            `json:"description"`
	Instructions       *string            `json:"instructions"`
	AvatarURL          *string            `json:"avatar_url"`
	RuntimeIDs         *[]string          `json:"runtime_ids"`
	GroupIDs           *[]string          `json:"group_ids"`
	RuntimeConfig      any                `json:"runtime_config"`
	CustomEnv          *map[string]string `json:"custom_env"`
	CustomArgs         *[]string          `json:"custom_args"`
	Visibility         *string            `json:"visibility"`
	Status             *string            `json:"status"`
	MaxConcurrentTasks *int32             `json:"max_concurrent_tasks"`
}

// canViewAgentEnv checks whether the requesting user is allowed to see the
// agent's custom environment variables. Only the agent owner or workspace
// owner/admin may view them; for everyone else the field is redacted.
func canViewAgentEnv(agent db.Agent, userID string, memberRole string) bool {
	if roleAllowed(memberRole, "owner", "admin") {
		return true
	}
	return uuidToString(agent.OwnerID) == userID
}

// redactEnv masks custom_env values in the response when the caller is not
// authorised to view them. Keys are preserved so members can see which
// variables are configured; values are replaced with "****".
func redactEnv(resp *AgentResponse) {
	masked := make(map[string]string, len(resp.CustomEnv))
	for k := range resp.CustomEnv {
		masked[k] = "****"
	}
	resp.CustomEnv = masked
	resp.CustomEnvRedacted = true
}

// canManageAgent checks whether the current user can update or archive an agent.
// Only the agent owner or workspace owner/admin can manage any agent,
// regardless of whether it is public or private.
func (h *Handler) canManageAgent(w http.ResponseWriter, r *http.Request, agent db.Agent) bool {
	wsID := uuidToString(agent.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, wsID, "agent not found", "owner", "admin", "member")
	if !ok {
		return false
	}
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	isAgentOwner := uuidToString(agent.OwnerID) == requestUserID(r)
	if !isAdmin && !isAgentOwner {
		writeError(w, http.StatusForbidden, "only the agent owner can manage this agent")
		return false
	}
	return true
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Enforce combined invariant: after this update the agent must still have
	// at least one runtime source (direct runtime or group).
	effectiveRuntimeCount := 0
	effectiveGroupCount := 0
	if req.RuntimeIDs != nil {
		effectiveRuntimeCount = len(*req.RuntimeIDs)
	} else {
		n, _ := h.Queries.CountAgentRuntimeAssignments(r.Context(), parseUUID(id))
		effectiveRuntimeCount = int(n)
	}
	if req.GroupIDs != nil {
		effectiveGroupCount = len(*req.GroupIDs)
	} else {
		grps, _ := h.Queries.ListAgentRuntimeGroupsByAgent(r.Context(), parseUUID(id))
		effectiveGroupCount = len(grps)
	}
	if effectiveRuntimeCount == 0 && effectiveGroupCount == 0 {
		writeError(w, http.StatusBadRequest, "at least one runtime_id or group_id is required")
		return
	}

	// Validate runtime_ids if provided.
	var runtimeUUIDs []pgtype.UUID
	var primaryMode string
	if req.RuntimeIDs != nil {
		runtimeUUIDs = make([]pgtype.UUID, 0, len(*req.RuntimeIDs))
		for i, rid := range *req.RuntimeIDs {
			rt, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
				ID:          parseUUID(rid),
				WorkspaceID: agent.WorkspaceID,
			})
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid runtime_id: %s", rid))
				return
			}
			if i == 0 {
				primaryMode = rt.RuntimeMode
			}
			runtimeUUIDs = append(runtimeUUIDs, rt.ID)
		}
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
	if req.Instructions != nil {
		params.Instructions = pgtype.Text{String: *req.Instructions, Valid: true}
	}
	if req.AvatarURL != nil {
		params.AvatarUrl = pgtype.Text{String: *req.AvatarURL, Valid: true}
	}
	if req.RuntimeConfig != nil {
		rc, _ := json.Marshal(req.RuntimeConfig)
		params.RuntimeConfig = rc
	}
	if req.CustomEnv != nil {
		ce, _ := json.Marshal(*req.CustomEnv)
		params.CustomEnv = ce
	}
	if req.CustomArgs != nil {
		ca, _ := json.Marshal(*req.CustomArgs)
		params.CustomArgs = ca
	}
	if req.RuntimeIDs != nil && len(runtimeUUIDs) > 0 && primaryMode != "" {
		params.RuntimeMode = pgtype.Text{String: primaryMode, Valid: true}
	} else if req.RuntimeIDs != nil && len(runtimeUUIDs) == 0 {
		// runtime_ids set to empty — derive mode from groups instead.
		var groupIDsToCheck []pgtype.UUID
		if req.GroupIDs != nil {
			for _, gid := range *req.GroupIDs {
				groupIDsToCheck = append(groupIDsToCheck, parseUUID(gid))
			}
		} else {
			existing, _ := h.Queries.ListAgentRuntimeGroupsByAgent(r.Context(), parseUUID(id))
			for _, g := range existing {
				groupIDsToCheck = append(groupIDsToCheck, g.GroupID)
			}
		}
		for _, gid := range groupIDsToCheck {
			members, _ := h.Queries.ListRuntimeGroupMembers(r.Context(), gid)
			if len(members) > 0 {
				rt, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
					ID:          members[0].RuntimeID,
					WorkspaceID: agent.WorkspaceID,
				})
				if err == nil {
					params.RuntimeMode = pgtype.Text{String: rt.RuntimeMode, Valid: true}
				}
				break
			}
		}
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

	// Run agent update + runtime assignment replacement in a single transaction.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	agent, err = qtx.UpdateAgent(r.Context(), params)
	if err != nil {
		slog.Warn("update agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update agent: "+err.Error())
		return
	}

	if req.RuntimeIDs != nil {
		// Add new assignments first (ON CONFLICT DO NOTHING preserves created_at
		// for surviving rows), then prune assignments not in the new set.
		for _, rtID := range runtimeUUIDs {
			if err := qtx.AddAgentRuntimeAssignment(r.Context(), db.AddAgentRuntimeAssignmentParams{
				AgentID:   agent.ID,
				RuntimeID: rtID,
			}); err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23503" {
					writeError(w, http.StatusBadRequest, "invalid runtime_id")
					return
				}
				slog.Warn("add agent runtime assignment failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
				writeError(w, http.StatusInternalServerError, "failed to assign runtime")
				return
			}
		}
		if err := qtx.RemoveAgentRuntimeAssignmentsNotIn(r.Context(), db.RemoveAgentRuntimeAssignmentsNotInParams{
			AgentID:    agent.ID,
			RuntimeIds: runtimeUUIDs,
		}); err != nil {
			slog.Warn("prune agent runtime assignments failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to prune runtime assignments")
			return
		}
	}

	if req.GroupIDs != nil {
		groupUUIDs := make([]pgtype.UUID, len(*req.GroupIDs))
		for i, gid := range *req.GroupIDs {
			if _, err := h.Queries.GetRuntimeGroupInWorkspace(r.Context(), db.GetRuntimeGroupInWorkspaceParams{
				ID:          parseUUID(gid),
				WorkspaceID: agent.WorkspaceID,
			}); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid group_id: %s", gid))
				return
			}
			groupUUIDs[i] = parseUUID(gid)
		}
		for _, gid := range groupUUIDs {
			if err := qtx.AddAgentRuntimeGroup(r.Context(), db.AddAgentRuntimeGroupParams{
				AgentID: agent.ID,
				GroupID: gid,
			}); err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23503" {
					writeError(w, http.StatusBadRequest, "invalid group_id")
					return
				}
				slog.Warn("add agent runtime group failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
				writeError(w, http.StatusInternalServerError, "failed to add group link")
				return
			}
		}
		if err := qtx.RemoveAgentRuntimeGroupsNotIn(r.Context(), db.RemoveAgentRuntimeGroupsNotInParams{
			AgentID:  agent.ID,
			GroupIds: groupUUIDs,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to prune group links")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	resp, err := h.buildAgentResponse(r.Context(), agent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}
	slog.Info("agent updated", append(logger.RequestAttrs(r), "agent_id", id, "workspace_id", uuidToString(agent.WorkspaceID))...)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(agent.WorkspaceID))
	h.publish(protocol.EventAgentStatus, uuidToString(agent.WorkspaceID), actorType, actorID, map[string]any{"agent": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ArchiveAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "agent is already archived")
		return
	}

	userID := requestUserID(r)
	archived, err := h.Queries.ArchiveAgent(r.Context(), db.ArchiveAgentParams{
		ID:         parseUUID(id),
		ArchivedBy: parseUUID(userID),
	})
	if err != nil {
		slog.Warn("archive agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to archive agent")
		return
	}

	// Cancel all pending/active tasks for this agent.
	if err := h.Queries.CancelAgentTasksByAgent(r.Context(), parseUUID(id)); err != nil {
		slog.Warn("cancel agent tasks on archive failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
	}

	wsID := uuidToString(archived.WorkspaceID)
	slog.Info("agent archived", append(logger.RequestAttrs(r), "agent_id", id, "workspace_id", wsID)...)
	resp, err := h.buildAgentResponse(r.Context(), archived)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, wsID)
	h.publish(protocol.EventAgentArchived, wsID, actorType, actorID, map[string]any{"agent": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RestoreAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	if !agent.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "agent is not archived")
		return
	}

	restored, err := h.Queries.RestoreAgent(r.Context(), parseUUID(id))
	if err != nil {
		slog.Warn("restore agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to restore agent")
		return
	}

	wsID := uuidToString(restored.WorkspaceID)
	slog.Info("agent restored", append(logger.RequestAttrs(r), "agent_id", id, "workspace_id", wsID)...)
	resp, err := h.buildAgentResponse(r.Context(), restored)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build response")
		return
	}
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, wsID)
	h.publish(protocol.EventAgentRestored, wsID, actorType, actorID, map[string]any{"agent": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListAgentTasks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.loadAgentForUser(w, r, id); !ok {
		return
	}

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
