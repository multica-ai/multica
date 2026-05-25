package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Mirrors AGENT_DESCRIPTION_MAX_LENGTH in packages/core/agents/constants.ts
// and the agent_description_length CHECK constraint in migration 060. Counted
// in unicode code points (utf8.RuneCountInString), matching Postgres
// char_length and the front-end's String.prototype.length-with-counter UX.
const maxAgentDescriptionLength = 255

type AgentResponse struct {
	ID                      string              `json:"id"`
	WorkspaceID             string              `json:"workspace_id"`
	RuntimeID               string              `json:"runtime_id"`
	Name                    string              `json:"name"`
	Description             string              `json:"description"`
	Instructions            string              `json:"instructions"`
	AvatarURL               *string             `json:"avatar_url"`
	RuntimeMode             string              `json:"runtime_mode"`
	RuntimeConfig           any                 `json:"runtime_config"`
	CustomEnv               map[string]string   `json:"custom_env"`
	CustomArgs              []string            `json:"custom_args"`
	McpConfig               json.RawMessage     `json:"mcp_config"`
	CustomEnvRedacted       bool                `json:"custom_env_redacted"`
	CustomEnvRedactedReason string              `json:"custom_env_redacted_reason,omitempty"`
	McpConfigRedacted       bool                `json:"mcp_config_redacted"`
	Visibility              string              `json:"visibility"`
	Status                  string              `json:"status"`
	MaxConcurrentTasks      int32               `json:"max_concurrent_tasks"`
	Model                   string              `json:"model"`
	ThinkingLevel           string              `json:"thinking_level"`
	OwnerID                 *string             `json:"owner_id"`
	AllowedUserIDs          []string            `json:"allowed_user_ids"`
	Skills                  []AgentSkillSummary `json:"skills"`
	CreatedAt               string              `json:"created_at"`
	UpdatedAt               string              `json:"updated_at"`
	ArchivedAt              *string             `json:"archived_at"`
	ArchivedBy              *string             `json:"archived_by"`
	CustomEnvCopiedPending  bool                `json:"custom_env_copied_pending"`
}

type AgentAllowedPrincipalResponse struct {
	ID        string  `json:"id"`
	AgentID   string  `json:"agent_id"`
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	CreatedAt string  `json:"created_at"`
}

func agentAllowedPrincipalToResponse(row db.ListAgentAllowedPrincipalsRow) AgentAllowedPrincipalResponse {
	return AgentAllowedPrincipalResponse{
		ID:        uuidToString(row.ID),
		AgentID:   uuidToString(row.AgentID),
		UserID:    uuidToString(row.PrincipalID),
		Name:      row.UserName,
		Email:     row.UserEmail,
		AvatarURL: textToPtr(row.UserAvatarUrl),
		CreatedAt: timestampToString(row.CreatedAt),
	}
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
		ID:                     uuidToString(a.ID),
		WorkspaceID:            uuidToString(a.WorkspaceID),
		RuntimeID:              uuidToString(a.RuntimeID),
		Name:                   a.Name,
		Description:            a.Description,
		Instructions:           a.Instructions,
		AvatarURL:              textToPtr(a.AvatarUrl),
		RuntimeMode:            a.RuntimeMode,
		RuntimeConfig:          rc,
		CustomEnv:              customEnv,
		CustomArgs:             customArgs,
		McpConfig:              mcpConfig,
		Visibility:             a.Visibility,
		Status:                 a.Status,
		MaxConcurrentTasks:     a.MaxConcurrentTasks,
		Model:                  a.Model.String,
		ThinkingLevel:          a.ThinkingLevel.String,
		OwnerID:                uuidToPtr(a.OwnerID),
		AllowedUserIDs:         []string{},
		Skills:                 []AgentSkillSummary{},
		CreatedAt:              timestampToString(a.CreatedAt),
		UpdatedAt:              timestampToString(a.UpdatedAt),
		ArchivedAt:             timestampToPtr(a.ArchivedAt),
		ArchivedBy:             uuidToPtr(a.ArchivedBy),
		CustomEnvCopiedPending: a.CustomEnvCopiedPending,
	}
}

// stripCustomEnvValuesForCopy keeps env keys from the source but clears all values.
// The second return is true when the source had at least one non-empty value (secrets
// that must be re-entered on the copy).
func stripCustomEnvValuesForCopy(src []byte) ([]byte, bool) {
	var m map[string]string
	if err := json.Unmarshal(src, &m); err != nil || len(m) == 0 {
		return []byte("{}"), false
	}
	hadSecret := false
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = ""
		if strings.TrimSpace(v) != "" {
			hadSecret = true
		}
	}
	b, _ := json.Marshal(out)
	return b, hadSecret
}

// RepoData holds repository information included in claim responses so the
// daemon can set up worktrees for each workspace repo.
type RepoData struct {
	URL string `json:"url"`
}

// ProjectResourceData is the wire shape for a project resource included in a
// claim response. The daemon reads this list and writes it into the agent's
// working directory so skills/agents can discover project-scoped context.
//
// resource_ref is type-specific JSON; the daemon doesn't interpret it beyond
// well-known fields like url for github_repo. New types can be added without
// changing this struct.
type ProjectResourceData struct {
	ID           string          `json:"id"`
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        string          `json:"label,omitempty"`
}

