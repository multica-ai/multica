// Package platformcatalog is the complete, code-owned catalog of Multica
// platform actions (FIR-2594 phase 1). The runtime tool catalog comes from the
// capability register (cerebro_capability) — runtimes report the CLI/MCP tools
// they expose. But the platform's own mutating operations and hardcoded policy
// checks (create an issue, trigger someone else's agent, manage
// roles/groups/credentials, …) are not reported by any runtime, so they never
// appear in the permission table the admin screen renders, and an admin cannot
// set Allow/Ask/Deny on them.
//
// This package closes that gap. It is the source of truth for the platform
// capabilities the tool-policy table surfaces alongside the reported tools, so
// every action an agent or user can take — in a runtime AND in Multica itself —
// lives in one catalog and is settable on every layer (workspace → runtime →
// agent → group → user) through the existing permgate/tool-policy engine.
//
// Completeness is a hard requirement (Jesper, 31-05-2026): the catalog must come
// from an EXHAUSTIVE audit of every hardcoded permission/policy check in server/,
// not just CRUD routes. It is enforced in TWO different ways, and the difference
// matters — do not read green CI as proof of both:
//
//   - ROUTE completeness is TEST-ENFORCED. catalog_test.go asserts that every
//     mutating HTTP route in permguard/inventory.json is EITHER owned by exactly
//     one capability (via Ops) OR listed in the Excluded map with a reason. A new
//     un-classified route fails CI. This half cannot silently drift.
//
//   - NON-ROUTE completeness is AUDIT-TRUST, not test-trust. Internal checks
//     (ownership/role/group-allowlist/scope) that no single route represents —
//     e.g. "may you trigger SOMEONE ELSE's agent" (CanUseAgent / CanTriggerMention)
//     or "use someone else's runtime" (CanUseRuntime) — are catalogued with
//     Evidence file:line instead of Ops. There is NO test that proves every such
//     check is captured; green CI does not prove non-route exhaustiveness. New
//     internal checks must be added here by hand. The grouppermissions Can* family
//     (create/use × agent/runtime) is captured in full; if that family grows, or a
//     new hardcoded gate appears in middleware/handlers, add a capability here.
//
// Design notes:
//
//   - Code, not seeded DB rows. The complete operation list already lives in
//     permguard/inventory.json (the regression tripwire that fails CI when an
//     un-inventoried operation lands). Deriving the catalog from that single
//     source keeps one source of truth, needs no backfill job, and lets the
//     coverage test prove every Op is a real, current route. The tool-policy
//     table appends these rows at read time (toolpolicy.appendPlatformRows); the
//     Allow/Ask/Deny choices persist to cerebro_tool_policy keyed on Key.
//
//   - ManagedExternally marks the "styret udefra" entries: actions whose access
//     is already decided by another mechanism (membership ACL, daemon token,
//     webhook secret, self-only owner check). They are listed for visibility but
//     the tool-policy gate is not their enforcement point, so phase 2 does not
//     route them through Allow/Ask/Deny.
//
//   - Granularity is action-level, not route-level. One capability can cover
//     several routes (e.g. update_issue covers edit/label/dependency/metadata)
//     because they are the same human-meaning action. Ops records exactly which
//     routes each owns, so the partition is explicit and the test can check it.
package platformcatalog

// Category groups capabilities for the permission table's facet sidebar. Values
// are stable display strings (the table groups by category verbatim).
const (
	CategoryIssues      = "Issues"
	CategoryComments    = "Comments"
	CategoryAutopilots  = "Autopilots"
	CategoryArtifacts   = "Artifacts"
	CategoryAgents      = "Agents"
	CategoryRuntimes    = "Runtimes"
	CategoryGroups      = "Groups"
	CategoryPermissions = "Permissions"
	CategoryProjects    = "Projects"
	CategoryWorkspace   = "Workspace"
	CategorySkills      = "Skills"
	CategorySquads      = "Squads"
	CategoryCredentials  = "Credentials"
	CategoryConnections  = "Connections"
	CategoryWorkflows    = "Workflows"
	CategoryChannels    = "Channels"
	CategoryReadAccess  = "Read access"
)

// Source is the value stamped on every platform capability row so the table and
// the resolver can tell a platform action apart from a reported runtime tool.
const Source = "platform"

// Capability is one platform action in the catalog.
type Capability struct {
	// Key is the stable tool_key the resolver keys on and Allow/Ask/Deny rows
	// bind to in cerebro_tool_policy. snake_case, never renamed once shipped
	// (a rename orphans every stored policy row). Reuses the existing
	// cerebro_capability gate names where one already exists (e.g. create_agent).
	Key string
	// Title and Category are the human labels the table shows instead of the key.
	Title    string
	Category string
	// Description is a one-line plain explanation of what the action does.
	Description string
	// ManagedExternally is true when the tool-policy gate is NOT this action's
	// enforcement point (see package doc). Shown for visibility; phase 2 skips it.
	ManagedExternally bool
	// Ops are the permguard inventory ids (http surface) this capability covers.
	// Traceability + coverage tripwire: every id must be a real, current route.
	Ops []string
	// Evidence is file:line pointers to the hardcoded check(s) a capability
	// represents when it is NOT a single HTTP route — e.g. an internal
	// ownership/group-allowlist/scope check. A capability MUST have at least one
	// of Ops or Evidence. These are the "hardcodede policies" the audit surfaced
	// (Jesper 31-05-2026) so they become settable instead of buried in code.
	Evidence []string
}

