package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type AgentResponse struct {
	ID                 string            `json:"id"`
	WorkspaceID        string            `json:"workspace_id"`
	RuntimeID          string            `json:"runtime_id"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Instructions       string            `json:"instructions"`
	AvatarURL          *string           `json:"avatar_url"`
	RuntimeMode        string            `json:"runtime_mode"`
	RuntimeConfig      any               `json:"runtime_config"`
	CustomEnv          map[string]string `json:"custom_env"`
	CustomArgs         []string          `json:"custom_args"`
	McpConfig          json.RawMessage   `json:"mcp_config"`
	CustomEnvRedacted  bool              `json:"custom_env_redacted"`
	McpConfigRedacted  bool              `json:"mcp_config_redacted"`
	Visibility         string            `json:"visibility"`
	Status             string            `json:"status"`
	MaxConcurrentTasks int32             `json:"max_concurrent_tasks"`
	OwnerID            *string           `json:"owner_id"`
	Skills             []SkillResponse   `json:"skills"`
	CreatedAt          string            `json:"created_at"`
	UpdatedAt          string            `json:"updated_at"`
	ArchivedAt         *string           `json:"archived_at"`
	ArchivedBy         *string           `json:"archived_by"`
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

	var mcpConfig json.RawMessage
	if a.McpConfig != nil {
		mcpConfig = json.RawMessage(a.McpConfig)
	}

	return AgentResponse{
		ID:                 uuidToString(a.ID),
		WorkspaceID:        uuidToString(a.WorkspaceID),
		RuntimeID:          uuidToString(a.RuntimeID),
		Name:               a.Name,
		Description:        a.Description,
		Instructions:       a.Instructions,
		AvatarURL:          textToPtr(a.AvatarUrl),
		RuntimeMode:        a.RuntimeMode,
		RuntimeConfig:      rc,
		CustomEnv:          customEnv,
		CustomArgs:         customArgs,
		McpConfig:          mcpConfig,
		Visibility:         a.Visibility,
		Status:             a.Status,
		MaxConcurrentTasks: a.MaxConcurrentTasks,
		OwnerID:            uuidToPtr(a.OwnerID),
		Skills:             []SkillResponse{},
		CreatedAt:          timestampToString(a.CreatedAt),
		UpdatedAt:          timestampToString(a.UpdatedAt),
		ArchivedAt:         timestampToPtr(a.ArchivedAt),
		ArchivedBy:         uuidToPtr(a.ArchivedBy),
	}
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
	SessionID             string         `json:"session_id,omitempty"`              // concrete session id from this task run
	WorkDir               string         `json:"work_dir,omitempty"`                // concrete work dir from this task run
	ResumeSessionID       string         `json:"resume_session_id,omitempty"`       // explicit session id selected for Continue/resume
	ResumeSource          string         `json:"resume_source,omitempty"`           // source label (task/external/manual)
	ResumeCommand         string         `json:"resume_command,omitempty"`          // ready-to-run command: codex resume <session_id>
	PriorSessionID        string         `json:"prior_session_id,omitempty"`        // session ID from a previous task on same issue
	PriorWorkDir          string         `json:"prior_work_dir,omitempty"`          // work_dir from a previous task on same issue
	TriggerCommentID      *string        `json:"trigger_comment_id,omitempty"`      // comment that triggered this task
	TriggerCommentContent string         `json:"trigger_comment_content,omitempty"` // content of the triggering comment
	ChatSessionID         string         `json:"chat_session_id,omitempty"`         // non-empty for chat tasks
	ChatMessage           string         `json:"chat_message,omitempty"`            // user message for chat tasks
}

type ExternalSessionResponse struct {
	SessionID    string `json:"session_id"`
	WorkDir      string `json:"work_dir,omitempty"`
	LastSeenAt   string `json:"last_seen_at"`
	IssueID      string `json:"issue_id,omitempty"`
	SourceTaskID string `json:"source_task_id,omitempty"`
	Source       string `json:"source,omitempty"`     // session_file/process/merged
	IsRunning    bool   `json:"is_running,omitempty"` // true when discovered from live process table
	LeaderPID    int    `json:"leader_pid,omitempty"` // process id that currently owns this resume session
	Command      string `json:"command,omitempty"`    // full command line from process table
	TTY          string `json:"tty,omitempty"`        // tty from process table, when available
}

type ResumeExternalSessionRequest struct {
	SessionID string `json:"session_id"`
	WorkDir   string `json:"work_dir"`
	IssueID   string `json:"issue_id"`
	Priority  *int32 `json:"priority,omitempty"`
}

type BindTaskIssueRequest struct {
	IssueID string `json:"issue_id"`
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
	McpConfig    json.RawMessage          `json:"mcp_config,omitempty"`
}

type codexSessionMetaEvent struct {
	Type    string `json:"type"`
	Payload struct {
		ID      string `json:"id"`
		Cwd     string `json:"cwd"`
		WorkDir string `json:"work_dir"`
	} `json:"payload"`
}

type codexSessionRecord struct {
	SessionID  string
	WorkDir    string
	LastSeenAt time.Time
}

type codexResumeProcess struct {
	SessionID string
	PID       int
	PPID      int
	TTY       string
	Command   string
}

const (
	defaultExternalSessionLookbackDays = 7
	maxExternalSessionLookbackDays     = 30
	defaultManualResumePriority        = 50
	resumeErrorCodeIssueRequired       = "issue_id_required"
)

var rolloutSessionIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
var codexResumeCommandSessionPattern = regexp.MustCompile(`(?i)\bresume\b\s+([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

func taskToResponse(t db.AgentTaskQueue) AgentTaskResponse {
	var result any
	if t.Result != nil {
		json.Unmarshal(t.Result, &result)
	}
	resumeSessionID, resumeSource := extractResumeMetadataFromTaskContext(t)
	resumeCommand := ""
	if resumeSessionID != "" {
		resumeCommand = fmt.Sprintf("codex resume %s", resumeSessionID)
	}

	resp := AgentTaskResponse{
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
		SessionID:        t.SessionID.String,
		WorkDir:          t.WorkDir.String,
		ResumeSessionID:  resumeSessionID,
		ResumeSource:     resumeSource,
		ResumeCommand:    resumeCommand,
		TriggerCommentID: uuidToPtr(t.TriggerCommentID),
		ChatSessionID:    uuidToString(t.ChatSessionID),
	}
	// Keep backward compatibility for clients that already read prior_session_id.
	if resp.PriorSessionID == "" && resp.ResumeSessionID != "" {
		resp.PriorSessionID = resp.ResumeSessionID
	}

	return resp
}

func extractResumeMetadataFromTaskContext(task db.AgentTaskQueue) (sessionID, source string) {
	if len(task.Context) == 0 {
		return "", ""
	}

	var taskCtx map[string]any
	if err := json.Unmarshal(task.Context, &taskCtx); err != nil {
		return "", ""
	}

	if rawSessionID, ok := taskCtx["resume_session_id"].(string); ok {
		sessionID = strings.TrimSpace(rawSessionID)
	}
	if rawSource, ok := taskCtx["resume_source"].(string); ok {
		source = strings.TrimSpace(rawSource)
	}
	return sessionID, source
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

	// Batch-load skills for all agents to avoid N+1.
	skillRows, err := h.Queries.ListAgentSkillsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	skillMap := map[string][]SkillResponse{}
	for _, row := range skillRows {
		agentID := uuidToString(row.AgentID)
		skillMap[agentID] = append(skillMap[agentID], SkillResponse{
			ID:          uuidToString(row.ID),
			Name:        row.Name,
			Description: row.Description,
		})
	}

	// All agents (including private) are visible to workspace members.
	visible := make([]AgentResponse, 0, len(agents))
	for _, a := range agents {
		resp := agentToResponse(a)
		if skills, ok := skillMap[resp.ID]; ok {
			resp.Skills = skills
		}
		// Redact sensitive fields for users who are not the agent owner or workspace owner/admin.
		if !canViewAgentEnv(a, userID, member.Role) {
			redactEnv(&resp)
			redactMcpConfig(&resp)
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
	resp := agentToResponse(agent)
	skills, err := h.Queries.ListAgentSkills(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	if len(skills) > 0 {
		resp.Skills = make([]SkillResponse, len(skills))
		for i, s := range skills {
			resp.Skills[i] = skillToResponse(s)
		}
	}

	// Redact sensitive fields for users who are not the agent owner or workspace owner/admin.
	userID := requestUserID(r)
	if member, ok := ctxMember(r.Context()); ok {
		if !canViewAgentEnv(agent, userID, member.Role) {
			redactEnv(&resp)
			redactMcpConfig(&resp)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type CreateAgentRequest struct {
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Instructions       string            `json:"instructions"`
	AvatarURL          *string           `json:"avatar_url"`
	RuntimeID          string            `json:"runtime_id"`
	RuntimeConfig      any               `json:"runtime_config"`
	CustomEnv          map[string]string `json:"custom_env"`
	CustomArgs         []string          `json:"custom_args"`
	McpConfig          json.RawMessage   `json:"mcp_config"`
	Visibility         string            `json:"visibility"`
	MaxConcurrentTasks int32             `json:"max_concurrent_tasks"`
}

func decodeJSONBodyWithRawFields(body io.Reader, dst any) (map[string]json.RawMessage, error) {
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(payload, dst); err != nil {
		return nil, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}

	return raw, nil
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	var req CreateAgentRequest
	rawFields, err := decodeJSONBodyWithRawFields(r.Body, &req)
	if err != nil {
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
		req.Visibility = "private"
	}
	if req.MaxConcurrentTasks == 0 {
		req.MaxConcurrentTasks = 6
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

	ce, _ := json.Marshal(req.CustomEnv)
	if req.CustomEnv == nil {
		ce = []byte("{}")
	}

	ca, _ := json.Marshal(req.CustomArgs)
	if req.CustomArgs == nil {
		ca = []byte("[]")
	}

	var mc []byte
	if rawMcpConfig, ok := rawFields["mcp_config"]; ok && !bytes.Equal(bytes.TrimSpace(rawMcpConfig), []byte("null")) {
		mc = append([]byte(nil), rawMcpConfig...)
	}

	agent, err := h.Queries.CreateAgent(r.Context(), db.CreateAgentParams{
		WorkspaceID:        parseUUID(workspaceID),
		Name:               req.Name,
		Description:        req.Description,
		Instructions:       req.Instructions,
		AvatarUrl:          ptrToText(req.AvatarURL),
		RuntimeMode:        runtime.RuntimeMode,
		RuntimeConfig:      rc,
		RuntimeID:          runtime.ID,
		Visibility:         req.Visibility,
		MaxConcurrentTasks: req.MaxConcurrentTasks,
		OwnerID:            parseUUID(ownerID),
		CustomEnv:          ce,
		CustomArgs:         ca,
		McpConfig:          mc,
	})
	if err != nil {
		// Unique constraint on (workspace_id, name) — return a clear conflict error
		// so the UI can show the right message instead of a generic 500.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "agent_workspace_name_unique" {
			writeError(w, http.StatusConflict, fmt.Sprintf("an agent named %q already exists in this workspace", req.Name))
			return
		}
		slog.Warn("create agent failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create agent: "+err.Error())
		return
	}
	slog.Info("agent created", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "name", agent.Name, "workspace_id", workspaceID)...)

	if runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(r.Context(), agent.ID)
		agent, _ = h.Queries.GetAgent(r.Context(), agent.ID)
	}

	resp := agentToResponse(agent)
	actorType, actorID := h.resolveActor(r, ownerID, workspaceID)
	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": resp})
	writeJSON(w, http.StatusCreated, resp)
}

type UpdateAgentRequest struct {
	Name               *string            `json:"name"`
	Description        *string            `json:"description"`
	Instructions       *string            `json:"instructions"`
	AvatarURL          *string            `json:"avatar_url"`
	RuntimeID          *string            `json:"runtime_id"`
	RuntimeConfig      any                `json:"runtime_config"`
	CustomEnv          *map[string]string `json:"custom_env"`
	CustomArgs         *[]string          `json:"custom_args"`
	McpConfig          *json.RawMessage   `json:"mcp_config"`
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

// redactMcpConfig removes the mcp_config value from the response when the caller is not
// authorised to view it. The field is set to null; McpConfigRedacted is set to true so
// callers know a config exists without seeing its contents (which may contain secrets).
func redactMcpConfig(resp *AgentResponse) {
	if resp.McpConfig != nil {
		resp.McpConfig = nil
		resp.McpConfigRedacted = true
	}
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
	rawFields, err := decodeJSONBodyWithRawFields(r.Body, &req)
	if err != nil {
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
	rawMcpConfig, hasMcpConfig := rawFields["mcp_config"]
	shouldClearMcpConfig := hasMcpConfig && bytes.Equal(bytes.TrimSpace(rawMcpConfig), []byte("null"))
	if hasMcpConfig && !shouldClearMcpConfig {
		params.McpConfig = append([]byte(nil), rawMcpConfig...)
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

	agent, err = h.Queries.UpdateAgent(r.Context(), params)
	if err != nil {
		slog.Warn("update agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update agent: "+err.Error())
		return
	}

	// mcp_config: null in the request means explicitly clear the field.
	// COALESCE in UpdateAgent cannot set a column to NULL, so we use a dedicated query.
	if shouldClearMcpConfig {
		agent, err = h.Queries.ClearAgentMcpConfig(r.Context(), parseUUID(id))
		if err != nil {
			slog.Warn("clear agent mcp_config failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear mcp_config: "+err.Error())
			return
		}
	}

	resp := agentToResponse(agent)
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
	resp := agentToResponse(archived)
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
	resp := agentToResponse(restored)
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

func (h *Handler) ResumeAgentTask(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	sourceTaskID := chi.URLParam(r, "taskId")

	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return
	}

	sourceTask, err := h.Queries.GetAgentTask(r.Context(), parseUUID(sourceTaskID))
	if err != nil {
		writeError(w, http.StatusNotFound, "source task not found")
		return
	}

	if uuidToString(sourceTask.AgentID) != agentID {
		writeError(w, http.StatusForbidden, "task does not belong to this agent")
		return
	}
	if sourceTask.Status != "completed" {
		writeError(w, http.StatusBadRequest, "only completed tasks can be resumed")
		return
	}
	if !sourceTask.IssueID.Valid {
		writeError(w, http.StatusBadRequest, "only issue tasks can be resumed")
		return
	}
	if !sourceTask.SessionID.Valid || sourceTask.SessionID.String == "" {
		writeError(w, http.StatusBadRequest, "source task has no resumable session")
		return
	}
	if !agent.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "agent has no runtime")
		return
	}
	if err := h.ensureWorkspaceHasRepos(r.Context(), agent.WorkspaceID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database executor unavailable")
		return
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(r.Context(), db.HasPendingTaskForIssueAndAgentParams{
		IssueID: sourceTask.IssueID,
		AgentID: sourceTask.AgentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check pending tasks")
		return
	}
	if hasPending {
		writeError(w, http.StatusConflict, "there is already a pending task for this issue")
		return
	}

	newTask, err := h.Queries.CreateAgentTask(r.Context(), db.CreateAgentTaskParams{
		AgentID:   sourceTask.AgentID,
		RuntimeID: agent.RuntimeID,
		IssueID:   sourceTask.IssueID,
		Priority:  sourceTask.Priority,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue resume task")
		return
	}

	resumeContext := map[string]any{
		"resume_session_id":   sourceTask.SessionID.String,
		"resume_source_task":  sourceTaskID,
		"resume_source_agent": agentID,
	}
	if sourceTask.WorkDir.Valid && sourceTask.WorkDir.String != "" {
		resumeContext["resume_work_dir"] = sourceTask.WorkDir.String
	}

	ctxJSON, err := json.Marshal(resumeContext)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode resume context")
		return
	}

	if _, err := h.DB.Exec(r.Context(), `UPDATE agent_task_queue SET context = $2 WHERE id = $1`, newTask.ID, ctxJSON); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save resume context")
		return
	}
	newTask.Context = ctxJSON

	writeJSON(w, http.StatusCreated, taskToResponse(newTask))
}

func (h *Handler) ListAgentExternalSessions(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return
	}
	if !agent.RuntimeID.Valid {
		writeJSON(w, http.StatusOK, []ExternalSessionResponse{})
		return
	}

	runtime, err := h.Queries.GetAgentRuntime(r.Context(), agent.RuntimeID)
	if err == nil && runtime.Provider != "codex" {
		writeJSON(w, http.StatusOK, []ExternalSessionResponse{})
		return
	}

	days := parseLookbackDays(r.URL.Query().Get("days"))
	root := resolveCodexSessionRoot()

	fileSessions := []ExternalSessionResponse{}
	if root != "" {
		scanned, err := scanCodexSessions(root, days)
		if err != nil {
			slog.Warn("scan codex session files failed", append(logger.RequestAttrs(r), "error", err, "root", root)...)
		} else {
			fileSessions = scanned
		}
	}

	liveSessions, err := scanRunningCodexSessions()
	if err != nil {
		slog.Warn("scan running codex sessions failed", append(logger.RequestAttrs(r), "error", err)...)
	}

	external := mergeExternalSessions(fileSessions, liveSessions)

	taskRows, err := h.Queries.ListAgentTasks(r.Context(), parseUUID(agentID))
	if err == nil {
		latestBySession := map[string]db.AgentTaskQueue{}
		for _, task := range taskRows {
			candidateSessionIDs := map[string]struct{}{}
			if task.SessionID.Valid && task.SessionID.String != "" {
				candidateSessionIDs[task.SessionID.String] = struct{}{}
			}
			if resumeSID, _ := extractResumeMetadataFromTaskContext(task); resumeSID != "" {
				candidateSessionIDs[resumeSID] = struct{}{}
			}

			for sid := range candidateSessionIDs {
				if cur, exists := latestBySession[sid]; !exists || taskReferenceTime(task).After(taskReferenceTime(cur)) {
					latestBySession[sid] = task
				}
			}
		}

		for i := range external {
			if task, exists := latestBySession[external[i].SessionID]; exists {
				external[i].IssueID = uuidToString(task.IssueID)
				external[i].SourceTaskID = uuidToString(task.ID)
				if external[i].WorkDir == "" && task.WorkDir.Valid {
					external[i].WorkDir = task.WorkDir.String
				}
			}
		}
	}

	sortExternalSessions(external)

	writeJSON(w, http.StatusOK, external)
}

func (h *Handler) ResumeExternalSession(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return
	}
	if !agent.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "agent has no runtime")
		return
	}
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database executor unavailable")
		return
	}

	var req ResumeExternalSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.SessionID = strings.TrimSpace(req.SessionID)
	req.WorkDir = strings.TrimSpace(req.WorkDir)
	req.IssueID = strings.TrimSpace(req.IssueID)
	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	if req.IssueID == "" {
		writeErrorWithCode(
			w,
			http.StatusBadRequest,
			resumeErrorCodeIssueRequired,
			"issue_id is required when continuing a Codex session; create or reuse an issue first",
		)
		return
	}
	if err := h.ensureWorkspaceHasRepos(r.Context(), agent.WorkspaceID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	issueID := parseUUID(req.IssueID)
	if !issueID.Valid {
		writeError(w, http.StatusBadRequest, "invalid issue_id")
		return
	}
	if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueID,
		WorkspaceID: agent.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "issue_id does not belong to this workspace")
		return
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(r.Context(), db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: parseUUID(agentID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check pending tasks")
		return
	}
	if hasPending {
		writeError(w, http.StatusConflict, "there is already a pending task for this issue")
		return
	}

	priority := int32(defaultManualResumePriority)
	if req.Priority != nil {
		priority = *req.Priority
	}

	newTask, err := h.Queries.CreateAgentTask(r.Context(), db.CreateAgentTaskParams{
		AgentID:   parseUUID(agentID),
		RuntimeID: agent.RuntimeID,
		IssueID:   issueID,
		Priority:  priority,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue resume task")
		return
	}

	resumeContext := map[string]any{
		"resume_session_id": req.SessionID,
		"resume_source":     "external_codex_session",
	}
	if req.WorkDir != "" {
		resumeContext["resume_work_dir"] = req.WorkDir
	}

	ctxJSON, err := json.Marshal(resumeContext)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode resume context")
		return
	}
	if _, err := h.DB.Exec(r.Context(), `UPDATE agent_task_queue SET context = $2 WHERE id = $1`, newTask.ID, ctxJSON); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save resume context")
		return
	}
	newTask.Context = ctxJSON

	writeJSON(w, http.StatusCreated, taskToResponse(newTask))
}

func (h *Handler) ensureWorkspaceHasRepos(ctx context.Context, workspaceID pgtype.UUID) error {
	workspace, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("failed to load workspace repositories")
	}

	repos := parseWorkspaceRepos(workspace.Repos)
	if len(repos) > 0 {
		return nil
	}

	return fmt.Errorf("workspace has no repositories configured; attach at least one repository in Settings > Repositories before continuing")
}

func (h *Handler) BindAgentTaskIssue(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	taskID := chi.URLParam(r, "taskId")

	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return
	}
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database executor unavailable")
		return
	}

	var req BindTaskIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.IssueID = strings.TrimSpace(req.IssueID)
	if req.IssueID == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}

	issueID := parseUUID(req.IssueID)
	if !issueID.Valid {
		writeError(w, http.StatusBadRequest, "invalid issue_id")
		return
	}
	if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueID,
		WorkspaceID: agent.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "issue_id does not belong to this workspace")
		return
	}

	task, err := h.Queries.GetAgentTask(r.Context(), parseUUID(taskID))
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if uuidToString(task.AgentID) != agentID {
		writeError(w, http.StatusForbidden, "task does not belong to this agent")
		return
	}
	if task.ChatSessionID.Valid {
		writeError(w, http.StatusBadRequest, "chat tasks cannot be bound to issues")
		return
	}
	if task.IssueID.Valid {
		if uuidToString(task.IssueID) == req.IssueID {
			writeJSON(w, http.StatusOK, taskToResponse(task))
			return
		}
		writeError(w, http.StatusConflict, "task is already bound to a different issue")
		return
	}
	if task.Status != "queued" && task.Status != "dispatched" && task.Status != "running" {
		writeError(w, http.StatusBadRequest, "only queued/dispatched/running tasks can be bound")
		return
	}

	cmdTag, err := h.DB.Exec(
		r.Context(),
		`UPDATE agent_task_queue
		 SET issue_id = $2
		 WHERE id = $1
		   AND issue_id IS NULL
		   AND status IN ('queued', 'dispatched', 'running')`,
		task.ID,
		issueID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "issue already has a pending task for this agent")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to bind task issue")
		return
	}
	if cmdTag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "task cannot be bound in current state")
		return
	}

	updatedTask, err := h.Queries.GetAgentTask(r.Context(), parseUUID(taskID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated task")
		return
	}

	// Re-broadcast a dispatch-style event so issue pages can pick up an already
	// running task immediately after it gets bound to an issue.
	dispatchPayload := map[string]any{}
	if len(updatedTask.Context) > 0 {
		_ = json.Unmarshal(updatedTask.Context, &dispatchPayload)
	}
	if dispatchPayload == nil {
		dispatchPayload = map[string]any{}
	}
	dispatchPayload["task_id"] = uuidToString(updatedTask.ID)
	dispatchPayload["runtime_id"] = uuidToString(updatedTask.RuntimeID)
	dispatchPayload["issue_id"] = uuidToString(updatedTask.IssueID)
	dispatchPayload["agent_id"] = uuidToString(updatedTask.AgentID)

	wsID := uuidToString(agent.WorkspaceID)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, wsID)
	h.publish(protocol.EventTaskDispatch, wsID, actorType, actorID, dispatchPayload)

	writeJSON(w, http.StatusOK, taskToResponse(updatedTask))
}

