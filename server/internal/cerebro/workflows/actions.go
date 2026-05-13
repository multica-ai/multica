package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// IssueActions is the narrow surface the action runner needs from the
// upstream issue queries. Using an interface keeps the engine testable and
// the upstream-zone import boundary one-way: we depend on db.Queries' shape
// rather than on the service package.
//
// PR 1 (phase 1): UpdateIssueStatus / CreateIssue / IncrementIssueCounter /
//                 GetIssue.
// PR 2 (phase 1): + CreateInboxItem (send_reminder).
// PR 1 (phase 2): + CreateComment, AttachLabelToIssue (comment_on_issue,
//                   extended create_sub_issue),
//                 + ListSkillSummariesByWorkspace, ListAgentSkills, GetAgent,
//                   CreateQuickCreateTask (run_skill resolution + enqueue).
// PR 1 (phase-2 ext, JEH-1114): + ListLabels (route_by_domain resolves the
//                   `<prefix><domain>` label by name within the workspace).
// PR 2 (phase-2 ext, JEH-1114): + ListCommentsForIssue,
//                   ListAttachmentsByIssue,
//                   ListAttachmentURLsByIssueOrComments
//                   (evidence_present condition op scans recent comments +
//                   attachments for a PR URL, image, or matching URL regex).
type IssueActions interface {
	UpdateIssueStatus(ctx context.Context, arg db.UpdateIssueStatusParams) (db.Issue, error)
	CreateIssue(ctx context.Context, arg db.CreateIssueParams) (db.Issue, error)
	IncrementIssueCounter(ctx context.Context, workspaceID pgtype.UUID) (int32, error)
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	CreateInboxItem(ctx context.Context, arg db.CreateInboxItemParams) (db.InboxItem, error)
	CreateComment(ctx context.Context, arg db.CreateCommentParams) (db.Comment, error)
	AttachLabelToIssue(ctx context.Context, arg db.AttachLabelToIssueParams) error
	ListSkillSummariesByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]db.ListSkillSummariesByWorkspaceRow, error)
	ListAgentSkills(ctx context.Context, agentID pgtype.UUID) ([]db.Skill, error)
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	CreateQuickCreateTask(ctx context.Context, arg db.CreateQuickCreateTaskParams) (db.AgentTaskQueue, error)
	ListLabels(ctx context.Context, workspaceID pgtype.UUID) ([]db.IssueLabel, error)
	ListCommentsForIssue(ctx context.Context, arg db.ListCommentsForIssueParams) ([]db.Comment, error)
	ListAttachmentsByIssue(ctx context.Context, arg db.ListAttachmentsByIssueParams) ([]db.Attachment, error)
	ListAttachmentURLsByIssueOrComments(ctx context.Context, issueID pgtype.UUID) ([]string, error)
	// Phase-3 (JEH-1108): assignee-only update for the reassign_issue action.
	UpdateIssueAssignee(ctx context.Context, arg db.UpdateIssueAssigneeParams) (db.Issue, error)
}

// runAction dispatches on the workflow's action_type. Any non-nil error
// triggers the retry ladder in Service.Execute.
func (s *Service) runAction(ctx context.Context, wf workflow, te TriggerEvent) error {
	switch wf.actionType {
	case ActionSetStatus:
		return s.actionSetStatus(ctx, wf, te)
	case ActionCreateSubIssue:
		_, err := s.actionCreateSubIssue(ctx, wf, te)
		return err
	case ActionSendReminder:
		return s.actionSendReminder(ctx, wf, te)
	case ActionRunSkill:
		return s.actionRunSkill(ctx, wf, te)
	case ActionCommentOnIssue:
		return s.actionCommentOnIssue(ctx, wf, te)
	case ActionRouteByDomain:
		return s.actionRouteByDomain(ctx, wf, te)
	case ActionEscalateToOwner:
		return s.actionEscalateToOwner(ctx, wf, te)
	case ActionReassignIssue:
		return s.actionReassignIssue(ctx, wf, te)
	case ActionWebhookOutbound:
		return s.actionWebhookOutbound(ctx, wf, te)
	default:
		return fmt.Errorf("unknown action_type %q", wf.actionType)
	}
}

