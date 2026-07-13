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
	CategoryCredentials = "Credentials"
	CategoryConnections = "Connections"
	CategoryWorkflows   = "Workflows"
	CategoryApps        = "Apps"
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
	// DescriptionZh is the Chinese (zh) translation of Description, surfaced in the
	// permission table next to the English help so the screen is self-explanatory in
	// both app languages (FIR-2175 phase 3). Empty falls back to the English
	// Description in the UI, so a not-yet-translated row degrades gracefully.
	DescriptionZh string
	// ManagedExternally is true when the tool-policy gate is NOT this action's
	// enforcement point (see package doc). Shown for visibility; phase 2 skips it.
	ManagedExternally bool
	// Surfaced marks a capability that appears in the Permissions table behind the
	// light agent-start gate (cerebro_agent_trigger_permissions), NOT only behind
	// the full-catalog flag (cerebro_platform_capabilities). It is set on the
	// "start someone else's agent" family (FIR-3091 slice 4) so an admin can see
	// and set those rules in the same screen as reported tools without opening the
	// whole platform catalog. SurfacedKeys() is the canonical list.
	Surfaced bool
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
	// --- Apps -----------------------------------------------------------------
	{
		Key:           "use_apps",
		Title:         "Use apps",
		Category:      CategoryApps,
		Description:   "Open and use published workspace apps and interactive app views.",
		DescriptionZh: "打开并使用已发布的工作区应用和交互式应用视图。",
		Ops: []string{
			"POST /api/cerebro/apps/{id}/token",
			"POST /api/cerebro/apps/workflow-runs/{runId}/token",
			"PUT /api/cerebro/apps/{id}/storage/{key}",
			"DELETE /api/cerebro/apps/{id}/storage/{key}",
			"POST /api/cerebro/apps/{id}/views/{viewId}/submissions",
			"POST /api/cerebro/app-workflows/{workflowId}/test",
		},
	},
	{
		Key:           "build_apps",
		Title:         "Build apps",
		Category:      CategoryApps,
		Description:   "Create, preview, publish, and roll back workspace apps.",
		DescriptionZh: "创建、预览、发布和回滚工作区应用。",
		Ops: []string{
			"POST /api/cerebro/apps/",
			"POST /api/cerebro/apps/{id}/preview",
			"POST /api/cerebro/apps/{id}/publish",
			"POST /api/cerebro/apps/{id}/rollback",
			"POST /api/cerebro/app-workflows/",
			"POST /api/cerebro/app-workflows/{workflowId}/{state:enable|disable}",
		},
	},
	{
		Key:           "approve_app_scopes",
		Title:         "Approve app scopes",
		Category:      CategoryApps,
		Description:   "Approve the registry data access ceiling requested by an app version.",
		DescriptionZh: "批准应用版本请求的 registry 数据访问上限。",
		Ops:           []string{"POST /api/cerebro/apps/{id}/approve-scopes"},
	},
	// --- Issues ---------------------------------------------------------------
	{
		Key:           "create_issue",
		Title:         "Create issue",
		Category:      CategoryIssues,
		Description:   "Create a new issue or sub-issue (a sub-issue is a create with a parent).",
		DescriptionZh: "创建新的工单或子工单（子工单即带父级的创建）。",
		Ops: []string{
			"POST /api/issues/",
			"POST /api/issues/quick-create",
			"POST /api/cerebro/issues/check-similar",
			"POST /api/cerebro/issues/check-similar/event",
		},
	},
	{
		Key:           "update_issue",
		Title:         "Edit issue",
		Category:      CategoryIssues,
		Description:   "Edit an issue: fields, status/close, labels, metadata, dependencies, references. Status change and close are part of this action.",
		DescriptionZh: "编辑工单：字段、状态/关闭、标签、元数据、依赖与引用。状态变更和关闭属于此操作。",
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
			"PUT /api/cerebro/issues/{issueID}/date-times/",
		},
	},
	{
		Key:           "delete_issue",
		Title:         "Delete issue",
		Category:      CategoryIssues,
		Description:   "Delete one issue or a batch of issues.",
		DescriptionZh: "删除单个或批量工单。",
		Ops: []string{
			"DELETE /api/issues/{id}/",
			"POST /api/issues/batch-delete",
		},
	},
	{
		Key:           "rerun_issue",
		Title:         "Re-run / cancel issue agent",
		Category:      CategoryIssues,
		Surfaced:      true,
		Description:   "Re-trigger the assigned agent on an issue, or cancel a running task.",
		DescriptionZh: "重新触发工单上分配的 agent，或取消正在运行的任务。",
		Ops: []string{
			"POST /api/issues/{id}/rerun",
			"POST /api/issues/{id}/tasks/{taskId}/cancel",
			"POST /api/tasks/{taskId}/cancel",
		},
	},
	{
		Key:           "subscribe_issue",
		Title:         "Subscribe / react to issue",
		Category:      CategoryIssues,
		Description:   "Subscribe or unsubscribe yourself, or add/remove a reaction on an issue.",
		DescriptionZh: "订阅或取消订阅工单，或添加/移除表情回应。",
		Ops: []string{
			"POST /api/issues/{id}/subscribe",
			"POST /api/issues/{id}/unsubscribe",
			"POST /api/issues/{id}/reactions",
			"DELETE /api/issues/{id}/reactions",
		},
	},
	{
		Key:           "manage_labels",
		Title:         "Define / edit labels",
		Category:      CategoryIssues,
		Description:   "Create, rename, or delete a workspace label (distinct from attaching a label to an issue).",
		DescriptionZh: "创建、重命名或删除工作区标签（区别于给工单附加标签）。",
		Ops: []string{
			"POST /api/labels/",
			"PUT /api/labels/{id}/",
			"DELETE /api/labels/{id}/",
		},
	},
	{
		Key:           "manage_issue_recurrence",
		Title:         "Manage issue recurrence",
		Category:      CategoryIssues,
		Description:   "Configure or remove an issue's recurrence (repeat schedule), or manually run a recurrence to spawn the next issue.",
		DescriptionZh: "配置或移除工单的周期（重复计划），或手动运行一次以生成下一个工单。",
		Ops: []string{
			"PUT /api/cerebro/issues/{issueID}/recurrence/",
			"DELETE /api/cerebro/issues/{issueID}/recurrence/",
			"POST /api/cerebro/issue-recurrences/{id}/run",
		},
	},

	// --- Comments -------------------------------------------------------------
	{
		Key:           "add_comment",
		Title:         "Add comment",
		Category:      CategoryComments,
		Description:   "Post a comment (or threaded reply) on an issue.",
		DescriptionZh: "在工单上发表评论（或在串中回复）。",
		Ops: []string{
			"POST /api/issues/{id}/comments",
		},
	},
	{
		Key:           "update_comment",
		Title:         "Edit / delete comment",
		Category:      CategoryComments,
		Description:   "Edit, delete, resolve, react to, or move an existing comment.",
		DescriptionZh: "编辑、删除、解决、回应或移动已有评论。",
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
	{
		Key:           "manage_sessions",
		Title:         "Manage sessions",
		Category:      CategoryComments,
		Description:   "Edit a session, or start a fresh session (with or without handoff) in an issue's comment thread.",
		DescriptionZh: "在工单的评论串中编辑会话，或开启新会话（可选交接）。",
		Ops: []string{
			"PATCH /api/cerebro/issues/{issueId}/sessions/{sessionId}",
			"POST /api/cerebro/issues/{issueId}/sessions/start-fresh",
		},
	},

	// --- Autopilots -----------------------------------------------------------
	{
		Key:           "create_autopilot",
		Title:         "Create / edit autopilot",
		Category:      CategoryAutopilots,
		Description:   "Create, edit, or delete an autopilot and its triggers.",
		DescriptionZh: "创建、编辑或删除 autopilot 及其触发器。",
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
		Key:           "trigger_autopilot",
		Title:         "Trigger autopilot",
		Category:      CategoryAutopilots,
		Surfaced:      true,
		Description:   "Fire an autopilot manually, or replay a past delivery.",
		DescriptionZh: "手动触发 autopilot，或重放一次过往投递。",
		Ops: []string{
			"POST /api/autopilots/{id}/trigger",
			"POST /api/autopilots/{id}/deliveries/{deliveryId}/replay",
		},
	},
	{
		Key:           "autopilot_scope",
		Title:         "Autopilot scope (who may see/edit/trigger)",
		Category:      CategoryAutopilots,
		Description:   "The hardcoded owner/group scope rule deciding who may see, edit, or trigger an autopilot. Today a code check, not on the controllable engine.",
		DescriptionZh: "决定谁可查看、编辑或触发 autopilot 的硬编码所有者/群组范围规则。目前为代码检查，尚未纳入可控引擎。",
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
		DescriptionZh:     "外部调用方通过 webhook URL 触发 autopilot。由每个触发器的 webhook 密钥管控，而非工具策略门。",
		ManagedExternally: true,
		Ops: []string{
			"POST /api/webhooks/autopilots/{token}",
		},
	},

	// --- Artifacts ------------------------------------------------------------
	{
		Key:           "manage_artifacts",
		Title:         "Create / edit / delete artifacts",
		Category:      CategoryArtifacts,
		Description:   "Create, upload, edit, move, re-scope, change folder access of, or delete artifacts, artifact folders, and attachments.",
		DescriptionZh: "创建、上传、编辑、移动、重设范围、更改文件夹访问权限或删除工件、工件文件夹和附件。",
		Ops: []string{
			"POST /api/artifacts/",
			"PUT /api/artifacts/{id}",
			"PUT /api/artifacts/{id}/folder",
			"PUT /api/artifacts/{id}/scope",
			"DELETE /api/artifacts/{id}",
			"POST /api/artifact-folders/",
			"PUT /api/artifact-folders/{id}",
			"PUT /api/artifact-folders/{id}/visibility",
			"DELETE /api/artifact-folders/{id}",
			// FIR-2697: folder suggestions — request one for an artifact, then
			// accept (moves the artifact into the suggested folder) or reject it.
			"POST /api/artifacts/{id}/folder-suggestion",
			"POST /api/artifact-folder-suggestions/{id}/accept",
			"POST /api/artifact-folder-suggestions/{id}/reject",
			"POST /api/artifact-uploads",
			"POST /api/upload-file",
			"DELETE /api/attachments/{id}",
		},
	},
	{
		Key:           "manage_notes",
		Title:         "Create / edit / delete notes",
		Category:      CategoryArtifacts,
		Description:   "Create, edit, delete, pin, change the visibility of, manage references on, comment/suggest on, version, or lock notes (built on the Document feature).",
		DescriptionZh: "创建、编辑、删除、置顶、更改可见性、管理引用、评论/建议、版本管理或锁定笔记（基于 Document 功能）。",
		Ops: []string{
			"POST /api/notes/",
			"PUT /api/notes/{id}",
			"DELETE /api/notes/{id}",
			"PUT /api/notes/{id}/pin",
			"PUT /api/notes/{id}/visibility",
			// FIR-2810: per-note toggle for per-line author-code stamping.
			"PUT /api/notes/{id}/author-codes",
			"POST /api/notes/{id}/references",
			"DELETE /api/notes/{id}/references/{refId}",
			// Wave 3 (TECH-3556): comments + suggestions, versions, edit lock.
			"POST /api/notes/{id}/comments",
			"POST /api/notes/{id}/comments/send",
			"PUT /api/notes/{id}/comments/{commentId}",
			"DELETE /api/notes/{id}/comments/{commentId}",
			"POST /api/notes/{id}/comments/{commentId}/resolve",
			"POST /api/notes/{id}/comments/{commentId}/suggestion",
			"POST /api/notes/{id}/versions",
			"POST /api/notes/{id}/versions/{versionId}/restore",
			"POST /api/notes/{id}/lock",
			"DELETE /api/notes/{id}/lock",
			// FIR-1317: conflict detection + AI merge dialog.
			"POST /api/notes/{id}/merge",
			// FIR-2595: grant a tagged member viewer access to a note
			// (the "Give access & send" choice in the mention flow).
			"POST /api/notes/{id}/mention-access",
		},
	},
	{
		Key:           "manage_note_types",
		Title:         "Create / edit / delete note types",
		Category:      CategoryArtifacts,
		Description:   "Create, edit, delete, or run note types — reusable note templates with recurrence (e.g. business reviews).",
		DescriptionZh: "创建、编辑、删除或运行笔记类型——可复用且可周期的笔记模板（如业务评审）。",
		Ops: []string{
			"POST /api/cerebro/note-types/",
			"PUT /api/cerebro/note-types/{id}",
			"DELETE /api/cerebro/note-types/{id}",
			"POST /api/cerebro/note-types/{id}/run",
		},
	},
	{
		Key:           "manage_entity_folders",
		Title:         "Create / edit / delete skill & autopilot folders",
		Category:      CategoryWorkspace,
		Description:   "Create, rename, delete, and file skills or autopilots into folders (with sub-folders) on the Skills and Autopilots pages (FIR-1412).",
		DescriptionZh: "在 Skills 和 Autopilots 页面创建、重命名、删除文件夹（含子文件夹），并将 skill 或 autopilot 归入其中（FIR-1412）。",
		Ops: []string{
			"POST /api/cerebro/entity-folders/",
			"PUT /api/cerebro/entity-folders/{id}",
			"DELETE /api/cerebro/entity-folders/{id}",
			"PUT /api/cerebro/entity-folder-items/",
		},
	},

	// --- Agents (styring af agenter) -----------------------------------------
	{
		Key:           "create_agent",
		Title:         "Create agent",
		Category:      CategoryAgents,
		Description:   "Create a new agent, from scratch or from a template.",
		DescriptionZh: "从零或基于模板创建新 agent。",
		Ops: []string{
			"POST /api/agents/",
			"POST /api/agents/from-template",
		},
	},
	{
		Key:           "update_agent",
		Title:         "Edit agent",
		Category:      CategoryAgents,
		Description:   "Edit an agent's config, skills, tools, env, archive/restore it, cancel its tasks, or govern its context (propose/review change-requests, roll back, transfer ownership).",
		DescriptionZh: "编辑 agent 的配置、skills、tools、环境变量，归档/恢复，取消其任务，或管理其上下文（提交/审阅变更请求、回滚、转让所有权）。",
		Ops: []string{
			"PUT /api/agents/{id}/",
			"POST /api/agents/{id}/archive",
			"POST /api/agents/{id}/restore",
			"POST /api/agents/{id}/cancel-tasks",
			"PUT /api/agents/{id}/skills",
			"POST /api/agents/{id}/skills/add",
			"PUT /api/agents/{id}/env",
			"PUT /api/agents/{id}/infisical-folders",
			"PUT /api/agents/{id}/tools/{name}/",
			"PUT /api/agents/{id}/tool-overrides/{toolName}",
			"DELETE /api/agents/{id}/tool-overrides/{toolName}",
			// Agent Office — versioning + governance for agent context (FIR-1775),
			// the direct analog of skill governance under manage_skills: editing an
			// agent's context (proposing a change-request, reviewing one, rolling
			// back a version, transferring context ownership) is an edit-agent action.
			"POST /api/agents/{id}/context/change-requests",
			"POST /api/agents/context/change-requests/{crId}/review",
			"POST /api/agents/{id}/context/rollback",
			"PUT /api/agents/{id}/context/ownership",
		},
	},
	{
		Key:           "create_memory",
		Title:         "Use agent memory",
		Category:      CategoryAgents,
		Description:   "Whether a group may use Cognee-backed memory: turn a per-(user,agent) memory read/write toggle on or off. FIR-1794 Gate 3 — default deny, so a group must be explicitly granted this before any member can enable memory for an agent, even when the workspace-wide memory flag is on.",
		DescriptionZh: "群组是否可使用 Cognee 支持的记忆功能：开启或关闭按（用户，agent）设置的记忆读写开关。FIR-1794 Gate 3——默认拒绝，即使工作区级记忆开关已打开，群组也必须被明确授予此能力后，成员才能为某个 agent 启用记忆。",
		Ops: []string{
			"PUT /api/agents/{id}/memory-settings",
		},
		Evidence: []string{
			"server/internal/cerebro/grouppermissions/permissions.go:45",   // CapabilityCreateMemory
			"server/internal/handler/agent_memory_settings_cerebro.go:118", // cerebroRequireCapability("create_memory") on the PUT route
		},
	},
	{
		Key:           "trigger_other_agent",
		Title:         "Trigger someone else's agent",
		Category:      CategoryAgents,
		Surfaced:      true,
		Description:   "Whether you may start an agent you do not own (via an @mention or run-request). FIR-2409: now settable on the controllable engine at workspace / group / member / agent level. With no rule set, the hardcoded default applies (owners/admins master key, members blocked); an explicit Deny removes the master key, an explicit Allow grants a blocked member.",
		DescriptionZh: "是否可启动不属于你的 agent（通过 @提及 或运行请求）。FIR-2409：现可在工作区/群组/成员/agent 层级于可控引擎上设置。未设规则时套用硬编码默认（所有者/管理员持万能钥匙，成员被阻止）；显式 Deny 移除万能钥匙，显式 Allow 放行被阻止的成员。",
		Evidence: []string{
			"server/internal/cerebro/grouppermissions/permissions.go:336", // CanUseAgent (baseline)
			"server/internal/cerebro/mentiongate/gate.go:31",              // CanTriggerMention (now resolves trigger_other_agent via toolpolicy)
		},
	},
	{
		Key:           "schedule_agent_wakeup",
		Title:         "Schedule agent wakeup",
		Category:      CategoryAgents,
		Surfaced:      true,
		Description:   "Create or cancel a scheduled wakeup that starts an agent on an issue later or when a watched event happens.",
		DescriptionZh: "创建或取消计划唤醒，在稍后或监视事件发生时于工单上启动 agent。",
		Ops: []string{
			"POST /api/cerebro/wakeups/",
			"POST /api/cerebro/wakeups/{id}/cancel",
		},
	},
	{
		Key:           "manage_agent_passes",
		Title:         "Manage agent passes",
		Category:      CategoryAgents,
		Description:   "Issue or revoke an agent pass (a scoped, time-boxed permission for an agent to act on an issue).",
		DescriptionZh: "签发或撤销 agent 通行证（一种限定范围、有时限、允许 agent 在工单上行动的权限）。",
		Ops: []string{
			"POST /api/cerebro/agent-passes/",
			"DELETE /api/cerebro/agent-passes/{passId}",
		},
	},
	{
		Key:           "manage_work_sessions",
		Title:         "Drive agent work session",
		Category:      CategoryAgents,
		Description:   "Start, fork, message, resume, rename, or complete an interactive agent work session.",
		DescriptionZh: "启动、分叉、发消息、恢复、重命名或完成交互式 agent 工作会话。",
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
		Key:           "manage_runtime",
		Title:         "Manage runtime",
		Category:      CategoryRuntimes,
		Description:   "Edit, pause, update, delete a runtime (machine), change its sandbox/tool config, or push local skills and models to it.",
		DescriptionZh: "编辑、暂停、更新、删除 runtime（机器），更改其沙箱/工具配置，或向其推送本地 skills 和模型。",
		Ops: []string{
			"PATCH /api/runtimes/{runtimeId}/",
			"DELETE /api/runtimes/{runtimeId}/",
			"POST /api/runtimes/{runtimeId}/pause",
			"POST /api/runtimes/{runtimeId}/unpause",
			"POST /api/runtimes/{runtimeId}/update",
			"POST /api/runtimes/{runtimeId}/archive-agents-and-delete",
			"PATCH /api/runtimes/{runtimeId}/sandbox",
			"PATCH /api/runtimes/{runtimeId}/sandbox-policy",
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
		Key:           "manage_runtime_tool_access",
		Title:         "Grant runtime tool access",
		Category:      CategoryRuntimes,
		Description:   "Grant or revoke a specific runtime tool for a group or user (per-tool allowlist on a machine, or through an agent's assigned runtime).",
		DescriptionZh: "为群组或用户授予或撤销特定 runtime 工具（机器上的按工具白名单，或通过 agent 所分配的 runtime）。",
		Ops: []string{
			"POST /api/agents/{id}/tool-grants",
			"DELETE /api/agents/{id}/tool-grants",
			"POST /api/runtimes/{runtimeId}/tools/{toolName}/groups/{groupId}",
			"DELETE /api/runtimes/{runtimeId}/tools/{toolName}/groups/{groupId}",
			"POST /api/runtimes/{runtimeId}/tools/{toolName}/users/{userId}",
			"DELETE /api/runtimes/{runtimeId}/tools/{toolName}/users/{userId}",
		},
	},
	{
		Key:           "manage_runtime_accounts",
		Title:         "Manage runtime accounts",
		Category:      CategoryRuntimes,
		Description:   "Add, remove, or change controls on the model/provider accounts a runtime uses.",
		DescriptionZh: "添加、移除或更改 runtime 所用的模型/供应商账户的管控。",
		Ops: []string{
			"POST /api/workspaces/{id}/accounts",
			"DELETE /api/workspaces/{id}/accounts/{accountID}",
			"PATCH /api/workspaces/{id}/accounts/{accountID}/controls",
		},
	},
	{
		Key:           "manage_cloud_runtime",
		Title:         "Manage cloud runtime nodes",
		Category:      CategoryRuntimes,
		Description:   "Provision, start, stop, reboot, delete, or exec on cloud runtime nodes.",
		DescriptionZh: "配置、启动、停止、重启、删除云 runtime 节点，或在其上执行命令。",
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
		Key:           "create_runtime",
		Title:         "Create runtime",
		Category:      CategoryRuntimes,
		Description:   "Register a new runtime (machine). A named group capability an admin can grant or deny today; no HTTP route — runtimes are created via the daemon — so it is settable only as a non-route capability. The runtime twin of create_agent.",
		DescriptionZh: "注册新 runtime（机器）。管理员目前可授予或拒绝的命名群组能力；无 HTTP 路由——runtime 通过守护进程创建——故仅可作为非路由能力设置。create_agent 的 runtime 对应物。",
		Evidence: []string{
			"server/internal/handler/group_permissions_cerebro.go:150",    // cerebroRequireCapability("create_runtime")
			"server/internal/cerebro/grouppermissions/permissions.go:295", // CanCreateRuntime
		},
	},
	{
		Key:           "create_local_runtime",
		Title:         "Create local runtime",
		Category:      CategoryRuntimes,
		Description:   "Register a new LOCAL (daemon-based) runtime that runs on the user's own machine. Settable Allow/Ask/Deny on every layer; enforced live at the runtime setup-token mint through the unified tool-policy chain (workspace > group > user). The local twin of manage_cloud_runtime (FIR-2672).",
		DescriptionZh: "注册运行在用户本机上的新本地（基于守护进程）runtime。各层级均可设置 Allow/Ask/Deny；在 runtime 设置令牌签发时经统一工具策略链实时强制（工作区 > 群组 > 用户）。manage_cloud_runtime 的本地对应物（FIR-2672）。",
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
		Key:           "use_other_runtime",
		Title:         "Use someone else's runtime",
		Category:      CategoryRuntimes,
		Description:   "Whether you may point an agent at a runtime you do not own. Today a hardcoded group allowlist, NOT on the controllable engine — the runtime twin of trigger_other_agent.",
		DescriptionZh: "是否可将 agent 指向不属于你的 runtime。目前为硬编码群组白名单，尚未纳入可控引擎——trigger_other_agent 的 runtime 对应物。",
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
		DescriptionZh:     "本地守护进程上报心跳、领取任务并提交结果。由守护进程令牌管控，而非工具策略门。",
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
		Key:           "manage_group",
		Title:         "Create / edit group",
		Category:      CategoryGroups,
		Description:   "Create, edit, or delete a group, and grant or revoke a group's capabilities.",
		DescriptionZh: "创建、编辑或删除群组，并授予或撤销群组能力。",
		Ops: []string{
			"POST /api/workspaces/{id}/groups",
			"PATCH /api/groups/{id}",
			"DELETE /api/groups/{id}",
			"POST /api/groups/{id}/capabilities",
			"DELETE /api/groups/{id}/capabilities/{capability}",
		},
	},
	{
		Key:           "manage_group_members",
		Title:         "Manage group members",
		Category:      CategoryGroups,
		Description:   "Add or remove users, agents, and runtimes from a group.",
		DescriptionZh: "在群组中添加或移除用户、agent 和 runtime。",
		Ops: []string{
			"POST /api/groups/{id}/members",
			"DELETE /api/groups/{id}/members/{userId}",
			"POST /api/groups/{id}/agents",
			"DELETE /api/groups/{id}/agents/{agentId}",
			"POST /api/groups/{id}/runtimes",
			"DELETE /api/groups/{id}/runtimes/{runtimeId}",
		},
	},

	// --- Permissions (styring af roller/policy) -------------------------------
	{
		Key:           "manage_collections",
		Title:         "Manage Collections folder access",
		Category:      CategoryPermissions,
		Description:   "Set or remove who may see a Collections folder (and everything inside it) — the per-folder access grants behind Settings → Collections, across Documents, Notes, Autopilots, and Skills (FIR-1590).",
		DescriptionZh: "设置或移除谁可查看 Collections 文件夹（及其内全部内容）——Settings → Collections 背后的按文件夹访问授权，涵盖 Documents、Notes、Autopilots 和 Skills（FIR-1590）。",
		Ops: []string{
			"PUT /api/cerebro/folder-grants/",
			"DELETE /api/cerebro/folder-grants/",
		},
	},
	{
		Key:           "manage_roles",
		Title:         "Manage roles",
		Category:      CategoryPermissions,
		Description:   "Create, edit, or delete roles, and assign or unassign a role to a member, agent, or group.",
		DescriptionZh: "创建、编辑或删除角色，并为成员、agent 或群组分配或取消角色。",
		Ops: []string{
			"POST /api/workspaces/{id}/roles",
			"PATCH /api/roles/{id}",
			"DELETE /api/roles/{id}",
			"POST /api/roles/{id}/assignments",
			"DELETE /api/roles/{id}/assignments/{subjectType}/{subjectId}",
		},
	},
	{
		Key:           "manage_tool_policy",
		Title:         "Manage tool policy",
		Category:      CategoryPermissions,
		Description:   "Set or clear an Allow/Ask/Deny choice in the permission table itself.",
		DescriptionZh: "在权限表中设置或清除 Allow/Ask/Deny 选择。",
		Ops: []string{
			"PUT /api/workspaces/{id}/tool-policy",
			"DELETE /api/workspaces/{id}/tool-policy",
		},
	},
	{
		Key:           "manage_group_overrides",
		Title:         "Manage group permissions",
		Category:      CategoryPermissions,
		Description:   "Author another member's own (User-layer) tool-policy or Connections access row — including an Allow that overrides a workspace-level Deny — for members who share a group with you. A WHEN condition on this permission (argument group_id) limits it to specific group(s). Can never be used on your own access. Workspace owners and admins are not limited by this: they can always change any member's access, including their own (FIR-2351).",
		DescriptionZh: "为与你共享群组的其他成员，编写其个人（User 层）工具策略或 Connections 访问规则——包括覆盖工作区级 Deny 的 Allow。此权限上的 WHEN 条件（参数 group_id）可将其限定到特定群组。永远不能用于你自己的访问权限。工作区所有者和管理员不受此限制：他们始终可以更改任何成员的访问权限，包括自己的（FIR-2351）。",
		Evidence: []string{
			"server/internal/handler/group_permissions_cerebro.go:433", // cerebroRequireDelegatedOverridePolicy
			"server/internal/cerebro/toolpolicy/override_grant.go:40",  // CanAuthorDelegatedOverride
		},
	},
	{
		Key:           "manage_workspace_overrides",
		Title:         "Manage permissions",
		Category:      CategoryPermissions,
		Description:   "Author tool-policy or Connections access rows at the User, Group, and Agent layers, for anyone and any agent in the workspace — including an Allow that overrides a workspace-level Deny. Workspace/runtime defaults themselves stay owner/admin-only. Can never be used on your own access. Workspace owners and admins are not limited by this: they can always change any member's access, including their own (FIR-2351).",
		DescriptionZh: "为工作区内任何成员和任何代理，编写 User、Group 和 Agent 层的工具策略或 Connections 访问规则——包括覆盖工作区级 Deny 的 Allow。工作区/运行时默认值本身仍仅限所有者/管理员修改，且永远不能用于你自己的访问权限。工作区所有者和管理员不受此限制：他们始终可以更改任何成员的访问权限，包括自己的（FIR-2351）。",
		Evidence: []string{
			"server/internal/handler/group_permissions_cerebro.go:433", // cerebroRequireDelegatedOverridePolicy
			"server/internal/cerebro/toolpolicy/override_grant.go:40",  // CanAuthorDelegatedOverride
		},
	},
	{
		Key:           "manage_agent_vault_access",
		Title:         "Manage Agent Vault access",
		Category:      CategoryPermissions,
		Description:   "Grant or revoke a private agent's access to an Agent Vault secret box (TECH-3196).",
		DescriptionZh: "授予或撤销私有 agent 对某个 Agent Vault 密钥箱的访问（TECH-3196）。",
		Ops: []string{
			"PUT /api/workspaces/{id}/agentvault/access",
			"DELETE /api/workspaces/{id}/agentvault/access",
		},
	},
	{
		Key:           "decide_approval",
		Title:         "Decide approval request",
		Category:      CategoryPermissions,
		Description:   "Raise, approve, reject, or delegate an approval request in the inbox.",
		DescriptionZh: "在收件箱中发起、批准、拒绝或委派审批请求。",
		Ops: []string{
			"POST /api/workspaces/{id}/approvals/intake",
			"POST /api/workspaces/{id}/approvals/{approvalId}/approve",
			"POST /api/workspaces/{id}/approvals/{approvalId}/reject",
			"POST /api/workspaces/{id}/approvals/{approvalId}/delegate",
		},
	},

	// --- Projects -------------------------------------------------------------
	{
		Key:           "manage_project",
		Title:         "Create / edit project",
		Category:      CategoryProjects,
		Description:   "Create, edit, or delete a project and its resources.",
		DescriptionZh: "创建、编辑或删除项目及其资源。",
		Ops: []string{
			"POST /api/projects/",
			"PUT /api/projects/{id}/",
			"DELETE /api/projects/{id}/",
			"PUT /api/projects/{id}/parent",
			"PUT /api/projects/reorder",
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
		DescriptionZh:     "添加/移除项目成员和群组访问。由项目自身的访问列表管控，而非工具策略门。",
		ManagedExternally: true,
		Ops: []string{
			"PATCH /api/projects/{id}/access",
			"POST /api/projects/{id}/members",
			"DELETE /api/projects/{id}/members/{userId}",
			"POST /api/projects/{id}/group-access",
			"DELETE /api/projects/{id}/group-access/{groupId}",
			"PUT /api/cerebro/project-grants/",
			"DELETE /api/cerebro/project-grants/",
		},
	},
	{
		Key:           "manage_status_models",
		Title:         "Manage status models",
		Category:      CategoryProjects,
		Description:   "Create, edit, or delete the custom status model for the workspace or a project.",
		DescriptionZh: "为工作区或项目创建、编辑或删除自定义状态模型。",
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
		Key:           "manage_project_sprints",
		Title:         "Manage project sprints",
		Category:      CategoryProjects,
		Description:   "Create, edit, complete, delete, and assign project sprints and recurring sprint tasks.",
		DescriptionZh: "创建、编辑、完成、删除并分配项目冲刺与周期性冲刺任务。",
		Ops: []string{
			"PUT /api/cerebro/projects/{projectID}/sprint-settings/",
			"DELETE /api/cerebro/projects/{projectID}/sprint-settings/",
			"POST /api/cerebro/projects/{projectID}/sprints/",
			"POST /api/cerebro/projects/{projectID}/sprint-sweep",
			"POST /api/cerebro/projects/{projectID}/sprint-recurring-tasks/",
			"PUT /api/cerebro/sprint-recurring-tasks/{id}/",
			"DELETE /api/cerebro/sprint-recurring-tasks/{id}/",
			"POST /api/cerebro/sprints/{sprintID}/complete",
			"PUT /api/cerebro/sprints/{sprintID}/",
			"DELETE /api/cerebro/sprints/{sprintID}/",
			"PUT /api/cerebro/issues/{issueID}/sprint/",
		},
	},

	// --- Workspace (medlemmer, settings, integrationer) ----------------------
	{
		Key:           "manage_workspace_members",
		Title:         "Manage workspace members",
		Category:      CategoryWorkspace,
		Description:   "Invite or remove members, change a member's role/budget, set a member's secret folders, or revoke an invitation.",
		DescriptionZh: "邀请或移除成员，更改成员角色/预算，设置成员的密钥文件夹，或撤销邀请。",
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
		Key:           "manage_workspace_settings",
		Title:         "Edit workspace settings",
		Category:      CategoryWorkspace,
		Description:   "Change workspace settings: name/config, feature flags, cost optimization, auth settings, and pausing all tasks.",
		DescriptionZh: "更改工作区设置：名称/配置、功能开关、成本优化、认证设置，以及暂停所有任务。",
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
			"PUT /api/workspaces/{id}/wakeup-settings",
			"PUT /api/workspaces/{id}/issue-start-settings",
			"PUT /api/workspaces/{id}/web-fetch-policy",
			"PUT /api/cerebro/workspaces/{id}/auth-settings/",
			"POST /api/workspaces/{id}/pause-tasks",
			"POST /api/workspaces/{id}/generate-logo",
		},
	},
	{
		Key:           "manage_analytics",
		Title:         "Use and manage analytics",
		Category:      CategoryWorkspace,
		Description:   "Query workspace analytics, rebuild the analytics projection, and create, update, or delete saved visuals.",
		DescriptionZh: "查询工作区分析、重建分析投影，以及创建、更新或删除已保存的可视化。",
		Ops: []string{
			"POST /api/analytics/query",
			"POST /api/analytics/backfill",
			"POST /api/analytics/visuals",
			"PUT /api/analytics/visuals/{visualId}",
			"DELETE /api/analytics/visuals/{visualId}",
		},
	},
	{
		Key:           "delete_workspace",
		Title:         "Delete workspace",
		Category:      CategoryWorkspace,
		Description:   "Permanently delete the entire workspace. Owner-only and irreversible.",
		DescriptionZh: "永久删除整个工作区。仅所有者可执行且不可逆。",
		Ops: []string{
			"DELETE /api/workspaces/{id}/",
		},
	},
	{
		Key:           "manage_integrations",
		Title:         "Manage integrations",
		Category:      CategoryWorkspace,
		Description:   "Connect or disconnect external integrations (e.g. a GitHub App installation).",
		DescriptionZh: "连接或断开外部集成（如 GitHub App 安装）。",
		Ops: []string{
			"DELETE /api/workspaces/{id}/github/installations/{installationId}",
			"DELETE /api/workspaces/{id}/lark/installations/{installationId}",
			"POST /api/workspaces/{id}/lark/install/begin",
		},
	},
	{
		Key:           "manage_model_registry",
		Title:         "Manage model registry",
		Category:      CategoryWorkspace,
		Description:   "Propose, review, approve, and roll back changes to the single-source model registry (prices, context windows, display labels) under Settings → Model registry (FIR-2698). The direct analog of manage_skills: reviewing/approving is additionally gated in-handler to the registry owner, a named approver, or a workspace admin (modelregistry/handler.go canManage).",
		DescriptionZh: "在 Settings → Model registry 下提交、审阅、批准和回滚对单一来源模型注册表（价格、上下文窗口、显示名称）的更改（FIR-2698）。与 manage_skills 直接类似：审阅/批准操作在处理程序内额外限定为注册表所有者、指定审批人或工作区管理员（modelregistry/handler.go canManage）。",
		Ops: []string{
			"POST /api/model-registry/change-requests",
			"POST /api/model-registry/change-requests/{crId}/review",
			"POST /api/model-registry/rollback",
			"PUT /api/model-registry/ownership",
		},
	},

	// --- Skills ---------------------------------------------------------------
	{
		Key:           "manage_skills",
		Title:         "Create / edit / delete skills",
		Category:      CategorySkills,
		Description:   "Create, import, edit, fork, delete skills and their files, review change-requests, transfer skill ownership, and record behavioral observations.",
		DescriptionZh: "创建、导入、编辑、分叉、删除 skills 及其文件，审阅变更请求，转让 skill 所有权，并记录行为观察。",
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
			// CEREBRO-PATCH(skill-autolearn-catalog): TECH-3692 — toggle a skill's self-learning switch.
			"POST /api/skills/{id}/auto-learn",
		},
	},

	// --- Squads ---------------------------------------------------------------
	{
		Key:           "manage_squad",
		Title:         "Create / edit squad",
		Category:      CategorySquads,
		Description:   "Create, edit, or delete a squad, manage its members, and change a member's role.",
		DescriptionZh: "创建、编辑或删除小队，管理其成员并更改成员角色。",
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
		Key:           "manage_connections",
		Title:         "Manage workspace connections",
		Category:      CategoryConnections,
		Description:   "Create, edit, or delete workspace API/MCP connections (external URLs and internal Sliplane paths).",
		DescriptionZh: "创建、编辑或删除工作区 API/MCP 连接（外部 URL 和内部 Sliplane 路径）。",
		Ops: []string{
			"POST /api/workspaces/{id}/mcp",
			"POST /api/workspaces/{id}/connections",
			"PUT /api/workspaces/{id}/connections/{connId}",
			"DELETE /api/workspaces/{id}/connections/{connId}",
		},
	},

	// --- Credentials ----------------------------------------------------------
	{
		Key:           "manage_credentials",
		Title:         "Manage credentials / secrets",
		Category:      CategoryCredentials,
		Description:   "Create, edit, delete, reveal, or rotate workspace credentials and their bindings. Highly sensitive.",
		DescriptionZh: "创建、编辑、删除、显示或轮换工作区凭据及其绑定。高度敏感。",
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
	{
		Key:           "manage_credential_access",
		Title:         "Grant credential access",
		Category:      CategoryCredentials,
		Description:   "Grant, deny, or clear an actor's (agent / group / user) access to a specific credential at a given layer (FIR-1479). The credential twin of \"Grant runtime tool access\". Highly sensitive.",
		DescriptionZh: "在指定层级授予、拒绝或清除某主体（agent/群组/用户）对特定凭据的访问（FIR-1479）。即「Grant runtime tool access」的凭据对应物。高度敏感。",
		Evidence: []string{
			"server/internal/handler/group_permissions_cerebro.go:308", // cerebroRequireCredentialGrantPolicy
			"server/internal/handler/group_permissions_cerebro.go:393", // RequireToolPolicyWritePolicy (credential.* branch)
		},
	},

	// --- Workflows ------------------------------------------------------------
	{
		Key:           "manage_workflows",
		Title:         "Manage workflows",
		Category:      CategoryWorkflows,
		Description:   "Create, edit, delete, toggle a workflow, or regenerate its tokens and signing secrets.",
		DescriptionZh: "创建、编辑、删除、启停工作流，或重新生成其令牌和签名密钥。",
		Ops: []string{
			"POST /api/cerebro/workflows/",
			"PUT /api/cerebro/workflows/{id}",
			"DELETE /api/cerebro/workflows/{id}",
			"POST /api/cerebro/workflows/{id}/toggle",
			"POST /api/cerebro/workflows/{id}/activate",
			"POST /api/cerebro/workflows/{id}/human-checks/{checkId}/approve",
			"POST /api/cerebro/workflows/{id}/regenerate-outbound-secret",
			"POST /api/cerebro/workflows/{id}/regenerate-signing-secret",
			"POST /api/cerebro/workflows/{id}/regenerate-token",
			"POST /api/cerebro/workflows/_test/cron-sweep",
		},
	},

	// --- Channels -------------------------------------------------------------
	{
		Key:           "manage_channels",
		Title:         "Manage channels",
		Category:      CategoryChannels,
		Description:   "Create, delete or archive a channel, set its per-channel permissions, and set an agent's listen mode in it.",
		DescriptionZh: "创建、删除或归档频道，设置其按频道权限，并设置 agent 在其中的监听模式。",
		Ops: []string{
			"POST /api/channels/",
			"POST /api/channels/{id}/archive",
			"DELETE /api/channels/{id}/archive",
			"DELETE /api/channels/{id}", // TECH-3758 — delete a channel for everyone (creator/admin/owner).
			"PUT /api/channels/{id}/permissions",
			"PUT /api/channels/{id}/agents/{agentId}/listen-mode",
		},
	},
	{
		Key:               "gateway_channel_delivery",
		Title:             "Gateway inbound channel message",
		Category:          CategoryChannels,
		Description:       "The Firtal Gateway delivering a webhook into a channel on behalf of a chosen principal (FIR-1766). Governed by the gateway service token plus the acting principal's channel membership, not the tool-policy gate.",
		DescriptionZh:     "Firtal Gateway 代表选定主体将 webhook 投递到频道（FIR-1766）。由网关服务令牌加上执行主体的频道成员资格管控，而非工具策略门。",
		ManagedExternally: true,
		Ops: []string{
			"POST /api/webhooks/gateway/channel-message",
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
		DescriptionZh:     "查看工单、评论及其历史。由工作区成员资格管控，而非工具策略门。",
		ManagedExternally: true,
		Evidence:          []string{"server/internal/middleware/workspace.go (RequireWorkspaceMember)"},
	},
	{
		Key:               "read_projects",
		Title:             "Read projects",
		Category:          CategoryReadAccess,
		Description:       "View projects and their contents. Governed by the project access list (and a group-membership path), not the tool-policy gate.",
		DescriptionZh:     "查看项目及其内容。由项目访问列表（及群组成员路径）管控，而非工具策略门。",
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
	"POST /api/workspaces/":                                  "self_only — create-workspace is pre-workspace; any authenticated user, no workspace context to gate within",
	"POST /api/workspaces/{id}/leave":                        "self_only — leaving is the caller's own membership",
	"POST /api/channels/{id}/leave":                          "self_only — TECH-3758, leaving a channel removes the caller's own subscription",
	"POST /api/workspaces/{id}/connections/test":             "admin_only — validates a connection config for the current workspace; not an agent runtime tool",
	"POST /api/workspaces/{id}/cerebro/copy":                 "admin_only — owner/admin-gated workspace copy (TECH-3582); RequireWorkspaceRoleFromURL, not wired to the tool-policy engine, no agent runtime tool equivalent",
	"POST /api/inbox/add-issue":                              "self_only — caller pinning any issue into their own inbox",
	"DELETE /api/me/profile":                                 "self_only — caller's own profile",
	"PATCH /api/me":                                          "self_only — caller's own profile",
	"PUT /api/me/profile":                                    "self_only — caller's own profile",
	"PATCH /api/me/onboarding":                               "self_only — caller's own onboarding",
	"PATCH /api/me/preferences":                              "self_only — caller's own preferences",
	"POST /api/me/onboarding/complete":                       "self_only — caller's own onboarding",
	"POST /api/me/onboarding/cloud-waitlist":                 "self_only — caller's own onboarding",
	"POST /api/me/onboarding/no-runtime-bootstrap":           "self_only — caller's own onboarding",
	"POST /api/me/onboarding/runtime-bootstrap":              "self_only — caller's own onboarding",
	"PUT /api/notification-preferences/":                     "self_only — caller's own notification prefs",
	"POST /api/feedback":                                     "self_only — caller submitting feedback",
	"POST /api/cli-token":                                    "self_only — caller's own CLI token",
	"POST /api/tokens/":                                      "self_only — caller's own API token",
	"POST /api/tokens/current/renew":                         "self_only — caller's own API token",
	"DELETE /api/tokens/{id}":                                "self_only — caller's own API token",
	"POST /api/pins/":                                        "self_only — caller's own pins",
	"PUT /api/pins/reorder":                                  "self_only — caller's own pins",
	"DELETE /api/pins/{itemType}/{itemId}":                   "self_only — caller's own pins",
	"POST /api/push/subscribe":                               "self_only — caller's own push subscription",
	"POST /api/push/unsubscribe":                             "self_only — caller's own push subscription",
	"POST /api/inbox/archive-all":                            "self_only — caller's own inbox",
	"POST /api/inbox/archive-all-read":                       "self_only — caller's own inbox",
	"POST /api/inbox/archive-completed":                      "self_only — caller's own inbox",
	"POST /api/inbox/mark-all-read":                          "self_only — caller's own inbox",
	"POST /api/inbox/notifications/archive-all":              "self_only — caller's own inbox",
	"POST /api/inbox/notifications/mark-all-read":            "self_only — caller's own inbox",
	"POST /api/inbox/reminders":                              "self_only — caller's own inbox",
	"POST /api/cerebro/reminders/":                           "self_only — caller's own reminders (FIR-394)",
	"POST /api/cerebro/reminders/{id}/done":                  "self_only — caller's own reminders (FIR-394)",
	"POST /api/cerebro/reminders/{id}/snooze":                "self_only — caller's own reminders (FIR-394)",
	"DELETE /api/cerebro/reminders/{id}":                     "self_only — caller's own reminders (FIR-394)",
	"POST /api/cerebro/rounds/":                              "self_only — caller's own inbox rounds (FIR-2736)",
	"PATCH /api/cerebro/rounds/{roundId}/":                   "self_only — caller's own inbox rounds (FIR-2736)",
	"DELETE /api/cerebro/rounds/{roundId}/":                  "self_only — caller's own inbox rounds (FIR-2736)",
	"POST /api/cerebro/rounds/{roundId}/members":             "self_only — caller's own inbox rounds (FIR-2736)",
	"DELETE /api/cerebro/rounds/{roundId}/members/{issueId}": "self_only — caller's own inbox rounds (FIR-2736)",
	"POST /api/cerebro/rounds/{roundId}/start":               "self_only — caller's own inbox rounds (FIR-2736)",
	"POST /api/cerebro/rounds/{roundId}/dismiss":             "self_only — caller's own inbox rounds (FIR-3114)",
	"POST /api/inbox/{id}/archive":                           "self_only — caller's own inbox",
	"POST /api/inbox/{id}/mute":                              "self_only — caller's own inbox",
	"DELETE /api/inbox/{id}/mute":                            "self_only — caller's own inbox",
	"POST /api/inbox/{id}/read":                              "self_only — caller's own inbox",
	"POST /api/inbox/{id}/unread":                            "self_only — caller's own inbox",
	"POST /api/inbox/{id}/unarchive":                         "self_only — caller's own inbox",
	"POST /api/inbox/{id}/run-private-agent":                 "self_only — caller running their own private agent from their inbox",

	// cerebro focus-list — personal task queue, caller's own items only.
	"POST /api/cerebro/focus-list/":            "self_only — caller's own focus list",
	"PATCH /api/cerebro/focus-list/{id}":       "self_only — caller's own focus list",
	"DELETE /api/cerebro/focus-list/{id}":      "self_only — caller's own focus list",
	"POST /api/cerebro/focus-list/{id}/done":   "self_only — caller's own focus list",
	"POST /api/cerebro/focus-list/{id}/snooze": "self_only — caller's own focus list",
	"POST /api/cerebro/focus-list/reorder":     "self_only — caller's own focus list",

	// cerebro saved-filters — personal saved issue filters, caller's own only;
	// loadOwned enforces owner identity so a member can never touch another
	// member's filter (FIR-1659).
	"POST /api/cerebro/saved-filters/":       "self_only — caller's own saved filters (FIR-1659)",
	"PATCH /api/cerebro/saved-filters/{id}":  "self_only — caller's own saved filters (FIR-1659)",
	"DELETE /api/cerebro/saved-filters/{id}": "self_only — caller's own saved filters (FIR-1659)",

	// cerebro dictation — one-shot audio→text proxy; self-authenticates via the
	// workspace token, persists no governable state, no agent tool equivalent.
	"POST /api/workspaces/{id}/cerebro/dictation/transcribe": "transcription-proxy — stateless audio→text, self-authenticates via workspace token, persists no governable state (FIR-1637)",
	"POST /api/workspaces/{id}/cerebro/dictation/warmup":     "transcription-proxy — stateless warm-up of the audio→text backend, self-authenticates via workspace token, persists no governable state (FIR-1637)",

	// cerebro interactive terminal — human watch/take-over of a live agent
	// session. Workspace-member gated UI action, not an agent-governable
	// capability; there is no agent tool that opens or closes a PTY session.
	"POST /api/cerebro/terminal/sessions":               "interactive-terminal — human opens a live agent session to watch/take over; UI-only, no agent tool equivalent",
	"DELETE /api/cerebro/terminal/sessions/{sessionId}": "interactive-terminal — human closes a live agent session; UI-only, no agent tool equivalent",

	// agent tool execution — server-side tool executor for external runtimes
	// (TECH-3226). Per-tool authorization is enforced inside the executor via the
	// grant cascade (GetCascadeEnabledToolsForAgent → 403 ErrToolNotPermitted),
	// not by gating this route; the route is workspace-member + own-agent scoped.
	"POST /api/agents/{id}/tools/{name}/invoke": "tool-execution — per-tool authorization is enforced by the executor grant cascade (TECH-3226), not by gating the invoke route; route is workspace-member + own-agent scoped",

	"POST /api/invitations/{id}/accept":             "self_only — caller accepting their own invitation",
	"POST /api/invitations/{id}/decline":            "self_only — caller declining their own invitation",
	"POST /api/channels/{id}/read":                  "self_only — caller marking a channel read",
	"POST /api/channels/{id}/threads/{rootId}/read": "self_only — caller marking one channel/DM thread read (FIR-1854)",
	"POST /api/channels/{id}/unread":                "self_only — caller marking a channel unread in their own inbox (TECH-3352)",
	"POST /api/channels/{id}/mute":                  "self_only — caller snoozing a channel in their own inbox (TECH-3352)",
	"DELETE /api/channels/{id}/mute":                "self_only — caller un-snoozing a channel in their own inbox (TECH-3352)",
	"POST /api/channels/{id}/convert":               "participant-action — any participant of a group may promote it to a named channel (FIR-2159); gated by group membership in the handler, not the manage_channels capability — naming is a deliberate per-participant step in the Slack model, not an admin action",
	"POST /api/cerebro/channels/{id}/typing":        "self_only — caller broadcasting their own typing indicator to a channel (TECH-3422); no capability gate needed",
	"POST /api/cerebro/presence/ping":               "self_only — caller pinging their own presence heartbeat (TECH-3422); no capability gate needed",
	"POST /api/lark/binding/redeem":                 "self_only — caller binding their own Lark open_id to their Multica account",

	// chat sessions — the caller's own AI chat, not an admin-governed action.
	"POST /api/chat/sessions/":                             "personal-chat — caller's own AI chat session",
	"PATCH /api/chat/sessions/{sessionId}/":                "personal-chat — caller's own AI chat session",
	"DELETE /api/chat/sessions/{sessionId}/":               "personal-chat — caller's own AI chat session",
	"POST /api/chat/sessions/{sessionId}/messages":         "personal-chat — caller's own AI chat session",
	"POST /api/chat/sessions/{sessionId}/agent-message":    "personal-chat — agent-initiated reply in the caller's own chat session; agent auth enforced in handler", // CEREBRO-PATCH(chat-agent-reply): TECH-3183
	"POST /api/chat/sessions/{sessionId}/read":             "personal-chat — caller's own AI chat session",
	"POST /api/chat/sessions/{sessionId}/mute":             "personal-chat — caller snoozing their own AI chat session (TECH-3352)",
	"DELETE /api/chat/sessions/{sessionId}/mute":           "personal-chat — caller un-snoozing their own AI chat session (TECH-3352)",
	"POST /api/chat/sessions/{sessionId}/unread":           "personal-chat — caller marking their own AI chat session unread (TECH-3352)",
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
	"POST /api/daemon/tasks/{taskId}/skill-usage":                                  "daemon-token — runtime daemon callback",
	"POST /api/daemon/tasks/{taskId}/wait-local-directory":                         "daemon-token — runtime daemon callback",
	"POST /api/daemon/workspaces/{workspaceId}/repo/check":                         "daemon-token — runtime daemon callback",
	"POST /api/daemon/workspaces/{workspaceId}/tool-policy/resolve":                "daemon-token — runtime daemon callback; the local-runtime per-tool resolve seam itself runs the tool-policy gate internally (TECH-3173)",

	// personal browser — the per-action resolve seam for the tools:personal-browser
	// capability. The capability itself is a reported runtime tool (surfaced and set
	// Allow/Ask/Deny via the tool-policy table on every layer), not a platform action;
	// this agent-only (mat_ token) route just runs the tool-policy chain internally
	// with Base=Deny and the request host, so it is the enforcement point, not a
	// separately-governable platform capability (FIR-2037).
	"POST /api/cerebro/personal-browser/authorize": "resolve-seam — agent-only per-action gate for the tools:personal-browser tool; runs the tool-policy chain internally with Base=Deny + host condition (FIR-2037), the enforcement point itself, not a separately-governable platform action",

	// connection-tools — agent-only (mat_ token) dispatch of an api-type workspace
	// connection as a server-side tool (FIR-2273). Per-endpoint authorization is
	// enforced inside the handler by the tool-policy chain with Base=Deny on
	// connection:<id>, behind the default-off cerebro_api_connection_tools flag —
	// the route is the enforcement point itself, not a separately-governable
	// platform capability (the connection:<id> tools are surfaced and set
	// Allow/Ask/Deny per layer in the tool-policy table, like other runtime tools).
	"POST /api/cerebro/connection-tools/call": "resolve-seam — agent-only per-action dispatch of an api-type connection tool; runs the tool-policy chain internally with Base=Deny on connection:<id> behind the cerebro_api_connection_tools flag (FIR-2273), the enforcement point itself, not a separately-governable platform action",

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
	"POST /api/agents/generate-avatar":               "cosmetic — avatar image generation",
	"POST /api/agents/{id}/generate-avatar":          "cosmetic — per-agent async avatar image generation",
	"POST /api/agents/backfill-avatars":              "cosmetic — avatar backfill maintenance",
	"POST /api/capabilities/report":                  "runtime-self-report — a runtime reporting its own tools, not a user action",
	"POST /api/workspaces/{id}/cerebro/test-as-user": "read-only — resolves another user+agent's tool verdict (Test as user); reads policy, changes no state; gated in-handler by tools:test-as-user",
	"POST /api/agents/context/lint/repo-file":        "read-only — drift lint of a repo CLAUDE.md/AGENTS.md's content posted in the body (FIR-1775 Phase 3); pure analysis, changes no state",
	"POST /api/issues/{id}/squad-evaluated":          "system-callback — squad-evaluation marker set by the platform, not a user action",
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

// SurfacedKeys returns the keys marked Surfaced (the agent-start family), in
// catalog order. These are the capabilities the Permissions table shows behind
// the light agent-start gate, without opening the whole platform catalog
// (FIR-3091 slice 4).
func SurfacedKeys() []string {
	var out []string
	for _, c := range catalog {
		if c.Surfaced {
			out = append(out, c.Key)
		}
	}
	return out
}

// notEnforcedKeys is the closed set of capability keys that are Surfaced in the
// Permissions table but whose stored setting does NOT yet gate anything at
// runtime — "surfaced but not yet wired". Today these are the three agent-start
// family keys added as Surfaced=true (FIR-3091 slice 4 / FIR-2409): the table
// shows and lets an admin author them, but only trigger_other_agent is actually
// read by the gate, so the other three are cosmetic until their gate lands. See
// the Surfaced doc + SurfacedKeys() above for the surfacing side.
//
// Kept as an explicit exception list, not a per-entry flag, so the DEFAULT is
// "enforced" (Jesper's rule, FIR-3091 punkt 8): every permission is enforced
// unless it is one of these known-unwired keys, and a new capability is enforced
// the moment it is added without touching every catalog entry.
var notEnforcedKeys = map[string]bool{
	"rerun_issue":           true,
	"schedule_agent_wakeup": true,
	"trigger_autopilot":     true,
}

// Enforced reports whether a policy setting authored on this tool_key is actually
// read by the runtime gate today (FIR-3091 punkt 8). It backs the per-permission
// detail page's "is this live or cosmetic" signal: true for every key except the
// known "surfaced but not yet wired" exceptions in notEnforcedKeys. An unknown
// key returns true (enforced by default), matching Jesper's rule that a
// capability is enforced unless explicitly listed otherwise.
func Enforced(key string) bool {
	return !notEnforcedKeys[key]
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