func parseLookbackDays(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultExternalSessionLookbackDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultExternalSessionLookbackDays
	}
	if n > maxExternalSessionLookbackDays {
		return maxExternalSessionLookbackDays
	}
	return n
}

func resolveCodexSessionRoot() string {
	candidates := []string{
		strings.TrimSpace(os.Getenv("MULTICA_CODEX_SESSIONS_ROOT")),
		strings.TrimSpace(os.Getenv("CODEX_SESSION_DIR")),
	}
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		candidates = append(candidates, filepath.Join(codexHome, "sessions"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".codex", "sessions"))
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func resolveCodexProcRoot() string {
	candidates := []string{
		strings.TrimSpace(os.Getenv("MULTICA_CODEX_PROC_ROOT")),
		"/proc",
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func scanCodexSessions(root string, days int) ([]ExternalSessionResponse, error) {
	now := time.Now()
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	bySessionID := map[string]codexSessionRecord{}

	for offset := 0; offset < days; offset++ {
		day := now.AddDate(0, 0, -offset)
		dayDir := filepath.Join(root,
			fmt.Sprintf("%04d", day.Year()),
			fmt.Sprintf("%02d", int(day.Month())),
			fmt.Sprintf("%02d", day.Day()),
		)

		files, err := filepath.Glob(filepath.Join(dayDir, "*.jsonl"))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			record, ok := parseCodexSessionFile(f)
			if !ok || record.SessionID == "" {
				continue
			}
			if record.LastSeenAt.Before(cutoff) {
				continue
			}
			if cur, exists := bySessionID[record.SessionID]; !exists || record.LastSeenAt.After(cur.LastSeenAt) {
				bySessionID[record.SessionID] = record
			}
		}
	}

	result := make([]ExternalSessionResponse, 0, len(bySessionID))
	for _, record := range bySessionID {
		result = append(result, ExternalSessionResponse{
			SessionID:  record.SessionID,
			WorkDir:    record.WorkDir,
			LastSeenAt: record.LastSeenAt.UTC().Format(time.RFC3339),
			Source:     "session_file",
		})
	}
	sortExternalSessions(result)
	return result, nil
}

func scanRunningCodexSessions() ([]ExternalSessionResponse, error) {
	if runtime.GOOS != "linux" {
		return []ExternalSessionResponse{}, nil
	}

	procRoot := resolveCodexProcRoot()
	if procRoot != "" {
		sessions, err := scanRunningCodexSessionsFromProc(procRoot)
		if err == nil {
			return sessions, nil
		}
		// Fallback to ps when proc-root scan fails, to keep behavior resilient.
		slog.Warn("scan codex sessions from proc root failed; fallback to ps", "proc_root", procRoot, "error", err)
	}

	return scanRunningCodexSessionsFromPS()
}

func scanRunningCodexSessionsFromPS() ([]ExternalSessionResponse, error) {
	cmd := exec.Command("ps", "-eo", "pid=,ppid=,tty=,args=")
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	processes := parseCodexResumeProcesses(string(raw))
	if len(processes) == 0 {
		return []ExternalSessionResponse{}, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	bySessionID := map[string]ExternalSessionResponse{}

	for _, proc := range processes {
		workDir := readProcessCwd("/proc", proc.PID)
		candidate := ExternalSessionResponse{
			SessionID:  proc.SessionID,
			WorkDir:    workDir,
			LastSeenAt: now,
			Source:     "process",
			IsRunning:  true,
			LeaderPID:  proc.PID,
			Command:    proc.Command,
			TTY:        proc.TTY,
		}

		if existing, ok := bySessionID[proc.SessionID]; ok {
			bySessionID[proc.SessionID] = mergeExternalSession(existing, candidate)
			continue
		}
		bySessionID[proc.SessionID] = candidate
	}

	result := make([]ExternalSessionResponse, 0, len(bySessionID))
	for _, session := range bySessionID {
		result = append(result, session)
	}
	sortExternalSessions(result)
	return result, nil
}

func scanRunningCodexSessionsFromProc(procRoot string) ([]ExternalSessionResponse, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	bySessionID := map[string]ExternalSessionResponse{}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}

		command := readProcessCommand(procRoot, pid)
		if command == "" {
			continue
		}

		sessionID := sessionIDFromCodexResumeCommand(command)
		if sessionID == "" {
			continue
		}

		candidate := ExternalSessionResponse{
			SessionID:  sessionID,
			WorkDir:    readProcessCwd(procRoot, pid),
			LastSeenAt: now,
			Source:     "process",
			IsRunning:  true,
			LeaderPID:  pid,
			Command:    command,
			TTY:        readProcessTTY(procRoot, pid),
		}

		if existing, ok := bySessionID[sessionID]; ok {
			bySessionID[sessionID] = mergeExternalSession(existing, candidate)
			continue
		}
		bySessionID[sessionID] = candidate
	}

	result := make([]ExternalSessionResponse, 0, len(bySessionID))
	for _, session := range bySessionID {
		result = append(result, session)
	}
	sortExternalSessions(result)
	return result, nil
}

func parseCodexResumeProcesses(psOutput string) []codexResumeProcess {
	scanner := bufio.NewScanner(strings.NewReader(psOutput))
	scanner.Buffer(make([]byte, 0, 256*1024), 2*1024*1024)

	result := make([]codexResumeProcess, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		tty := strings.TrimSpace(fields[2])
		command := strings.TrimSpace(strings.Join(fields[3:], " "))
		if command == "" {
			continue
		}

		sessionID := sessionIDFromCodexResumeCommand(command)
		if sessionID == "" {
			continue
		}

		result = append(result, codexResumeProcess{
			SessionID: sessionID,
			PID:       pid,
			PPID:      ppid,
			TTY:       tty,
			Command:   command,
		})
	}

	return result
}

func sessionIDFromCodexResumeCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	lowerCommand := strings.ToLower(command)
	if !strings.Contains(lowerCommand, "codex") || !strings.Contains(lowerCommand, "resume") {
		return ""
	}

	match := codexResumeCommandSessionPattern.FindStringSubmatch(command)
	if len(match) < 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(match[1]))
}

