package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// rather than on the service package. CreateInboxItem joined the interface
// in PR 2 when the send_reminder action shipped.
type IssueActions interface {
	UpdateIssueStatus(ctx context.Context, arg db.UpdateIssueStatusParams) (db.Issue, error)
	CreateIssue(ctx context.Context, arg db.CreateIssueParams) (db.Issue, error)
	IncrementIssueCounter(ctx context.Context, workspaceID pgtype.UUID) (int32, error)
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	CreateInboxItem(ctx context.Context, arg db.CreateInboxItemParams) (db.InboxItem, error)
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
	default:
		return fmt.Errorf("unknown action_type %q", wf.actionType)
	}
}

// actionSendReminder writes a single inbox_item to the configured recipient
// and publishes inbox:new on the bus so the desktop / mobile notifier picks
// it up live. PR 1 returned ErrUnimplementedAction here; PR 2 finishes the
// implementation. Mobile-push fan-out happens downstream in the existing
// inbox-listener chain — no extra plumbing needed on this side.
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
	body := renderTitle(cfg.Message, te.Raw)

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

	// Best-effort: publish inbox:new so the recipient sees the reminder
	// live without a refetch. Failure to publish only delays delivery to
	// the next query refresh — the row is already persisted, so we don't
	// retry the whole action on a bus issue.
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
		Title:         renderTitle(cfg.Title, te.Raw),
		Description:   nullableText(cfg.Description),
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
	return created, nil
}

// renderTitle does a minimal {{title}} substitution against the raw payload
// so the most common template ("Follow-up on {{title}}") works without the
// full template language. PR 2 lands the form-builder which validates the
// full set of placeholders before save.
func renderTitle(tpl string, raw map[string]any) string {
	if raw == nil {
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
	return out
}