type AgentTaskResponse struct {
	ID          string `json:"id"`
	AgentID     string `json:"agent_id"`
	RuntimeID   string `json:"runtime_id"`
	IssueID     string `json:"issue_id"`
	WorkspaceID string `json:"workspace_id"`
	// WorkspaceContext is the workspace-level system prompt set in workspace
	// settings (`workspace.context` DB column). Injected into the agent brief
	// as `## Workspace Context` so every agent running in this workspace —
	// regardless of issue / chat / autopilot / quick-create — sees the same
	// shared context. Empty when the workspace owner hasn't set it.
	WorkspaceContext        string                `json:"workspace_context,omitempty"`
	Status                  string                `json:"status"`
	Priority                int32                 `json:"priority"`
	DispatchedAt            *string               `json:"dispatched_at"`
	StartedAt               *string               `json:"started_at"`
	CompletedAt             *string               `json:"completed_at"`
	Result                  any                   `json:"result"`
	Error                   *string               `json:"error"`
	Context                 any                   `json:"context,omitempty"`
	FailureReason           string                `json:"failure_reason,omitempty"` // see TaskService.MaybeRetryFailedTask
	Attempt                 int32                 `json:"attempt"`
	MaxAttempts             int32                 `json:"max_attempts"`
	ParentTaskID            *string               `json:"parent_task_id,omitempty"`
	Agent                   *TaskAgentData        `json:"agent,omitempty"`
	Repos                   []RepoData            `json:"repos,omitempty"`
	ProjectID               string                `json:"project_id,omitempty"`        // issue's project, when present
	ProjectTitle            string                `json:"project_title,omitempty"`     // for surfacing in agent context
	ProjectResources        []ProjectResourceData `json:"project_resources,omitempty"` // resources attached to the project
	CreatedAt               string                `json:"created_at"`
	PriorSessionID          string                `json:"prior_session_id,omitempty"`          // session ID from a previous task on same issue
	PriorWorkDir            string                `json:"prior_work_dir,omitempty"`            // work_dir from a previous task on same issue
	WorkDir                 string                `json:"work_dir,omitempty"`                  // local working directory pinned for this task; populated once the daemon reports it
	TriggerCommentID        *string               `json:"trigger_comment_id,omitempty"`        // comment that triggered this task
	TriggerCommentContent   string                `json:"trigger_comment_content,omitempty"`   // content of the triggering comment
	TriggerSummary          *string               `json:"trigger_summary,omitempty"`           // canonical short description snapshot — comment text / autopilot title — taken at task creation; survives source edits/deletes
	TriggerAuthorType       string                `json:"trigger_author_type,omitempty"`       // "agent" or "member" — author kind of the triggering comment
	TriggerAuthorName       string                `json:"trigger_author_name,omitempty"`       // display name of the triggering comment author
	ChatSessionID           string                `json:"chat_session_id,omitempty"`           // non-empty for chat tasks
	ChatMessage             string                `json:"chat_message,omitempty"`              // user message for chat tasks
	ChatMessageAttachments  []ChatAttachmentMeta  `json:"chat_message_attachments,omitempty"`  // attachments on the user message — agent calls `multica attachment download <id>` per entry
	AutopilotRunID          string                `json:"autopilot_run_id,omitempty"`          // non-empty for autopilot-spawned tasks
	AutopilotID             string                `json:"autopilot_id,omitempty"`              // autopilot that spawned this task
	AutopilotTitle          string                `json:"autopilot_title,omitempty"`           // autopilot title used as task context
	AutopilotDescription    string                `json:"autopilot_description,omitempty"`     // autopilot description used as task prompt
	AutopilotSource         string                `json:"autopilot_source,omitempty"`          // manual, schedule, webhook, or api
	AutopilotTriggerPayload json.RawMessage       `json:"autopilot_trigger_payload,omitempty"` // optional trigger payload for webhook/api runs
	QuickCreatePrompt       string                `json:"quick_create_prompt,omitempty"`       // user's natural-language input for quick-create tasks
	SquadID                 string                `json:"squad_id,omitempty"`                  // for quick-create tasks where the picker was a squad; Agent is still the resolved leader
	SquadName               string                `json:"squad_name,omitempty"`                // display name for the picker squad
	// RequestingUserName + RequestingUserProfileDescription mirror the user
	// the agent is acting on behalf of (see daemon/types.go). v1 sources them
	// from the runtime owner so they're populated for daemon runtimes and
	// empty otherwise. The daemon emits both into the brief under
	// `## Requesting User`; the heading is skipped entirely when description
	// is empty.
	RequestingUserName               string `json:"requesting_user_name,omitempty"`
	RequestingUserProfileDescription string `json:"requesting_user_profile_description,omitempty"`
	Kind                             string `json:"kind"` // discriminator: "comment" | "autopilot" | "chat" | "quick_create" | "direct" — used by the activity row to label tasks that have no linked issue
}

// ChatAttachmentMeta is the structured attachment metadata embedded in
// claim responses for chat tasks. The agent uses these to run
// `multica attachment download <id>` rather than guessing from the
// markdown URL (which is signed and 30-min expiring on private CDN).
// The mirror struct on the daemon side lives in internal/daemon/types.go
// and uses the same JSON field names.
type ChatAttachmentMeta struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
}

// TaskAgentData holds agent info included in claim responses so the daemon
// can set up the execution environment (branch naming, skill files, instructions).
type TaskAgentData struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Visibility    string                   `json:"visibility"`
	OwnerID       string                   `json:"owner_id"`
	Instructions  string                   `json:"instructions"`
	Skills        []service.AgentSkillData `json:"skills,omitempty"`
	CustomEnv     map[string]string        `json:"custom_env,omitempty"`
	CustomArgs    []string                 `json:"custom_args,omitempty"`
	McpConfig     json.RawMessage          `json:"mcp_config,omitempty"`
	Model         string                   `json:"model,omitempty"`
	RuntimeConfig json.RawMessage          `json:"runtime_config,omitempty"`
	ThinkingLevel string                   `json:"thinking_level,omitempty"`
}

