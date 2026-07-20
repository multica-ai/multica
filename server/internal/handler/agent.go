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
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
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
	HasCustomEnv      bool   `json:"has_custom_env"`
	CustomEnvKeyCount int    `json:"custom_env_key_count"`
	McpConfigRedacted bool   `json:"mcp_config_redacted"`
	Visibility        string `json:"visibility"`
	// PermissionMode is the invocation-permission mode (MUL-3963):
	// "private" (owner only) or "public_to" (allow-list in InvocationTargets).
	// Replaces Visibility as the authorization source; Visibility is kept as a
	// derived legacy field so old clients never see a permission widening.
	PermissionMode string `json:"permission_mode"`
	// InvocationTargets is the allow-list for a public_to agent. Empty for
	// private agents. Only populated on the detail / list / create / update
	// responses that load it; broadcast payloads leave it empty.
	InvocationTargets  []AgentInvocationTargetDTO `json:"invocation_targets"`
	Status             string                     `json:"status"`
	MaxConcurrentTasks int32                      `json:"max_concurrent_tasks"`
	Model              string                     `json:"model"`
	// ThinkingLevel is the runtime-native reasoning/effort token persisted
	// for this agent (empty = use runtime default). The picker is per-runtime
	// per-model; the API never normalizes across providers. See MUL-2339.
	ThinkingLevel string `json:"thinking_level"`
	// ComposioToolkitAllowlist is the subset of Composio toolkit slugs this
	// agent is allowed to mount as MCP at task dispatch — for ANY run that
	// passes the agent's invocation permission, using the agent OWNER's
	// Composio connection (MUL-3963; no longer gated on originator == owner).
	// NULL or empty = no overlay. Like mcp_config, this is
	// owner-only data: the slugs themselves are not secret, but the
	// "this is what {agent owner} is willing to surface" view is — surfacing
	// it cross-account is privacy-confusing UX and would let workspace
	// members infer another member's integration footprint. Redacted to
	// `nil` + `composio_toolkit_allowlist_redacted=true` for non-owners,
	// mirroring the existing mcp_config redaction contract.
	ComposioToolkitAllowlist         []string            `json:"composio_toolkit_allowlist,omitempty"`
	ComposioToolkitAllowlistRedacted bool                `json:"composio_toolkit_allowlist_redacted,omitempty"`
	OwnerID                          *string             `json:"owner_id"`
	Skills                           []AgentSkillSummary `json:"skills"`
	CreatedAt                        string              `json:"created_at"`
	UpdatedAt                        string              `json:"updated_at"`
	ArchivedAt                       *string             `json:"archived_at"`
	ArchivedBy                       *string             `json:"archived_by"`
}

// runtimeConfigGatewayTokenMask is the placeholder the API substitutes for
// any non-empty `runtime_config.gateway.token` (openclaw gateway mode, issue
// #3260). The token is a bearer credential; surfacing the real value through
// GET responses would let anyone with read access to the agent dump the
// gateway secret. The mask is a sentinel — when the UI later PATCHes the
// agent and submits the same mask verbatim under that field, the update
// handler restores the persisted token instead of overwriting it.
const runtimeConfigGatewayTokenMask = "***"

func agentToResponse(a db.Agent) AgentResponse {
	var rc any
	if a.RuntimeConfig != nil {
		json.Unmarshal(a.RuntimeConfig, &rc)
	}
	if rc == nil {
		rc = map[string]any{}
	}
	maskGatewayToken(rc)

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

	// composio_toolkit_allowlist: the column is stored as TEXT[] and arrives
	// here as a []string (sqlc). NULL and `{}` both serialize as nil through
	// the postgres driver — both correctly mean "no toolkits", but the API
	// surface keeps them distinguishable from "owner has not opened the
	// integration yet" only via the trio (slice nil / slice empty / slice
	// non-empty). We hand the slice through verbatim so the redaction +
	// owner-only gate below can decide.
	composioAllowlist := a.ComposioToolkitAllowlist

	return AgentResponse{
		ID:                       uuidToString(a.ID),
		WorkspaceID:              uuidToString(a.WorkspaceID),
		RuntimeID:                uuidToString(a.RuntimeID),
		Name:                     a.Name,
		Description:              a.Description,
		Instructions:             a.Instructions,
		AvatarURL:                textToPtr(a.AvatarUrl),
		RuntimeMode:              a.RuntimeMode,
		RuntimeConfig:            rc,
		CustomArgs:               customArgs,
		McpConfig:                mcpConfig,
		HasCustomEnv:             envKeyCount > 0,
		CustomEnvKeyCount:        envKeyCount,
		Visibility:               a.Visibility,
		PermissionMode:           a.PermissionMode,
		InvocationTargets:        []AgentInvocationTargetDTO{},
		Status:                   a.Status,
		MaxConcurrentTasks:       a.MaxConcurrentTasks,
		Model:                    a.Model.String,
		ThinkingLevel:            a.ThinkingLevel.String,
		ComposioToolkitAllowlist: composioAllowlist,
		OwnerID:                  uuidToPtr(a.OwnerID),
		Skills:                   []AgentSkillSummary{},
		CreatedAt:                timestampToString(a.CreatedAt),
		UpdatedAt:                timestampToString(a.UpdatedAt),
		ArchivedAt:               timestampToPtr(a.ArchivedAt),
		ArchivedBy:               uuidToPtr(a.ArchivedBy),
	}
}

// maskGatewayToken replaces runtime_config.gateway.token with the public
// mask sentinel when a non-empty value is present. No-op for any other
// shape so non-openclaw / non-gateway agents pass through untouched.
func maskGatewayToken(rc any) {
	root, ok := rc.(map[string]any)
	if !ok {
		return
	}
	gw, ok := root["gateway"].(map[string]any)
	if !ok {
		return
	}
	tok, _ := gw["token"].(string)
	if tok == "" {
		return
	}
	gw["token"] = runtimeConfigGatewayTokenMask
}

// preserveMaskedGatewayToken substitutes the previously persisted gateway
// token back into an incoming runtime_config when the request submitted the
// public mask sentinel under `gateway.token`. Without this the next PATCH
// after a GET would round-trip the masked sentinel into the database and
// silently destroy the real secret. The previous value is taken from the
// agent row the handler has just loaded for ownership / scoping checks.
func preserveMaskedGatewayToken(incoming any, persistedRuntimeConfig []byte) {
	root, ok := incoming.(map[string]any)
	if !ok {
		return
	}
	gw, ok := root["gateway"].(map[string]any)
	if !ok {
		return
	}
	tok, _ := gw["token"].(string)
	if tok != runtimeConfigGatewayTokenMask {
		return
	}
	// The incoming token is the mask — fish the real one out of the row.
	var prev struct {
		Gateway struct {
			Token string `json:"token"`
		} `json:"gateway"`
	}
	if len(persistedRuntimeConfig) == 0 {
		// No prior token to keep; the field becomes effectively empty.
		delete(gw, "token")
		return
	}
	if err := json.Unmarshal(persistedRuntimeConfig, &prev); err != nil || prev.Gateway.Token == "" {
		delete(gw, "token")
		return
	}
	gw["token"] = prev.Gateway.Token
}