// actionReassignIssue re-points the triggered issue's assignee to the
// configured (member|agent, uuid) pair. Phase 3 — issue-bound action so the
// trigger event must carry an issue_id; cron-triggered reassignments are
// out of scope (no issue context).
func (s *Service) actionReassignIssue(ctx context.Context, wf workflow, te TriggerEvent) error {
	if te.IssueID == "" {
		return errors.New("reassign_issue: trigger event has no issue_id")
	}
	var cfg ActionConfigReassignIssue
	if err := json.Unmarshal(wf.actionConfig, &cfg); err != nil {
		return fmt.Errorf("reassign_issue: parse config: %w", err)
	}
	if cfg.AssigneeID == "" {
		return errors.New("reassign_issue: assignee_id is required")
	}
	if cfg.AssigneeType != AssigneeTypeMember && cfg.AssigneeType != AssigneeTypeAgent {
		return fmt.Errorf("reassign_issue: unsupported assignee_type %q", cfg.AssigneeType)
	}

	id, err := parseUUID(te.IssueID)
	if err != nil {
		return fmt.Errorf("reassign_issue: %w", err)
	}
	assigneeID, err := parseUUID(cfg.AssigneeID)
	if err != nil {
		return fmt.Errorf("reassign_issue: %w", err)
	}
	if _, err := s.issues.UpdateIssueAssignee(ctx, db.UpdateIssueAssigneeParams{
		ID:           id,
		AssigneeType: pgtype.Text{String: cfg.AssigneeType, Valid: true},
		AssigneeID:   assigneeID,
	}); err != nil {
		return fmt.Errorf("reassign_issue: %w", err)
	}
	return nil
}

// actionSendReminder writes a single inbox_item to the configured recipient
// and publishes inbox:new on the bus so the desktop / mobile notifier picks
// it up live.
func (s *Service) actionSendReminder(ctx context.Context, wf workflow, te TriggerEvent) error {
	var cfg ActionConfigSendReminder
	if err := json.Unmarshal(wf.actionConfig, &cfg); err != nil {
		return fmt.Errorf("send_reminder: parse config: %w", err)
	}
	if cfg.RecipientID == "" || cfg.RecipientType == "" {
		return errors.New("send_reminder: recipient_id and recipient_type are required")
	}
	if cfg.RecipientType != "member" && cfg.RecipientType != "agent" {
		return fmt.Errorf("send_reminder: unsupported recipient_type %q", cfg.RecipientType)
	}
	if cfg.Message == "" {
		return errors.New("send_reminder: message is required")
	}

	recipientID, err := parseUUID(cfg.RecipientID)
	if err != nil {
		return fmt.Errorf("send_reminder: %w", err)
	}
	wsID, err := parseUUID(te.WorkspaceID)
	if err != nil {
		return fmt.Errorf("send_reminder: %w", err)
	}

	var issueID pgtype.UUID
	if te.IssueID != "" {
		if id, ok := optionalUUID(te.IssueID); ok {
			issueID = id
		}
	}

	title := "Workflow reminder"
	body := renderTemplate(cfg.Message, te.Raw)

	item, err := s.issues.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   wsID,
		RecipientType: cfg.RecipientType,
		RecipientID:   recipientID,
		Type:          "workflow_reminder",
		Severity:      "info",
		IssueID:       issueID,
		Title:         title,
		Body:          nullableText(body),
		ActorType:     pgtype.Text{String: wf.createdByType, Valid: wf.createdByType != ""},
		ActorID:       wf.createdByID,
		Details: mustJSON(map[string]any{
			"workflow_id":   uuidString(wf.id),
			"workflow_name": cfg.Message,
			"trigger_type":  te.Type,
		}),
		Route: "inbox",
	})
	if err != nil {
		return fmt.Errorf("send_reminder: inbox write: %w", err)
	}

	if s.bus != nil {
		s.bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: te.WorkspaceID,
			ActorType:   wf.createdByType,
			ActorID:     uuidString(wf.createdByID),
			Payload:     map[string]any{"item_id": util.UUIDToString(item.ID)},
		})
	}
	return nil
}

func (s *Service) actionSetStatus(ctx context.Context, wf workflow, te TriggerEvent) error {
	if te.IssueID == "" {
		return errors.New("set_status: trigger event has no issue_id")
	}
	var cfg ActionConfigSetStatus
	if err := json.Unmarshal(wf.actionConfig, &cfg); err != nil {
		return fmt.Errorf("set_status: parse config: %w", err)
	}
	if cfg.Status == "" {
		return errors.New("set_status: action_config.status is required")
	}

	id, err := parseUUID(te.IssueID)
	if err != nil {
		return fmt.Errorf("set_status: %w", err)
	}
	if _, err := s.issues.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     id,
		Status: cfg.Status,
	}); err != nil {
		return fmt.Errorf("set_status: %w", err)
	}
	return nil
}

