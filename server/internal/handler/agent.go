package handler

import (
	"bytes"
	"context"
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
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
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

const (
	agentServiceTierFast    = "fast"
	agentServiceTierDefault = "default"
)

type AgentResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	RuntimeID     string          `json:"runtime_id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Instructions  string          `json:"instructions"`
	AvatarURL     *string         `json:"avatar_url"`
	RuntimeMode   string          `json:"runtime_mode"`
	RuntimeConfig any             `json:"runtime_config"`
	CustomArgs    []string        `json:"custom_args"`
	McpConfig     json.RawMessage `json:"mcp_config"`
	// custom_env is intentionally NOT serialized on agent resources. The
	// agent_list/get/create/update/archive/restore responses and WS events
	// only expose coarse metadata (has_custom_env, custom_env_key_count) so
	// the UI can show "N variables configured" without dragging secrets
	// across the API surface. Reading values requires the dedicated, audited
	// `GET /api/agents/{id}/env` endpoint; writing requires `PUT` to the
	// same path. agent-actor tokens are denied there. See MUL-2600.
	HasCustomEnv       bool   `json:"has_custom_env"`
	CustomEnvKeyCount  int    `json:"custom_env_key_count"`
	McpConfigRedacted  bool   `json:"mcp_config_redacted"`
	Visibility         string `json:"visibility"`
	Status             string `json:"status"`
	MaxConcurrentTasks int32  `json:"max_concurrent_tasks"`
	Model              string `json:"model"`
	// ThinkingLevel is the runtime-native reasoning/effort token persisted
	// for this agent (empty = use runtime default). The picker is per-runtime
	// per-model; the API never normalizes across providers. See MUL-2339.
	ThinkingLevel          string              `json:"thinking_level"`
	ServiceTier            *string             `json:"service_tier,omitempty"`
	OwnerID                *string             `json:"owner_id"`
	AllowedUserIDs         []string            `json:"allowed_user_ids"`
	Skills                 []AgentSkillSummary `json:"skills"`
	CreatedAt              string              `json:"created_at"`
	UpdatedAt              string              `json:"updated_at"`
	ArchivedAt             *string             `json:"archived_at"`
	ArchivedBy             *string             `json:"archived_by"`
	CustomEnvCopiedPending bool                `json:"custom_env_copied_pending"`
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
	return agentToResponseForProvider(a, "")
}

func agentToResponseForProvider(a db.Agent, provider string) AgentResponse {
	var rc any
	if a.RuntimeConfig != nil {
		json.Unmarshal(a.RuntimeConfig, &rc)
	}
	if rc == nil {
		rc = map[string]any{}
	}

	// Compute env metadata WITHOUT exposing the values. We unmarshal here
	// only to count keys; the map never reaches the response. A coarse
	// has_custom_env / key_count is what the UI gets — to read the values
	// the caller must hit GET /api/agents/{id}/env (owner/admin only,
	// audited).
	envKeyCount := 0
	if a.CustomEnv != nil {
		var customEnv map[string]string
		if err := json.Unmarshal(a.CustomEnv, &customEnv); err != nil {
			slog.Warn("failed to unmarshal agent custom_env", "agent_id", uuidToString(a.ID), "error", err)
		}
		envKeyCount = len(customEnv)
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

	resp := AgentResponse{
		ID:                     uuidToString(a.ID),
		WorkspaceID:            uuidToString(a.WorkspaceID),
		RuntimeID:              uuidToString(a.RuntimeID),
		Name:                   a.Name,
		Description:            a.Description,
		Instructions:           a.Instructions,
		AvatarURL:              textToPtr(a.AvatarUrl),
		RuntimeMode:            a.RuntimeMode,
		RuntimeConfig:          rc,
		CustomArgs:             customArgs,
		McpConfig:              mcpConfig,
		HasCustomEnv:           envKeyCount > 0,
		CustomEnvKeyCount:      envKeyCount,
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
	if provider == "codex" {
		serviceTier := a.ServiceTier.String
		resp.ServiceTier = &serviceTier
	}
	return resp
}

func (h *Handler) agentToResponseForRuntime(ctx context.Context, a db.Agent) AgentResponse {
	provider, _ := h.resolveAgentProvider(ctx, a.WorkspaceID, a.RuntimeID)
	return agentToResponseForProvider(a, provider)
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

// copyCustomEnvForAgentCopy preserves env values only when the source agent
// already belongs to the current user. Copying another user's agent keeps the
// keys for usability but drops values to avoid leaking secrets.
func copyCustomEnvForAgentCopy(src []byte, sourceOwnerID pgtype.UUID, userID string) ([]byte, bool) {
	if uuidToString(sourceOwnerID) != userID {
		return stripCustomEnvValuesForCopy(src)
	}
	if len(src) == 0 {
		return []byte("{}"), false
	}
	return append([]byte(nil), src...), false
}

// RepoData holds repository information included in claim responses so the
// daemon can set up worktrees for each workspace repo.
type RepoData struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
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
	WorkspaceContext string                `json:"workspace_context,omitempty"`
	Status           string                `json:"status"`
	Priority         int32                 `json:"priority"`
	DispatchedAt     *string               `json:"dispatched_at"`
	StartedAt        *string               `json:"started_at"`
	CompletedAt      *string               `json:"completed_at"`
	Result           any                   `json:"result"`
	Error            *string               `json:"error"`
	Context          any                   `json:"context,omitempty"`
	FailureReason    string                `json:"failure_reason,omitempty"` // see TaskService.MaybeRetryFailedTask
	Attempt          int32                 `json:"attempt"`
	MaxAttempts      int32                 `json:"max_attempts"`
	ParentTaskID     *string               `json:"parent_task_id,omitempty"`
	Agent            *TaskAgentData        `json:"agent,omitempty"`
	Repos            []RepoData            `json:"repos,omitempty"`
	ProjectID        string                `json:"project_id,omitempty"`        // issue's project, when present
	ProjectTitle     string                `json:"project_title,omitempty"`     // for surfacing in agent context
	ProjectResources []ProjectResourceData `json:"project_resources,omitempty"` // resources attached to the project
	CreatedAt        string                `json:"created_at"`
	PriorSessionID   string                `json:"prior_session_id,omitempty"` // session ID from a previous task on same issue
	PriorWorkDir     string                `json:"prior_work_dir,omitempty"`   // work_dir from a previous task on same issue
	WorkDir          string                `json:"work_dir,omitempty"`         // local working directory pinned for this task; populated once the daemon reports it
	// RelativeWorkDir is a privacy-safe display form of WorkDir intended for
	// the UI. For standard tasks it strips the daemon's workspaces root so
	// the user sees `<wsUUID>/<taskShort>/workdir`; for local_directory
	// tasks the absolute path lives outside the envRoot layout, so we strip
	// recognised home-directory prefixes (`/Users/<name>/`, `/home/<name>/`,
	// `<drive>:/Users/<name>/`) and otherwise fall back to the basename so
	// the field never carries the user's home dir or account name. Empty
	// when WorkDir is empty, or when stripping leaves nothing. See
	// relativeWorkDir() for the full rules. Older clients can still read
	// WorkDir directly; newer UIs should prefer RelativeWorkDir.
	RelativeWorkDir         string               `json:"relative_work_dir,omitempty"`
	TriggerCommentID        *string              `json:"trigger_comment_id,omitempty"`        // comment that triggered this task
	TriggerThreadID         string               `json:"trigger_thread_id,omitempty"`         // root comment ID for the triggering thread
	TriggerCommentContent   string               `json:"trigger_comment_content,omitempty"`   // content of the triggering comment
	TriggerSummary          *string              `json:"trigger_summary,omitempty"`           // canonical short description snapshot — comment text / autopilot title — taken at task creation; survives source edits/deletes
	TriggerAuthorType       string               `json:"trigger_author_type,omitempty"`       // "agent" or "member" — author kind of the triggering comment
	TriggerAuthorName       string               `json:"trigger_author_name,omitempty"`       // display name of the triggering comment author
	NewCommentCount         int                  `json:"new_comment_count,omitempty"`         // trigger-thread comments since last run; excludes injected trigger + own comments; omitempty so old daemons ignore it
	NewCommentsSince        string               `json:"new_comments_since,omitempty"`        // RFC3339 anchor (last run's started_at) the count is measured from; omitempty so old daemons ignore it
	ChatSessionID           string               `json:"chat_session_id,omitempty"`           // non-empty for chat tasks
	ChatMessage             string               `json:"chat_message,omitempty"`              // user message for chat tasks
	ChatMessageAttachments  []ChatAttachmentMeta `json:"chat_message_attachments,omitempty"`  // attachments on the user message — agent calls `multica attachment download <id>` per entry
	AutopilotRunID          string               `json:"autopilot_run_id,omitempty"`          // non-empty for autopilot-spawned tasks
	AutopilotID             string               `json:"autopilot_id,omitempty"`              // autopilot that spawned this task
	AutopilotTitle          string               `json:"autopilot_title,omitempty"`           // autopilot title used as task context
	AutopilotDescription    string               `json:"autopilot_description,omitempty"`     // autopilot description used as task prompt
	AutopilotSource         string               `json:"autopilot_source,omitempty"`          // manual, schedule, webhook, or api
	AutopilotTriggerPayload json.RawMessage      `json:"autopilot_trigger_payload,omitempty"` // optional trigger payload for webhook/api runs
	QuickCreatePrompt       string               `json:"quick_create_prompt,omitempty"`       // user's natural-language input for quick-create tasks
	SquadID                 string               `json:"squad_id,omitempty"`                  // for quick-create tasks where the picker was a squad; Agent is still the resolved leader
	SquadName               string               `json:"squad_name,omitempty"`                // display name for the picker squad
	ParentIssueID           string               `json:"parent_issue_id,omitempty"`           // for quick-create tasks opened from "Add sub issue" — UUID of the parent issue the new issue should be filed under
	ParentIssueIdentifier   string               `json:"parent_issue_identifier,omitempty"`   // human-readable identifier (e.g. MUL-123) of the quick-create parent issue, resolved on claim for prompt context
	// RequestingUserName + RequestingUserProfileDescription mirror the user
	// the agent is acting on behalf of (see daemon/types.go). v1 sources them
	// from the runtime owner so they're populated for daemon runtimes and
	// empty otherwise. The daemon emits both into the brief under
	// `## Requesting User`; the heading is skipped entirely when description
	// is empty.
	RequestingUserName               string `json:"requesting_user_name,omitempty"`
	RequestingUserProfileDescription string `json:"requesting_user_profile_description,omitempty"`
	Kind                             string `json:"kind"` // discriminator: "comment" | "autopilot" | "chat" | "quick_create" | "direct" — used by the activity row to label tasks that have no linked issue
	// AuthToken is the task-scoped `mat_` token the daemon must inject as
	// MULTICA_TOKEN in the agent process environment. The server binds it to
	// this (agent_id, task_id) pair at claim time and treats any request
	// authenticated with it as actor=agent, regardless of headers — so the
	// agent process cannot use it to read another agent's secrets via the
	// env-management endpoint. Empty only for legacy ownerless runtimes; local
	// owner runtimes must receive a token or the daemon fails closed.
	AuthToken string `json:"auth_token,omitempty"`
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
	Description   string                   `json:"description,omitempty"`
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
	ServiceTier   string                   `json:"service_tier,omitempty"`
}