// RepoData holds repository information included in claim responses so the
// daemon can set up worktrees for each workspace repo.
type RepoData struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Ref         string `json:"ref,omitempty"`
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

// ConnectedAppData keeps the daemon-claim wire field local to handler types
// while sharing the canonical JSON shape with the runtime app metadata package.
type ConnectedAppData = runtimeapps.ConnectedApp

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
	WorkspaceContext   string                `json:"workspace_context,omitempty"`
	ThreadName         string                `json:"thread_name,omitempty"` // semantic title for provider-native session/thread history
	Status             string                `json:"status"`
	Priority           int32                 `json:"priority"`
	DispatchedAt       *string               `json:"dispatched_at"`
	StartedAt          *string               `json:"started_at"`
	CompletedAt        *string               `json:"completed_at"`
	Result             any                   `json:"result"`
	Error              *string               `json:"error"`
	FailureReason      string                `json:"failure_reason,omitempty"` // see TaskService.MaybeRetryFailedTask
	Attempt            int32                 `json:"attempt"`
	MaxAttempts        int32                 `json:"max_attempts"`
	ParentTaskID       *string               `json:"parent_task_id,omitempty"`
	IsLeaderTask       bool                  `json:"is_leader_task,omitempty"`
	Agent              *TaskAgentData        `json:"agent,omitempty"`
	ConnectedApps      []ConnectedAppData    `json:"connected_apps,omitempty"` // daemon-claim only: per-run app capabilities mounted through runtime MCP overlays
	Repos              []RepoData            `json:"repos,omitempty"`
	ProjectID          string                `json:"project_id,omitempty"`          // issue's project, when present
	ProjectTitle       string                `json:"project_title,omitempty"`       // for surfacing in agent context
	ProjectDescription string                `json:"project_description,omitempty"` // durable project-level context injected into the brief
	ProjectResources   []ProjectResourceData `json:"project_resources,omitempty"`   // resources attached to the project
	CreatedAt          string                `json:"created_at"`
	PriorSessionID     string                `json:"prior_session_id,omitempty"` // session ID from a previous task on same issue
	PriorWorkDir       string                `json:"prior_work_dir,omitempty"`   // work_dir from a previous task on same issue
	WorkDir            string                `json:"work_dir,omitempty"`         // local working directory pinned for this task; populated once the daemon reports it
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
	RelativeWorkDir          string                 `json:"relative_work_dir,omitempty"`
	TriggerCommentID         *string                `json:"trigger_comment_id,omitempty"`          // comment that triggered this task
	CoalescedCommentIDs      []string               `json:"coalesced_comment_ids,omitempty"`       // MUL-4195: earlier comments folded into this run when it had not yet started, so a single run still covers every deliberate comment; trigger_comment_id is the newest. Surfaced so the UI can show which comments a run covered. omitempty so old clients ignore it
	CoalescedComments        []CoalescedCommentData `json:"coalesced_comments,omitempty"`          // MUL-4195: full detail (thread_id/author/created_at/content) of the folded comments, so the daemon prompt can address each without assuming they share the triggering thread. omitempty so old clients ignore it
	DeliveredCommentIDs      []string               `json:"delivered_comment_ids"`                 // always present: [] is an authoritative empty receipt, while field absence identifies responses from legacy servers
	TriggerThreadID          string                 `json:"trigger_thread_id,omitempty"`           // root comment ID for the triggering thread
	TriggerCommentContent    string                 `json:"trigger_comment_content,omitempty"`     // content of the triggering comment
	TriggerSummary           *string                `json:"trigger_summary,omitempty"`             // canonical short description snapshot — comment text / autopilot title — taken at task creation; survives source edits/deletes
	TriggerAuthorType        string                 `json:"trigger_author_type,omitempty"`         // "agent" or "member" — author kind of the triggering comment
	TriggerAuthorName        string                 `json:"trigger_author_name,omitempty"`         // display name of the triggering comment author
	NewCommentCount          int                    `json:"new_comment_count,omitempty"`           // trigger-thread comments since last run; excludes injected trigger + own comments; omitempty so old daemons ignore it
	NewCommentsSince         string                 `json:"new_comments_since,omitempty"`          // RFC3339 anchor (last run's started_at) the count is measured from; omitempty so old daemons ignore it
	ChatSessionID            string                 `json:"chat_session_id,omitempty"`             // non-empty for chat tasks
	ChatChannelType          string                 `json:"chat_channel_type,omitempty"`           // "slack" when the chat session is backed by an IM channel; empty for a web-only chat. Makes the agent channel-aware (read history from the channel, not Multica)
	ChatInThread             bool                   `json:"chat_in_thread,omitempty"`              // true when the latest @mention was a thread reply; tells the agent to start with `multica chat thread` vs `multica chat history`
	ChatMessage              string                 `json:"chat_message,omitempty"`                // user message for chat tasks
	ChatMessageAttachments   []ChatAttachmentMeta   `json:"chat_message_attachments,omitempty"`    // attachments on the user message — agent calls `multica attachment download <id>` per entry
	ChatIntro                bool                   `json:"chat_intro,omitempty"`                  // true for the agent's proactive self-introduction chat (is_agent_intro session, no user message); the daemon builds an intro prompt instead of a reply prompt
	AutopilotRunID           string                 `json:"autopilot_run_id,omitempty"`            // non-empty for autopilot-spawned tasks
	AutopilotID              string                 `json:"autopilot_id,omitempty"`                // autopilot that spawned this task
	AutopilotTitle           string                 `json:"autopilot_title,omitempty"`             // autopilot title used as task context
	AutopilotDescription     string                 `json:"autopilot_description,omitempty"`       // autopilot description used as task prompt
	AutopilotSource          string                 `json:"autopilot_source,omitempty"`            // manual, schedule, webhook, or api
	AutopilotTriggerPayload  json.RawMessage        `json:"autopilot_trigger_payload,omitempty"`   // optional trigger payload for webhook/api runs
	QuickCreatePrompt        string                 `json:"quick_create_prompt,omitempty"`         // user's natural-language input for quick-create tasks
	QuickCreatePriority      string                 `json:"quick_create_priority,omitempty"`       // explicit priority selected in quick-create
	QuickCreateDueDate       string                 `json:"quick_create_due_date,omitempty"`       // explicit calendar due date selected in quick-create
	QuickCreateAttachmentIDs []string               `json:"quick_create_attachment_ids,omitempty"` // attachment ids uploaded in the quick-create prompt and bound on issue create
	HandoffNote              string                 `json:"handoff_note,omitempty"`                // assignment handoff instruction; rendered into the run's opening prompt + issue_context.md (omitempty so old daemons ignore it)
	SquadID                  string                 `json:"squad_id,omitempty"`                    // for quick-create tasks where the picker was a squad; Agent is still the resolved leader
	SquadName                string                 `json:"squad_name,omitempty"`                  // display name for the picker squad
	ParentIssueID            string                 `json:"parent_issue_id,omitempty"`             // for quick-create tasks opened from "Add sub issue" — UUID of the parent issue the new issue should be filed under
	ParentIssueIdentifier    string                 `json:"parent_issue_identifier,omitempty"`     // human-readable identifier (e.g. MUL-123) of the quick-create parent issue, resolved on claim for prompt context
	// RequestingUserName + RequestingUserProfileDescription mirror the user
	// the agent is acting on behalf of (see daemon/types.go). v1 sources them
	// from the runtime owner so they're populated for daemon runtimes and
	// empty otherwise. The daemon emits both into the brief under
	// `## Requesting User`; the heading is skipped entirely when description
	// is empty.
	RequestingUserName               string `json:"requesting_user_name,omitempty"`
	RequestingUserProfileDescription string `json:"requesting_user_profile_description,omitempty"`
	// Initiator* identify the actor who triggered THIS task — the real
	// requester behind the current comment/mention or chat message — as
	// distinct from the runtime owner whose credentials the agent runs with.
	// Resolved at claim time: comment-triggered tasks use the triggering
	// comment's author; chat tasks use the chat session creator. Empty for
	// task kinds with no attributable human initiator (on-assign, autopilot,
	// quick-create). InitiatorEmail is set only for member initiators
	// ("member"); agent initiators ("agent") carry a name but no email. The
	// daemon emits these into the brief under `## Task Initiator` so a
	// workspace-visible, multi-user agent can attribute the request and apply
	// per-person privacy / access rules instead of seeing every requester as
	// the owner. The agent's effective Multica credentials stay owner-scoped —
	// this is an attested identity, not a credential. See MUL-2645.
	InitiatorType  string `json:"initiator_type,omitempty"`  // "member" or "agent"
	InitiatorID    string `json:"initiator_id,omitempty"`    // user UUID (member) or agent UUID
	InitiatorName  string `json:"initiator_name,omitempty"`  // display name of the initiator
	InitiatorEmail string `json:"initiator_email,omitempty"` // member email; empty for agent initiators
	Kind           string `json:"kind"`                      // discriminator: "comment" | "autopilot" | "chat" | "quick_create" | "direct" — used by the activity row to label tasks that have no linked issue
	// Attribution is the resolved accountable-human provenance for this run
	// (MUL-4302 §9): the source label + precise flag, the initiator (accountable)
	// and originator refs, the evidence pointer, and lineage. Always present (the
	// pure taskToResponse builds the labels + raw ids); initiator/originator names
	// are hydrated from the global user table only on user-facing surfaces.
	Attribution *TaskAttribution `json:"attribution,omitempty"`
	// AuthToken is the task-scoped `mat_` token the daemon must inject as
	// MULTICA_TOKEN in the agent process environment. The server binds it to
	// this (agent_id, task_id) pair at claim time and treats any request
	// authenticated with it as actor=agent, regardless of headers — so the
	// agent process cannot use it to read another agent's secrets via the
	// env-management endpoint. Claim fails closed when the runtime has no
	// owning user; the daemon must not fall back to its own credential. See
	// MUL-3292.
	AuthToken string `json:"auth_token,omitempty"`
}