// actionCreateSubIssue creates a child issue under the triggered issue. The
// returned issue id is reused by the retry-escalation path (which also
// creates a sub-issue) so the same code is exercised in both happy and
// failure flows.
//
// Phase 2 extension: LabelIDs are attached via AttachLabelToIssue after
// the sub-issue is created. Label-attach errors are non-fatal — we log and
// continue so a single missing label doesn't undo the whole action.
func (s *Service) actionCreateSubIssue(ctx context.Context, wf workflow, te TriggerEvent) (db.Issue, error) {
	if te.IssueID == "" {
		return db.Issue{}, errors.New("create_sub_issue: trigger event has no issue_id")
	}
	var cfg ActionConfigCreateSubIssue
	if err := json.Unmarshal(wf.actionConfig, &cfg); err != nil {
		return db.Issue{}, fmt.Errorf("create_sub_issue: parse config: %w", err)
	}
	if cfg.Title == "" {
		return db.Issue{}, errors.New("create_sub_issue: action_config.title is required")
	}

	parentID, err := parseUUID(te.IssueID)
	if err != nil {
		return db.Issue{}, fmt.Errorf("create_sub_issue: %w", err)
	}
	wsID, err := parseUUID(te.WorkspaceID)
	if err != nil {
		return db.Issue{}, fmt.Errorf("create_sub_issue: %w", err)
	}

	parent, err := s.issues.GetIssue(ctx, parentID)
	if err != nil {
		return db.Issue{}, fmt.Errorf("create_sub_issue: load parent: %w", err)
	}

	number, err := s.issues.IncrementIssueCounter(ctx, wsID)
	if err != nil {
		return db.Issue{}, fmt.Errorf("create_sub_issue: number: %w", err)
	}

	params := db.CreateIssueParams{
		WorkspaceID:   wsID,
		Title:         renderTemplate(cfg.Title, te.Raw),
		Description:   nullableText(renderTemplate(cfg.Description, te.Raw)),
		Status:        "todo",
		Priority:      "none",
		CreatorType:   wf.createdByType,
		CreatorID:     wf.createdByID,
		ParentIssueID: parentID,
		Number:        number,
		ProjectID:     parent.ProjectID,
		Kind:          pgtype.Text{String: "issue", Valid: true},
	}
	if cfg.AssigneeID != "" && cfg.AssigneeType != "" {
		uid, perr := parseUUID(cfg.AssigneeID)
		if perr == nil {
			params.AssigneeID = uid
			params.AssigneeType = pgtype.Text{String: cfg.AssigneeType, Valid: true}
		}
	}
	created, err := s.issues.CreateIssue(ctx, params)
	if err != nil {
		return db.Issue{}, fmt.Errorf("create_sub_issue: %w", err)
	}

	for _, raw := range cfg.LabelIDs {
		labelID, perr := parseUUID(raw)
		if perr != nil {
			continue
		}
		if attachErr := s.issues.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
			IssueID:     created.ID,
			LabelID:     labelID,
			WorkspaceID: wsID,
		}); attachErr != nil {
			// Non-fatal: a missing label shouldn't roll back the sub-issue;
			// the workspace-guarded INSERT already silently no-ops if the
			// label is in another workspace.
			slog.Warn("create_sub_issue: label attach failed",
				"workflow_id", uuidString(wf.id),
				"label_id", raw,
				"error", attachErr,
			)
		}
	}

	return created, nil
}

// actionRunSkill enqueues an agent_task_queue row asking the configured
// agent to run a named skill with the supplied input. The daemon-side
// skill-load path is unchanged: the LLM receives the agent's full skill
// bundle and a prompt instructing it to use the named skill. Per JEH-920,
// skills are referenced by name alone — no versioning in phase 2.
//
// The action returns success as soon as the task is queued; the actual
// skill execution is tracked in the upstream Tasks view, not the workflow
// run log, because the agent runtime is asynchronous and may take minutes.
//
// We reuse the quick_create task variant (context.type = "quick_create")
// so no daemon-side change is needed — the daemon already understands how
// to run an agent against a prompt-only task. workflow_id is included in
// the context for log correlation.
func (s *Service) actionRunSkill(ctx context.Context, wf workflow, te TriggerEvent) error {
	var cfg ActionConfigRunSkill
	if err := json.Unmarshal(wf.actionConfig, &cfg); err != nil {
		return fmt.Errorf("run_skill: parse config: %w", err)
	}
	skillName := strings.TrimSpace(cfg.SkillName)
	if skillName == "" {
		return errors.New("run_skill: skill_name is required")
	}
	if cfg.AgentID == "" {
		return errors.New("run_skill: agent_id is required")
	}
	agentID, err := parseUUID(cfg.AgentID)
	if err != nil {
		return fmt.Errorf("run_skill: %w", err)
	}
	wsID, err := parseUUID(te.WorkspaceID)
	if err != nil {
		return fmt.Errorf("run_skill: %w", err)
	}

	skills, err := s.issues.ListSkillSummariesByWorkspace(ctx, wsID)
	if err != nil {
		return fmt.Errorf("run_skill: list skills: %w", err)
	}
	if !skillNameExists(skills, skillName) {
		return fmt.Errorf("run_skill: skill %q not found in workspace", skillName)
	}

	// The chosen agent must have the skill bundle attached; otherwise the
	// daemon serves the model a bundle without skill X and the prompt
	// would silently fall back to "best-effort". Fail loudly instead.
	agentSkills, err := s.issues.ListAgentSkills(ctx, agentID)
	if err != nil {
		return fmt.Errorf("run_skill: list agent skills: %w", err)
	}
	if !agentSkillAttached(agentSkills, skillName) {
		return fmt.Errorf("run_skill: agent does not have skill %q attached", skillName)
	}

	agent, err := s.issues.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("run_skill: load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return errors.New("run_skill: agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return errors.New("run_skill: agent has no runtime")
	}

	prompt := buildRunSkillPrompt(skillName, cfg.SkillInput)
	contextJSON := mustJSON(map[string]any{
		"type":         "quick_create",
		"prompt":       prompt,
		"workspace_id": te.WorkspaceID,
		"requester_id": uuidString(wf.createdByID),
		// Bookkeeping fields the daemon ignores but the workflow log can use.
		"workflow_id":          uuidString(wf.id),
		"workflow_skill_name":  skillName,
		"workflow_skill_input": cfg.SkillInput,
		"workflow_target_issue_id": te.IssueID,
	})

	if _, err := s.issues.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   agentID,
		RuntimeID: agent.RuntimeID,
		Priority:  0,
		Context:   contextJSON,
	}); err != nil {
		return fmt.Errorf("run_skill: enqueue task: %w", err)
	}
	// We deliberately do NOT publish a daemon wakeup here — that lives on
	// TaskService, which the workflow engine doesn't depend on. The task
	// claim cycle picks it up within the daemon's normal poll window;
	// workflows are async by nature so the brief delay is fine.
	return nil
}