// taskToSlimResponse builds a response without the heavy Context and Result
// blobs. Used by list endpoints (snapshot, task-runs) where these fields are
// never consumed by the frontend and dominate response size.
func taskToSlimResponse(t db.AgentTaskQueue, workspaceID string) AgentTaskResponse {
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
		RelativeWorkDir:  relativeWorkDir(workDir, workspaceID, uuidToString(t.ID)),
		ChatSessionID:    uuidToString(t.ChatSessionID),
		AutopilotRunID:   uuidToString(t.AutopilotRunID),
		Kind:             computeTaskKind(t),
	}
}

// taskToResponse maps a queue row to its wire shape. workspaceID is threaded
// in because the row itself doesn't carry one (workspace lives on the agent
// / issue / chat session) — we ask the caller to resolve it once and pass it
// down. It populates WorkspaceID and powers the privacy-safe RelativeWorkDir
// derivation; pass "" only on daemon-facing paths that genuinely don't have
// it, in which case RelativeWorkDir falls back to the existing WorkDir.
func taskToResponse(t db.AgentTaskQueue, workspaceID string) AgentTaskResponse {
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
		WorkspaceID:      workspaceID,
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
		RelativeWorkDir:  relativeWorkDir(workDir, workspaceID, uuidToString(t.ID)),
		// Surface task source so the UI can distinguish issue-linked tasks
		// from chat-spawned or autopilot-spawned ones; all three may arrive
		// with issue_id = "" once a task has no linked issue.
		ChatSessionID:  uuidToString(t.ChatSessionID),
		AutopilotRunID: uuidToString(t.AutopilotRunID),
		Kind:           computeTaskKind(t),
	}
}