// catalog is the full ordered list. Order is category-major for readability; the
// table re-sorts, so order here is not load-bearing.
var catalog = []Capability{
	// --- Issues ---------------------------------------------------------------
	{
		Key:         "create_issue",
		Title:       "Create issue",
		Category:    CategoryIssues,
		Description: "Create a new issue or sub-issue (a sub-issue is a create with a parent).",
		Ops: []string{
			"POST /api/issues/",
			"POST /api/issues/quick-create",
			"POST /api/cerebro/issues/check-similar",
			"POST /api/cerebro/issues/check-similar/event",
		},
	},
	{
		Key:         "update_issue",
		Title:       "Edit issue",
		Category:    CategoryIssues,
		Description: "Edit an issue: fields, status/close, labels, metadata, dependencies, references. Status change and close are part of this action.",
		Ops: []string{
			"PUT /api/issues/{id}",
			"POST /api/issues/batch-update",
			"PUT /api/issues/{id}/metadata/{key}",
			"DELETE /api/issues/{id}/metadata/{key}",
			"POST /api/issues/{id}/labels",
			"DELETE /api/issues/{id}/labels/{labelId}",
			"POST /api/issues/{id}/blocks",
			"DELETE /api/issues/{id}/blocks/{otherId}",
			"POST /api/issues/{id}/blocked-by",
			"DELETE /api/issues/{id}/blocked-by/{otherId}",
			"POST /api/issues/{id}/related",
			"DELETE /api/issues/{id}/related/{otherId}",
			"POST /api/issues/{id}/references",
			"PATCH /api/cerebro/references/{refId}",
			"DELETE /api/cerebro/references/{refId}",
			"PUT /api/cerebro/issues/{issueId}/custom-status/",
			"DELETE /api/cerebro/issues/{issueId}/custom-status/",
		},
	},
	{
		Key:         "delete_issue",
		Title:       "Delete issue",
		Category:    CategoryIssues,
		Description: "Delete one issue or a batch of issues.",
		Ops: []string{
			"DELETE /api/issues/{id}/",
			"POST /api/issues/batch-delete",
		},
	},
	{
		Key:         "rerun_issue",
		Title:       "Re-run / cancel issue agent",
		Category:    CategoryIssues,
		Description: "Re-trigger the assigned agent on an issue, or cancel a running task.",
		Ops: []string{
			"POST /api/issues/{id}/rerun",
			"POST /api/issues/{id}/tasks/{taskId}/cancel",
			"POST /api/tasks/{taskId}/cancel",
		},
	},
	{
		Key:         "subscribe_issue",
		Title:       "Subscribe / react to issue",
		Category:    CategoryIssues,
		Description: "Subscribe or unsubscribe yourself, or add/remove a reaction on an issue.",
		Ops: []string{
			"POST /api/issues/{id}/subscribe",
			"POST /api/issues/{id}/unsubscribe",
			"POST /api/issues/{id}/reactions",
			"DELETE /api/issues/{id}/reactions",
		},
	},
	{
		Key:         "manage_labels",
		Title:       "Define / edit labels",
		Category:    CategoryIssues,
		Description: "Create, rename, or delete a workspace label (distinct from attaching a label to an issue).",
		Ops: []string{
			"POST /api/labels/",
			"PUT /api/labels/{id}/",
			"DELETE /api/labels/{id}/",
		},
	},
	{
		Key:         "manage_share_tokens",
		Title:       "Share issue externally",
		Category:    CategoryIssues,
		Description: "Create or revoke a public share-token link that exposes an issue outside the workspace.",
		Ops: []string{
			"POST /api/cerebro/issues/{id}/share-tokens",
			"DELETE /api/cerebro/share-tokens/{tokenId}",
		},
	},

	// --- Comments -------------------------------------------------------------
	{
		Key:         "add_comment",
		Title:       "Add comment",
		Category:    CategoryComments,
		Description: "Post a comment (or threaded reply) on an issue.",
		Ops: []string{
			"POST /api/issues/{id}/comments",
		},
	},
	{
		Key:         "update_comment",
		Title:       "Edit / delete comment",
		Category:    CategoryComments,
		Description: "Edit, delete, resolve, react to, or move an existing comment.",
		Ops: []string{
			"PUT /api/comments/{commentId}/",
			"DELETE /api/comments/{commentId}/",
			"POST /api/comments/{commentId}/resolve",
			"DELETE /api/comments/{commentId}/resolve",
			"POST /api/comments/{commentId}/reactions",
			"DELETE /api/comments/{commentId}/reactions",
			"POST /api/comments/move-to-thread",
			"POST /api/comments/{commentId}/move-to-subissue",
		},
	},

	// --- Autopilots -----------------------------------------------------------
	{
		Key:         "create_autopilot",
		Title:       "Create / edit autopilot",
		Category:    CategoryAutopilots,
		Description: "Create, edit, or delete an autopilot and its triggers.",
		Ops: []string{
			"POST /api/autopilots/",
			"PATCH /api/autopilots/{id}/",
			"DELETE /api/autopilots/{id}/",
			"POST /api/autopilots/{id}/triggers",
			"PATCH /api/autopilots/{id}/triggers/{triggerId}/",
			"DELETE /api/autopilots/{id}/triggers/{triggerId}/",
			"PUT /api/autopilots/{id}/triggers/{triggerId}/signing-secret",
			"POST /api/autopilots/{id}/triggers/{triggerId}/rotate-webhook-token",
		},
	},
	{
		Key:         "trigger_autopilot",
		Title:       "Trigger autopilot",
		Category:    CategoryAutopilots,
		Description: "Fire an autopilot manually, or replay a past delivery.",
		Ops: []string{
			"POST /api/autopilots/{id}/trigger",
			"POST /api/autopilots/{id}/deliveries/{deliveryId}/replay",
		},
	},
	{
		Key:         "autopilot_scope",
		Title:       "Autopilot scope (who may see/edit/trigger)",
		Category:    CategoryAutopilots,
		Description: "The hardcoded owner/group scope rule deciding who may see, edit, or trigger an autopilot. Today a code check, not on the controllable engine.",
		Evidence: []string{
			"server/internal/cerebro/access/autopilot_scope.go:48",  // ValidateScope
			"server/internal/cerebro/access/autopilot_scope.go:150", // CanEdit
			"server/internal/cerebro/access/autopilot_scope.go:179", // CanTrigger
		},
	},
	{
		Key:               "autopilot_webhook",
		Title:             "Autopilot inbound webhook",
		Category:          CategoryAutopilots,
		Description:       "An external caller firing an autopilot via its webhook URL. Governed by the per-trigger webhook secret, not the tool-policy gate.",
		ManagedExternally: true,
		Ops: []string{
			"POST /api/webhooks/autopilots/{token}",
		},
	},

	// --- Artifacts ------------------------------------------------------------
	{
		Key:         "manage_artifacts",
		Title:       "Create / edit / delete artifacts",
		Category:    CategoryArtifacts,
		Description: "Create, upload, edit, move, re-scope, or delete artifacts, artifact folders, and attachments.",
		Ops: []string{
			"POST /api/artifacts/",
			"PUT /api/artifacts/{id}",
			"PUT /api/artifacts/{id}/folder",
			"PUT /api/artifacts/{id}/scope",
			"DELETE /api/artifacts/{id}",
			"POST /api/artifact-folders/",
			"PUT /api/artifact-folders/{id}",
			"DELETE /api/artifact-folders/{id}",
			"POST /api/artifact-uploads",
			"POST /api/upload-file",
			"DELETE /api/attachments/{id}",
		},
	},

	// --- Agents (styring af agenter) -----------------------------------------
	{
		Key:         "create_agent",
		Title:       "Create agent",
		Category:    CategoryAgents,
		Description: "Create a new agent, from scratch or from a template.",
		Ops: []string{
			"POST /api/agents/",
			"POST /api/agents/from-template",
		},
	},
	{
		Key:         "update_agent",
		Title:       "Edit agent",
		Category:    CategoryAgents,
		Description: "Edit an agent's config, skills, tools, env, archive/restore it, or cancel its tasks.",
		Ops: []string{
			"PUT /api/agents/{id}/",
			"POST /api/agents/{id}/archive",
			"POST /api/agents/{id}/restore",
			"POST /api/agents/{id}/cancel-tasks",
			"PUT /api/agents/{id}/skills",
			"PUT /api/agents/{id}/env",
			"PUT /api/agents/{id}/infisical-folders",
			"PUT /api/agents/{id}/tools/{name}/",
			"PUT /api/agents/{id}/tool-overrides/{toolName}",
			"DELETE /api/agents/{id}/tool-overrides/{toolName}",
		},
	},
	{
		Key:         "trigger_other_agent",
		Title:       "Trigger someone else's agent",
		Category:    CategoryAgents,
		Description: "Whether you may start an agent you do not own (via an @mention or run-request). FIR-2409: now settable on the controllable engine at workspace / group / member / agent level. With no rule set, the hardcoded default applies (owners/admins master key, members blocked); an explicit Deny removes the master key, an explicit Allow grants a blocked member.",
		Evidence: []string{
			"server/internal/cerebro/grouppermissions/permissions.go:336", // CanUseAgent (baseline)
			"server/internal/cerebro/mentiongate/gate.go:31",              // CanTriggerMention (now resolves trigger_other_agent via toolpolicy)
		},
	},
	{
		Key:         "schedule_agent_wakeup",
		Title:       "Schedule agent wakeup",
		Category:    CategoryAgents,
		Description: "Create or cancel a scheduled wakeup that starts an agent on an issue later or when a watched event happens.",
		Ops: []string{
			"POST /api/cerebro/wakeups/",
			"POST /api/cerebro/wakeups/{id}/cancel",
		},
	},
	{
		Key:         "manage_agent_passes",
		Title:       "Manage agent passes",
		Category:    CategoryAgents,
		Description: "Issue or revoke an agent pass (a scoped, time-boxed permission for an agent to act on an issue).",
		Ops: []string{
			"POST /api/cerebro/agent-passes/",
			"DELETE /api/cerebro/agent-passes/{passId}",
		},
	},
	{
		Key:         "manage_work_sessions",
		Title:       "Drive agent work session",
		Category:    CategoryAgents,
		Description: "Start, fork, message, resume, rename, or complete an interactive agent work session.",
		Ops: []string{
			"POST /api/work-sessions/",
			"POST /api/work-sessions/{id}/fork",
			"POST /api/work-sessions/{id}/messages",
			"POST /api/work-sessions/{id}/resume",
			"PUT /api/work-sessions/{id}/complete",
			"PUT /api/work-sessions/{id}/name",
		},
	},

	// --- Runtimes (styring af runtimes) --------------------------------------
	{
		Key:         "manage_runtime",
		Title:       "Manage runtime",
		Category:    CategoryRuntimes,
		Description: "Edit, pause, update, delete a runtime (machine), change its sandbox/tool config, or push local skills and models to it.",
		Ops: []string{
			"PATCH /api/runtimes/{runtimeId}/",
			"DELETE /api/runtimes/{runtimeId}/",
			"POST /api/runtimes/{runtimeId}/pause",
			"POST /api/runtimes/{runtimeId}/unpause",
			"POST /api/runtimes/{runtimeId}/update",
			"POST /api/runtimes/{runtimeId}/archive-agents-and-delete",
			"PATCH /api/runtimes/{runtimeId}/sandbox",
			"PATCH /api/runtimes/{runtimeId}/sandbox-policy",
			"PATCH /api/runtimes/{runtimeId}/persona-sandbox",
			"PATCH /api/runtimes/{runtimeId}/tools-config",
			"PUT /api/cerebro/terminal/runtimes/{runtimeId}/presentation-mode",
			"PATCH /api/runtimes/{runtimeId}/tools/{toolName}",
			"POST /api/runtimes/{runtimeId}/tools/scan-now",
			"POST /api/runtimes/{runtimeId}/local-skills",
			"POST /api/runtimes/{runtimeId}/local-skills/import",
			"POST /api/runtimes/{runtimeId}/models",
		},
	},
	{
		Key:         "manage_runtime_tool_access",
		Title:       "Grant runtime tool access",
		Category:    CategoryRuntimes,
		Description: "Grant or revoke a specific runtime tool for a group or user (per-tool allowlist on a machine).",
		Ops: []string{
			"POST /api/runtimes/{runtimeId}/tools/{toolName}/groups/{groupId}",
			"DELETE /api/runtimes/{runtimeId}/tools/{toolName}/groups/{groupId}",
			"POST /api/runtimes/{runtimeId}/tools/{toolName}/users/{userId}",
			"DELETE /api/runtimes/{runtimeId}/tools/{toolName}/users/{userId}",
		},
	},
	{
		Key:         "manage_runtime_accounts",
		Title:       "Manage runtime accounts",
		Category:    CategoryRuntimes,
		Description: "Add, remove, or change controls on the model/provider accounts a runtime uses.",
		Ops: []string{
			"POST /api/workspaces/{id}/accounts",
			"DELETE /api/workspaces/{id}/accounts/{id}",
			"PATCH /api/workspaces/{id}/accounts/{id}/controls",
		},
	},
	{
		Key:         "manage_cloud_runtime",
		Title:       "Manage cloud runtime nodes",
		Category:    CategoryRuntimes,
		Description: "Provision, start, stop, reboot, delete, or exec on cloud runtime nodes.",
		Ops: []string{
			"POST /api/cloud-runtime/nodes",
			"DELETE /api/cloud-runtime/nodes",
			"POST /api/cloud-runtime/nodes/exec",
			"POST /api/cloud-runtime/nodes/reboot",
			"POST /api/cloud-runtime/nodes/start",
			"POST /api/cloud-runtime/nodes/stop",
			"POST /api/cloud-runtime/nodes/status",
		},
	},
	{
		Key:         "create_runtime",
		Title:       "Create runtime",
		Category:    CategoryRuntimes,
		Description: "Register a new runtime (machine). A named group capability an admin can grant or deny today; no HTTP route — runtimes are created via the daemon — so it is settable only as a non-route capability. The runtime twin of create_agent.",
		Evidence: []string{
			"server/internal/handler/group_permissions_cerebro.go:150",    // cerebroRequireCapability("create_runtime")
			"server/internal/cerebro/grouppermissions/permissions.go:295", // CanCreateRuntime
		},
	},
	{
		Key:         "create_local_runtime",
		Title:       "Create local runtime",
		Category:    CategoryRuntimes,
		Description: "Register a new LOCAL (daemon-based) runtime that runs on the user's own machine. Settable Allow/Ask/Deny on every layer; enforced live at the runtime setup-token mint through the unified tool-policy chain (workspace > group > user). The local twin of manage_cloud_runtime (FIR-2672).",
		Ops: []string{
			"POST /api/runtime-setup/tokens",
			"POST /api/workspaces/{id}/runtime-setup-token",
		},
		Evidence: []string{
			"server/internal/handler/runtime_setup.go:54",              // CreateRuntimeSetupToken create_local_runtime tool-policy gate
			"server/internal/handler/group_permissions_cerebro.go:185", // cerebroRequireLocalRuntimePolicy (tool-policy Resolve)
		},
	},
	{
		Key:         "use_other_runtime",
		Title:       "Use someone else's runtime",
		Category:    CategoryRuntimes,
		Description: "Whether you may point an agent at a runtime you do not own. Today a hardcoded group allowlist, NOT on the controllable engine — the runtime twin of trigger_other_agent.",
		Evidence: []string{
			"server/internal/cerebro/grouppermissions/permissions.go:322", // CanUseRuntime
			"server/internal/handler/group_permissions_cerebro.go:187",    // cerebroRequireRuntimeAccess call site
		},
	},
	{
		Key:               "daemon_runtime_callback",
		Title:             "Daemon runtime callbacks",
		Category:          CategoryRuntimes,
		Description:       "The local daemon reporting heartbeats, claiming tasks, and posting results. Governed by the daemon token, not the tool-policy gate.",
		ManagedExternally: true,
		Ops: []string{
			"POST /api/daemon/register",
			"POST /api/daemon/heartbeat",
			"POST /api/daemon/deregister",
			"POST /api/daemon/runtimes/{runtimeId}/tasks/claim",
			"POST /api/daemon/runtimes/{runtimeId}/refresh-capabilities",
		},
	},

	// --- Groups ---------------------------------------------------------------
	{
		Key:         "manage_group",
		Title:       "Create / edit group",
		Category:    CategoryGroups,
		Description: "Create, edit, or delete a group, and grant or revoke a group's capabilities.",
		Ops: []string{
			"POST /api/workspaces/{id}/groups",
			"PATCH /api/groups/{id}",
			"DELETE /api/groups/{id}",
			"POST /api/groups/{id}/capabilities",
			"DELETE /api/groups/{id}/capabilities/{capability}",
		},
	},
	{
		Key:         "manage_group_members",
		Title:       "Manage group members",
		Category:    CategoryGroups,
		Description: "Add or remove users, agents, and runtimes from a group.",
		Ops: []string{
			"POST /api/groups/{id}/members",
			"DELETE /api/groups/{id}/members/{userId}",
			"POST /api/groups/{id}/agents",
			"DELETE /api/groups/{id}/agents/{agentId}",
			"POST /api/groups/{id}/runtimes",
			"DELETE /api/groups/{id}/runtimes/{runtimeId}",
		},
	},

	// --- Permissions (styring af grants/roller/policy) ------------------------
	{
		Key:         "manage_grants",
		Title:       "Manage grants",
		Category:    CategoryPermissions,
		Description: "Create, edit, or delete permission grants for the workspace.",
		Ops: []string{
			"POST /api/workspaces/{id}/grants",
			"PATCH /api/workspaces/{id}/grants/{grantId}",
			"DELETE /api/workspaces/{id}/grants/{grantId}",
		},
	},
	{
		Key:         "manage_roles",
		Title:       "Manage roles",
		Category:    CategoryPermissions,
		Description: "Create, edit, or delete roles, and assign or unassign a role to a member, agent, or group.",
		Ops: []string{
			"POST /api/workspaces/{id}/roles",
			"PATCH /api/roles/{id}",
			"DELETE /api/roles/{id}",
			"POST /api/roles/{id}/assignments",
			"DELETE /api/roles/{id}/assignments/{subjectType}/{subjectId}",
		},
	},
	{
		Key:         "manage_tool_policy",
		Title:       "Manage tool policy",
		Category:    CategoryPermissions,
		Description: "Set or clear an Allow/Ask/Deny choice in the permission table itself.",
		Ops: []string{
			"PUT /api/workspaces/{id}/tool-policy",
			"DELETE /api/workspaces/{id}/tool-policy",
		},
	},
	{
		Key:         "decide_approval",
		Title:       "Decide approval request",
		Category:    CategoryPermissions,
		Description: "Raise, approve, reject, or delegate an approval request in the inbox.",
		Ops: []string{
			"POST /api/workspaces/{id}/approvals/intake",
			"POST /api/workspaces/{id}/approvals/{approvalId}/approve",
			"POST /api/workspaces/{id}/approvals/{approvalId}/reject",
			"POST /api/workspaces/{id}/approvals/{approvalId}/delegate",
		},
	},

	// --- Projects -------------------------------------------------------------
	{
		Key:         "manage_project",
		Title:       "Create / edit project",
		Category:    CategoryProjects,
		Description: "Create, edit, or delete a project and its resources.",
		Ops: []string{
			"POST /api/projects/",
			"PUT /api/projects/{id}/",
			"DELETE /api/projects/{id}/",
			"PUT /api/projects/{id}/parent",
			"PUT /api/projects/{id}/show-descendants",
			"POST /api/projects/{id}/resources",
			"PUT /api/projects/{id}/resources/{resourceId}",
			"DELETE /api/projects/{id}/resources/{resourceId}",
		},
	},
	{
		Key:               "manage_project_access",
		Title:             "Manage project access",
		Category:          CategoryProjects,
		Description:       "Add/remove project members and group access. Governed by the project's own access list, not the tool-policy gate.",
		ManagedExternally: true,
		Ops: []string{
			"PATCH /api/projects/{id}/access",
			"POST /api/projects/{id}/members",
			"DELETE /api/projects/{id}/members/{userId}",
			"POST /api/projects/{id}/group-access",
			"DELETE /api/projects/{id}/group-access/{groupId}",
		},
	},
	{
		Key:         "manage_status_models",
		Title:       "Manage status models",
		Category:    CategoryProjects,
		Description: "Create, edit, or delete the custom status model for the workspace or a project.",
		Ops: []string{
			"POST /api/cerebro/status-models/",
			"PUT /api/cerebro/status-models/{id}",
			"DELETE /api/cerebro/status-models/{id}",
			"PATCH /api/cerebro/status-models/{id}/set-default",
			"DELETE /api/cerebro/status-models/default",
			"PUT /api/cerebro/projects/{projectId}/status-model/",
			"DELETE /api/cerebro/projects/{projectId}/status-model/",
		},
	},
	{
		Key:         "manage_project_sprints",
		Title:       "Manage project sprints",
		Category:    CategoryProjects,
		Description: "Create, edit, delete, and assign project sprints and recurring sprint tasks.",
		Ops: []string{
			"PUT /api/cerebro/projects/{projectID}/sprint-settings/",
			"DELETE /api/cerebro/projects/{projectID}/sprint-settings/",
			"POST /api/cerebro/projects/{projectID}/sprints/",
			"POST /api/cerebro/projects/{projectID}/sprint-sweep",
			"POST /api/cerebro/projects/{projectID}/sprint-recurring-tasks/",
			"PUT /api/cerebro/sprint-recurring-tasks/{id}/",
			"DELETE /api/cerebro/sprint-recurring-tasks/{id}/",
			"PUT /api/cerebro/sprints/{sprintID}/",
			"DELETE /api/cerebro/sprints/{sprintID}/",
			"PUT /api/cerebro/issues/{issueID}/sprint/",
		},
	},

	// --- Workspace (medlemmer, settings, integrationer) ----------------------
	{
		Key:         "manage_workspace_members",
		Title:       "Manage workspace members",
		Category:    CategoryWorkspace,
		Description: "Invite or remove members, change a member's role/budget, set a member's secret folders, or revoke an invitation.",
		Ops: []string{
			"POST /api/workspaces/{id}/members",
			"PATCH /api/workspaces/{id}/members/{memberId}/",
			"DELETE /api/workspaces/{id}/members/{memberId}/",
			"PATCH /api/workspaces/{id}/members/{memberId}/budget-enforcement",
			"PUT /api/workspaces/{id}/members/{memberId}/infisical-folders",
			"DELETE /api/workspaces/{id}/invitations/{invitationId}",
		},
	},
	{
		Key:         "manage_workspace_settings",
		Title:       "Edit workspace settings",
		Category:    CategoryWorkspace,
		Description: "Change workspace settings: name/config, feature flags, cost optimization, auth settings, and pausing all tasks.",
		Ops: []string{
			"PATCH /api/workspaces/{id}/",
			"PUT /api/workspaces/{id}/",
			"PUT /api/workspaces/{id}/feature-flags/{key}",
			"PUT /api/workspaces/{id}/feature-flags/{key}/workspace",
			"DELETE /api/workspaces/{id}/feature-flags/{key}/workspace",
			"PUT /api/workspaces/{id}/cost-optimization/{key}",
			"DELETE /api/workspaces/{id}/cost-optimization/{key}",
			"PUT /api/workspaces/{id}/cost-optimization/holdout/{key}",
			"DELETE /api/workspaces/{id}/cost-optimization/holdout/{key}",
			"PUT /api/workspaces/{id}/display-currency",
			"PUT /api/cerebro/workspaces/{id}/auth-settings/",
			"POST /api/workspaces/{id}/pause-tasks",
			"POST /api/workspaces/{id}/generate-logo",
		},
	},
	{
		Key:         "delete_workspace",
		Title:       "Delete workspace",
		Category:    CategoryWorkspace,
		Description: "Permanently delete the entire workspace. Owner-only and irreversible.",
		Ops: []string{
			"DELETE /api/workspaces/{id}/",
		},
	},
	{
		Key:         "manage_integrations",
		Title:       "Manage integrations",
		Category:    CategoryWorkspace,
		Description: "Connect or disconnect external integrations (e.g. a GitHub App installation).",
		Ops: []string{
			"DELETE /api/workspaces/{id}/github/installations/{installationId}",
		},
	},

	// --- Skills ---------------------------------------------------------------
	{
		Key:         "manage_skills",
		Title:       "Create / edit / delete skills",
		Category:    CategorySkills,
		Description: "Create, import, edit, fork, delete skills and their files, review change-requests, transfer skill ownership, and record behavioral observations.",
		Ops: []string{
			"POST /api/skills/",
			"POST /api/skills/import",
			"POST /api/skills/{id}/change-requests",
			"POST /api/skills/{id}/forks",
			"POST /api/skills/change-requests/{crId}/review",
			"PUT /api/skills/{id}/",
			"PUT /api/skills/{id}/files",
			"PUT /api/skills/{id}/ownership",
			"DELETE /api/skills/{id}/",
			"DELETE /api/skills/{id}/files/{fileId}",
			// CEREBRO-PATCH(skill-observations-catalog): TECH-3077 agent runtime learning signal.
			"POST /api/skill-observations",
		},
	},

	// --- Squads ---------------------------------------------------------------
	{
		Key:         "manage_squad",
		Title:       "Create / edit squad",
		Category:    CategorySquads,
		Description: "Create, edit, or delete a squad, manage its members, and change a member's role.",
		Ops: []string{
			"POST /api/squads/",
			"PUT /api/squads/{id}/",
			"DELETE /api/squads/{id}/",
			"POST /api/squads/{id}/members",
			"DELETE /api/squads/{id}/members",
			"PATCH /api/squads/{id}/members/role",
		},
	},

	// --- Connections (CEREBRO-PATCH cerebro-connections-catalog: TECH-3108) ---
	{
		Key:         "manage_connections",
		Title:       "Manage workspace connections",
		Category:    CategoryConnections,
		Description: "Create, edit, or delete workspace API/MCP connections (external URLs and internal Sliplane paths).",
		Ops: []string{
			"POST /api/workspaces/{id}/connections",
			"PUT /api/workspaces/{id}/connections/{connId}",
			"DELETE /api/workspaces/{id}/connections/{connId}",
		},
	},

	// --- Credentials ----------------------------------------------------------
	{
		Key:         "manage_credentials",
		Title:       "Manage credentials / secrets",
		Category:    CategoryCredentials,
		Description: "Create, edit, delete, reveal, or rotate workspace credentials and their bindings. Highly sensitive.",
		Ops: []string{
			"POST /api/workspaces/{id}/credentials",
			"PATCH /api/workspaces/{id}/credentials/{credId}",
			"DELETE /api/workspaces/{id}/credentials/{credId}",
			"POST /api/workspaces/{id}/credentials/{credId}/bindings",
			"DELETE /api/workspaces/{id}/credentials/{credId}/bindings/{bindingId}",
			"POST /api/workspaces/{id}/credentials/{credId}/reveal",
			"POST /api/workspaces/{id}/credentials/{credId}/rotate",
		},
	},

	// --- Workflows ------------------------------------------------------------
	{
		Key:         "manage_workflows",
		Title:       "Manage workflows",
		Category:    CategoryWorkflows,
		Description: "Create, edit, delete, toggle a workflow, or regenerate its tokens and signing secrets.",
		Ops: []string{
			"POST /api/cerebro/workflows/",
			"PUT /api/cerebro/workflows/{id}",
			"DELETE /api/cerebro/workflows/{id}",
			"POST /api/cerebro/workflows/{id}/toggle",
			"POST /api/cerebro/workflows/{id}/regenerate-outbound-secret",
			"POST /api/cerebro/workflows/{id}/regenerate-signing-secret",
			"POST /api/cerebro/workflows/{id}/regenerate-token",
			"POST /api/cerebro/workflows/_test/cron-sweep",
		},
	},

	// --- Channels -------------------------------------------------------------
	{
		Key:         "manage_channels",
		Title:       "Manage channels",
		Category:    CategoryChannels,
		Description: "Create or archive a channel and set an agent's listen mode in it.",
		Ops: []string{
			"POST /api/channels/",
			"POST /api/channels/{id}/archive",
			"DELETE /api/channels/{id}/archive",
			"PUT /api/channels/{id}/agents/{agentId}/listen-mode",
		},
	},

	// --- Read access (læse-adgang til sager/projekter) -----------------------
	// Reads are governed by workspace membership and project access lists, not
	// the tool-policy gate; listed (marked) so an admin can see the platform
	// exposes them and a future phase can choose to gate them.
	{
		Key:               "read_issues",
		Title:             "Read issues",
		Category:          CategoryReadAccess,
		Description:       "View issues, comments, and their history. Governed by workspace membership, not the tool-policy gate.",
		ManagedExternally: true,
		Evidence:          []string{"server/internal/middleware/workspace.go (RequireWorkspaceMember)"},
	},
	{
		Key:               "read_projects",
		Title:             "Read projects",
		Category:          CategoryReadAccess,
		Description:       "View projects and their contents. Governed by the project access list (and a group-membership path), not the tool-policy gate.",
		ManagedExternally: true,
		Evidence: []string{
			"server/internal/handler/project_access.go (canAccessProject)",
			"server/internal/cerebro/grouppermissions/permissions.go:352", // CanSeeProjectViaGroup
		},
	},
}