// buildRunSkillPrompt renders the skill-input map into the prompt the
// agent's model sees. Sorted JSON keeps the text deterministic between
// runs (json.MarshalIndent sorts keys).
func buildRunSkillPrompt(skillName string, input map[string]any) string {
	if len(input) == 0 {
		return fmt.Sprintf("Run the skill %q.", skillName)
	}
	encoded, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return fmt.Sprintf("Run the skill %q.", skillName)
	}
	return fmt.Sprintf("Run the skill %q with the following input:\n%s", skillName, string(encoded))
}

func skillNameExists(skills []db.ListSkillSummariesByWorkspaceRow, name string) bool {
	for _, sk := range skills {
		if sk.Name == name {
			return true
		}
	}
	return false
}

func agentSkillAttached(skills []db.Skill, name string) bool {
	for _, sk := range skills {
		if sk.Name == name {
			return true
		}
	}
	return false
}

// actionCommentOnIssue posts a workflow-authored comment on the triggered
// issue (target="self") or its parent (target="parent"). Phase 2 surface;
// per the issue spec we keep target tight (self/parent) and defer
// arbitrary-issue-uuid until we see a concrete use case.
func (s *Service) actionCommentOnIssue(ctx context.Context, wf workflow, te TriggerEvent) error {
	var cfg ActionConfigCommentOnIssue
	if err := json.Unmarshal(wf.actionConfig, &cfg); err != nil {
		return fmt.Errorf("comment_on_issue: parse config: %w", err)
	}
	if cfg.Content == "" {
		return errors.New("comment_on_issue: content is required")
	}
	target := cfg.Target
	if target == "" {
		target = CommentTargetSelf
	}
	if target != CommentTargetSelf && target != CommentTargetParent {
		return fmt.Errorf("comment_on_issue: unsupported target %q", target)
	}
	if te.IssueID == "" {
		return errors.New("comment_on_issue: trigger event has no issue_id")
	}

	issueID, err := parseUUID(te.IssueID)
	if err != nil {
		return fmt.Errorf("comment_on_issue: %w", err)
	}
	wsID, err := parseUUID(te.WorkspaceID)
	if err != nil {
		return fmt.Errorf("comment_on_issue: %w", err)
	}

	commentIssueID := issueID
	if target == CommentTargetParent {
		parent, perr := s.issues.GetIssue(ctx, issueID)
		if perr != nil {
			return fmt.Errorf("comment_on_issue: load issue: %w", perr)
		}
		if !parent.ParentIssueID.Valid {
			return errors.New("comment_on_issue: triggered issue has no parent")
		}
		commentIssueID = parent.ParentIssueID
	}

	// For comment_mention triggers, project trigger_config into te.Raw under
	// "trigger" so the rendered content can reference {{trigger.target}} or
	// the shorthand {{agent_id}} (the mention-agent startpakke uses the
	// shorthand to inline a working mention://agent/<uuid> link).
	raw := te.Raw
	if wf.triggerType == TriggerCommentMention {
		var tc TriggerConfigCommentMention
		if jerr := json.Unmarshal(wf.triggerConfig, &tc); jerr == nil {
			cloned := make(map[string]any, len(raw)+2)
			for k, v := range raw {
				cloned[k] = v
			}
			cloned["trigger"] = map[string]any{
				"target":     tc.Target,
				"match_mode": tc.MatchMode,
			}
			cloned["agent_id"] = tc.Target
			raw = cloned
		}
	}
	content := renderCommentTemplate(cfg.Content, raw)
	if _, err := s.issues.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     commentIssueID,
		WorkspaceID: wsID,
		AuthorType:  wf.createdByType,
		AuthorID:    wf.createdByID,
		Content:     content,
		Type:        "comment",
		ParentID:    pgtype.UUID{},
	}); err != nil {
		return fmt.Errorf("comment_on_issue: insert comment: %w", err)
	}
	return nil
}

