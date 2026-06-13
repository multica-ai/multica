// Package contextguard implements the MUL-4059 "no-context runtime safeguard".
//
// Before enqueuing an agent task, callers consult HasUsableContext to learn
// whether the destination agent has enough execution context to actually
// run. "Usable" means one of:
//   - (A) a linked GitHub repo on the workspace (workspace.repos non-empty)
//   - (B) a project_resource of type 'local_directory' on the agent's
//     project whose path validates
//
// Workspace.context (a free-form system prompt) is intentionally NOT
// sufficient on its own — it's how an agent carries background knowledge,
// not how an agent invokes tools. A chat-only session that has zero repos
// and zero project directories is not "runnable", it's "answerable".
//
// The caller then translates the verdict into a Policy action. The
// default policy is block_and_notify (see PolicyDefault below); self-host
// operators may downgrade to warn or off via workspace.settings or
// AGENT_CONTEXT_GUARD_DEFAULT_POLICY.
package contextguard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Policy is the strategy applied when a task fails the context guard. The
// four values map 1:1 to the four operational profiles documented in the
// MUL-4059 design. Default is block_and_notify — see policyDefault
// constant and AGENT_CONTEXT_GUARD_DEFAULT_POLICY env override.
//
//   - reject: hard-fail the enqueue with ErrContextMissing; no task row is
//     created. Used by API/CLI surfaces that explicitly opt into strict
//     validation (rare).
//   - block_and_notify (DEFAULT): create the task in 'pending_context'
//     status, flip the issue to 'blocked', publish a system event so the
//     UI surfaces a "link a repo to run this" banner. The sweeper
//     revalidates when context is added; if it stays missing for the
//     retry budget the task is cancelled.
//   - warn: enqueue normally and let the daemon claim path run. We still
//     log a warning so operators can see the misconfiguration, but no
//     user-facing state flips. This is the recommended shadow-mode policy
//     during rollout (Stage 1 in the MUL-4059 rollout plan).
//   - off: skip the guard entirely. Used by self-host operators who run
//     their own explicit safety rails, or to disable the feature during
//     incident response.
type Policy string

const (
	PolicyReject         Policy = "reject"
	PolicyBlockAndNotify Policy = "block_and_notify"
	PolicyWarn           Policy = "warn"
	PolicyOff            Policy = "off"

	// PolicyDefault is the value used when no workspace-level override is
	// configured. Picked to match the MUL-4059 GATE 1 decision: default
	// behaviour is the one that surfaces the misconfiguration rather than
	// letting an agent silently spin for 20 minutes before the inactivity
	// timeout fires.
	PolicyDefault = PolicyBlockAndNotify
)

// ErrContextMissing is returned by the service when the policy is
// reject and the guard verdict was negative. Callers map it to a
// 422 response — UI surfaces a "this agent has no execution context"
// message.
var ErrContextMissing = errors.New("agent task context guard: no usable execution context")

// Reason captures the (A) repos and (B) project resources the guard saw
// at decision time. Stored on agent_task_queue.context_guard JSONB so a
// follow-up read can answer "why was this task parked?" without
// re-running the guard. Also embedded in the system comment posted on
// the issue so users can see what they need to add.
//
// JSON tags are camelCase because the front-end reads them straight
// from the WebSocket payload.
type Reason struct {
	Policy             Policy  `json:"policy"`
	OK                 bool    `json:"ok"`
	WorkspaceID        string  `json:"workspace_id"`
	ProjectID          string  `json:"project_id,omitempty"`
	HasWorkspaceRepos  bool    `json:"has_workspace_repos"`
	ProjectResources   []string `json:"project_resources,omitempty"` // resource_type entries
	HasLocalDirectory  bool    `json:"has_local_directory"`
	CheckedAt          string  `json:"checked_at"` // RFC3339Nano
	Hint               string  `json:"hint,omitempty"`
}

// QueryRunner is the minimal DB surface we need. Both *db.Queries and
// the TxStarter-bound *db.Queries satisfy this; production callers pass
// the former, transactionally-wrapped callers (sweepPendingContextTasks)
// pass the latter via TxStarter.
type QueryRunner interface {
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	ListProjectResources(ctx context.Context, projectID pgtype.UUID) ([]db.ProjectResource, error)
}

// Service consults the guard. A zero-value Service is safe to use —
// policies default to block_and_notify and Config may be nil if the
// caller does not pass one.
type Service struct {
	Queries QueryRunner
	// Defaults is consulted for the workspace/instance default policy
	// when workspace.settings.context_guard_policy is unset. May be nil
	// — the guard falls back to PolicyDefault.
	Defaults Defaults
}

// Defaults holds the server-side default for the guard policy and the
// inactivity timeout. Loaded once at startup from environment / flag
// and passed in here. The values live on ServerConfig (see
// serverconfig package) and are immutable for the life of a request.
type Defaults struct {
	Policy Policy
}

// NewService is a thin constructor so callers don't accidentally swap
// QueryRunner and Defaults (the order would still type-check but the
// intent gets lost).
func NewService(q QueryRunner, def Defaults) *Service {
	return &Service{Queries: q, Defaults: def}
}

// ResolvePolicy reads the workspace.settings.context_guard_policy
// value (if any) and falls back through Defaults / PolicyDefault.
// Pure function — kept here rather than inlined so the policy chain
// is testable without DB fixtures.
func (s *Service) ResolvePolicy(workspace db.Workspace) Policy {
	if p := readPolicyFromSettings(workspace.Settings); p != "" {
		return p
	}
	if s.Defaults.Policy != "" {
		return s.Defaults.Policy
	}
	return PolicyDefault
}

