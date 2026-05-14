package handler

// CEREBRO-PATCH(agent-handler): cerebro modification of upstream file

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Mirrors AGENT_DESCRIPTION_MAX_LENGTH in packages/core/agents/constants.ts
// and the agent_description_length CHECK constraint in migration 060. Counted
// in unicode code points (utf8.RuneCountInString), matching Postgres
// char_length and the front-end's String.prototype.length-with-counter UX.
const maxAgentDescriptionLength = 255

type AgentResponse struct {
	ID                 string              `json:"id"`
	WorkspaceID        string              `json:"workspace_id"`
	RuntimeID          string              `json:"runtime_id"`
	Name               string              `json:"name"`
	Description        string              `json:"description"`
	Instructions       string              `json:"instructions"`
	AvatarURL          *string             `json:"avatar_url"`
	RuntimeMode        string              `json:"runtime_mode"`
	RuntimeConfig      any                 `json:"runtime_config"`
	CustomEnv          map[string]string   `json:"custom_env"`
	CustomArgs         []string            `json:"custom_args"`
	McpConfig          json.RawMessage     `json:"mcp_config"`
	CustomEnvRedacted  bool                `json:"custom_env_redacted"`
	McpConfigRedacted  bool                `json:"mcp_config_redacted"`
	Visibility         string              `json:"visibility"`
	Status             string              `json:"status"`
	MaxConcurrentTasks int32               `json:"max_concurrent_tasks"`
	Model              string              `json:"model"`
	OwnerID            *string             `json:"owner_id"`
	Skills             []AgentSkillSummary `json:"skills"`
	CreatedAt          string              `json:"created_at"`
	UpdatedAt          string              `json:"updated_at"`
	ArchivedAt         *string             `json:"archived_at"`
	ArchivedBy         *string             `json:"archived_by"`
	// PersonaSandbox is the name of the persona sandbox this agent runs in,
	// or empty when no persona gating is configured. The daemon uses it at
	// spawn time to fetch the matching profile and wire the PreToolUse hook.
	PersonaSandbox string `json:"persona_sandbox"`
	// CEREBRO-PATCH(agent-can-trigger): JEH-1066 — visibility/trigger split.
	// True iff the caller is allowed to trigger this agent under the cerebro
	// group permission model. Non-cerebro deployments default to true (the
	// upstream/group seam never denies visibility). The field is set by the
	// handlers via setCanTrigger* after agentToResponse runs; agentToResponse
	// itself leaves it false so callers must make an explicit decision.
	CanTrigger bool `json:"can_trigger"`
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
		Model:              a.Model.String,
		OwnerID:            uuidToPtr(a.OwnerID),
		Skills:             []AgentSkillSummary{},
		CreatedAt:          timestampToString(a.CreatedAt),
		UpdatedAt:          timestampToString(a.UpdatedAt),
		ArchivedAt:         timestampToPtr(a.ArchivedAt),
		ArchivedBy:         uuidToPtr(a.ArchivedBy),
		PersonaSandbox:     a.PersonaSandbox.String,
	}
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