// renderTemplate substitutes templated placeholders against the trigger
// event's Raw map. Supported syntaxes:
//
//   - {{title}}, {{status}}, {{priority}} — phase-1 shorthand for the
//     corresponding issue field.
//   - {{issue.<field>}} — phase-2, any top-level string field of
//     raw["issue"].
//   - {{payload.<path>...}} — phase-3, dotted-path lookup against
//     raw["payload"] (the parsed webhook body for webhook_inbound triggers).
//   - {{trigger.<path>...}} — phase-3, dotted-path lookup against
//     raw["trigger"] (the workflow's trigger_config, when the action runner
//     injected it before rendering).
//
// Missing keys at any level render as the empty string — a slightly off
// payload shape degrades gracefully without crashing the action.
func renderTemplate(tpl string, raw map[string]any) string {
	if tpl == "" || raw == nil {
		return tpl
	}
	out := tpl
	if issue, ok := raw["issue"].(map[string]any); ok {
		for _, key := range []string{"title", "status", "priority"} {
			if v, ok := issue[key].(string); ok {
				out = strings.ReplaceAll(out, "{{"+key+"}}", v)
			}
		}
		for key, val := range issue {
			s, ok := val.(string)
			if !ok {
				continue
			}
			out = strings.ReplaceAll(out, "{{issue."+key+"}}", s)
		}
	}
	out = renderDottedPaths(out, "payload", raw)
	out = renderDottedPaths(out, "trigger", raw)
	return out
}

// renderDottedPaths scans tpl for {{<root>.a.b.c}} placeholders and replaces
// each with the string value at raw[root][a][b][c]. Non-string leaf values
// (numbers, bools) are stringified via fmt.Sprintf("%v"); missing keys yield
// empty strings.
//
// Note: this is intentionally minimal — no array indexing, no fallback
// expression syntax. Webhook payloads we expect (GitHub, Stripe, etc.) are
// object-shaped trees of strings, so the simple recursion covers the common
// case without dragging in a full template engine.
func renderDottedPaths(tpl, root string, raw map[string]any) string {
	prefix := "{{" + root + "."
	if !strings.Contains(tpl, prefix) {
		return tpl
	}
	rootMap, _ := raw[root].(map[string]any)
	if rootMap == nil {
		rootMap = map[string]any{}
	}
	out := tpl
	for {
		i := strings.Index(out, prefix)
		if i < 0 {
			break
		}
		j := strings.Index(out[i:], "}}")
		if j < 0 {
			break
		}
		j += i
		path := out[i+len(prefix) : j]
		val := lookupDotted(rootMap, strings.Split(path, "."))
		out = out[:i] + val + out[j+2:]
	}
	return out
}