func readProcessCommand(procRoot string, pid int) string {
	if procRoot == "" || pid <= 0 {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil || len(raw) == 0 {
		return ""
	}

	parts := strings.Split(string(raw), "\x00")
	args := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		args = append(args, part)
	}
	if len(args) == 0 {
		return ""
	}
	return strings.Join(args, " ")
}

func readProcessCwd(procRoot string, pid int) string {
	if procRoot == "" || pid <= 0 {
		return ""
	}
	cwd, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), "cwd"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cwd)
}

func readProcessTTY(procRoot string, pid int) string {
	if procRoot == "" || pid <= 0 {
		return ""
	}
	target, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), "fd", "0"))
	if err != nil {
		return ""
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	target = strings.TrimPrefix(target, "/dev/")
	if strings.HasPrefix(target, "pts/") || target == "tty" || target == "console" {
		return target
	}
	return ""
}

func mergeExternalSessions(groups ...[]ExternalSessionResponse) []ExternalSessionResponse {
	bySessionID := map[string]ExternalSessionResponse{}

	for _, group := range groups {
		for _, candidate := range group {
			candidate.SessionID = strings.TrimSpace(candidate.SessionID)
			if candidate.SessionID == "" {
				continue
			}

			if existing, ok := bySessionID[candidate.SessionID]; ok {
				bySessionID[candidate.SessionID] = mergeExternalSession(existing, candidate)
				continue
			}
			bySessionID[candidate.SessionID] = candidate
		}
	}

	result := make([]ExternalSessionResponse, 0, len(bySessionID))
	for _, session := range bySessionID {
		result = append(result, session)
	}
	sortExternalSessions(result)
	return result
}

