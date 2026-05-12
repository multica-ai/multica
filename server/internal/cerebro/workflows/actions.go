package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrUnimplementedAction is returned for action_types whose runtime path
// ships in a later PR. Service.Execute treats it as a terminal failure (no
// retries) and marks the run failed without scheduling a backoff.
var ErrUnimplementedAction = errors.New("action not yet implemented in this build")

// IssueActions is the narrow surface the action runner needs from the
// upstream issue queries. Using an interface keeps the engine testable and
// the upstream-zone import boundary one-way: we depend on db.Queries' shape
// rather than on the service package.
type IssueActions interface {
	UpdateIssueStatus(ctx context.Context, arg db.UpdateIssueStatusParams) (db.Issue, error)
	CreateIssue(ctx context.Context, arg db.CreateIssueParams) (db.Issue, error)
	IncrementIssueCounter(ctx context.Context, workspaceID pgtype.UUID) (int32, error)
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
}

// runAction dispatches on the workflow's action_type. The error return is
// what controls the retry ladder in Service.Execute: ErrUnimplementedAction
// is terminal, other errors trigger backoff.
func (s *Service) runAction(ctx context.Context, wf workflow, te TriggerEvent) error {
	switch wf.actionType {
	case ActionSetStatus:
		return s.actionSetStatus(ctx, wf, te)
	case ActionCreateSubIssue:
		_, err := s.actionCreateSubIssue(ctx, wf, te)
		return err
	case ActionSendReminder:
		return ErrUnimplementedAction
	default:
		return fmt.Errorf("unknown action_type %q", wf.actionType)
	}
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