func lookupDotted(node map[string]any, parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	cur, ok := node[parts[0]]
	if !ok {
		return ""
	}
	if len(parts) == 1 {
		switch v := cur.(type) {
		case string:
			return v
		case nil:
			return ""
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	next, ok := cur.(map[string]any)
	if !ok {
		return ""
	}
	return lookupDotted(next, parts[1:])
}

// renderCommentTemplate extends renderTemplate with two phase-3 shorthands
// that only make sense for comment_on_issue actions fired from comment_mention
// triggers:
//
//   - {{agent_id}} → the trigger_config.target UUID (commonly the agent the
//     comment mentioned), so the startpakke can post a working mention link
//     back to that agent without users hand-writing the URL.
//   - {{trigger.<key>}} — already handled by renderTemplate, but listed here
//     so callers know both forms work.
//
// The substitution layers are applied in order: renderTemplate first (which
// is also where {{trigger.target}} is resolved), then the bare {{agent_id}}
// shorthand. If raw["agent_id"] is missing, the placeholder is left intact
// rather than silently emptied — the mention link without a UUID looks worse
// in a comment thread than the literal placeholder.
func renderCommentTemplate(tpl string, raw map[string]any) string {
	out := renderTemplate(tpl, raw)
	if id, ok := raw["agent_id"].(string); ok && id != "" {
		out = strings.ReplaceAll(out, "{{agent_id}}", id)
	}
	return out
}

// renderTitle is the pre-phase-2 name for renderTemplate kept for any
// external callers; new code should use renderTemplate directly.
func renderTitle(tpl string, raw map[string]any) string {
	return renderTemplate(tpl, raw)
}

// actionRouteByDomain (JEH-1114, phase-2 ext) classifies the triggered issue
// into one of four domains and attaches `<LabelPrefix><domain>`. Composes
// with phase-1 conditions: a downstream workflow can filter on the resulting
// label to invoke skills, escalate, or comment.
//
// The classifier is a deterministic keyword + extension heuristic. It never
// calls an LLM in PR 1 — the JEH-1114 RFC commits to "fast and cheap default,
// LLM fallback opt-in later". Wrong-classification is a recoverable mistake
// (the next event re-classifies; user can override the label by hand) so
// false positives are preferred over a missed action.
//
// Failure modes:
//
//   - Issue lookup fails              → action error (retried).
//   - Workspace label catalog fails   → action error (retried).
//   - Resolved label name not found   → action error (terminal-shaped, but
//                                       still retried by the engine — operators
//                                       fix the catalog and the next retry
//                                       picks up). Message includes which
//                                       label name we looked for so the fix
//                                       is obvious.
func (s *Service) actionRouteByDomain(ctx context.Context, wf workflow, te TriggerEvent) error {
	if te.IssueID == "" {
		return errors.New("route_by_domain: trigger event has no issue_id")
	}
	var cfg ActionConfigRouteByDomain
	if len(wf.actionConfig) > 0 {
		if err := json.Unmarshal(wf.actionConfig, &cfg); err != nil {
			return fmt.Errorf("route_by_domain: parse config: %w", err)
		}
	}
	prefix := cfg.LabelPrefix
	if prefix == "" {
		prefix = "domain:"
	}
	defaultDomain := cfg.DefaultDomain
	if defaultDomain == "" {
		defaultDomain = DomainBusiness
	}
	if !isKnownDomain(defaultDomain) {
		return fmt.Errorf("route_by_domain: default_domain %q is not one of code/business/design/content", defaultDomain)
	}

	issueID, err := parseUUID(te.IssueID)
	if err != nil {
		return fmt.Errorf("route_by_domain: %w", err)
	}
	wsID, err := parseUUID(te.WorkspaceID)
	if err != nil {
		return fmt.Errorf("route_by_domain: %w", err)
	}

	issue, err := s.issues.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("route_by_domain: load issue: %w", err)
	}

	domain := classifyDomain(issue.Title, issue.Description.String, defaultDomain)
	wantLabelName := prefix + domain

	labels, err := s.issues.ListLabels(ctx, wsID)
	if err != nil {
		return fmt.Errorf("route_by_domain: list labels: %w", err)
	}
	var labelID pgtype.UUID
	for _, l := range labels {
		if strings.EqualFold(l.Name, wantLabelName) {
			labelID = l.ID
			break
		}
	}
	if !labelID.Valid {
		return fmt.Errorf("route_by_domain: workspace has no label %q (create it under Settings → Labels)", wantLabelName)
	}

	if err := s.issues.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID:     issueID,
		LabelID:     labelID,
		WorkspaceID: wsID,
	}); err != nil {
		return fmt.Errorf("route_by_domain: attach %q: %w", wantLabelName, err)
	}
	slog.Debug("route_by_domain attached",
		"workflow_id", uuidString(wf.id),
		"issue_id", te.IssueID,
		"label", wantLabelName,
	)
	return nil
}

// classifyDomain picks one of code/business/design/content based on simple
// keyword + file-extension heuristics over title + description. The intent is
// "right most of the time, fast every time" — wrong picks are corrected by
// the next event or a manual label edit.
//
// Order matters: code patterns are checked first because file paths /
// repo URLs are the strongest deterministic signal we have. Design and
// content checks come next, with `defaultDomain` as the catch-all. The
// rules favour precision over recall — if nothing matches confidently, we
// fall back to the configured default rather than guessing wildly.
func classifyDomain(title, description, defaultDomain string) string {
	hay := strings.ToLower(title + "\n" + description)

	if matchesAny(hay, codeFileExtensions) {
		return DomainCode
	}
	if containsAnyWord(hay, codeKeywords) || strings.Contains(hay, "github.com/") {
		return DomainCode
	}
	if containsAnyWord(hay, designKeywords) || strings.Contains(hay, "figma.com/") {
		return DomainDesign
	}
	if containsAnyWord(hay, contentKeywords) {
		return DomainContent
	}
	return defaultDomain
}

