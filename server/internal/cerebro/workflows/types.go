package workflows

// Trigger and action types are stored as TEXT in the DB so we can extend the
// catalog without migrations. The check constraints in 9020_cerebro_workflows
// (phase 1) and 9021_cerebro_workflows_phase2 pin the currently-known values.
const (
	TriggerStatusChanged  = "status_changed"
	TriggerDueDateReached = "due_date_reached"
	TriggerDueTimeReached = "due_time_reached"

	ActionSetStatus      = "set_status"
	ActionCreateSubIssue = "create_sub_issue"
	ActionSendReminder   = "send_reminder"

	// Phase-2 actions (JEH-1103).
	ActionRunSkill        = "run_skill"
	ActionCommentOnIssue  = "comment_on_issue"

	// Phase-2 extension actions (JEH-1114).
	ActionRouteByDomain = "route_by_domain"
)

// Domain values produced by the route_by_domain classifier. The label that
// gets attached is `<LabelPrefix><domain>` (default prefix: "domain:").
const (
	DomainCode     = "code"
	DomainBusiness = "business"
	DomainDesign   = "design"
	DomainContent  = "content"
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