// taskToSlimResponse builds a response without the heavy Context and Result
// blobs. Used by list endpoints (snapshot, task-runs) where these fields are
// never consumed by the frontend and dominate response size.
func taskToSlimResponse(t db.AgentTaskQueue) AgentTaskResponse {
	failureReason := ""
	if t.FailureReason.Valid {
		failureReason = t.FailureReason.String
	}
	workDir := ""
	if t.WorkDir.Valid {
		workDir = t.WorkDir.String
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
		Error:            textToPtr(t.Error),
		FailureReason:    failureReason,
		Attempt:          t.Attempt,
		MaxAttempts:      t.MaxAttempts,
		ParentTaskID:     uuidToPtr(t.ParentTaskID),
		CreatedAt:        timestampToString(t.CreatedAt),
		TriggerCommentID: uuidToPtr(t.TriggerCommentID),
		TriggerSummary:   textToPtr(t.TriggerSummary),
		WorkDir:          workDir,
		ChatSessionID:    uuidToString(t.ChatSessionID),
		AutopilotRunID:   uuidToString(t.AutopilotRunID),
		Kind:             computeTaskKind(t),
	}
}

func taskToResponse(t db.AgentTaskQueue) AgentTaskResponse {
	var result any
	if t.Result != nil {
		json.Unmarshal(t.Result, &result)
	}
	var context any
	if t.Context != nil {
		json.Unmarshal(t.Context, &context)
	}
	failureReason := ""
	if t.FailureReason.Valid {
		failureReason = t.FailureReason.String
	}
	workDir := ""
	if t.WorkDir.Valid {
		workDir = t.WorkDir.String
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
		Context:          context,
		FailureReason:    failureReason,
		Attempt:          t.Attempt,
		MaxAttempts:      t.MaxAttempts,
		ParentTaskID:     uuidToPtr(t.ParentTaskID),
		CreatedAt:        timestampToString(t.CreatedAt),
		TriggerCommentID: uuidToPtr(t.TriggerCommentID),
		TriggerSummary:   textToPtr(t.TriggerSummary),
		WorkDir:          workDir,
		// Surface task source so the UI can distinguish issue-linked tasks
		// from chat-spawned or autopilot-spawned ones; all three may arrive
		// with issue_id = "" once a task has no linked issue.
		ChatSessionID:  uuidToString(t.ChatSessionID),
		AutopilotRunID: uuidToString(t.AutopilotRunID),
		Kind:           computeTaskKind(t),
	}
}

