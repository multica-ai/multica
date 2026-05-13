package workflows

// Trigger and action types are stored as TEXT in the DB so we can extend the
// catalog without migrations. The check constraints in 9020_cerebro_workflows
// (phase 1), 9021_cerebro_workflows_phase2, and 9022_cerebro_workflows_phase3
// pin the currently-known values.
const (
	TriggerStatusChanged  = "status_changed"
	TriggerDueDateReached = "due_date_reached"
	TriggerDueTimeReached = "due_time_reached"

	// Phase-3 triggers (JEH-1108).
	TriggerCron            = "cron"
	TriggerWebhookInbound  = "webhook_inbound"
	TriggerCommentMention  = "comment_mention"
	TriggerAllChildrenDone = "all_children_done"
	TriggerSubIssueCreated = "sub_issue_created"

	ActionSetStatus      = "set_status"
	ActionCreateSubIssue = "create_sub_issue"
	ActionSendReminder   = "send_reminder"

	// Phase-2 actions (JEH-1103).
	ActionRunSkill       = "run_skill"
	ActionCommentOnIssue = "comment_on_issue"

	// Phase-2 extension actions (JEH-1114).
	ActionRouteByDomain = "route_by_domain"

	// Phase-3 actions (JEH-1108). webhook_outbound is whitelisted by the
	// CHECK constraint and the handler validator in this PR, but the action
	// runner returns ErrUnimplementedAction until PR 2 ships the outbound
	// HTTP delivery worker.
	ActionWebhookOutbound = "webhook_outbound"
	ActionReassignIssue   = "reassign_issue"
)

// Domain values produced by the route_by_domain classifier. The label that
// gets attached is `<LabelPrefix><domain>` (default prefix: "domain:").
const (
	DomainCode     = "code"
	DomainBusiness = "business"
	DomainDesign   = "design"
	DomainContent  = "content"
)

// Phase-2 ext (JEH-1114, PR 2) condition op. Stored on a Condition row in
// the workflow's `conditions` JSONB column. Unlike the phase-1 ops (eq, ne,
// in, is_null, is_not_null) this one does a DB lookup — it scans the
// triggered issue's recent comments + attachments for evidence of work done
// (PR URL, screenshot, or a configurable URL regex).
//
// Intent: gate `set_status: done` workflows so they only fire when an agent
// has actually shipped something. Composes with phase-1: a workflow with
// trigger `status_changed → in_review`, condition `evidence_present`, and
// action `set_status: done` becomes "auto-done if evidence is in the
// thread, otherwise stay in_review".
const OpEvidencePresent = "evidence_present"

// EvidenceConfig is the shape stored under Condition.Value when Op is
// "evidence_present". All fields are optional; sensible defaults apply when
// the field is empty:
//
//	Require            ["pr_url"]   any-of (match if ANY entry is found)
//	URLRegex           ""           used only when Require contains "url_regex"
//	LookbackComments   20           how many recent comments to scan; capped
//	                                at 200 by the engine
//
// Recognised Require entries: "pr_url" | "image_attachment" | "url_regex".
// Unknown entries are ignored (forward-compat) so adding a future signal
// like "ci_run_url" can land without bumping config consumers.
type EvidenceConfig struct {
	Require          []string `json:"require,omitempty"`
	URLRegex         string   `json:"url_regex,omitempty"`
	LookbackComments int      `json:"lookback_comments,omitempty"`
}

// Recognised Require values for EvidenceConfig. Keep these stable — they
// are user-facing in workflow JSON and templates pin them by name.
const (
	EvidenceKindPRURL           = "pr_url"
	EvidenceKindImageAttachment = "image_attachment"
	EvidenceKindURLRegex        = "url_regex"
)

// CommentMatchMode constants are the allowed values of
// TriggerConfigCommentMention.MatchMode.
const (
	CommentMatchAgent   = "agent"
	CommentMatchMember  = "member"
	CommentMatchKeyword = "keyword"
	CommentMatchRegex   = "regex"
)

// Assignee types accepted by ActionConfigReassignIssue.AssigneeType.
const (
	AssigneeTypeMember = "member"
	AssigneeTypeAgent  = "agent"
)

// Editor modes for the workflow rule. Stored on cerebro_workflow.editor_mode
// (phase 2). Form mode is the phase-1 builder; canvas mode is the xyflow-based
// node editor introduced in PR 2.
const (
	EditorModeForm   = "form"
	EditorModeCanvas = "canvas"
)