// TaskAttribution is the wire shape of a run's accountable-human provenance
// (MUL-4302 §9). Source/Precise/Evidence/lineage come straight from the row (pure);
// Initiator/Originator carry the raw user id always and the display name/email/avatar
// only after hydration on a user-facing surface.
type TaskAttribution struct {
	// Source is the waterfall level that resolved the accountable human:
	// direct_human | delegation | comment_source | rule_owner | owner_fallback |
	// backfill | unattributed. Never blank (a pre-migration NULL renders "unattributed").
	Source string `json:"source"`
	// Precise is false for degraded sources (owner_fallback / backfill / unattributed);
	// the UI marks these distinctly and they count against the coverage metric.
	Precise bool `json:"precise"`
	// Initiator is the accountable human (accountable_user_id). Nil when unattributed.
	Initiator *AttributionUser `json:"initiator,omitempty"`
	// Originator is the authorization human (originator_user_id); nil for autopilot
	// (rule_owner / owner_fallback), where no human authorized the run.
	Originator *AttributionUser `json:"originator,omitempty"`
	// Evidence points at the direct cause of the run so the UI can jump to it.
	Evidence            *TaskEvidence `json:"evidence,omitempty"`
	RuleVersionID       string        `json:"rule_version_id,omitempty"`
	DelegatedFromTaskID string        `json:"delegated_from_task_id,omitempty"`
	RetryOfTaskID       string        `json:"retry_of_task_id,omitempty"`
	RerunOfTaskID       string        `json:"rerun_of_task_id,omitempty"`
}

// AttributionUser is a departed-member-safe user ref, resolved from the global user
// table. Name/Email/AvatarURL are empty until hydrated.
type AttributionUser struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// TaskEvidence is the kind-tagged handle to a run's direct cause (a comment,
// autopilot run, rule version, source task, ...).
type TaskEvidence struct {
	Kind  string `json:"kind"`
	RefID string `json:"ref_id"`
}

// taskAttributionBase builds the pure (no-DB) part of a run's attribution from the
// row: the source label + precise flag, evidence, lineage, and the raw initiator /
// originator user ids. Names are filled later by hydrateTaskAttributions.
func taskAttributionBase(t db.AgentTaskQueue) *TaskAttribution {
	src := attribution.Source(t.OriginatorSource.String)
	attr := &TaskAttribution{
		Source:              src.String(), // empty (pre-migration) → "unattributed"
		Precise:             src.Precise(),
		RuleVersionID:       uuidToString(t.RuleVersionID),
		DelegatedFromTaskID: uuidToString(t.DelegatedFromTaskID),
		RetryOfTaskID:       uuidToString(t.RetryOfTaskID),
		RerunOfTaskID:       uuidToString(t.RerunOfTaskID),
	}
	if t.AccountableUserID.Valid {
		attr.Initiator = &AttributionUser{ID: uuidToString(t.AccountableUserID)}
	}
	if t.OriginatorUserID.Valid {
		attr.Originator = &AttributionUser{ID: uuidToString(t.OriginatorUserID)}
	}
	if t.TriggerEvidenceKind.Valid && t.TriggerEvidenceKind.String != "" {
		attr.Evidence = &TaskEvidence{Kind: t.TriggerEvidenceKind.String, RefID: uuidToString(t.TriggerEvidenceRefID)}
	}
	return attr
}