func mergeExternalSession(existing ExternalSessionResponse, incoming ExternalSessionResponse) ExternalSessionResponse {
	merged := existing

	if merged.WorkDir == "" || (incoming.IsRunning && incoming.WorkDir != "") {
		merged.WorkDir = incoming.WorkDir
	}

	if merged.IssueID == "" && incoming.IssueID != "" {
		merged.IssueID = incoming.IssueID
	}
	if merged.SourceTaskID == "" && incoming.SourceTaskID != "" {
		merged.SourceTaskID = incoming.SourceTaskID
	}

	if compareSessionTimes(incoming.LastSeenAt, merged.LastSeenAt) > 0 {
		merged.LastSeenAt = incoming.LastSeenAt
	}

	merged.IsRunning = merged.IsRunning || incoming.IsRunning

	if merged.LeaderPID == 0 || (incoming.LeaderPID > 0 && incoming.LeaderPID < merged.LeaderPID) {
		merged.LeaderPID = incoming.LeaderPID
	}
	if merged.Command == "" && incoming.Command != "" {
		merged.Command = incoming.Command
	}
	if merged.TTY == "" && incoming.TTY != "" {
		merged.TTY = incoming.TTY
	}

	switch {
	case merged.Source == "":
		merged.Source = incoming.Source
	case incoming.Source == "":
	case merged.Source == incoming.Source:
	default:
		merged.Source = "merged"
	}

	return merged
}