// computeTaskKind picks the source-discriminator string the activity UI uses
// to choose how to render a task row. Computed from the existing FK shape so
// no extra DB lookup is needed: chat / autopilot / comment-on-issue (any
// triggered task with both an issue_id and trigger_comment_id) / quick_create
// (no linked source — the agent is creating the issue itself) / direct
// (assignee-driven task on an existing issue).
func computeTaskKind(t db.AgentTaskQueue) string {
	if uuidToString(t.ChatSessionID) != "" {
		return "chat"
	}
	if uuidToString(t.AutopilotRunID) != "" {
		return "autopilot"
	}
	if uuidToString(t.IssueID) == "" {
		return "quick_create"
	}
	if uuidToString(t.TriggerCommentID) != "" {
		return "comment"
	}
	return "direct"
}

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID := requestUserID(r)

	ownerFilter := r.URL.Query().Get("owner")
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	slim := r.URL.Query().Get("slim") == "true"

	var agents []db.Agent
	var err error
	switch {
	case ownerFilter == "me":
		byOwner := db.ListAgentsByOwnerParams{
			WorkspaceID: parseUUID(workspaceID),
			OwnerID:     parseUUID(userID),
		}
		if includeArchived {
			agents, err = h.Queries.ListAllAgentsByOwner(r.Context(), db.ListAllAgentsByOwnerParams{
				WorkspaceID: byOwner.WorkspaceID,
				OwnerID:     byOwner.OwnerID,
			})
		} else {
			agents, err = h.Queries.ListAgentsByOwner(r.Context(), byOwner)
		}
	case includeArchived:
		agents, err = h.Queries.ListAllAgents(r.Context(), parseUUID(workspaceID))
	default:
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
	skillMap := map[string][]AgentSkillSummary{}
	for _, row := range skillRows {
		agentID := uuidToString(row.AgentID)
		skillMap[agentID] = append(skillMap[agentID], AgentSkillSummary{
			ID:          uuidToString(row.ID),
			Name:        row.Name,
			Description: row.Description,
		})
	}

	allowedRows, err := h.Queries.ListAgentAllowedPrincipalIDsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent allowed users")
		return
	}
	allowedUserMap := map[string][]string{}
	for _, row := range allowedRows {
		agentID := uuidToString(row.AgentID)
		allowedUserMap[agentID] = append(allowedUserMap[agentID], uuidToString(row.PrincipalID))
	}

	// Check workspace-level always-redact setting.
	var alwaysRedact bool
	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("GetWorkspace failed for redact check", "workspace_id", workspaceID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	alwaysRedact = workspaceAlwaysRedactEnv(ws.Settings)

	// Resolve the request actor once. Agents bypass the private-agent gate
	// to preserve A2A collaboration; members must be allowed to see private
	// agents by ownership, workspace role, or the explicit allowlist.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	visible := make([]AgentResponse, 0, len(agents))
	for _, a := range agents {
		if a.Visibility == "private" && actorType == "member" {
			if !memberAllowedForPrivateAgentWithAllowlist(a, actorID, member.Role, allowedUserMap[uuidToString(a.ID)]) {
				continue
			}
		}
		resp := agentToResponse(a)
		if slim {
			resp.Instructions = ""
			resp.RuntimeConfig = nil
			resp.McpConfig = nil
		}
		if skills, ok := skillMap[resp.ID]; ok {
			resp.Skills = skills
		}
		if allowedUserIDs, ok := allowedUserMap[resp.ID]; ok {
			resp.AllowedUserIDs = allowedUserIDs
		}
		// Redact sensitive fields: unconditionally when workspace opts into always_redact_env,
		// or for users who are not the agent owner or workspace owner/admin.
		if alwaysRedact {
			redactEnv(&resp)
			redactMcpConfig(&resp)
			resp.CustomEnvRedactedReason = "policy"
		} else if !canViewAgentEnv(a, userID, member.Role) {
			redactEnv(&resp)
			redactMcpConfig(&resp)
			resp.CustomEnvRedactedReason = "role"
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
	// Private-agent gate: members must be in allowed_principals to view
	// (and therefore navigate to) a private agent. The 403 lets the front-end
	// render an explicit "no access" placeholder instead of a 404 — see
	// agent-detail-page.tsx.
	workspaceID := uuidToString(agent.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}
	resp := agentToResponse(agent)
	// Use the summary query (no `content` column) — the embedded
	// AgentSkillSummary only needs id/name/description, and reading large
	// SKILL.md bodies just to discard them is the exact regression we fixed
	// in #2174.
	skills, err := h.Queries.ListAgentSkillSummaries(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	if len(skills) > 0 {
		resp.Skills = make([]AgentSkillSummary, len(skills))
		for i, s := range skills {
			resp.Skills[i] = AgentSkillSummary{
				ID:          uuidToString(s.ID),
				Name:        s.Name,
				Description: s.Description,
			}
		}
	}
	allowedRows, err := h.Queries.ListAgentAllowedPrincipals(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent allowed users")
		return
	}
	resp.AllowedUserIDs = make([]string, 0, len(allowedRows))
	for _, row := range allowedRows {
		resp.AllowedUserIDs = append(resp.AllowedUserIDs, uuidToString(row.PrincipalID))
	}

	// Redact sensitive fields for users who are not the agent owner or workspace owner/admin,
	// or unconditionally when the workspace opts into always_redact_env.
	userID := requestUserID(r)
	var alwaysRedact bool
	ws, err := h.Queries.GetWorkspace(r.Context(), agent.WorkspaceID)
	if err != nil {
		slog.Warn("GetWorkspace failed for redact check", "workspace_id", uuidToString(agent.WorkspaceID), "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	alwaysRedact = workspaceAlwaysRedactEnv(ws.Settings)
	if alwaysRedact {
		redactEnv(&resp)
		redactMcpConfig(&resp)
		resp.CustomEnvRedactedReason = "policy"
	} else if member, ok := ctxMember(r.Context()); ok {
		if !canViewAgentEnv(agent, userID, member.Role) {
			redactEnv(&resp)
			redactMcpConfig(&resp)
			resp.CustomEnvRedactedReason = "role"
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
	Model              string            `json:"model"`
	ThinkingLevel      string            `json:"thinking_level"`
	// Template records which template slug was used to seed this agent
	// (e.g. "coding" / "planning" / "writing" / "assistant"). Empty when
	// the caller didn't come from a template picker — the `agent_created`
	// event still fires with `template=""`, which is the correct signal
	// for "manually authored agent".
	Template string `json:"template"`
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
	if utf8.RuneCountInString(req.Description) > maxAgentDescriptionLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("description must be %d characters or fewer", maxAgentDescriptionLength))
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

	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: wsUUID,
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
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can create agents on it")
		return
	}

	// thinking_level validation: provider-level enum only. Per-model gaps
	// are enforced by the daemon at execution time (MUL-2339, Trump's
	// review note — keep API behaviour consistent: literal-invalid →
	// always 400; combination-invalid → daemon-side task error).
	if !agent.IsKnownThinkingValue(runtime.Provider, req.ThinkingLevel) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("thinking_level %q is not a recognised value for runtime %q", req.ThinkingLevel, runtime.Provider))
		return
	}

	// Probe workspace agent count BEFORE the insert so the funnel has a
	// clean "first agent ever in this workspace" signal.
	isFirstAgent := false
	if existing, listErr := h.Queries.ListAgents(r.Context(), wsUUID); listErr == nil {
		isFirstAgent = len(existing) == 0
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

	created, err := h.Queries.CreateAgent(r.Context(), db.CreateAgentParams{
		WorkspaceID:            wsUUID,
		Name:                   req.Name,
		Description:            req.Description,
		Instructions:           req.Instructions,
		AvatarUrl:              ptrToText(req.AvatarURL),
		RuntimeMode:            runtime.RuntimeMode,
		RuntimeConfig:          rc,
		RuntimeID:              runtime.ID,
		Visibility:             req.Visibility,
		MaxConcurrentTasks:     req.MaxConcurrentTasks,
		OwnerID:                parseUUID(ownerID),
		CustomEnv:              ce,
		CustomArgs:             ca,
		McpConfig:              mc,
		Model:                  pgtype.Text{String: req.Model, Valid: req.Model != ""},
		ThinkingLevel:          pgtype.Text{String: req.ThinkingLevel, Valid: req.ThinkingLevel != ""},
		CustomEnvCopiedPending: false,
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
	slog.Info("agent created", append(logger.RequestAttrs(r), "agent_id", uuidToString(created.ID), "name", created.Name, "workspace_id", workspaceID)...)

	if runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(r.Context(), created.ID)
		created, _ = h.Queries.GetAgent(r.Context(), created.ID)
	}

	resp := agentToResponse(created)
	actorType, actorID := h.resolveActor(r, ownerID, workspaceID)
	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": resp})

	h.Analytics.Capture(analytics.AgentCreated(
		ownerID,
		workspaceID,
		uuidToString(created.ID),
		runtime.Provider,
		runtime.RuntimeMode,
		req.Template,
		isFirstAgent,
	))

	writeJSON(w, http.StatusCreated, resp)
}

type CopyAgentRequest struct {
	Name string `json:"name"`
}

// Matches a trailing " (N)" suffix where N is a non-negative integer (e.g. "Agent (12)").
var agentNumberedNameSuffix = regexp.MustCompile(`^(.+?) \((\d+)\)$`)

func agentDuplicateBaseName(name string) string {
	if m := agentNumberedNameSuffix.FindStringSubmatch(name); m != nil {
		return m[1]
	}
	return name
}

// nextDuplicateAgentName picks the next "base (N)" name not colliding with existing agent
// names in the workspace. The base is derived from sourceName by stripping one trailing
// " (N)" suffix if present (so duplicating "Agent (1)" uses base "Agent").
func nextDuplicateAgentName(existingNames []string, sourceName string) string {
	base := agentDuplicateBaseName(sourceName)
	maxN := 0
	prefix := base + " ("
	for _, n := range existingNames {
		if n == base {
			continue
		}
		if strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ")") {
			inner := n[len(prefix) : len(n)-1]
			if num, err := strconv.Atoi(inner); err == nil && num > maxN {
				maxN = num
			}
		}
	}
	return fmt.Sprintf("%s (%d)", base, maxN+1)
}

func (h *Handler) CopyAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sourceAgent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	wsID := uuidToString(sourceAgent.WorkspaceID)
	// OPE-817: any workspace member can duplicate any visible agent for
	// learning/reference purposes. The copy becomes owned by the current user.
	_, ok = h.requireWorkspaceRole(w, r, wsID, "agent not found", "owner", "admin", "member")
	if !ok {
		return
	}
	userID := requestUserID(r)

	var req CopyAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	newName := strings.TrimSpace(req.Name)
	if newName == "" {
		allAgents, err := h.Queries.ListAllAgents(r.Context(), sourceAgent.WorkspaceID)
		if err != nil {
			slog.Warn("failed to list agents for duplicate name", "workspace_id", wsID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to duplicate agent: "+err.Error())
			return
		}
		names := make([]string, len(allAgents))
		for i, a := range allAgents {
			names[i] = a.Name
		}
		newName = nextDuplicateAgentName(names, sourceAgent.Name)
	}

	skills, err := h.Queries.ListAgentSkills(r.Context(), sourceAgent.ID)
	if err != nil {
		slog.Warn("failed to load agent skills for copy", "agent_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}

	rc := sourceAgent.RuntimeConfig
	if len(rc) == 0 {
		rc = []byte("{}")
	} else {
		rc = append([]byte(nil), rc...)
	}
	ce, envCopiedPending := stripCustomEnvValuesForCopy(sourceAgent.CustomEnv)
	ca := sourceAgent.CustomArgs
	if len(ca) == 0 {
		ca = []byte("[]")
	} else {
		ca = append([]byte(nil), ca...)
	}
	var mc []byte
	if len(sourceAgent.McpConfig) > 0 {
		mc = append([]byte(nil), sourceAgent.McpConfig...)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	newAgent, err := qtx.CreateAgent(r.Context(), db.CreateAgentParams{
		WorkspaceID:            sourceAgent.WorkspaceID,
		Name:                   newName,
		Description:            sourceAgent.Description,
		Instructions:           sourceAgent.Instructions,
		AvatarUrl:              sourceAgent.AvatarUrl,
		RuntimeMode:            sourceAgent.RuntimeMode,
		RuntimeConfig:          rc,
		RuntimeID:              sourceAgent.RuntimeID,
		Visibility:             sourceAgent.Visibility,
		MaxConcurrentTasks:     sourceAgent.MaxConcurrentTasks,
		OwnerID:                parseUUID(userID),
		CustomEnv:              ce,
		CustomArgs:             ca,
		McpConfig:              mc,
		Model:                  sourceAgent.Model,
		CustomEnvCopiedPending: envCopiedPending,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "agent_workspace_name_unique" {
			writeError(w, http.StatusConflict, fmt.Sprintf("an agent named %q already exists in this workspace", newName))
			return
		}
		slog.Warn("duplicate agent failed", append(logger.RequestAttrs(r), "source_agent_id", id, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to duplicate agent: "+err.Error())
		return
	}

	for _, s := range skills {
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: newAgent.ID,
			SkillID: s.ID,
		}); err != nil {
			slog.Warn("failed to copy agent skill", append(logger.RequestAttrs(r), "source_agent_id", id, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to copy agent skills")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	slog.Info("agent duplicated", append(logger.RequestAttrs(r), "source_agent_id", id, "new_agent_id", uuidToString(newAgent.ID))...)

	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          newAgent.RuntimeID,
		WorkspaceID: newAgent.WorkspaceID,
	})
	if err == nil && runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(r.Context(), newAgent.ID)
		newAgent, _ = h.Queries.GetAgent(r.Context(), newAgent.ID)
	}

	resp := agentToResponse(newAgent)
	skillRows, err := h.Queries.ListAgentSkillSummaries(r.Context(), newAgent.ID)
	if err == nil && len(skillRows) > 0 {
		resp.Skills = make([]AgentSkillSummary, len(skillRows))
		for i, s := range skillRows {
			resp.Skills[i] = AgentSkillSummary{
				ID:          uuidToString(s.ID),
				Name:        s.Name,
				Description: s.Description,
			}
		}
	}

	actorType, actorID := h.resolveActor(r, userID, wsID)
	h.publish(protocol.EventAgentCreated, wsID, actorType, actorID, map[string]any{"agent": resp})
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
	Model              *string            `json:"model"`
	// ThinkingLevel is treated as a tri-state per-MUL-2339:
	//   - field omitted → no change (leave existing value alone)
	//   - field present with "" → explicit clear (use runtime default)
	//   - field present with non-empty value → set (validated server-side)
	// Distinguishing those modes is why this is a pointer; the raw-fields
	// map captured at decode time tells us whether the key was sent.
	ThinkingLevel *string `json:"thinking_level"`
}

// workspaceAlwaysRedactEnv checks whether the workspace has opted into
// unconditional redaction of custom_env and mcp_config on read responses,
// regardless of the caller's role. This is useful for single-tenant
// self-hosts or security-conscious teams that never want plaintext secrets
// returned from the API.
func workspaceAlwaysRedactEnv(settings []byte) bool {
	if len(settings) == 0 {
		return false
	}
	var s struct {
		AlwaysRedactEnv bool `json:"always_redact_env"`
	}
	if err := json.Unmarshal(settings, &s); err != nil {
		return false
	}
	return s.AlwaysRedactEnv
}

// canViewAgentEnv checks whether the requesting user is allowed to see the
// agent's custom environment variables. OPE-817: only the agent owner may
// view them; admin/owner roles do NOT bypass this gate. Legacy agents
// (owner_id null) fall back to admin for backward compat.
func canViewAgentEnv(agent db.Agent, userID string, memberRole string) bool {
	if uuidToString(agent.OwnerID) == userID {
		return true
	}
	// Legacy agents (owner_id null): allow admin for backward compat.
	if !agent.OwnerID.Valid && roleAllowed(memberRole, "owner", "admin") {
		return true
	}
	return false
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
// OPE-817: only the agent owner can manage their own agent. Admin/owner roles
// do NOT bypass this gate. Legacy agents (owner_id null) fall back to admin
// management for backward compatibility.
func (h *Handler) canManageAgent(w http.ResponseWriter, r *http.Request, agent db.Agent) bool {
	wsID := uuidToString(agent.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, wsID, "agent not found", "owner", "admin", "member")
	if !ok {
		return false
	}
	isAgentOwner := uuidToString(agent.OwnerID) == requestUserID(r)
	if isAgentOwner {
		return true
	}
	// Legacy agents (owner_id null): allow admin for backward compat.
	if !agent.OwnerID.Valid && roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	writeError(w, http.StatusForbidden, "only the agent owner can manage this agent")
	return false
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, existing) {
		return
	}

	var req UpdateAgentRequest
	rawFields, err := decodeJSONBodyWithRawFields(r.Body, &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateAgentParams{
		ID: existing.ID,
	}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		if utf8.RuneCountInString(*req.Description) > maxAgentDescriptionLength {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("description must be %d characters or fewer", maxAgentDescriptionLength))
			return
		}
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
		if existing.CustomEnvCopiedPending {
			params.CustomEnvCopiedPending = pgtype.Bool{Bool: false, Valid: true}
		}
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

	// Resolve the runtime that will be in force after this update so the
	// thinking_level validation hits the right provider enum. When the
	// request doesn't move the agent, we still need to load the *current*
	// runtime to validate a thinking_level change. Resolve once and reuse.
	targetRuntimeID := existing.RuntimeID
	if req.RuntimeID != nil {
		runtimeUUID, ok := parseUUIDOrBadRequest(w, *req.RuntimeID, "runtime_id")
		if !ok {
			return
		}
		// Only the agent owner may change its runtime binding.
		// Workspace admins can manage other agent fields but not rebind the runtime.
		userID := requestUserID(r)
		agentOwner := uuidToString(existing.OwnerID)
		if agentOwner != "" && agentOwner != userID {
			writeError(w, http.StatusForbidden, "only the agent owner can change the runtime")
			return
		}

		runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
			ID:          runtimeUUID,
			WorkspaceID: existing.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid runtime_id")
			return
		}
		// Same gate as CreateAgent — prevents UpdateAgent from being used to
		// re-bind an agent onto someone else's private runtime, which would
		// otherwise be a quiet end-run around the CreateAgent check.
		member, ok := h.workspaceMember(w, r, uuidToString(existing.WorkspaceID))
		if !ok {
			return
		}
		if !canUseRuntimeForAgent(member, runtime) {
			writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can move agents onto it")
			return
		}
		params.RuntimeID = runtime.ID
		params.RuntimeMode = pgtype.Text{String: runtime.RuntimeMode, Valid: true}
		targetRuntimeID = runtime.ID
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
	if req.Model != nil {
		params.Model = pgtype.Text{String: *req.Model, Valid: true}
	}

	// thinking_level handling (MUL-2339). Tri-state semantics:
	//   - field omitted  → leave column alone (COALESCE narg), but if a
	//     runtime change in this same request would make the *existing*
	//     value literal-invalid for the new provider, reject 400. This
	//     closes the gap Elon's review flagged: previously, switching a
	//     Claude agent storing `max` to a Codex runtime would silently
	//     keep `max` and forward it to the daemon.
	//   - field set to "" → explicit clear (run ClearAgentThinkingLevel post-update)
	//   - field set to value → validate against the target runtime's provider
	//     enum; reject literal-invalid with 400. Per-model combination checks
	//     run in the daemon at execution time, not here — see Trump's review
	//     constraint that API behaviour stays consistent across change paths.
	shouldClearThinkingLevel := false
	if req.ThinkingLevel != nil {
		value := *req.ThinkingLevel
		if value == "" {
			shouldClearThinkingLevel = true
		} else {
			// Need the target runtime's provider to validate. Re-fetch only when
			// we haven't already loaded it above (i.e. the request didn't change
			// runtime_id), to keep the no-change path one DB roundtrip.
			provider, ok := h.resolveAgentProvider(r, existing.WorkspaceID, targetRuntimeID)
			if !ok {
				writeError(w, http.StatusInternalServerError, "failed to resolve runtime for thinking_level validation")
				return
			}
			if !agent.IsKnownThinkingValue(provider, value) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("thinking_level %q is not a recognised value for runtime %q", value, provider))
				return
			}
			params.ThinkingLevel = pgtype.Text{String: value, Valid: true}
		}
	} else if req.RuntimeID != nil && existing.ThinkingLevel.Valid && existing.ThinkingLevel.String != "" {
		// Runtime is changing but the caller didn't touch thinking_level.
		// If the existing value is not in the new provider's enum at all,
		// preserving it would smuggle a literal-invalid token to the daemon.
		// Hold the same line as the explicit-set path: always 400 on
		// literal-invalid, never silently coerce. The caller can either
		// pass `thinking_level: ""` to clear or pick a value valid for the
		// new runtime.
		provider, ok := h.resolveAgentProvider(r, existing.WorkspaceID, targetRuntimeID)
		if !ok {
			writeError(w, http.StatusInternalServerError, "failed to resolve runtime for thinking_level validation")
			return
		}
		if !agent.IsKnownThinkingValue(provider, existing.ThinkingLevel.String) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"existing thinking_level %q is not valid for runtime %q; pass thinking_level=\"\" to clear or set a value valid for the new runtime",
				existing.ThinkingLevel.String, provider,
			))
			return
		}
	}

	updated, err := h.Queries.UpdateAgent(r.Context(), params)
	if err != nil {
		slog.Warn("update agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update agent: "+err.Error())
		return
	}

	// mcp_config / thinking_level: null/empty in the request means explicitly
	// clear the field. COALESCE in UpdateAgent cannot set a column to NULL,
	// so we use dedicated clear queries.
	if shouldClearMcpConfig {
		updated, err = h.Queries.ClearAgentMcpConfig(r.Context(), updated.ID)
		if err != nil {
			slog.Warn("clear agent mcp_config failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear mcp_config: "+err.Error())
			return
		}
	}
	if shouldClearThinkingLevel {
		updated, err = h.Queries.ClearAgentThinkingLevel(r.Context(), updated.ID)
		if err != nil {
			slog.Warn("clear agent thinking_level failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear thinking_level: "+err.Error())
			return
		}
	}

	resp := agentToResponse(updated)
	slog.Info("agent updated", append(logger.RequestAttrs(r), "agent_id", id, "workspace_id", uuidToString(updated.WorkspaceID))...)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(updated.WorkspaceID))
	h.publish(protocol.EventAgentStatus, uuidToString(updated.WorkspaceID), actorType, actorID, map[string]any{"agent": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListAgentAllowedPrincipals(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgentAllowedPrincipals(w, r, agent) {
		return
	}

	rows, err := h.Queries.ListAgentAllowedPrincipals(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent allowed users")
		return
	}
	resp := make([]AgentAllowedPrincipalResponse, len(rows))
	for i, row := range rows {
		resp[i] = agentAllowedPrincipalToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

type UpdateAgentAllowedPrincipalsRequest struct {
	UserIDs []string `json:"user_ids"`
}

func (h *Handler) UpdateAgentAllowedPrincipals(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgentAllowedPrincipals(w, r, agent) {
		return
	}

	var req UpdateAgentAllowedPrincipalsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	workspaceID := uuidToString(agent.WorkspaceID)
	seen := map[string]struct{}{}
	userIDs := make([]pgtype.UUID, 0, len(req.UserIDs))
	for _, rawID := range req.UserIDs {
		userID, ok := parseUUIDOrBadRequest(w, rawID, "user id")
		if !ok {
			return
		}
		userIDStr := uuidToString(userID)
		if _, exists := seen[userIDStr]; exists {
			continue
		}
		seen[userIDStr] = struct{}{}
		if _, err := h.getWorkspaceMember(r.Context(), userIDStr, workspaceID); err != nil {
			writeError(w, http.StatusBadRequest, "allowed user must be a workspace member")
			return
		}
		userIDs = append(userIDs, userID)
	}

	if err := h.Queries.ReplaceAgentAllowedPrincipals(r.Context(), db.ReplaceAgentAllowedPrincipalsParams{
		WorkspaceID:  agent.WorkspaceID,
		AgentID:      agent.ID,
		PrincipalIds: userIDs,
		CreatedBy:    parseUUID(requestUserID(r)),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agent allowed users")
		return
	}

	rows, err := h.Queries.ListAgentAllowedPrincipals(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent allowed users")
		return
	}
	resp := make([]AgentAllowedPrincipalResponse, len(rows))
	for i, row := range rows {
		resp[i] = agentAllowedPrincipalToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) canManageAgentAllowedPrincipals(w http.ResponseWriter, r *http.Request, agent db.Agent) bool {
	userID := requestUserID(r)
	if uuidToString(agent.OwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the agent owner can manage allowed users")
		return false
	}
	return true
}
// resolveAgentProvider returns the provider name for the runtime that
// will own this agent after the in-flight update applies. Used by the
// thinking_level validator so a runtime/model swap and a level swap
// validated in the same request both consult the same provider.
func (h *Handler) resolveAgentProvider(r *http.Request, workspaceID pgtype.UUID, runtimeID pgtype.UUID) (string, bool) {
	rt, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return "", false
	}
	return rt.Provider, true
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
		ID:         agent.ID,
		ArchivedBy: parseUUID(userID),
	})
	if err != nil {
		slog.Warn("archive agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to archive agent")
		return
	}

	// Cancel all pending/active tasks for this agent. Discard the returned
	// rows here — the agent:archived event below already triggers a full
	// active-tasks invalidation on every connected client, so per-task
	// task:cancelled events would be redundant noise.
	if cancelled, err := h.Queries.CancelAgentTasksByAgent(r.Context(), agent.ID); err != nil {
		slog.Warn("cancel agent tasks on archive failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
	} else {
		h.TaskService.CaptureCancelledTasks(r.Context(), cancelled)
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

	restored, err := h.Queries.RestoreAgent(r.Context(), agent.ID)
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

// CancelAgentTasks bulk-cancels every active task (queued/dispatched/running)
// belonging to an agent. Powers the agents-list "Cancel all tasks" row
// action. Same permission gate as archive (canManageAgent — owner or
// workspace admin/owner). Each cancelled row triggers a task:cancelled WS
// event so connected clients clear their live cards immediately.
//
// Note: a `running` task on the daemon side won't actually halt for up to
// ~5 seconds (daemon polls GetTaskStatus on that interval). The DB row is
// marked cancelled instantly, but the child process keeps going briefly;
// see daemon/daemon.go:919-942 for the polling loop. Surface this in the
// confirm-dialog copy so users aren't surprised by trailing transcript
// lines.
type cancelAgentTasksResponse struct {
	Cancelled int `json:"cancelled"`
}

func (h *Handler) CancelAgentTasks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	cancelled, err := h.TaskService.CancelTasksForAgent(r.Context(), parseUUID(id))
	if err != nil {
		slog.Warn("cancel agent tasks failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to cancel tasks")
		return
	}

	slog.Info("agent tasks cancelled",
		append(logger.RequestAttrs(r), "agent_id", id, "count", len(cancelled))...)
	writeJSON(w, http.StatusOK, cancelAgentTasksResponse{Cancelled: len(cancelled)})
}

func (h *Handler) ListAgentTasks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	// Run history is part of the private-agent gate ("查看历史会话"). Same
	// 403 semantics as GetAgent.
	workspaceID := uuidToString(agent.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}

	tasks, err := h.Queries.ListAgentTasks(r.Context(), agent.ID)
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

// AgentActivityBucket is one day-bucketed throughput sample for the
// Agents-list ACTIVITY sparkline. bucket_at is midnight UTC of the day.
type AgentActivityBucket struct {
	AgentID     string `json:"agent_id"`
	BucketAt    string `json:"bucket_at"`
	TaskCount   int32  `json:"task_count"`
	FailedCount int32  `json:"failed_count"`
}

// AgentRunCount is the trailing-30-day total task run count per agent,
// powering the Agents-list RUNS column.
type AgentRunCount struct {
	AgentID  string `json:"agent_id"`
	RunCount int32  `json:"run_count"`
}

// GetWorkspaceAgentRunCounts returns 30-day total run counts for every
// agent in the workspace. Same single-fetch pattern as live-tasks /
// activity to keep the Agents list cheap regardless of agent count.
func (h *Handler) GetWorkspaceAgentRunCounts(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	rows, err := h.Queries.GetWorkspaceAgentRunCounts(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent run counts")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	resp := make([]AgentRunCount, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if _, ok := allowed[agentID]; !ok {
			continue
		}
		resp = append(resp, AgentRunCount{
			AgentID:  agentID,
			RunCount: row.RunCount,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// GetWorkspaceAgentActivity30d returns per-agent daily task counts for the
// last 30 days, anchored on completed_at. Single workspace-wide read backs
// both the Agents list sparkline (uses the trailing 7 buckets) and the
// agent detail "Last 30 days" panel (uses all 30) — one fetch is cheaper
// than two. Front-end fills missing days with zero; the back-end omits
// empty buckets to keep the response small.
func (h *Handler) GetWorkspaceAgentActivity30d(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	rows, err := h.Queries.GetWorkspaceAgentActivity30d(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent activity")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	resp := make([]AgentActivityBucket, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if _, ok := allowed[agentID]; !ok {
			continue
		}
		resp = append(resp, AgentActivityBucket{
			AgentID:     agentID,
			BucketAt:    timestampToString(row.Bucket),
			TaskCount:   row.TaskCount,
			FailedCount: row.FailedCount,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListWorkspaceAgentTaskSnapshot returns the task data the front-end needs to
// derive each agent's presence: every active task (queued/dispatched/running)
// plus each agent's most recent OUTCOME task (completed/failed only). Cancelled
// tasks are excluded from the outcome half by design — cancel is a procedural
// signal ("attempt aborted"), not an outcome, so it must not mask a prior
// failure. The front-end picks "active wins, else latest outcome"; a failed
// outcome stays sticky until the user starts a new task or one succeeds.
// Per-agent filtering happens in the front-end against this workspace-wide
// snapshot.
func (h *Handler) ListWorkspaceAgentTaskSnapshot(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	tasks, err := h.Queries.ListWorkspaceAgentTaskSnapshot(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent task snapshot")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	resp := make([]AgentTaskResponse, 0, len(tasks))
	for _, t := range tasks {
		if _, ok := allowed[uuidToString(t.AgentID)]; !ok {
			continue
		}
		resp = append(resp, taskToSlimResponse(t))
	}

	writeJSON(w, http.StatusOK, resp)
}