// CEREBRO-PATCH(handler-agent-firtal-gateway-chat-history): expose bounded transcript to stateless managed HTTP runtimes.
type ChatHistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AgentTaskResponse struct {
	ID                    string                `json:"id"`
	AgentID               string                `json:"agent_id"`
	RuntimeID             string                `json:"runtime_id"`
	IssueID               string                `json:"issue_id"`
	WorkspaceID           string                `json:"workspace_id"`
	Status                string                `json:"status"`
	Priority              int32                 `json:"priority"`
	DispatchedAt          *string               `json:"dispatched_at"`
	StartedAt             *string               `json:"started_at"`
	CompletedAt           *string               `json:"completed_at"`
	Result                any                   `json:"result"`
	Error                 *string               `json:"error"`
	FailureReason         string                `json:"failure_reason,omitempty"` // see TaskService.MaybeRetryFailedTask
	Attempt               int32                 `json:"attempt"`
	MaxAttempts           int32                 `json:"max_attempts"`
	ParentTaskID          *string               `json:"parent_task_id,omitempty"`
	Agent                 *TaskAgentData        `json:"agent,omitempty"`
	Repos                 []RepoData            `json:"repos,omitempty"`
	ProjectID             string                `json:"project_id,omitempty"`        // issue's project, when present
	ProjectTitle          string                `json:"project_title,omitempty"`     // for surfacing in agent context
	ProjectResources      []ProjectResourceData `json:"project_resources,omitempty"` // resources attached to the project
	CreatedAt             string                `json:"created_at"`
	PriorSessionID        string                `json:"prior_session_id,omitempty"`        // session ID from a previous task on same issue
	PriorWorkDir          string                `json:"prior_work_dir,omitempty"`          // work_dir from a previous task on same issue
	WorkDir               string                `json:"work_dir,omitempty"`                // local working directory pinned for this task; populated once the daemon reports it
	TriggerCommentID      *string               `json:"trigger_comment_id,omitempty"`      // comment that triggered this task
	TriggerCommentContent string                `json:"trigger_comment_content,omitempty"` // content of the triggering comment
	TriggerSummary        *string               `json:"trigger_summary,omitempty"`         // canonical short description snapshot — comment text / autopilot title — taken at task creation; survives source edits/deletes
	Title                 *string               `json:"title,omitempty"`                   // CEREBRO-PATCH(task-title-builder): short generated headline.
	TriggerAuthorType     string                `json:"trigger_author_type,omitempty"`     // "agent" or "member" — author kind of the triggering comment
	TriggerAuthorName     string                `json:"trigger_author_name,omitempty"`     // display name of the triggering comment author
	ChatSessionID         string                `json:"chat_session_id,omitempty"`         // non-empty for chat tasks
	// CEREBRO-PATCH(chat-message-id-claim): JEH-1083 — pre-created assistant chat_message row exposed to the agent as MULTICA_CHAT_MESSAGE_ID so it can attach files mid-turn.
	ChatMessageID string `json:"chat_message_id,omitempty"` // pre-created assistant chat_message for this chat task
	ChatMessage   string `json:"chat_message,omitempty"`    // user message for chat tasks (also: legacy single-message field for daemons pre-JEH-330)
	// CEREBRO-PATCH(agent-task-cerebro-fields): cerebro-only daemon fields.
	ChatHistory       []ChatHistoryMessage `json:"chat_history,omitempty"`        // capped chat transcript for stateless managed HTTP runtimes
	ChatMessages      []string             `json:"chat_messages,omitempty"`       // user messages newer than the last assistant reply (oldest first)
	UserProfilePrompt string               `json:"user_profile_prompt,omitempty"` // compiled per-user communication prompt (JEH-304)
	// TaskToken is a short-lived per-task token (mtt_ prefix) the daemon
	// injects as MULTICA_TOKEN for the spawned agent process. Scoped to
	// the task's issue/agent/workspace so an exfiltrated token cannot be
	// used for general API access.
	TaskToken string `json:"task_token,omitempty"`
	// SandboxEnabled is the per-runtime sandbox override (JEH-418), copied
	// from agent_runtime.sandbox_enabled at claim time so the daemon picks
	// up changes without restarting. nil = inherit daemon's env-var default.
	SandboxEnabled          *bool           `json:"sandbox_enabled,omitempty"`
	AutopilotRunID          string          `json:"autopilot_run_id,omitempty"`          // non-empty for autopilot-spawned tasks
	AutopilotID             string          `json:"autopilot_id,omitempty"`              // autopilot that spawned this task
	AutopilotTitle          string          `json:"autopilot_title,omitempty"`           // autopilot title used as task context
	AutopilotDescription    string          `json:"autopilot_description,omitempty"`     // autopilot description used as task prompt
	AutopilotSource         string          `json:"autopilot_source,omitempty"`          // manual, schedule, webhook, or api
	AutopilotTriggerPayload json.RawMessage `json:"autopilot_trigger_payload,omitempty"` // optional trigger payload for webhook/api runs
	QuickCreatePrompt       string          `json:"quick_create_prompt,omitempty"`       // user's natural-language input for quick-create tasks
	Kind                    string          `json:"kind"`                                // discriminator: "comment" | "autopilot" | "chat" | "quick_create" | "direct" — used by the activity row to label tasks that have no linked issue
	// CEREBRO-PATCH(persona-spawn-subject): JEH-1080 — the human user the agent is acting on behalf of, plus that user's group memberships at claim time.
	PersonaSpawnUserID   string   `json:"persona_spawn_user_id,omitempty"`
	PersonaSpawnGroupIDs []string `json:"persona_spawn_group_ids,omitempty"`
	// RuntimePersonaSandbox is the runtime-level persona sandbox upper
	// CEREBRO-PATCH(agent): persona integration additions.
	// bound (E1). Empty = no upper bound, the agent's persona_sandbox
	// decides alone. Non-empty (e.g. "claude-readonly") = the daemon must
	// use this sandbox at spawn time and ignore the agent-level value, so
	// an admin's runtime-wide cap can't be bypassed by an agent owner who
	// picked a more permissive sandbox on their agent.
	RuntimePersonaSandbox  string               `json:"runtime_persona_sandbox,omitempty"`
	ChatMessageAttachments []ChatAttachmentMeta `json:"chat_message_attachments,omitempty"` // attachments on the user message - agent calls `multica attachment download <id>` per entry
	SquadID                string               `json:"squad_id,omitempty"`                 // for quick-create tasks where the picker was a squad; Agent is still the resolved leader
	SquadName              string               `json:"squad_name,omitempty"`               // display name for the picker squad
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
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Instructions string                   `json:"instructions"`
	Skills       []service.AgentSkillData `json:"skills,omitempty"`
	CustomEnv    map[string]string        `json:"custom_env,omitempty"`
	CustomArgs   []string                 `json:"custom_args,omitempty"`
	McpConfig    json.RawMessage          `json:"mcp_config,omitempty"`
	Model        string                   `json:"model,omitempty"`
	// PersonaSandbox is the agent's persona sandbox name (e.g. "claude-developer")
	// passed to the daemon at task spawn time. Empty = no persona gating.
	PersonaSandbox string `json:"persona_sandbox,omitempty"`
}