// readPolicyFromSettings extracts context_guard_policy from the
// workspace.settings JSONB blob. Returns "" if the key is missing or
// the value is not a string. Returns the canonical Policy value when
// the user-supplied string is one of the four known policies, else ""
// (i.e. fall through to the next default).
//
// We do NOT silently coerce unknown strings to a fallback; a typo
// "block_and_noitfy" deserves to be flagged, not absorbed. The
// slog.Warn below is the operator's breadcrumb.
func readPolicyFromSettings(raw []byte) Policy {
	if len(raw) == 0 {
		return ""
	}
	var settings struct {
		ContextGuardPolicy string `json:"context_guard_policy"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return ""
	}
	candidate := strings.ToLower(strings.TrimSpace(settings.ContextGuardPolicy))
	switch Policy(candidate) {
	case PolicyReject, PolicyBlockAndNotify, PolicyWarn, PolicyOff:
		return Policy(candidate)
	default:
		if candidate != "" {
			slog.Warn("context guard: unknown policy in workspace.settings, ignoring",
				"value", candidate)
		}
		return ""
	}
}

// HasUsableContext is the single source of truth for "does this agent
// have enough execution context to run?". Walks the (A) workspace repos
// and (B) project local_directory checks, returning a Reason that the
// caller stores on the task row (audit trail) and surfaces to the UI
// (human-readable hint).
//
// Pass projectID as an invalid pgtype.UUID (the zero value) for chat /
// autopilot / quick_create-without-project cases. The guard then
// short-circuits to the (A)-only check; project-scoped tasks evaluate
// both rules.
//
// workspaceID MUST be valid; the function returns Reason{OK: false}
// with the workspace ID echoed back if the lookup fails. The caller
// is expected to log the underlying error before proceeding (we
// don't propagate it because the guard decision is the same either
// way: there's no context).
func (s *Service) HasUsableContext(ctx context.Context, workspaceID, projectID pgtype.UUID) (Reason, error) {
	reason := Reason{
		WorkspaceID: util.UUIDToString(workspaceID),
		ProjectID:   util.UUIDToString(projectID),
	}
	if !workspaceID.Valid {
		reason.OK = false
		reason.Hint = "workspace is missing"
		return reason, fmt.Errorf("context guard: workspace_id invalid")
	}

	workspace, err := s.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		reason.OK = false
		reason.Hint = "workspace not found"
		return reason, fmt.Errorf("context guard: load workspace: %w", err)
	}
	reason.HasWorkspaceRepos = workspaceHasRepos(workspace)

	// (B) project local_directory check is gated on a valid project_id.
	// Chat / autopilot / quick_create without a project skip this rule
	// and rely on (A) alone. The hint differentiates the two paths so
	// the system comment can tell the user "add a repo" vs "link a
	// project local directory".
	if projectID.Valid {
		resources, err := s.Queries.ListProjectResources(ctx, projectID)
		if err != nil {
			// ListProjectResources failing is treated the same as an empty
			// list: we still answer "no local directory" rather than
			// failing the whole enqueue. The sweep tick will retry and
			// surface a real error then.
			slog.Warn("context guard: list project resources failed",
				"workspace_id", util.UUIDToString(workspaceID),
				"project_id", util.UUIDToString(projectID),
				"error", err)
		} else {
			for _, r := range resources {
				reason.ProjectResources = append(reason.ProjectResources, r.ResourceType)
				if r.ResourceType == "local_directory" {
					reason.HasLocalDirectory = true
				}
			}
		}
	}

	reason.OK = reason.HasWorkspaceRepos || reason.HasLocalDirectory
	switch {
	case reason.OK:
		reason.Hint = "context guard passed"
	case !reason.HasWorkspaceRepos && reason.HasLocalDirectory:
		reason.Hint = "workspace has no linked repo; project provides a local directory"
	case reason.HasWorkspaceRepos && !reason.HasLocalDirectory:
		reason.Hint = "workspace repos cover execution context"
	case !reason.HasWorkspaceRepos && !reason.HasLocalDirectory && projectID.Valid:
		reason.Hint = "link a GitHub repo on the workspace or add a local_directory resource to this project"
	case !reason.HasWorkspaceRepos && !projectID.Valid:
		reason.Hint = "link a GitHub repo on this workspace so the agent has somewhere to run"
	}
	return reason, nil
}

// workspaceHasRepos interprets the workspace.repos JSONB column. Empty
// or missing JSON is treated as "no repos" (no error). A non-array
// JSON value is treated as malformed and rejected; we surface a
// warning so operators see the schema drift but still answer the
// guard question so the enqueue can proceed.
func workspaceHasRepos(w db.Workspace) bool {
	if len(w.Repos) == 0 {
		return false
	}
	// Trim a "[]" case explicitly — JSON unmarshal into a typed slice
	// distinguishes empty from absent, but the column default is '[]'
	// so a never-touched workspace lands here as exactly that.
	var probe []any
	if err := json.Unmarshal(w.Repos, &probe); err != nil {
		slog.Warn("context guard: workspace.repos is not a JSON array",
			"workspace_id", util.UUIDToString(w.ID),
			"error", err)
		return false
	}
	return len(probe) > 0
}

// EncodeReason serialises a Reason to JSONB bytes for the
// agent_task_queue.context_guard column. Kept as a free function
// (rather than a Reason method) so the package can be imported by
// both server/service and server/handler without circular deps.
//
// P2-11 review fix: the previous version had a vestigial `if
// r.CheckedAt == "" {}` block that did nothing — a TODO that was
// never followed up on. The unused guard was deleted; callers
// that need a timestamp stamp it on the Reason struct before
// calling this helper.
func EncodeReason(r Reason) ([]byte, error) {
	return json.Marshal(r)
}