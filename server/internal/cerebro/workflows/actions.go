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
	default:
		return fmt.Errorf("unknown action_type %q", wf.actionType)
	}
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

	content := renderTemplate(cfg.Content, te.Raw)
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

// renderTemplate substitutes {{title}}, {{status}}, {{priority}} (the legacy
// phase-1 set), and additionally every top-level string field on the issue
// payload via {{issue.<field>}} syntax. The full path lookup lets phase-2
// templates reference fields that weren't promoted to bare placeholders.
func renderTemplate(tpl string, raw map[string]any) string {
	if tpl == "" || raw == nil {
		return tpl
	}
	issue, _ := raw["issue"].(map[string]any)
	if issue == nil {
		return tpl
	}
	out := tpl
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
	return out
}

// renderTitle is the pre-phase-2 name for renderTemplate kept for any
// external callers; new code should use renderTemplate directly.
func renderTitle(tpl string, raw map[string]any) string {
	return renderTemplate(tpl, raw)
}
