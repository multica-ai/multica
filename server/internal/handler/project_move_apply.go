package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func applyProjectMove(ctx context.Context, q *db.Queries, plan ProjectMovePlan, user string) error {
	source, target, project := parseUUID(plan.SourceWorkspaceID), parseUUID(plan.TargetWorkspaceID), parseUUID(plan.ProjectID)
	ids := make([]pgtype.UUID, len(plan.Issues))
	for i, item := range plan.Issues {
		ids[i] = parseUUID(item.ID)
	}
	if err := q.RehomeProjectMoveData(ctx, db.RehomeProjectMoveDataParams{SourceWorkspaceID: source, TargetWorkspaceID: target, ProjectID: project, IssueIds: ids}); err != nil {
		return fmt.Errorf("rehome project data: %w", err)
	}
	for _, label := range plan.Labels {
		targetID := label.TargetID
		if targetID == "" {
			targetID = uuid.NewString()
		}
		if err := q.MapProjectMoveLabel(ctx, db.MapProjectMoveLabelParams{SourceWorkspaceID: source, TargetWorkspaceID: target, SourceLabelID: parseUUID(label.SourceID), TargetLabelID: parseUUID(targetID), IssueIds: ids}); err != nil {
			return fmt.Errorf("map label: %w", err)
		}
	}
	if err := q.AllocateProjectMoveNumbers(ctx, db.AllocateProjectMoveNumbersParams{TargetWorkspaceID: target, Count: plan.freshNumbers}); err != nil {
		return err
	}
	for _, item := range plan.Issues {
		id := parseUUID(item.ID)
		if err := q.RecordLeavingIssueAlias(ctx, db.RecordLeavingIssueAliasParams{SourceWorkspaceID: source, Prefix: plan.sourcePrefix, Number: item.Number, IssueID: id}); err != nil {
			return err
		}
		if err := q.ConsumeReturningIssueAlias(ctx, db.ConsumeReturningIssueAliasParams{TargetWorkspaceID: target, IssueID: id}); err != nil {
			return err
		}
		properties, err := json.Marshal(item.Properties)
		if err != nil {
			return err
		}
		n, err := q.MoveProjectIssue(ctx, db.MoveProjectIssueParams{TargetWorkspaceID: target, SourceWorkspaceID: source, IssueID: id, TargetNumber: item.TargetNumber, TargetStatus: item.TargetStatus, Properties: properties, ClearAssignee: item.AssigneeCleared})
		if err != nil {
			return fmt.Errorf("move issue: %w", err)
		}
		if n != 1 {
			return fmt.Errorf("issue changed during move")
		}
	}
	n, err := q.MoveProjectRoot(ctx, db.MoveProjectRootParams{TargetWorkspaceID: target, SourceWorkspaceID: source, ProjectID: project, ClearLead: plan.LeadCleared})
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("project changed during move")
	}
	summary, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	return q.RecordProjectMove(ctx, db.RecordProjectMoveParams{ProjectID: project, SourceWorkspaceID: source, TargetWorkspaceID: target, MovedBy: parseUUID(user), Summary: summary})
}