// hydrateTaskAttributions fills the display name / email / avatar on the initiator
// and originator refs of the given attributions, resolving from the GLOBAL user
// table in one batch (departed-safe, N+1-free). Best-effort: on a lookup failure the
// raw user ids already present are left as-is — names are display sugar, not gating.
func (h *Handler) hydrateTaskAttributions(ctx context.Context, attrs []*TaskAttribution) {
	seen := make(map[string]struct{})
	var ids []pgtype.UUID
	add := func(ref *AttributionUser) {
		if ref == nil || ref.ID == "" {
			return
		}
		if _, ok := seen[ref.ID]; ok {
			return
		}
		if u, err := util.ParseUUID(ref.ID); err == nil {
			seen[ref.ID] = struct{}{}
			ids = append(ids, u)
		}
	}
	for _, a := range attrs {
		if a == nil {
			continue
		}
		add(a.Initiator)
		add(a.Originator)
	}
	if len(ids) == 0 {
		return
	}
	users, err := h.Queries.GetUsersByIDs(ctx, ids)
	if err != nil {
		return
	}
	byID := make(map[string]db.GetUsersByIDsRow, len(users))
	for _, u := range users {
		byID[uuidToString(u.ID)] = u
	}
	fill := func(ref *AttributionUser) {
		if ref == nil {
			return
		}
		if u, ok := byID[ref.ID]; ok {
			ref.Name = u.Name
			ref.Email = u.Email
			if u.AvatarUrl.Valid {
				ref.AvatarURL = u.AvatarUrl.String
			}
		}
	}
	for _, a := range attrs {
		if a == nil {
			continue
		}
		fill(a.Initiator)
		fill(a.Originator)
	}
}