// containsAnyWord checks whether `hay` (already lower-cased) contains any of
// the words as a whole-word match so "test" doesn't match "latest". The
// boundary is non-letter-non-digit on both sides, including string edges.
func containsAnyWord(hay string, words []string) bool {
	for _, w := range words {
		if w == "" {
			continue
		}
		idx := 0
		for {
			pos := strings.Index(hay[idx:], w)
			if pos < 0 {
				break
			}
			absolute := idx + pos
			before := byte(0)
			if absolute > 0 {
				before = hay[absolute-1]
			}
			after := byte(0)
			if absolute+len(w) < len(hay) {
				after = hay[absolute+len(w)]
			}
			if !isWordByte(before) && !isWordByte(after) {
				return true
			}
			idx = absolute + 1
			if idx >= len(hay) {
				break
			}
		}
	}
	return false
}

// matchesAny is the substring variant — used for file extensions where word
// boundaries don't help (".go" can legitimately appear inside paths).
func matchesAny(hay string, needles []string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(hay, n) {
			return true
		}
	}
	return false
}

func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_':
		return true
	}
	return false
}

func isKnownDomain(d string) bool {
	switch d {
	case DomainCode, DomainBusiness, DomainDesign, DomainContent:
		return true
	}
	return false
}

// actionEscalateToOwner (JEH-1114, phase-2 ext PR 3) walks the triggered
// issue's parent_issue_id chain up to the first ancestor that has a
// non-null assignee, and posts a backlinked comment there. The escalation
// only fires when the triggered issue has been stalled (no updated_at
// change) longer than MaxAgeHours — so a workflow listening on
// `due_date_reached` can attach this action without firing every five
// minutes during a healthy sprint.
//
// Lookup fallback order (each step checked, first hit wins):
//   1. Walk parent_issue_id up to maxParentHops, return first issue with
//      a non-null assignee.
//   2. If no ancestor has an assignee, fall back to the workflow creator
//      (createdBy) so the escalation never silently disappears.
//
// If the workflow itself isn't bound to an issue (no te.IssueID), the
// action errors — escalation has no anchor to walk from. Same goes for
// loading the triggered issue.
//
// The default comment template auto-builds:
//
//   ⚠️ Stalled sub-issue: [#<number>: <title>](mention://issue/<uuid>)
//   Reason: no status change for <hours>h (threshold: <max>h).
//
// A custom ContentTemplate is rendered through renderTemplate so {{title}}
// / {{issue.<field>}} keep working. The trigger event raw map gets two
// new keys for templates that want to reference them: `escalation.age_hours`
// and `escalation.threshold_hours`.
func (s *Service) actionEscalateToOwner(ctx context.Context, wf workflow, te TriggerEvent) error {
	if te.IssueID == "" {
		return errors.New("escalate_to_owner: trigger event has no issue_id")
	}
	var cfg ActionConfigEscalateToOwner
	if len(wf.actionConfig) > 0 {
		if err := json.Unmarshal(wf.actionConfig, &cfg); err != nil {
			return fmt.Errorf("escalate_to_owner: parse config: %w", err)
		}
	}
	maxAge := cfg.MaxAgeHours
	if maxAge <= 0 {
		maxAge = 12
	}

	issueID, err := parseUUID(te.IssueID)
	if err != nil {
		return fmt.Errorf("escalate_to_owner: %w", err)
	}
	wsID, err := parseUUID(te.WorkspaceID)
	if err != nil {
		return fmt.Errorf("escalate_to_owner: %w", err)
	}

	stalled, err := s.issues.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("escalate_to_owner: load issue: %w", err)
	}

	// Threshold check. UpdatedAt is set on every status / field change
	// upstream, so "no movement in N hours" is the right freshness signal.
	// Use nowFunc so retry-backoff tests can pin time deterministically.
	if stalled.UpdatedAt.Valid {
		ageHours := nowFunc().Sub(stalled.UpdatedAt.Time).Hours()
		if ageHours < float64(maxAge) {
			slog.Debug("escalate_to_owner: under threshold",
				"workflow_id", uuidString(wf.id),
				"issue_id", te.IssueID,
				"age_hours", ageHours,
				"max_age_hours", maxAge,
			)
			return nil
		}
	}

	target, err := s.findEscalationTarget(ctx, stalled, wf)
	if err != nil {
		return fmt.Errorf("escalate_to_owner: %w", err)
	}

	ageHours := 0
	if stalled.UpdatedAt.Valid {
		ageHours = int(nowFunc().Sub(stalled.UpdatedAt.Time).Hours())
	}

	content := renderEscalationContent(cfg.ContentTemplate, stalled, ageHours, maxAge, te.Raw)
	if _, err := s.issues.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     target,
		WorkspaceID: wsID,
		AuthorType:  wf.createdByType,
		AuthorID:    wf.createdByID,
		Content:     content,
		Type:        "comment",
		ParentID:    pgtype.UUID{},
	}); err != nil {
		return fmt.Errorf("escalate_to_owner: insert comment: %w", err)
	}
	slog.Debug("escalate_to_owner posted",
		"workflow_id", uuidString(wf.id),
		"stalled_issue_id", te.IssueID,
		"escalated_to_issue_id", uuidString(target),
		"age_hours", ageHours,
	)
	return nil
}