func taskToResponse(t db.AgentTaskQueue) AgentTaskResponse {
	var result any
	if t.Result != nil {
		json.Unmarshal(t.Result, &result)
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
		FailureReason:    failureReason,
		Attempt:          t.Attempt,
		MaxAttempts:      t.MaxAttempts,
		ParentTaskID:     uuidToPtr(t.ParentTaskID),
		CreatedAt:        timestampToString(t.CreatedAt),
		TriggerCommentID: uuidToPtr(t.TriggerCommentID),
		TriggerSummary:   textToPtr(t.TriggerSummary),
		// CEREBRO-PATCH(task-title-builder): surface generated title to clients.
		Title:   textToPtr(t.Title),
		WorkDir: workDir,
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
	skillMap := map[string][]AgentSkillSummary{}
	for _, row := range skillRows {
		agentID := uuidToString(row.AgentID)
		skillMap[agentID] = append(skillMap[agentID], AgentSkillSummary{
			ID:          uuidToString(row.ID),
			Name:        row.Name,
			Description: row.Description,
		})
	}

	// CEREBRO-PATCH(list-agents-visibility-split): JEH-1066 — workspace
	// members always SEE every agent in the workspace; the cerebro group
	// allowlist only decides whether each agent is triggerable, surfaced as
	// `can_trigger` on the response. The trigger gate stays the upstream
	// `cerebroRequireAgentAccess` / `cerebroCanUseAgent` path — visibility
	// here is purely informational so users no longer face an empty picker
	// they can't reason about (replaces the older
	// list-agents-group-filter / list-agents-owner-exempt skip logic).
	visibleAgents, hasFilter, ownerExempt, _ := h.cerebroVisibleAgentIDSet(r.Context(), r, workspaceID)
	// Resolve the request actor once. Agents bypass the private-agent gate
	// to preserve A2A collaboration; members must be in allowed_principals
	// (agent owner or workspace owner/admin) to see private agents.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	visible := make([]AgentResponse, 0, len(agents))
	for _, a := range agents {
		if a.Visibility == "private" && actorType == "member" {
			if !memberAllowedForPrivateAgent(a, actorID, member.Role) {
				continue
			}
		}
		resp := agentToResponse(a)
		if skills, ok := skillMap[resp.ID]; ok {
			resp.Skills = skills
		}
		// Redact sensitive fields for users who are not the agent owner or workspace owner/admin.
		if !canViewAgentEnv(a, userID, member.Role) {
			redactEnv(&resp)
			redactMcpConfig(&resp)
		}
		// hasFilter=false means admin / nil-seam — every agent is triggerable.
		// hasFilter=true means cerebro group rules apply: group allowlist OR
		// owner-exemption (own agent + create_agent capability, JEH-1057).
		if !hasFilter {
			resp.CanTrigger = true
		} else {
			_, allowedByGroup := visibleAgents[uuidToString(a.ID)]
			resp.CanTrigger = allowedByGroup || ownerExempt(a.OwnerID)
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

	// Redact sensitive fields for users who are not the agent owner or workspace owner/admin.
	userID := requestUserID(r)
	if member, ok := ctxMember(r.Context()); ok {
		if !canViewAgentEnv(agent, userID, member.Role) {
			redactEnv(&resp)
			redactMcpConfig(&resp)
		}
	}

	// CEREBRO-PATCH(agent-can-trigger): JEH-1066 — surface trigger eligibility
	// so the UI can render the lock + tooltip without a second round-trip.
	resp.CanTrigger = h.cerebroCanTrigger(r.Context(), r, uuidToString(agent.WorkspaceID), agent.ID, agent.OwnerID)

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
	// Template records which template slug was used to seed this agent
	// (e.g. "coding" / "planning" / "writing" / "assistant"). Empty when
	// the caller didn't come from a template picker — the `agent_created`
	// event still fires with `template=""`, which is the correct signal
	// for "manually authored agent".
	Template string `json:"template"`
	// PersonaSandbox names a persona sandbox (e.g. "claude-developer") to
	// gate this agent's tool calls. Empty = no persona gating; the daemon
	// falls back to its existing per-runtime sandbox config.
	PersonaSandbox string `json:"persona_sandbox"`
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

	// CEREBRO-PATCH(create-agent-capability-gate): JEH-1009 require create_agent
	if !h.cerebroRequireCapability(w, r, workspaceID, "create_agent") {
		return
	}
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
	// CEREBRO-PATCH(create-agent-runtime-allowlist): JEH-1009 / JEH-1152 — gate + approval issue
	if !h.cerebroRequireRuntimeAccess(w, r, workspaceID, runtimeUUID, req.Name) {
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

	// Probe workspace agent count BEFORE the insert so the funnel has a
	// clean "first agent ever in this workspace" signal — Step 4 of
	// onboarding always lands in this branch. A non-fatal read: if the
	// list fails we fall through with isFirstAgent=false rather than
	// blocking creation, since the primary DB operation is the insert.
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

	agent, err := h.Queries.CreateAgent(r.Context(), db.CreateAgentParams{
		WorkspaceID:        wsUUID,
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
		Model:              pgtype.Text{String: req.Model, Valid: req.Model != ""},
		PersonaSandbox:     personaSandboxToText(req.PersonaSandbox),
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
	// CEREBRO-PATCH(agent-can-trigger): JEH-1066 — surface trigger eligibility.
	resp.CanTrigger = h.cerebroCanTrigger(r.Context(), r, workspaceID, agent.ID, agent.OwnerID)
	actorType, actorID := h.resolveActor(r, ownerID, workspaceID)
	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": resp})

	h.Analytics.Capture(analytics.AgentCreated(
		ownerID,
		workspaceID,
		uuidToString(agent.ID),
		runtime.Provider,
		runtime.RuntimeMode,
		req.Template,
		isFirstAgent,
	))

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
	// PersonaSandbox: empty-string clears the assignment (no persona gating);
	// "claude-developer" or similar attaches the named sandbox.
	PersonaSandbox *string `json:"persona_sandbox"`
}

// personaSandboxToText converts the API form (empty = unset) into the
// pgtype.Text shape sqlc expects. Unset values become an invalid pgtype.Text
// so the COALESCE in CreateAgent / UpdateAgent leaves the column NULL or
// untouched respectively.
func personaSandboxToText(name string) pgtype.Text {
	name = strings.TrimSpace(name)
	if name == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: name, Valid: true}
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
// regardless of whether it is public or private. Returns the resolved member
// so callers can do additional role-scoped checks (e.g. workspace-admin-only
// fields like persona_sandbox) without re-loading.
func (h *Handler) canManageAgent(w http.ResponseWriter, r *http.Request, agent db.Agent) (db.Member, bool) {
	wsID := uuidToString(agent.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, wsID, "agent not found", "owner", "admin", "member")
	if !ok {
		return db.Member{}, false
	}
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	isAgentOwner := uuidToString(agent.OwnerID) == requestUserID(r)
	if !isAdmin && !isAgentOwner {
		writeError(w, http.StatusForbidden, "only the agent owner can manage this agent")
		return db.Member{}, false
	}
	return member, true
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	member, ok := h.canManageAgent(w, r, agent)
	if !ok {
		return
	}

	var req UpdateAgentRequest
	rawFields, err := decodeJSONBodyWithRawFields(r.Body, &req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// E2: persona_sandbox is workspace-scoped policy, not per-agent config.
	// canManageAgent above lets either the agent owner or a workspace
	// owner/admin pass; restrict the sandbox field to admins so an agent
	// owner cannot self-elevate by switching to a more permissive sandbox.
	if _, hasPersonaSandboxField := rawFields["persona_sandbox"]; hasPersonaSandboxField {
		if !roleAllowed(member.Role, "owner", "admin") {
			writeError(w, http.StatusForbidden, "only workspace owner/admin can change persona_sandbox")
			return
		}
	}

	params := db.UpdateAgentParams{
		ID: agent.ID,
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
		runtimeUUID, ok := parseUUIDOrBadRequest(w, *req.RuntimeID, "runtime_id")
		if !ok {
			return
		}
		runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
			ID:          runtimeUUID,
			WorkspaceID: agent.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid runtime_id")
			return
		}
		// Same gate as CreateAgent — prevents UpdateAgent from being used to
		// re-bind an agent onto someone else's private runtime, which would
		// otherwise be a quiet end-run around the CreateAgent check.
		member, ok := h.workspaceMember(w, r, uuidToString(agent.WorkspaceID))
		if !ok {
			return
		}
		if !canUseRuntimeForAgent(member, runtime) {
			writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can move agents onto it")
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
	if req.Model != nil {
		params.Model = pgtype.Text{String: *req.Model, Valid: true}
	}
	// persona_sandbox: nil pointer = leave alone, "" = clear (handled below
	// because COALESCE can't set NULL), otherwise = update to the named value.
	rawPersonaSandbox, hasPersonaSandbox := rawFields["persona_sandbox"]
	shouldClearPersonaSandbox := hasPersonaSandbox && (bytes.Equal(bytes.TrimSpace(rawPersonaSandbox), []byte("null")) || bytes.Equal(bytes.TrimSpace(rawPersonaSandbox), []byte(`""`)))
	if req.PersonaSandbox != nil && !shouldClearPersonaSandbox {
		params.PersonaSandbox = personaSandboxToText(*req.PersonaSandbox)
	}

	agent, err = h.Queries.UpdateAgent(r.Context(), params)
	if err != nil {
		slog.Warn("update agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update agent: "+err.Error())
		return
	}

	// persona_sandbox: empty string in the request means explicitly clear.
	if shouldClearPersonaSandbox {
		agent, err = h.Queries.ClearAgentPersonaSandbox(r.Context(), parseUUID(id))
		if err != nil {
			slog.Warn("clear agent persona_sandbox failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear persona_sandbox: "+err.Error())
			return
		}
	}

	// W4.6: audit-log persona_sandbox changes so workspace owners can see
	// "who switched my agent off claude-readonly?" in Multica's UI without
	// digging through stdout. We emit on either an explicit value change
	// or a clear, and only when the field was actually present in the
	// request body (rawFields check below).
	if hasPersonaSandbox {
		newValue := ""
		if !shouldClearPersonaSandbox && req.PersonaSandbox != nil {
			newValue = *req.PersonaSandbox
		}
		details, _ := json.Marshal(map[string]any{
			"agent_id":   id,
			"agent_name": agent.Name,
			"new":        newValue,
		})
		_, _ = h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
			WorkspaceID: agent.WorkspaceID,
			IssueID:     pgtype.UUID{},
			ActorType:   pgtype.Text{String: "member", Valid: true},
			ActorID:     parseUUID(requestUserID(r)),
			Action:      "agent_persona_sandbox_changed",
			Details:     details,
		})
	}

	// mcp_config: null in the request means explicitly clear the field.
	// COALESCE in UpdateAgent cannot set a column to NULL, so we use a dedicated query.
	if shouldClearMcpConfig {
		agent, err = h.Queries.ClearAgentMcpConfig(r.Context(), agent.ID)
		if err != nil {
			slog.Warn("clear agent mcp_config failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear mcp_config: "+err.Error())
			return
		}
	}

	resp := agentToResponse(agent)
	// CEREBRO-PATCH(agent-can-trigger): JEH-1066 — surface trigger eligibility.
	resp.CanTrigger = h.cerebroCanTrigger(r.Context(), r, uuidToString(agent.WorkspaceID), agent.ID, agent.OwnerID)
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
	if _, ok := h.canManageAgent(w, r, agent); !ok {
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
	// CEREBRO-PATCH(agent-can-trigger): JEH-1066 — surface trigger eligibility.
	resp.CanTrigger = h.cerebroCanTrigger(r.Context(), r, wsID, archived.ID, archived.OwnerID)
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
	if _, ok := h.canManageAgent(w, r, agent); !ok {
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
	// CEREBRO-PATCH(agent-can-trigger): JEH-1066 — surface trigger eligibility.
	resp.CanTrigger = h.cerebroCanTrigger(r.Context(), r, wsID, restored.ID, restored.OwnerID)
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
	if _, ok := h.canManageAgent(w, r, agent); !ok {
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
		resp = append(resp, taskToResponse(t))
	}

	writeJSON(w, http.StatusOK, resp)
}