// excluded lists every mutating HTTP route that is deliberately NOT a catalog
// capability, each with a reason. The coverage test asserts that every mutating
// route in permguard/inventory.json is EITHER owned by a capability (Ops) OR in
// this map — so "complete" is machine-enforced and a new un-classified route
// fails CI. Reasons follow the permguard exemption vocabulary (self_only,
// daemon-token, pre-auth, billing) plus a few cerebro-specific ones.
var excluded = map[string]string{
	// self_only — operates on the caller's own data/identity; there is no other
	// actor to gate on, so the tool-policy engine has nothing to decide.
	"POST /api/workspaces/":                        "self_only — create-workspace is pre-workspace; any authenticated user, no workspace context to gate within",
	"POST /api/workspaces/{id}/leave":              "self_only — leaving is the caller's own membership",
	"POST /api/inbox/add-issue":                    "self_only — caller pinning any issue into their own inbox",
	"DELETE /api/me/profile":                       "self_only — caller's own profile",
	"PATCH /api/me":                                "self_only — caller's own profile",
	"PUT /api/me/profile":                          "self_only — caller's own profile",
	"PATCH /api/me/onboarding":                     "self_only — caller's own onboarding",
	"PATCH /api/me/preferences":                    "self_only — caller's own preferences",
	"POST /api/me/onboarding/complete":             "self_only — caller's own onboarding",
	"POST /api/me/onboarding/cloud-waitlist":       "self_only — caller's own onboarding",
	"POST /api/me/onboarding/no-runtime-bootstrap": "self_only — caller's own onboarding",
	"POST /api/me/onboarding/runtime-bootstrap":    "self_only — caller's own onboarding",
	"PUT /api/notification-preferences/":           "self_only — caller's own notification prefs",
	"POST /api/feedback":                           "self_only — caller submitting feedback",
	"POST /api/cli-token":                          "self_only — caller's own CLI token",
	"POST /api/tokens/":                            "self_only — caller's own API token",
	"POST /api/tokens/current/renew":               "self_only — caller's own API token",
	"DELETE /api/tokens/{id}":                      "self_only — caller's own API token",
	"POST /api/pins/":                              "self_only — caller's own pins",
	"PUT /api/pins/reorder":                        "self_only — caller's own pins",
	"DELETE /api/pins/{itemType}/{itemId}":         "self_only — caller's own pins",
	"POST /api/push/subscribe":                     "self_only — caller's own push subscription",
	"POST /api/push/unsubscribe":                   "self_only — caller's own push subscription",
	"POST /api/inbox/archive-all":                  "self_only — caller's own inbox",
	"POST /api/inbox/archive-all-read":             "self_only — caller's own inbox",
	"POST /api/inbox/archive-completed":            "self_only — caller's own inbox",
	"POST /api/inbox/add-issue":                    "self_only — caller manually adding an issue to their own inbox",
	"POST /api/inbox/mark-all-read":                "self_only — caller's own inbox",
	"POST /api/inbox/notifications/archive-all":    "self_only — caller's own inbox",
	"POST /api/inbox/notifications/mark-all-read":  "self_only — caller's own inbox",
	"POST /api/inbox/reminders":                    "self_only — caller's own inbox",
	"POST /api/inbox/{id}/archive":                 "self_only — caller's own inbox",
	"POST /api/inbox/{id}/mute":                    "self_only — caller's own inbox",
	"DELETE /api/inbox/{id}/mute":                  "self_only — caller's own inbox",
	"POST /api/inbox/{id}/read":                    "self_only — caller's own inbox",
	"POST /api/inbox/{id}/unread":                  "self_only — caller's own inbox",
	"POST /api/inbox/{id}/unarchive":               "self_only — caller's own inbox",
	"POST /api/inbox/{id}/run-private-agent":       "self_only — caller running their own private agent from their inbox",

	// cerebro focus-list — personal task queue, caller's own items only.
	"POST /api/cerebro/focus-list/":            "self_only — caller's own focus list",
	"PATCH /api/cerebro/focus-list/{id}":       "self_only — caller's own focus list",
	"DELETE /api/cerebro/focus-list/{id}":      "self_only — caller's own focus list",
	"POST /api/cerebro/focus-list/{id}/done":   "self_only — caller's own focus list",
	"POST /api/cerebro/focus-list/{id}/snooze": "self_only — caller's own focus list",
	"POST /api/cerebro/focus-list/reorder":     "self_only — caller's own focus list",

	// cerebro interactive terminal — human watch/take-over of a live agent
	// session. Workspace-member gated UI action, not an agent-governable
	// capability; there is no agent tool that opens or closes a PTY session.
	"POST /api/cerebro/terminal/sessions":               "interactive-terminal — human opens a live agent session to watch/take over; UI-only, no agent tool equivalent",
	"DELETE /api/cerebro/terminal/sessions/{sessionId}": "interactive-terminal — human closes a live agent session; UI-only, no agent tool equivalent",
	"POST /api/invitations/{id}/accept":                 "self_only — caller accepting their own invitation",
	"POST /api/invitations/{id}/decline":                "self_only — caller declining their own invitation",
	"POST /api/persona/approvals/{id}/approve":          "self_only — handler sets subject_actor_id to the CALLER's own persona actor (persona_approvals.go:138); no admin-acts-for-others path",
	"POST /api/persona/approvals/{id}/deny":             "self_only — handler sets subject_actor_id to the CALLER's own persona actor (persona_approvals.go:138); no admin-acts-for-others path",
	"POST /api/channels/{id}/read":                      "self_only — caller marking a channel read",

	// chat sessions — the caller's own AI chat, not an admin-governed action.
	"POST /api/chat/sessions/":                             "personal-chat — caller's own AI chat session",
	"PATCH /api/chat/sessions/{sessionId}/":                "personal-chat — caller's own AI chat session",
	"DELETE /api/chat/sessions/{sessionId}/":               "personal-chat — caller's own AI chat session",
	"POST /api/chat/sessions/{sessionId}/messages":         "personal-chat — caller's own AI chat session",
	"POST /api/chat/sessions/{sessionId}/read":             "personal-chat — caller's own AI chat session",
	"POST /api/chat/sessions/{sessionId}/convert-to-issue": "personal-chat — reached through the caller's own chat; the resulting create is gated by create_issue. PHASE-2 REQUIREMENT: enforcement must verify convert-to-issue actually routes through the create_issue gate, else it is an ungated create-bypass",

	// daemon-token — runtime daemon callbacks; the daemon token binds the call to
	// a runtime, which is the authorisation. Represented by daemon_runtime_callback.
	"POST /api/daemon/accounts/{id}/usage":                                         "daemon-token — runtime daemon callback",
	"POST /api/daemon/runtimes/{runtimeId}/local-skills/import/{requestId}/result": "daemon-token — runtime daemon callback",
	"POST /api/daemon/runtimes/{runtimeId}/local-skills/{requestId}/result":        "daemon-token — runtime daemon callback",
	"POST /api/daemon/runtimes/{runtimeId}/models/{requestId}/result":              "daemon-token — runtime daemon callback",
	"POST /api/daemon/runtimes/{runtimeId}/recover-orphans":                        "daemon-token — runtime daemon callback",
	"POST /api/daemon/runtimes/{runtimeId}/tool-scan":                              "daemon-token — runtime daemon callback",
	"POST /api/daemon/runtimes/{runtimeId}/update/{updateId}/result":               "daemon-token — runtime daemon callback",
	"POST /api/daemon/tasks/{taskId}/complete":                                     "daemon-token — runtime daemon callback",
	"POST /api/daemon/tasks/{taskId}/fail":                                         "daemon-token — runtime daemon callback",
	"POST /api/daemon/tasks/{taskId}/messages":                                     "daemon-token — runtime daemon callback",
	"POST /api/daemon/tasks/{taskId}/progress":                                     "daemon-token — runtime daemon callback",
	"POST /api/daemon/tasks/{taskId}/session":                                      "daemon-token — runtime daemon callback",
	"POST /api/daemon/tasks/{taskId}/start":                                        "daemon-token — runtime daemon callback",
	"POST /api/daemon/tasks/{taskId}/usage":                                        "daemon-token — runtime daemon callback",
	"POST /api/daemon/tasks/{taskId}/wait-local-directory":                         "daemon-token — runtime daemon callback",
	"POST /api/daemon/workspaces/{workspaceId}/repo/check":                         "daemon-token — runtime daemon callback",

	// pre-auth — establishes or clears identity, or a signed external webhook;
	// there is no authenticated actor yet to gate on.
	"POST /auth/google":                      "pre-auth — establishes identity",
	"POST /auth/send-code":                   "pre-auth — establishes identity",
	"POST /auth/verify-code":                 "pre-auth — establishes identity",
	"POST /auth/logout":                      "pre-auth — clears identity",
	"POST /api/webhooks/github":              "pre-auth signed webhook — GitHub HMAC authorises delivery",
	"POST /api/webhooks/stripe":              "pre-auth signed webhook — Stripe HMAC authorises delivery",
	"POST /api/cerebro/github/pull-requests": "pre-auth service-to-service — CEREBRO_GITHUB_LINK_KEY bearer token authorises the firtal-data-registry poll-based PR-link push (FIR-2568)",
	"POST /api/cerebro/exchange-rates":       "pre-auth service-to-service — CEREBRO_EXCHANGE_INGEST_KEY bearer token authorises the multica-hatchet-worker's daily FX snapshot push (FIR-43)",
	"POST /api/contact-sales":                "pre-auth — public marketing form",
	"POST /api/runtime-setup/exchange":       "bootstrap — exchanges a single-use setup token before a daemon has a user session",

	// billing — external Stripe checkout/portal, human-actor only.
	"POST /api/cloud-billing/checkout-sessions": "billing — external Stripe, human-actor only",
	"POST /api/cloud-billing/portal-sessions":   "billing — external Stripe, human-actor only",

	// cosmetic / system — not access-bearing actions.
	"POST /api/agents/generate-avatar":          "cosmetic — avatar image generation",
	"POST /api/agents/backfill-avatars":         "cosmetic — avatar backfill maintenance",
	"POST /api/capabilities/report":             "runtime-self-report — a runtime reporting its own tools, not a user action",
	"POST /api/workspaces/{id}/grants/evaluate": "read-only — dry-run evaluation of a grant decision; reads policy, changes no state",
	"POST /api/issues/{id}/squad-evaluated":     "system-callback — squad-evaluation marker set by the platform, not a user action",
}

// All returns a copy of the catalog so callers cannot mutate the package state.
func All() []Capability {
	out := make([]Capability, len(catalog))
	copy(out, catalog)
	return out
}

// Keys returns just the stable capability keys, in catalog order.
func Keys() []string {
	out := make([]string, len(catalog))
	for i, c := range catalog {
		out[i] = c.Key
	}
	return out
}

// ByKey looks a capability up by its stable key.
func ByKey(key string) (Capability, bool) {
	for _, c := range catalog {
		if c.Key == key {
			return c, true
		}
	}
	return Capability{}, false
}

// Excluded returns a copy of the route→reason exclusion map (mutating routes that
// are deliberately not capabilities). Used by the coverage test.
func Excluded() map[string]string {
	out := make(map[string]string, len(excluded))
	for k, v := range excluded {
		out[k] = v
	}
	return out
}