// TriggerConfigStatusChanged is the payload stored in cerebro_workflow.trigger_config
// for the TriggerStatusChanged trigger. Either field may be empty:
//
//	{ "from_status": "in_progress", "to_status": "in_review" }
//	{ "to_status": "done" }              -> any → done
//	{ }                                  -> any → any (rarely useful)
type TriggerConfigStatusChanged struct {
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`
}

// ActionConfigSetStatus — change the triggered issue's status.
type ActionConfigSetStatus struct {
	Status string `json:"status"`
}

// ActionConfigCreateSubIssue — create a sub-issue under the triggered issue.
// Title and Description may contain {{issue.field}}-style placeholders;
// rendering applies the renderTemplate helper.
//
// Phase 2 additions: AssigneeID/AssigneeType already exist; LabelIDs is new
// and is iterated post-create via AttachLabelToIssue.
type ActionConfigCreateSubIssue struct {
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	AssigneeID   string   `json:"assignee_id,omitempty"`
	AssigneeType string   `json:"assignee_type,omitempty"`
	LabelIDs     []string `json:"label_ids,omitempty"`
}

// ActionConfigSendReminder — write an inbox row to the recipient and
// publish inbox:new so the desktop/mobile notifier picks it up live.
type ActionConfigSendReminder struct {
	RecipientID   string `json:"recipient_id"`
	RecipientType string `json:"recipient_type"`
	Message       string `json:"message"`
}

// ActionConfigRunSkill — enqueue an agent_task_queue row asking the agent
// to run a named skill with the supplied input. Skill is identified by name
// alone (per JEH-920); AgentID is the agent that will execute it. Phase 1
// of skill versioning is intentionally absent — workflows always run the
// latest version of the skill bundled on the agent.
type ActionConfigRunSkill struct {
	SkillName  string         `json:"skill_name"`
	AgentID    string         `json:"agent_id"`
	SkillInput map[string]any `json:"skill_input,omitempty"`
}

// ActionConfigCommentOnIssue — post a workflow-authored comment on either
// the triggered issue (`self`) or its parent (`parent`). Content supports
// the same {{issue.field}} placeholders as create_sub_issue.
type ActionConfigCommentOnIssue struct {
	Target  string `json:"target"`
	Content string `json:"content"`
}

// Allowed values for ActionConfigCommentOnIssue.Target.
const (
	CommentTargetSelf   = "self"
	CommentTargetParent = "parent"
)

// ActionConfigRouteByDomain — classify the triggered issue into one of four
// domains and attach a `<LabelPrefix><domain>` label so downstream workflows
// (filtered by `issue.labels` conditions) can react. The classifier is a
// keyword + extension heuristic; per the JEH-1114 RFC we pick deterministic
// rules over an LLM call to keep the default fast and cheap. A future
// `fallback_skill` field can opt in to LLM-based reclassification, but is
// not implemented in PR 1.
//
// Defaults applied when the corresponding field is empty:
//
//	LabelPrefix    "domain:"
//	DefaultDomain  "business"
//
// The label `<prefix><domain>` MUST already exist in the workspace; the
// action does NOT auto-create labels (avoids polluting label catalogs in
// shared workspaces).
type ActionConfigRouteByDomain struct {
	LabelPrefix   string `json:"label_prefix,omitempty"`
	DefaultDomain string `json:"default_domain,omitempty"`
}

// --- Phase-3 trigger config payloads (JEH-1108) ---

// TriggerConfigCron — scheduler-driven trigger. ScheduleExpr is a cron-style
// expression interpreted by the PR-2 sweeper. Timezone is an IANA tz name;
// the sweeper defaults to "Europe/Copenhagen" when empty so existing
// Firtal-internal workflows stay on the office clock.
type TriggerConfigCron struct {
	ScheduleExpr string `json:"schedule_expr"`
	Timezone     string `json:"timezone,omitempty"`
}

// TriggerConfigWebhookInbound — placeholder for symmetry. The token + signing
// secret live on the cerebro_workflow row (managed by PR 2's regenerate-token
// endpoint) rather than in trigger_config, so the struct itself has no
// fields. Future filters (path-suffix, content-type allowlist, etc.) would
// land here.
type TriggerConfigWebhookInbound struct{}

// TriggerConfigCommentMention — fire when a new comment matches a pattern.
// MatchMode is one of the CommentMatch* constants. Target is interpreted per
// mode: an agent/member UUID for "agent"/"member", or a literal string for
// "keyword"/"regex".
type TriggerConfigCommentMention struct {
	MatchMode string `json:"match_mode"`
	Target    string `json:"target"`
}

// TriggerConfigAllChildrenDone — fire when every child of a parent issue
// reaches status='done'. No per-config filter today; the listener already
// scopes by the triggered issue's parent so workspace-wide misfires aren't
// possible.
type TriggerConfigAllChildrenDone struct{}

// TriggerConfigSubIssueCreated — fire when a sub-issue is created. An empty
// ParentIssueID matches any sub-issue in the workspace; setting it pins the
// trigger to a specific parent (e.g. "fire when a child is added to MUL-42").
type TriggerConfigSubIssueCreated struct {
	ParentIssueID string `json:"parent_issue_id,omitempty"`
}

// --- Phase-3 action config payloads (JEH-1108) ---

// ActionConfigWebhookOutbound — deliver an HTTP request to URL on the
// configured trigger. Headers is an optional per-call header map (the worker
// always sets Content-Type=application/json). IncludeIssueSnapshot decides
// whether the body bundles the rendered issue object alongside the trigger
// event. PR 2 wires the worker.
type ActionConfigWebhookOutbound struct {
	URL                  string            `json:"url"`
	Headers              map[string]string `json:"headers,omitempty"`
	IncludeIssueSnapshot bool              `json:"include_issue_snapshot,omitempty"`
}

// ActionConfigReassignIssue — re-point an issue's assignee to a member or
// agent. AssigneeType is one of the AssigneeType* constants.
type ActionConfigReassignIssue struct {
	AssigneeID   string `json:"assignee_id"`
	AssigneeType string `json:"assignee_type"`
}