// relativeWorkDir produces a privacy-safe display form of the daemon-reported
// absolute work_dir. The contract: the returned string must never contain
// the user's home directory prefix or their account name. The chip is
// rendered in transcripts that frequently end up in screen shares,
// screenshots, and recordings, so this function is the only guard.
//
//   - For standard tasks (work_dir laid out as `<workspacesRoot>/<wsUUID>/
//     <taskShort>/workdir` by execenv.Prepare), it strips everything up to and
//     including the workspaces root, returning `<wsUUID>/<taskShort>/workdir`.
//   - For local_directory tasks the absolute path lives outside the envRoot
//     layout. We try to recognise common home-directory prefixes
//     (`/Users/<name>/`, `/home/<name>/`, `<drive>:/Users/<name>/`) and strip
//     them, returning the remainder (e.g. `repos/foo`). When the prefix
//     can't be recognised — unusual home layouts, network mounts, paths
//     under `/opt`, `/srv`, etc. — we fall back to the basename so we never
//     accidentally render a path component that happens to be a username.
//
// Returns empty when work_dir is empty, or when stripping leaves nothing
// (i.e. work_dir was exactly the user's home — rendering nothing is
// preferable to a chip that says `<name>`). shortTaskID() must stay in
// lock-step with server/internal/daemon/execenv/git.go:shortID — both
// consume the same task UUID; if that helper changes, this one must too
// or the envRoot match silently degrades to the local_directory fallback.
func relativeWorkDir(workDir, workspaceID, taskID string) string {
	if workDir == "" {
		return ""
	}
	// Normalize Windows separators so the rest of the function only
	// reasons about forward slashes.
	normalized := strings.ReplaceAll(workDir, "\\", "/")

	if workspaceID != "" && taskID != "" {
		envRootSuffix := workspaceID + "/" + shortTaskID(taskID)
		if idx := strings.Index(normalized, envRootSuffix); idx >= 0 {
			return normalized[idx:]
		}
	}

	if stripped, ok := stripHomePrefix(normalized); ok {
		return stripped
	}

	return basename(normalized)
}