func sortExternalSessions(sessions []ExternalSessionResponse) {
	sort.Slice(sessions, func(i, j int) bool {
		ti := parseSessionTimestamp(sessions[i].LastSeenAt)
		tj := parseSessionTimestamp(sessions[j].LastSeenAt)
		if ti.Equal(tj) {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return ti.After(tj)
	})
}

func parseSessionTimestamp(raw string) time.Time {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func compareSessionTimes(a, b string) int {
	ta := parseSessionTimestamp(a)
	tb := parseSessionTimestamp(b)
	switch {
	case ta.After(tb):
		return 1
	case ta.Before(tb):
		return -1
	default:
		return 0
	}
}

func parseCodexSessionFile(path string) (codexSessionRecord, bool) {
	stat, err := os.Stat(path)
	if err != nil {
		return codexSessionRecord{}, false
	}

	f, err := os.Open(path)
	if err != nil {
		return codexSessionRecord{}, false
	}
	defer f.Close()

	record := codexSessionRecord{
		LastSeenAt: stat.ModTime(),
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"session_meta"`) {
			continue
		}

		var evt codexSessionMetaEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil || evt.Type != "session_meta" {
			continue
		}

		record.SessionID = strings.TrimSpace(evt.Payload.ID)
		record.WorkDir = strings.TrimSpace(evt.Payload.Cwd)
		if record.WorkDir == "" {
			record.WorkDir = strings.TrimSpace(evt.Payload.WorkDir)
		}
		if record.SessionID != "" {
			return record, true
		}
	}

	record.SessionID = sessionIDFromRolloutFilename(path)
	if record.SessionID != "" {
		return record, true
	}

	return codexSessionRecord{}, false
}

func sessionIDFromRolloutFilename(path string) string {
	base := filepath.Base(path)
	return rolloutSessionIDPattern.FindString(base)
}

func taskReferenceTime(task db.AgentTaskQueue) time.Time {
	if task.CompletedAt.Valid {
		return task.CompletedAt.Time
	}
	if task.StartedAt.Valid {
		return task.StartedAt.Time
	}
	if task.DispatchedAt.Valid {
		return task.DispatchedAt.Time
	}
	if task.CreatedAt.Valid {
		return task.CreatedAt.Time
	}
	return time.Time{}
}