// attributionsOf collects the (non-nil) TaskAttribution pointers from a slice of
// task responses so a list endpoint can hydrate them all in one batch.
func attributionsOf(resps []AgentTaskResponse) []*TaskAttribution {
	out := make([]*TaskAttribution, 0, len(resps))
	for i := range resps {
		if resps[i].Attribution != nil {
			out = append(out, resps[i].Attribution)
		}
	}
	return out
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

// CoalescedCommentData carries the full detail of a comment that was folded
// into a not-yet-started run (MUL-4195) so the daemon can embed it directly in
// the prompt. The earlier merge path only shipped comment IDs plus a
// "they are in the triggering thread" hint, which is WRONG when the folded
// comments span multiple threads (an issue's assignee can be triggered from
// different threads). Shipping thread_id / author / created_at / content lets
// the prompt address each folded comment without assuming a single thread or
// relying on a `--recent N` window that may not cover them all. The mirror
// struct on the daemon side lives in internal/daemon/types.go with the same
// JSON field names.
type CoalescedCommentData struct {
	ID         string `json:"id"`
	ThreadID   string `json:"thread_id,omitempty"`
	AuthorType string `json:"author_type,omitempty"`
	AuthorName string `json:"author_name,omitempty"`
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at,omitempty"`
}

// TaskAgentData holds agent info included in claim responses so the daemon
// can set up the execution environment (branch naming, skill files, instructions).
type TaskAgentData struct {
	ID            string                      `json:"id"`
	Name          string                      `json:"name"`
	Instructions  string                      `json:"instructions"`
	Skills        []service.AgentSkillData    `json:"skills,omitempty"`
	SkillRefs     []service.AgentSkillRefData `json:"skill_refs,omitempty"`
	CustomEnv     map[string]string           `json:"custom_env,omitempty"`
	CustomArgs    []string                    `json:"custom_args,omitempty"`
	McpConfig     json.RawMessage             `json:"mcp_config,omitempty"`
	Model         string                      `json:"model,omitempty"`
	ThinkingLevel string                      `json:"thinking_level,omitempty"`
	// RuntimeConfig is the agent's saved runtime_config JSON as-is. The
	// daemon decodes it per-provider — e.g. the openclaw backend reads
	// `mode` + `gateway.*` to choose between embedded and gateway routing
	// (issue #3260). Other providers ignore the payload entirely. Sent
	// raw so the daemon can evolve its schema without a server roundtrip.
	RuntimeConfig json.RawMessage `json:"runtime_config,omitempty"`
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
	failureReason := ""
	if t.FailureReason.Valid {
		failureReason = t.FailureReason.String
	}
	workDir := ""
	if t.WorkDir.Valid {
		workDir = t.WorkDir.String
	}
	handoffNote := ""
	if t.HandoffNote.Valid {
		handoffNote = t.HandoffNote.String
	}
	return AgentTaskResponse{
		ID:                  uuidToString(t.ID),
		AgentID:             uuidToString(t.AgentID),
		RuntimeID:           uuidToString(t.RuntimeID),
		IssueID:             uuidToString(t.IssueID),
		WorkspaceID:         workspaceID,
		Status:              t.Status,
		Priority:            t.Priority,
		DispatchedAt:        timestampToPtr(t.DispatchedAt),
		StartedAt:           timestampToPtr(t.StartedAt),
		CompletedAt:         timestampToPtr(t.CompletedAt),
		Result:              result,
		Error:               textToPtr(t.Error),
		FailureReason:       failureReason,
		Attempt:             t.Attempt,
		MaxAttempts:         t.MaxAttempts,
		ParentTaskID:        uuidToPtr(t.ParentTaskID),
		IsLeaderTask:        t.IsLeaderTask,
		CreatedAt:           timestampToString(t.CreatedAt),
		TriggerCommentID:    uuidToPtr(t.TriggerCommentID),
		CoalescedCommentIDs: uuidsToStrings(t.CoalescedCommentIds),
		DeliveredCommentIDs: uuidStringsOrEmpty(t.DeliveredCommentIds),
		TriggerSummary:      textToPtr(t.TriggerSummary),
		HandoffNote:         handoffNote,
		WorkDir:             workDir,
		RelativeWorkDir:     relativeWorkDir(workDir, workspaceID, uuidToString(t.ID)),
		// Surface task source so the UI can distinguish issue-linked tasks
		// from chat-spawned or autopilot-spawned ones; all three may arrive
		// with issue_id = "" once a task has no linked issue.
		ChatSessionID:  uuidToString(t.ChatSessionID),
		AutopilotRunID: uuidToString(t.AutopilotRunID),
		Kind:           computeTaskKind(t),
		// Attribution labels + evidence + lineage + raw user ids (pure). Names are
		// hydrated separately on user-facing surfaces (MUL-4302 §9).
		Attribution: taskAttributionBase(t),
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
			Enabled:     row.Enabled,
		})
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

	// Resolve the request actor once. Agents bypass the view gate to preserve
	// A2A collaboration; members see a private agent only when they own it or
	// are workspace owner/admin, and a public_to agent only when on its
	// invocation allow-list. Targets are batch-loaded to avoid an N+1 and
	// reused to enrich each response's invocation_targets.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	targetsByAgent, ok := h.loadInvocationTargetsByAgent(r.Context(), agents)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to load agent invocation targets")
		return
	}
	visible := make([]AgentResponse, 0, len(agents))
	for _, a := range agents {
		targets := targetsByAgent[uuidToString(a.ID)]
		if actorType == "member" {
			if !memberAllowedToViewAgent(a, targets, actorID, member.Role) {
				continue
			}
		}
		resp := agentToResponse(a)
		applyInvocationTargetsToResponse(&resp, targets)
		if skills, ok := skillMap[resp.ID]; ok {
			resp.Skills = skills
		}
		// Agent actors NEVER see mcp_config secrets, even when their host's
		// PAT would normally satisfy the owner/admin role gate. Otherwise an
		// agent running under an owner's daemon could read other agents'
		// MCP configs (which routinely embed third-party API tokens) — the
		// same lateral-movement vector MUL-2600 closed for custom_env.
		if actorType == "agent" || alwaysRedact || !canViewAgentSecrets(a, userID, member.Role) {
			redactMcpConfig(&resp)
		}
		// composio_toolkit_allowlist is owner-only — not because the slugs
		// are secret but because surfacing "what {owner} has opted into"
		// across the workspace leaks the owner's integration footprint and
		// confuses non-owners who cannot actually edit it. Workspace
		// owner/admin do NOT bypass this gate (unlike mcp_config): the overlay
		// uses the OWNER's connection and follows invocation permission
		// (MUL-3963), so surfacing the slugs to admins gives them nothing
		// actionable. Agent actors are also redacted (same A2A
		// lateral-movement reasoning as mcp_config).
		if !h.composioMCPAppsEnabled(r.Context()) {
			suppressComposioToolkitAllowlist(&resp)
		} else if actorType == "agent" || uuidToString(a.OwnerID) != userID {
			redactComposioToolkitAllowlist(&resp)
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
	if !h.enrichAgentResponseWithTargetsHTTP(w, r, &resp, agent.ID) {
		return
	}
	// Use the summary query (no `content` column) — the embedded
	// AgentSkillSummary only needs id/name/description, and reading large
	// SKILL.md bodies just to discard them is the exact regression we fixed
	// in #2174.
	if err := h.attachAgentSkills(r.Context(), &resp, agent.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
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
	// composio_toolkit_allowlist visibility is strictly owner-only (see
	// ListAgents for the rationale). No workspace owner/admin bypass.
	if !h.composioMCPAppsEnabled(r.Context()) {
		suppressComposioToolkitAllowlist(&resp)
	} else if actorType == "agent" || uuidToString(agent.OwnerID) != userID {
		redactComposioToolkitAllowlist(&resp)
	}

	writeJSON(w, http.StatusOK, resp)
}

type CreateAgentRequest struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Instructions  string            `json:"instructions"`
	AvatarURL     *string           `json:"avatar_url"`
	RuntimeID     string            `json:"runtime_id"`
	RuntimeConfig any               `json:"runtime_config"`
	CustomEnv     map[string]string `json:"custom_env"`
	CustomArgs    []string          `json:"custom_args"`
	McpConfig     json.RawMessage   `json:"mcp_config"`
	Visibility    string            `json:"visibility"`
	// PermissionMode + InvocationTargets are the new invocation-permission
	// inputs (MUL-3963). When permission_mode is present it is authoritative
	// and Visibility is ignored; when absent, legacy Visibility is mapped
	// (private -> private, workspace -> public_to+workspace target). On create
	// only the caller can be the owner, so targets are accepted unconditionally.
	PermissionMode     *string                    `json:"permission_mode"`
	InvocationTargets  []AgentInvocationTargetDTO `json:"invocation_targets"`
	MaxConcurrentTasks int32                      `json:"max_concurrent_tasks"`
	Model              string                     `json:"model"`
	ThinkingLevel      string                     `json:"thinking_level"`
	// ComposioToolkitAllowlist seeds the per-task overlay gate (MUL-3869). On
	// create only the calling user can be the owner, so we accept the field
	// unconditionally here; the cross-owner permission gate lives on PUT.
	// Nil = leave column NULL (no overlay). Empty slice = explicit `{}` (no
	// overlay either, but the column reads as "configured" — distinct from
	// "owner has never opened the integration").
	ComposioToolkitAllowlist []string `json:"composio_toolkit_allowlist"`
	// Template records which template slug was used to seed this agent
	// (e.g. "coding" / "planning" / "writing" / "assistant"). Empty when
	// the caller didn't come from a template picker — the `agent_created`
	// event still fires with `template=""`, which is the correct signal
	// for "manually authored agent".
	Template string `json:"template"`
	// SkillIDs are attached inside the same transaction as the agent row so a
	// create never becomes visible in a partially configured state.
	SkillIDs []string `json:"skill_ids"`
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

	// Resolve invocation permission (MUL-3963). permission_mode is
	// authoritative when present; otherwise the legacy visibility value is
	// mapped. On create the caller is always the owner, so targets are
	// accepted unconditionally.
	_, hasTargets := rawFields["invocation_targets"]
	legacyVis := req.Visibility
	perm, _, permErr := parsePermissionInput(wsUUID, req.PermissionMode, req.InvocationTargets, req.PermissionMode != nil, hasTargets, &legacyVis)
	if permErr != nil {
		writeError(w, http.StatusBadRequest, permErr.Error())
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

	// thinking_level validation: fixed-enum providers reject unknown literals;
	// dynamic-catalog providers (Codex/OpenCode) reject malformed tokens here.
	// Per-model gaps are enforced by the daemon at execution time (MUL-2339):
	// combination-invalid values are logged and omitted from the invocation.
	if !agent.IsKnownThinkingValue(runtime.Provider, req.ThinkingLevel) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("thinking_level %q is not a recognised value for runtime %q", req.ThinkingLevel, runtime.Provider))
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

	// A create has no prior token to restore, so if the caller submitted the
	// public mask sentinel as gateway.token (e.g. replayed a masked GET body)
	// drop it rather than persisting a literal "***" as a real bearer token.
	preserveMaskedGatewayToken(req.RuntimeConfig, nil)
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

	// composio_toolkit_allowlist: the JSON field is a list-of-slugs that gets
	// stored as TEXT[]. We normalise here (lowercase + trim + dedupe) so the
	// dispatch path can compare against per-user connection rows with a
	// straight equality. A nil slice (or absent JSON key) maps to a NULL
	// column on insert. An explicitly empty list (`[]`) is preserved as an
	// empty TEXT[] (the dispatch path treats NULL and `{}` identically).
	allowlist := normaliseComposioToolkitAllowlist(req.ComposioToolkitAllowlist)
	if !h.composioMCPAppsEnabled(r.Context()) {
		allowlist = nil
	}

	skillUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.SkillIDs, "skill_ids")
	if !ok {
		return
	}
	for _, skillID := range skillUUIDs {
		if _, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
			ID:          skillID,
			WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "skill does not belong to this workspace")
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start agent create transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	created, err := qtx.CreateAgent(r.Context(), db.CreateAgentParams{
		WorkspaceID:              wsUUID,
		Name:                     req.Name,
		Description:              req.Description,
		Instructions:             req.Instructions,
		AvatarUrl:                ptrToText(req.AvatarURL),
		RuntimeMode:              runtime.RuntimeMode,
		RuntimeConfig:            rc,
		RuntimeID:                runtime.ID,
		Visibility:               perm.legacyVisibility(),
		PermissionMode:           perm.mode,
		MaxConcurrentTasks:       req.MaxConcurrentTasks,
		OwnerID:                  parseUUID(ownerID),
		CustomEnv:                ce,
		CustomArgs:               ca,
		McpConfig:                mc,
		Model:                    pgtype.Text{String: req.Model, Valid: req.Model != ""},
		ThinkingLevel:            pgtype.Text{String: req.ThinkingLevel, Valid: req.ThinkingLevel != ""},
		ComposioToolkitAllowlist: allowlist,
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
	if err := replaceInvocationTargetsWithQueries(r.Context(), qtx, created.ID, parseUUID(ownerID), perm.targets); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save agent access")
		return
	}
	for _, skillID := range skillUUIDs {
		if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
			AgentID: created.ID,
			SkillID: skillID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to attach agent skill")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit agent create")
		return
	}
	slog.Info("agent created", append(logger.RequestAttrs(r), "agent_id", uuidToString(created.ID), "name", created.Name, "workspace_id", workspaceID)...)

	if runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(r.Context(), created.ID)
		created, _ = h.Queries.GetAgent(r.Context(), created.ID)
	}

	resp := agentToResponse(created)
	if err := h.attachAgentSkills(r.Context(), &resp, created.ID); err != nil {
		slog.Warn("create agent: load skills for response failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(created.ID))...)
	}
	if err := h.enrichAgentResponseWithTargets(r.Context(), &resp, created.ID); err != nil {
		slog.Warn("create agent: load invocation targets for response failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(created.ID))...)
	}
	actorType, actorID := h.resolveActor(r, ownerID, workspaceID)
	h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})

	// Start the existing proactive introduction only after the complete Agent
	// configuration has committed, so the first run sees its skills and access.
	h.sendAgentWelcomeChat(r.Context(), created, ownerID, workspaceID)

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
	if !h.composioMCPAppsEnabled(r.Context()) {
		suppressComposioToolkitAllowlist(&resp)
	}
	writeJSON(w, http.StatusCreated, resp)
}