// maxParentHops bounds the parent walk so a malformed cycle in the issue
// tree (shouldn't exist; the schema doesn't prevent it explicitly) can't
// pin the action in an infinite loop. 16 is many more levels than any
// realistic project tree we've seen.
const maxParentHops = 16

// findEscalationTarget walks parent_issue_id from the stalled issue until
// it finds an ancestor with a non-null assignee. Returns that ancestor's
// id. Falls back to the workflow's creator-issue (the one being escalated
// from) if no ancestor has an assignee — better to comment on the stalled
// issue itself than to drop the escalation silently.
func (s *Service) findEscalationTarget(ctx context.Context, stalled db.Issue, wf workflow) (pgtype.UUID, error) {
	current := stalled
	for i := 0; i < maxParentHops; i++ {
		if !current.ParentIssueID.Valid {
			break
		}
		parent, err := s.issues.GetIssue(ctx, current.ParentIssueID)
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("walk parent: %w", err)
		}
		if parent.AssigneeID.Valid {
			return parent.ID, nil
		}
		current = parent
	}
	// No ancestor has an assignee — escalate on the stalled issue itself
	// so the workflow creator (createdBy) sees a comment they can act on.
	// The CreatedByID lives on the workflow, but the comment's IssueID
	// must be a valid issue, so target the stalled one.
	_ = wf
	return stalled.ID, nil
}

// renderEscalationContent builds the comment body. When tpl is empty,
// uses a sensible default with the backlink + threshold reason. When tpl
// is set, runs it through the existing renderTemplate so {{title}} keys
// work, then appends two extra placeholders unique to this action:
// {{escalation.age_hours}} and {{escalation.threshold_hours}}.
func renderEscalationContent(tpl string, stalled db.Issue, ageHours, maxHours int, raw map[string]any) string {
	body := tpl
	if body == "" {
		body = "⚠️ Stalled sub-issue: [#{{issue.number}}: {{issue.title}}](mention://issue/{{issue.id}})\n" +
			"Reason: no status change for {{escalation.age_hours}}h (threshold: {{escalation.threshold_hours}}h)."
	}

	// Augment the raw map with the issue id + number + escalation
	// details so the template lookup picks them up.
	augmented := map[string]any{}
	if raw != nil {
		for k, v := range raw {
			augmented[k] = v
		}
	}
	issue, _ := augmented["issue"].(map[string]any)
	if issue == nil {
		issue = map[string]any{}
	}
	// Always overwrite from the freshly-loaded row, in case the trigger
	// payload was stale or missing fields.
	if uuidString(stalled.ID) != "" {
		issue["id"] = uuidString(stalled.ID)
	}
	if stalled.Title != "" {
		issue["title"] = stalled.Title
	}
	issue["number"] = fmt.Sprintf("%d", stalled.Number)
	augmented["issue"] = issue

	body = renderTemplate(body, augmented)
	body = strings.ReplaceAll(body, "{{escalation.age_hours}}", fmt.Sprintf("%d", ageHours))
	body = strings.ReplaceAll(body, "{{escalation.threshold_hours}}", fmt.Sprintf("%d", maxHours))
	return body
}

// Heuristic vocabularies. Tuned for the firtal stack — file extensions
// reflect real repos (Go backend, TS frontend, Python pipelines, SQL).
// Keep these lower-case; classifyDomain lower-cases the haystack before
// matching.
var (
	codeFileExtensions = []string{
		".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs",
		".sql", ".yaml", ".yml", ".sh", ".dockerfile",
		".java", ".kt", ".rb", ".php",
	}
	codeKeywords = []string{
		"pr", "pull request", "merge", "rebase", "commit", "branch",
		"deploy", "build", "ci", "endpoint", "api", "function",
		"class", "import", "migration", "schema", "refactor", "bug",
		"crash", "stack trace", "regression",
	}
	designKeywords = []string{
		"design", "designer", "figma", "mockup", "wireframe", "sketch",
		"icon", "color", "palette", "spacing", "layout",
		"prototype", "ui", "ux", "visual",
	}
	contentKeywords = []string{
		"copy", "content", "blog", "post", "article", "marketing",
		"newsletter", "headline", "tagline", "campaign", "seo",
	}
)