// shortTaskID mirrors execenv.shortID — first 8 hex chars of the UUID
// with dashes stripped. Kept inline here so the agent handler has zero
// imports from the daemon package (which would create an unwanted cycle
// between handler and daemon).
func shortTaskID(uuid string) string {
	s := strings.ReplaceAll(uuid, "-", "")
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// homeDirPattern matches the well-known per-user home layouts on macOS,
// Linux, and Windows after backslash normalization:
//
//	/Users/<name>[/<rest>]
//	/home/<name>[/<rest>]
//	<drive>:/Users/<name>[/<rest>]
//
// Case-insensitive because macOS and Windows are case-insensitive at the
// filesystem layer; matching `/users/...` the same as `/Users/...` keeps
// the strip robust against unusual casings seen on shared drives.
// Capture group 1 is the optional remainder after the username segment.
var homeDirPattern = regexp.MustCompile(`(?i)^(?:[A-Za-z]:)?/(?:Users|home)/[^/]+(?:/(.*))?$`)

// stripHomePrefix recognises common home-directory layouts and returns
// the path remainder after the username segment. Returns (remainder, true)
// when a known home prefix matched. The remainder may be the empty string
// (work_dir was exactly the home directory) — the caller treats that as
// "nothing safe to display".
func stripHomePrefix(p string) (string, bool) {
	m := homeDirPattern.FindStringSubmatch(p)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// basename returns the last non-empty segment of a forward-slash path.
// Used as the ultimate privacy-safe fallback when we can't otherwise
// recognise the path: a single segment can never expose the home prefix,
// and the leaf is almost always the most useful piece of context anyway
// (typically the repo directory name for local_directory tasks).
func basename(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[idx+1:]
	}
	return p
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

	// mcp_config still uses the workspace-level always-redact setting and
	// the per-row owner/admin gate — secrets in MCP server configs follow
	// the same exposure rules as custom_env used to. custom_env itself is
	// never serialized on agent resources anymore (MUL-2600); see the
	// AgentResponse comment.
	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("GetWorkspace failed for redact check", "workspace_id", workspaceID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	alwaysRedact := workspaceAlwaysRedactSecrets(ws.Settings)

	// Resolve the request actor once. Agents bypass the private-agent gate
	// to preserve A2A collaboration; members must be allowed to see private
	// agents by ownership, workspace role, or the explicit allowlist.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	runtimes, err := h.Queries.ListAgentRuntimes(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent runtimes")
		return
	}
	visible := make([]AgentResponse, 0, len(agents))
	runtimesByID := map[string]db.AgentRuntime{}
	for _, rt := range runtimes {
		runtimesByID[uuidToString(rt.ID)] = rt
	}
	for _, a := range agents {
		if a.Visibility == "private" && actorType == "member" {
			if !memberAllowedForPrivateAgentWithAllowlist(a, actorID, member.Role, allowedUserMap[uuidToString(a.ID)]) {
				continue
			}
		}
		provider := ""
		if rt, ok := runtimesByID[uuidToString(a.RuntimeID)]; ok {
			provider = rt.Provider
		}
		resp := agentToResponseForProvider(a, provider)
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
		// Agent actors NEVER see mcp_config secrets, even when their host's
		// PAT would normally satisfy the owner/admin role gate. Otherwise an
		// agent running under an owner's daemon could read other agents'
		// MCP configs (which routinely embed third-party API tokens) — the
		// same lateral-movement vector MUL-2600 closed for custom_env.
		if actorType == "agent" || alwaysRedact || !canViewAgentSecrets(a, userID, member.Role) {
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
	provider, _ := h.resolveAgentProvider(r.Context(), agent.WorkspaceID, agent.RuntimeID)
	resp := agentToResponseForProvider(agent, provider)
	// Use the summary query (no `content` column) — the embedded
	// AgentSkillSummary only needs id/name/description, and reading large
	// SKILL.md bodies just to discard them is the exact regression we fixed
	// in #2174.
	if err := h.attachAgentSkills(r.Context(), &resp, agent.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
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

	// mcp_config redaction (custom_env was removed from this response shape
	// in MUL-2600; secrets are now fetched via GET /api/agents/{id}/env).
	userID := requestUserID(r)
	ws, err := h.Queries.GetWorkspace(r.Context(), agent.WorkspaceID)
	if err != nil {
		slog.Warn("GetWorkspace failed for redact check", "workspace_id", uuidToString(agent.WorkspaceID), "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	alwaysRedact := workspaceAlwaysRedactSecrets(ws.Settings)
	// Agent actors NEVER see mcp_config (see ListAgents for the rationale).
	if actorType == "agent" || alwaysRedact {
		redactMcpConfig(&resp)
	} else if member, ok := ctxMember(r.Context()); ok {
		if !canViewAgentSecrets(agent, userID, member.Role) {
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
	Model              string            `json:"model"`
	ThinkingLevel      string            `json:"thinking_level"`
	ServiceTier        string            `json:"service_tier"`
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
	serviceTier := ""
	if runtime.Provider == "codex" {
		if !isKnownAgentServiceTier(req.ServiceTier) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("service_tier %q is not a recognised Codex service tier", req.ServiceTier))
			return
		}
		serviceTier = req.ServiceTier
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
		ServiceTier:            pgtype.Text{String: serviceTier, Valid: serviceTier != ""},
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

	resp := agentToResponseForProvider(created, runtime.Provider)
	actorType, actorID := h.resolveActor(r, ownerID, workspaceID)
	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.AgentCreated(
		ownerID,
		workspaceID,
		uuidToString(created.ID),
		runtime.Provider,
		runtime.RuntimeMode,
		req.Template,
		isFirstAgent,
	))

	redactAgentResponseForActor(&resp, actorType)
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
	ce, envCopiedPending := copyCustomEnvForAgentCopy(sourceAgent.CustomEnv, sourceAgent.OwnerID, userID)
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
	sourceProvider, _ := h.resolveAgentProvider(r.Context(), sourceAgent.WorkspaceID, sourceAgent.RuntimeID)
	serviceTier := pgtype.Text{}
	if sourceProvider == "codex" {
		serviceTier = sourceAgent.ServiceTier
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
		ThinkingLevel:          sourceAgent.ThinkingLevel,
		ServiceTier:            serviceTier,
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

	resp := h.agentToResponseForRuntime(r.Context(), newAgent)
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
	Name          *string `json:"name"`
	Description   *string `json:"description"`
	Instructions  *string `json:"instructions"`
	AvatarURL     *string `json:"avatar_url"`
	RuntimeID     *string `json:"runtime_id"`
	RuntimeConfig any     `json:"runtime_config"`
	// custom_env is intentionally NOT updatable through this endpoint.
	// Use `PUT /api/agents/{id}/env` for env changes — that path is
	// owner/admin-only, denies agent actors, and writes a persisted
	// audit log entry. A `PUT /api/agents/{id}` body that carries
	// `custom_env` is rejected with 400 in the handler below so a
	// caller never believes they rotated a secret when the value is
	// actually unchanged, and so a client that round-tripped a
	// previously-returned masked map cannot silently overwrite real
	// secret values with literal `****`. See MUL-2600.
	CustomArgs         *[]string        `json:"custom_args"`
	McpConfig          *json.RawMessage `json:"mcp_config"`
	Visibility         *string          `json:"visibility"`
	Status             *string          `json:"status"`
	MaxConcurrentTasks *int32           `json:"max_concurrent_tasks"`
	Model              *string          `json:"model"`
	// ThinkingLevel is treated as a tri-state per-MUL-2339:
	//   - field omitted → no change (leave existing value alone)
	//   - field present with "" → explicit clear (use runtime default)
	//   - field present with non-empty value → set (validated server-side)
	// Distinguishing those modes is why this is a pointer; the raw-fields
	// map captured at decode time tells us whether the key was sent.
	ThinkingLevel *string `json:"thinking_level"`
	// ServiceTier is Codex-only Fast mode state:
	//   - omitted → no change
	//   - "" → clear (follow local Codex config/default)
	//   - "fast" → enable Fast mode
	//   - "default" → explicitly use standard tier
	// Non-Codex runtimes ignore the field and clear any hidden persisted value.
	ServiceTier *string `json:"service_tier"`
}

// workspaceAlwaysRedactSecrets reports whether the workspace has opted
// into unconditional redaction of secret-bearing fields (currently
// `mcp_config`) on read responses, regardless of the caller's role.
//
// The legacy JSON key is still `always_redact_env` for backwards-
// compatibility with workspaces that flipped the setting before MUL-2600
// shipped. The setting no longer affects `custom_env` because that field
// is never serialized on agent resources anymore — secrets there are
// fetched exclusively through `GET /api/agents/{id}/env` with audit
// logging — so the flag now only governs `mcp_config` exposure.
func workspaceAlwaysRedactSecrets(settings []byte) bool {
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

func isKnownAgentServiceTier(value string) bool {
	switch value {
	case "", agentServiceTierFast, agentServiceTierDefault:
		return true
	default:
		return false
	}
}

// canViewAgentSecrets checks whether the requesting user is allowed to
// see the agent's secret-bearing fields (currently `mcp_config`). Only
// the agent owner or workspace owner/admin qualify; for everyone else
// the response is redacted. `custom_env` is no longer part of an agent
// resource response (see MUL-2600), so this predicate is shared only by
// the remaining mcp_config redaction path.
func canViewAgentSecrets(agent db.Agent, userID string, memberRole string) bool {
	if agent.OwnerID.Valid && uuidToString(agent.OwnerID) == userID {
		return true
	}
	if roleAllowed(memberRole, "owner", "admin") {
		return true
	}
	return false
}

// canViewAgentEnv checks whether the requesting user is allowed to see or
// mutate the agent's custom environment variables. OPE-817 keeps this stricter
// than mcp_config: only the agent owner may access env values. Legacy ownerless
// agents fall back to workspace owner/admin for backward compatibility.
func canViewAgentEnv(agent db.Agent, userID string, memberRole string) bool {
	if agent.OwnerID.Valid && uuidToString(agent.OwnerID) == userID {
		return true
	}
	if !agent.OwnerID.Valid && roleAllowed(memberRole, "owner", "admin") {
		return true
	}
	return false
}

// broadcastAgentResponse strips secret-bearing fields from an
// AgentResponse before it goes onto the WebSocket bus. Mutation
// handlers call this when fanning out create/update/archive/restore
// events: subscribers (which include agent processes that have
// authenticated with their own task tokens) must not learn another
// agent's mcp_config via a WS push that bypassed the read-path
// redaction in ListAgents / GetAgent. The caller still receives the
// canonical form in the HTTP response; only the broadcast copy is
// redacted.
func broadcastAgentResponse(resp AgentResponse) AgentResponse {
	out := resp
	redactMcpConfig(&out)
	return out
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

// redactAgentResponseForActor strips secret-bearing fields from an agent
// resource HTTP response when the request actor is an agent. Read
// handlers already gate on actorType — mutation handlers
// (create/update/archive/restore) must apply the same rule, otherwise
// an agent with a host owner/admin token can do an unrelated mutation
// (e.g. flip max_concurrent_tasks) on a target agent and harvest the
// target's mcp_config from the mutation response. MUL-2600.
func redactAgentResponseForActor(resp *AgentResponse, actorType string) {
	if actorType == "agent" {
		redactMcpConfig(resp)
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

	// Hard-reject any attempt to write custom_env through the generic
	// update endpoint. Silently dropping the field (which is what an
	// `omitempty` field would do) was the pre-PR behaviour and led to
	// users believing they had rotated a secret when the value was
	// actually unchanged. env values move only through `PUT
	// /api/agents/{id}/env` — that endpoint is owner/admin-only, denies
	// agent actors, and writes a queryable audit row.
	if _, ok := rawFields["custom_env"]; ok {
		writeError(w, http.StatusBadRequest, "custom_env is no longer accepted on this endpoint; use PUT /api/agents/{id}/env (or `multica agent env set`)")
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
	targetRuntimeProvider := ""
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
		targetRuntimeProvider = runtime.Provider
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

	targetProvider := func() (string, bool) {
		if targetRuntimeProvider != "" {
			return targetRuntimeProvider, true
		}
		provider, ok := h.resolveAgentProvider(r.Context(), existing.WorkspaceID, targetRuntimeID)
		if ok {
			targetRuntimeProvider = provider
		}
		return provider, ok
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
			provider, ok := targetProvider()
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
		provider, ok := targetProvider()
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

	// service_tier handling (OPE-2421). Codex-only; non-Codex runtimes ignore
	// incoming values and clear any persisted tier so hidden config cannot
	// survive a runtime switch.
	shouldClearServiceTier := false
	if req.ServiceTier != nil {
		value := *req.ServiceTier
		provider, ok := targetProvider()
		if !ok {
			writeError(w, http.StatusInternalServerError, "failed to resolve runtime for service_tier validation")
			return
		}
		if provider != "codex" {
			shouldClearServiceTier = existing.ServiceTier.Valid
		} else if value == "" {
			shouldClearServiceTier = true
		} else {
			if !isKnownAgentServiceTier(value) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("service_tier %q is not a recognised Codex service tier", value))
				return
			}
			params.ServiceTier = pgtype.Text{String: value, Valid: true}
		}
	} else if req.RuntimeID != nil {
		provider, ok := targetProvider()
		if !ok {
			writeError(w, http.StatusInternalServerError, "failed to resolve runtime for service_tier validation")
			return
		}
		if provider != "codex" && existing.ServiceTier.Valid {
			shouldClearServiceTier = true
		}
	}

	updated, err := h.Queries.UpdateAgent(r.Context(), params)
	if err != nil {
		slog.Warn("update agent failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update agent: "+err.Error())
		return
	}

	// mcp_config / thinking_level / service_tier: null/empty in the request means explicitly
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
	if shouldClearServiceTier {
		updated, err = h.Queries.ClearAgentServiceTier(r.Context(), updated.ID)
		if err != nil {
			slog.Warn("clear agent service_tier failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear service_tier: "+err.Error())
			return
		}
	}

	provider, _ := targetProvider()
	resp := agentToResponseForProvider(updated, provider)
	// agentToResponse always initialises Skills as []; junction-table rows
	// are untouched by the SQL update, so we reload them here to keep the
	// response (and the broadcast that mirrors it) in sync with reality.
	// Without this, callers see "skills": [] after every metadata-only
	// update and assume their bindings were cleared — see #3459.
	if err := h.attachAgentSkills(r.Context(), &resp, updated.ID); err != nil {
		slog.Warn("load agent skills after update failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	slog.Info("agent updated", append(logger.RequestAttrs(r), "agent_id", id, "workspace_id", uuidToString(updated.WorkspaceID))...)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(updated.WorkspaceID))
	h.publish(protocol.EventAgentStatus, uuidToString(updated.WorkspaceID), actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	redactAgentResponseForActor(&resp, actorType)
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
	UserIDs       *[]string `json:"user_ids"`
	AddUserIDs    []string  `json:"add_user_ids"`
	RemoveUserIDs []string  `json:"remove_user_ids"`
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
	parseAllowedUserIDs := func(rawIDs []string) ([]pgtype.UUID, bool) {
		seen := map[string]struct{}{}
		userIDs := make([]pgtype.UUID, 0, len(rawIDs))
		for _, rawID := range rawIDs {
			userID, ok := parseUUIDOrBadRequest(w, rawID, "user id")
			if !ok {
				return nil, false
			}
			userIDStr := uuidToString(userID)
			if _, exists := seen[userIDStr]; exists {
				continue
			}
			seen[userIDStr] = struct{}{}
			if _, err := h.getWorkspaceMember(r.Context(), userIDStr, workspaceID); err != nil {
				writeError(w, http.StatusBadRequest, "allowed user must be a workspace member")
				return nil, false
			}
			userIDs = append(userIDs, userID)
		}
		return userIDs, true
	}

	var replaceUserIDs []pgtype.UUID
	if req.UserIDs != nil {
		replaceUserIDs, ok = parseAllowedUserIDs(*req.UserIDs)
		if !ok {
			return
		}
	}
	addUserIDs, ok := parseAllowedUserIDs(req.AddUserIDs)
	if !ok {
		return
	}
	removeUserIDs, ok := parseAllowedUserIDs(req.RemoveUserIDs)
	if !ok {
		return
	}

	for _, addUserID := range addUserIDs {
		addUserIDStr := uuidToString(addUserID)
		for _, removeUserID := range removeUserIDs {
			if addUserIDStr == uuidToString(removeUserID) {
				writeError(w, http.StatusBadRequest, "allowed user cannot be both added and removed")
				return
			}
		}
	}

	if req.UserIDs != nil && (len(req.AddUserIDs) > 0 || len(req.RemoveUserIDs) > 0) {
		writeError(w, http.StatusBadRequest, "user_ids cannot be combined with add_user_ids or remove_user_ids")
		return
	}

	q := h.Queries
	var commit func() error
	if len(addUserIDs) > 0 && len(removeUserIDs) > 0 {
		tx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start transaction")
			return
		}
		defer tx.Rollback(r.Context())
		q = h.Queries.WithTx(tx)
		commit = func() error { return tx.Commit(r.Context()) }
	}

	switch {
	case req.UserIDs != nil:
		if err := q.ReplaceAgentAllowedPrincipals(r.Context(), db.ReplaceAgentAllowedPrincipalsParams{
			WorkspaceID:  agent.WorkspaceID,
			AgentID:      agent.ID,
			PrincipalIds: replaceUserIDs,
			CreatedBy:    parseUUID(requestUserID(r)),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update agent allowed users")
			return
		}
	case len(addUserIDs) > 0 || len(removeUserIDs) > 0:
		if len(removeUserIDs) > 0 {
			if err := q.RemoveAgentAllowedPrincipals(r.Context(), db.RemoveAgentAllowedPrincipalsParams{
				AgentID:      agent.ID,
				PrincipalIds: removeUserIDs,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update agent allowed users")
				return
			}
		}
		if len(addUserIDs) > 0 {
			if err := q.AddAgentAllowedPrincipals(r.Context(), db.AddAgentAllowedPrincipalsParams{
				WorkspaceID:  agent.WorkspaceID,
				AgentID:      agent.ID,
				PrincipalIds: addUserIDs,
				CreatedBy:    parseUUID(requestUserID(r)),
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update agent allowed users")
				return
			}
		}
	default:
	}

	if commit != nil {
		if err := commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update agent allowed users")
			return
		}
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

// attachAgentSkills populates resp.Skills from the agent_skill junction
// table for the given agent. agentToResponse zeros the field; mutation
// handlers that don't refresh it would otherwise serve a misleading
// empty array on every successful response (#3459).
func (h *Handler) attachAgentSkills(ctx context.Context, resp *AgentResponse, agentID pgtype.UUID) error {
	skills, err := h.Queries.ListAgentSkillSummaries(ctx, agentID)
	if err != nil {
		return err
	}
	if len(skills) == 0 {
		return nil
	}
	out := make([]AgentSkillSummary, len(skills))
	for i, s := range skills {
		out[i] = AgentSkillSummary{
			ID:          uuidToString(s.ID),
			Name:        s.Name,
			Description: s.Description,
		}
	}
	resp.Skills = out
	return nil
}

// resolveAgentProvider returns the provider name for the runtime that
// will own this agent after the in-flight update applies. Used by the
// thinking_level validator so a runtime/model swap and a level swap
// validated in the same request both consult the same provider.
func (h *Handler) resolveAgentProvider(ctx context.Context, workspaceID pgtype.UUID, runtimeID pgtype.UUID) (string, bool) {
	rt, err := h.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
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
	resp := h.agentToResponseForRuntime(r.Context(), archived)
	if err := h.attachAgentSkills(r.Context(), &resp, archived.ID); err != nil {
		slog.Warn("load agent skills after archive failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, wsID)
	h.publish(protocol.EventAgentArchived, wsID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	redactAgentResponseForActor(&resp, actorType)
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
	resp := h.agentToResponseForRuntime(r.Context(), restored)
	if err := h.attachAgentSkills(r.Context(), &resp, restored.ID); err != nil {
		slog.Warn("load agent skills after restore failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, wsID)
	h.publish(protocol.EventAgentRestored, wsID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	redactAgentResponseForActor(&resp, actorType)
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
		resp[i] = taskToResponse(t, workspaceID)
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
		resp = append(resp, taskToSlimResponse(t, workspaceID))
	}

	writeJSON(w, http.StatusOK, resp)
}