// sendAgentWelcomeChat creates a "meet your new agent" chat: a session owned by
// the agent's creator, flagged is_agent_intro, then enqueues a real agent run so
// the agent introduces itself — the intro is LLM-generated by the agent, not a
// static template. No user message is persisted: the intro run is driven
// server-side (the daemon builds a self-introduction prompt for is_agent_intro
// sessions, see buildChatPrompt) so the thread reads as the agent proactively
// messaging its creator, not the creator prompting the agent (MUL-4230). Best
// effort: any failure is logged and never blocks the (already-committed) agent
// creation.
func (h *Handler) sendAgentWelcomeChat(ctx context.Context, agent db.Agent, creatorID, workspaceID string) {
	if !agent.RuntimeID.Valid {
		return // no runtime → the agent can't run; skip the welcome
	}
	// Create inside a tx that first takes FOR KEY SHARE on the workspace row — the
	// creator half of the #5219 delete/create protocol, so the intro session cannot
	// be created into a workspace mid-delete (see LockWorkspaceForChatSessionCreate).
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("agent welcome: begin tx failed", "agent_id", uuidToString(agent.ID), "error", err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, parseUUID(workspaceID)); err != nil {
		slog.Warn("agent welcome: lock workspace failed", "agent_id", uuidToString(agent.ID), "error", err)
		return
	}
	session, err := qtx.CreateChatSession(ctx, db.CreateChatSessionParams{
		WorkspaceID:  parseUUID(workspaceID),
		AgentID:      agent.ID,
		CreatorID:    parseUUID(creatorID),
		Title:        "👋 " + agent.Name,
		IsAgentIntro: true,
	})
	if err != nil {
		slog.Warn("agent welcome: create session failed", "agent_id", uuidToString(agent.ID), "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("agent welcome: commit session failed", "agent_id", uuidToString(agent.ID), "error", err)
		return
	}

	if _, err := h.TaskService.EnqueueChatTask(ctx, session, parseUUID(creatorID), false); err != nil {
		slog.Warn("agent welcome: enqueue task failed", "chat_session_id", uuidToString(session.ID), "error", err)
	}
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
	CustomArgs *[]string        `json:"custom_args"`
	McpConfig  *json.RawMessage `json:"mcp_config"`
	Visibility *string          `json:"visibility"`
	// PermissionMode + InvocationTargets are the invocation-permission inputs
	// (MUL-3963). Owner-only writes (like composio_toolkit_allowlist): a
	// non-owner admin passing them is silently ignored, because the invoke
	// gate is owner/allow-list based and an admin-authored allow-list would
	// confuse the owner about who can run their agent. permission_mode is
	// authoritative when present; otherwise legacy visibility is mapped.
	PermissionMode     *string                     `json:"permission_mode"`
	InvocationTargets  *[]AgentInvocationTargetDTO `json:"invocation_targets"`
	Status             *string                     `json:"status"`
	MaxConcurrentTasks *int32                      `json:"max_concurrent_tasks"`
	Model              *string                     `json:"model"`
	// ThinkingLevel is treated as a tri-state per-MUL-2339:
	//   - field omitted → no change (leave existing value alone)
	//   - field present with "" → explicit clear (use runtime default)
	//   - field present with non-empty value → set (validated server-side)
	// Distinguishing those modes is why this is a pointer; the raw-fields
	// map captured at decode time tells us whether the key was sent.
	ThinkingLevel *string `json:"thinking_level"`
	// ComposioToolkitAllowlist is a tri-state, same pattern as
	// thinking_level, mcp_config:
	//   - field omitted → no change (column preserved as-is)
	//   - field present with null → explicit clear (ClearAgent... query)
	//   - field present with [] → store empty TEXT[] (configured, no toolkits)
	//   - field present with non-empty → store deduped lowercase slugs
	// The decode-time raw fields map disambiguates "omitted" from "explicit
	// null" (a *[]string can't, because a nil pointer is the same wire
	// representation as both). MUL-3869.
	ComposioToolkitAllowlist *[]string `json:"composio_toolkit_allowlist"`
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

// canViewAgentSecrets checks whether the requesting user is allowed to
// see the agent's secret-bearing fields (currently `mcp_config`). Only
// the agent owner or workspace owner/admin qualify; for everyone else
// the response is redacted. `custom_env` is no longer part of an agent
// resource response (see MUL-2600), so this predicate is shared only by
// the remaining mcp_config redaction path.
func canViewAgentSecrets(agent db.Agent, userID string, memberRole string) bool {
	if roleAllowed(memberRole, "owner", "admin") {
		return true
	}
	return uuidToString(agent.OwnerID) == userID
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
//
// composio_toolkit_allowlist follows the same fan-out rule: every
// workspace member subscribes to agent:created/updated/archived, so
// a non-redacted broadcast would leak the agent owner's per-toolkit
// allowlist to every member regardless of whether they would have
// been allowed to read it via GET. Redact unconditionally on the
// broadcast copy.
func broadcastAgentResponse(resp AgentResponse) AgentResponse {
	out := resp
	redactMcpConfig(&out)
	redactComposioToolkitAllowlist(&out)
	// Belt-and-suspenders: agentToResponse already masks gateway.token on
	// every read, so by the time a response reaches this broadcast helper
	// the field is already "***". Re-mask anyway so a future refactor that
	// bypasses agentToResponse (e.g. constructing AgentResponse from raw
	// db.Agent in a new handler) cannot silently leak the token to every
	// WebSocket subscriber on the workspace, agent processes included.
	maskGatewayToken(out.RuntimeConfig)
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

// redactComposioToolkitAllowlist removes the composio_toolkit_allowlist
// value from the response when the caller is not the agent owner. The slug
// list itself is not secret, but the "what {agent owner} has opted into"
// view leaks the owner's integration footprint across the workspace, which
// is the same privacy concern that gates mcp_config visibility behind
// owner-only canViewAgentSecrets. We surface a coarse `_redacted` flag so
// the front-end can render "Configured" without the contents (parity with
// mcp_config_redacted). The clearing matters: the JSON `omitempty` only
// drops nil slices, so reset to nil rather than `[]string{}`.
func redactComposioToolkitAllowlist(resp *AgentResponse) {
	if resp.ComposioToolkitAllowlist != nil {
		resp.ComposioToolkitAllowlist = nil
		resp.ComposioToolkitAllowlistRedacted = true
	}
}

func suppressComposioToolkitAllowlist(resp *AgentResponse) {
	resp.ComposioToolkitAllowlist = nil
	resp.ComposioToolkitAllowlistRedacted = false
}

// normaliseComposioToolkitAllowlist canonicalises an incoming allowlist
// payload before persisting. Each slug is trimmed + lowercased so the
// dispatch path (which compares against user_composio_connection.toolkit_slug,
// stored lowercased by the Composio service) does a flat string match
// without needing a CITEXT or per-query LOWER(). Empty / whitespace-only
// strings are dropped and duplicates collapsed so a sloppy UI payload
// can't waste DB row-length or surface twice in the response.
//
// Contract:
//   - nil in → nil out: "field absent / explicit null" preserved. Combined
//     with sqlc.narg('composio_toolkit_allowlist')::text[] in UpdateAgent,
//     this is what makes "omit field" mean "leave column alone".
//   - empty slice in → empty slice out: "owner cleared all toolkits".
//     Distinct from nil only at the column-NULL level; the dispatch path
//     treats both identically as "no overlay".
//   - non-empty in → trimmed, lowercased, deduped, stable order.
func normaliseComposioToolkitAllowlist(in []string) []string {
	if in == nil {
		return nil
	}
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// redactAgentResponseForActor strips secret-bearing fields from an agent
// resource HTTP response when the request actor is an agent. Read
// handlers already gate on actorType — mutation handlers
// (create/update/archive/restore) must apply the same rule, otherwise
// an agent with a host owner/admin token can do an unrelated mutation
// (e.g. flip max_concurrent_tasks) on a target agent and harvest the
// target's mcp_config from the mutation response. MUL-2600.
//
// composio_toolkit_allowlist is redacted under the same logic: an agent
// runs with its host owner's PAT, so a mutation against a sibling agent
// could otherwise return the sibling owner's allowlist in the response.
func redactAgentResponseForActor(resp *AgentResponse, actorType string) {
	if actorType == "agent" {
		redactMcpConfig(resp)
		redactComposioToolkitAllowlist(resp)
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
		// Restore the persisted gateway token when the request submitted the
		// public mask sentinel. Without this, a UI that GETs the agent and
		// PATCHes the same payload back round-trips "***" into the database
		// and silently destroys the real secret (issue #3260).
		preserveMaskedGatewayToken(req.RuntimeConfig, existing.RuntimeConfig)
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
	targetProvider := ""
	if req.RuntimeID != nil {
		runtimeUUID, ok := parseUUIDOrBadRequest(w, *req.RuntimeID, "runtime_id")
		if !ok {
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
		targetProvider = runtime.Provider
	}
	// Invocation permission (MUL-3963). OWNER-ONLY write: access is the one
	// agent property a workspace admin may NOT change (only the owner decides
	// who can run their agent — the overlay uses the owner's own Composio
	// connection, so admin-authored access would be confusing and unsafe).
	//
	// Non-owner behaviour: a *real* change is rejected with 403 so the contract
	// is explicit and matches the owner-only UI (the picker is read-only for
	// non-owners). A no-op resubmit — an admin editing OTHER fields via a
	// PATCH-as-PUT client that echoes the unchanged permission back — is
	// tolerated (dropped) so it doesn't break legitimate admin edits.
	_, hasPermissionMode := rawFields["permission_mode"]
	_, hasTargets := rawFields["invocation_targets"]
	permissionTouched := hasPermissionMode || hasTargets || req.Visibility != nil
	replacePermissionTargets := false
	var resolvedPerm resolvedPermission
	if permissionTouched {
		isAgentOwner := uuidToString(existing.OwnerID) == requestUserID(r)
		if !isAgentOwner {
			changed, permErr := h.permissionInputChangesAgent(r.Context(), existing, req, hasPermissionMode, hasTargets)
			if permErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to evaluate invocation permission change")
				return
			}
			if changed {
				writeError(w, http.StatusForbidden, "only the agent owner can change access (permission_mode / invocation_targets)")
				return
			}
			slog.Debug("update agent: non-owner permission fields matched current state; ignored",
				append(logger.RequestAttrs(r), "agent_id", id)...)
		} else {
			var targetsDTO []AgentInvocationTargetDTO
			if req.InvocationTargets != nil {
				targetsDTO = *req.InvocationTargets
			}
			perm, _, permErr := parsePermissionInput(existing.WorkspaceID, req.PermissionMode, targetsDTO, hasPermissionMode, hasTargets, req.Visibility)
			if permErr != nil {
				writeError(w, http.StatusBadRequest, permErr.Error())
				return
			}
			resolvedPerm = perm
			replacePermissionTargets = true
			params.PermissionMode = pgtype.Text{String: perm.mode, Valid: true}
			params.Visibility = pgtype.Text{String: perm.legacyVisibility(), Valid: true}
		}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.MaxConcurrentTasks != nil {
		params.MaxConcurrentTasks = pgtype.Int4{Int32: *req.MaxConcurrentTasks, Valid: true}
	}
	if req.Model != nil {
		params.Model = pgtype.Text{String: *req.Model, Valid: true}
	} else if req.RuntimeID != nil && existing.Model.Valid && agent.ModelKnownIncompatibleWithProvider(targetProvider, existing.Model.String) {
		// Model is runtime-native. When moving an agent across known provider
		// families and the caller did not choose a replacement model, clear the
		// old value so the new runtime falls back to its own default instead of
		// receiving an obvious foreign model ID (e.g. Claude Code -> Codex).
		// Unknown/custom model strings are preserved by the helper.
		params.Model = pgtype.Text{String: "", Valid: true}
	}

	// thinking_level handling (MUL-2339). Tri-state semantics:
	//   - field omitted  → leave column alone (COALESCE narg), but if a
	//     runtime change in this same request would make the *existing*
	//     value invalid for the new provider's fixed enum or token syntax,
	//     reject 400. Exact dynamic-catalog compatibility is daemon-owned.
	//   - field set to "" → explicit clear (run ClearAgentThinkingLevel post-update)
	//   - field set to value → validate against the target runtime's fixed enum
	//     or dynamic-token syntax; reject literal-invalid with 400. Per-model
	//     combination checks run in the daemon at execution time, not here.
	shouldClearThinkingLevel := false
	if req.ThinkingLevel != nil {
		value := *req.ThinkingLevel
		if value == "" {
			shouldClearThinkingLevel = true
		} else {
			// Need the target runtime's provider to validate. Re-fetch only when
			// we haven't already loaded it above (i.e. the request didn't change
			// runtime_id), to keep the no-change path one DB roundtrip.
			provider := targetProvider
			if provider == "" {
				var ok bool
				provider, ok = h.resolveAgentProvider(r, existing.WorkspaceID, targetRuntimeID)
				if !ok {
					writeError(w, http.StatusInternalServerError, "failed to resolve runtime for thinking_level validation")
					return
				}
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
		provider := targetProvider
		if provider == "" {
			var ok bool
			provider, ok = h.resolveAgentProvider(r, existing.WorkspaceID, targetRuntimeID)
			if !ok {
				writeError(w, http.StatusInternalServerError, "failed to resolve runtime for thinking_level validation")
				return
			}
		}
		if !agent.IsKnownThinkingValue(provider, existing.ThinkingLevel.String) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"existing thinking_level %q is not valid for runtime %q; pass thinking_level=\"\" to clear or set a value valid for the new runtime",
				existing.ThinkingLevel.String, provider,
			))
			return
		}
	}

	// composio_toolkit_allowlist handling (MUL-3869). Tri-state semantics
	// mirror thinking_level (see above): omitted → no change, null →
	// ClearAgentComposioToolkitAllowlist, slice → wholesale replace.
	//
	// Owner-only WRITE. The caller is already past canManageAgent, which lets
	// workspace owner/admins through alongside the agent owner — but the
	// Composio overlay uses the agent OWNER's connection (MUL-3963), so an
	// admin editing someone else's allowlist would silently reshape what the
	// OWNER exposes through their own connected apps, confusing the owner
	// about what their agent surfaces. Keep it owner-only.
	// Drop the field with a debug log instead of erroring so an over-eager
	// UI that sends the whole agent payload back on every save (PATCH-as-PUT)
	// keeps working — same "silent ignore" stance the issue calls out, and
	// the same one mcp_config takes for the broader admin pattern.
	shouldClearComposioAllowlist := false
	if _, hasAllowlist := rawFields["composio_toolkit_allowlist"]; hasAllowlist {
		isAgentOwner := uuidToString(existing.OwnerID) == requestUserID(r)
		if !h.composioMCPAppsEnabled(r.Context()) {
			slog.Debug("update agent: composio_toolkit_allowlist write dropped because feature flag is disabled",
				append(logger.RequestAttrs(r), "agent_id", id)...)
		} else if !isAgentOwner {
			slog.Debug("update agent: composio_toolkit_allowlist write by non-owner silently dropped",
				append(logger.RequestAttrs(r), "agent_id", id)...)
		} else if req.ComposioToolkitAllowlist == nil {
			// JSON null → explicit clear via the dedicated query.
			shouldClearComposioAllowlist = true
		} else {
			// Normalise (trim/lowercase/dedupe). Empty slice is preserved as
			// an empty TEXT[] so the persisted value distinguishes "owner
			// cleared every toolkit" from "owner has never opened the
			// integration" (the dispatch path treats both as "no overlay"
			// either way, but the column tells UX whether to show a primed
			// vs empty picker).
			params.ComposioToolkitAllowlist = normaliseComposioToolkitAllowlist(*req.ComposioToolkitAllowlist)
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
	if shouldClearComposioAllowlist {
		updated, err = h.Queries.ClearAgentComposioToolkitAllowlist(r.Context(), updated.ID)
		if err != nil {
			slog.Warn("clear agent composio_toolkit_allowlist failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to clear composio_toolkit_allowlist: "+err.Error())
			return
		}
	}

	// Invocation targets (MUL-3963): replace wholesale when the owner touched
	// permission. Done after the row update so a permission_mode flip and its
	// targets land together.
	if replacePermissionTargets {
		if err := h.replaceInvocationTargets(r.Context(), updated.ID, parseUUID(requestUserID(r)), resolvedPerm.targets); err != nil {
			slog.Warn("update agent: persist invocation targets failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to update invocation targets: "+err.Error())
			return
		}
	}

	resp := agentToResponse(updated)
	if err := h.enrichAgentResponseWithTargets(r.Context(), &resp, updated.ID); err != nil {
		slog.Warn("update agent: load invocation targets for response failed", append(logger.RequestAttrs(r), "error", err, "agent_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to load agent invocation targets")
		return
	}
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
	// Workspace admins / non-owner members pass canManageAgent for legitimate
	// admin actions (e.g. bulk reassigning agents off a leaving member's
	// runtime), but they must not learn the agent owner's composio allowlist
	// from the mutation response. See ListAgents/GetAgent for the same gate.
	if !h.composioMCPAppsEnabled(r.Context()) {
		suppressComposioToolkitAllowlist(&resp)
	} else if uuidToString(updated.OwnerID) != userID {
		redactComposioToolkitAllowlist(&resp)
	}
	writeJSON(w, http.StatusOK, resp)
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
			Enabled:     s.Enabled,
		}
	}
	resp.Skills = out
	return nil
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
	resp := agentToResponse(restored)
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
	h.hydrateTaskAttributions(r.Context(), attributionsOf(resp))

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
		resp = append(resp, taskToResponse(t, workspaceID))
	}
	h.hydrateTaskAttributions(r.Context(), attributionsOf(resp))

	writeJSON(w, http.StatusOK, resp)
}
