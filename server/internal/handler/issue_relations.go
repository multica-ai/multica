package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func issueLabelToResponse(label db.IssueLabel) IssueLabelResponse {
	return IssueLabelResponse{
		ID:          uuidToString(label.ID),
		WorkspaceID: uuidToString(label.WorkspaceID),
		Name:        label.Name,
		Color:       label.Color,
	}
}

func hasDependencyGroups(groups IssueDependencyGroupsResponse) bool {
	return len(groups.Blocks) > 0 || len(groups.BlockedBy) > 0 || len(groups.Related) > 0 || len(groups.Copy) > 0
}

func (h *Handler) buildIssueDetailResponse(ctx context.Context, issue db.Issue) (IssueResponse, error) {
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)

	if issue.ParentIssueID.Valid {
		parentIssue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          issue.ParentIssueID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err == nil {
			parentResp := issueToReferenceResponse(parentIssue, prefix)
			resp.ParentIssue = &parentResp
		}
	}

	childIssues, err := h.Queries.ListChildIssues(ctx, issue.ID)
	if err != nil {
		return IssueResponse{}, err
	}
	if len(childIssues) > 0 {
		resp.ChildIssues = make([]IssueReferenceResponse, len(childIssues))
		for index, childIssue := range childIssues {
			resp.ChildIssues[index] = issueToReferenceResponse(childIssue, prefix)
		}
	}

	labels, err := h.Queries.ListIssueLabels(ctx, issue.ID)
	if err != nil {
		return IssueResponse{}, err
	}
	if len(labels) > 0 {
		resp.Labels = make([]IssueLabelResponse, len(labels))
		for index, label := range labels {
			resp.Labels[index] = issueLabelToResponse(label)
		}
	}

	dependencies, err := h.Queries.ListIssueDependenciesForIssue(ctx, issue.ID)
	if err != nil {
		return IssueResponse{}, err
	}
	if groups, err := h.buildIssueDependencyGroups(ctx, issue, dependencies); err == nil && groups != nil {
		resp.Dependencies = groups
	} else if err != nil {
		return IssueResponse{}, err
	}

	reactions, err := h.Queries.ListIssueReactions(ctx, issue.ID)
	if err == nil && len(reactions) > 0 {
		resp.Reactions = make([]IssueReactionResponse, len(reactions))
		for index, reaction := range reactions {
			resp.Reactions[index] = issueReactionToResponse(reaction)
		}
	}

	attachments, err := h.Queries.ListAttachmentsByIssue(ctx, db.ListAttachmentsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err == nil && len(attachments) > 0 {
		resp.Attachments = make([]AttachmentResponse, len(attachments))
		for index, attachment := range attachments {
			resp.Attachments[index] = h.attachmentToResponse(attachment)
		}
	}

	return resp, nil
}

func (h *Handler) buildIssueDependencyGroups(ctx context.Context, issue db.Issue, dependencies []db.IssueDependency) (*IssueDependencyGroupsResponse, error) {
	if len(dependencies) == 0 {
		return nil, nil
	}

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	cache := make(map[string]IssueReferenceResponse)
	currentIssueID := uuidToString(issue.ID)
	groups := IssueDependencyGroupsResponse{}

	loadIssueReference := func(issueID pgtype.UUID) (IssueReferenceResponse, error) {
		issueKey := uuidToString(issueID)
		if cached, ok := cache[issueKey]; ok {
			return cached, nil
		}

		dependencyIssue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          issueID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return IssueReferenceResponse{}, err
		}

		ref := issueToReferenceResponse(dependencyIssue, prefix)
		cache[issueKey] = ref
		return ref, nil
	}

	for _, dependency := range dependencies {
		dependencyID := uuidToString(dependency.ID)
		issueID := uuidToString(dependency.IssueID)
		dependsOnIssueID := uuidToString(dependency.DependsOnIssueID)

		switch dependency.Type {
		case "blocks":
			if issueID == currentIssueID {
				ref, err := loadIssueReference(dependency.DependsOnIssueID)
				if err != nil {
					return nil, err
				}
				groups.Blocks = append(groups.Blocks, IssueDependencyEntryResponse{
					ID:    dependencyID,
					Type:  "blocks",
					Issue: ref,
				})
				continue
			}

			if dependsOnIssueID == currentIssueID {
				ref, err := loadIssueReference(dependency.IssueID)
				if err != nil {
					return nil, err
				}
				groups.BlockedBy = append(groups.BlockedBy, IssueDependencyEntryResponse{
					ID:    dependencyID,
					Type:  "blocked_by",
					Issue: ref,
				})
			}
		case "related":
			var otherIssueID pgtype.UUID
			if issueID == currentIssueID {
				otherIssueID = dependency.DependsOnIssueID
			} else if dependsOnIssueID == currentIssueID {
				otherIssueID = dependency.IssueID
			} else {
				continue
			}

			ref, err := loadIssueReference(otherIssueID)
			if err != nil {
				return nil, err
			}
			groups.Related = append(groups.Related, IssueDependencyEntryResponse{
				ID:    dependencyID,
				Type:  "related",
				Issue: ref,
			})
		case "copy":
			var otherIssueID pgtype.UUID
			if issueID == currentIssueID {
				otherIssueID = dependency.DependsOnIssueID
			} else if dependsOnIssueID == currentIssueID {
				otherIssueID = dependency.IssueID
			} else {
				continue
			}

			ref, err := loadIssueReference(otherIssueID)
			if err != nil {
				return nil, err
			}
			groups.Copy = append(groups.Copy, IssueDependencyEntryResponse{
				ID:    dependencyID,
				Type:  "copy",
				Issue: ref,
			})
		}
	}

	if !hasDependencyGroups(groups) {
		return nil, nil
	}

	return &groups, nil
}

func (h *Handler) validateParentIssue(ctx context.Context, workspaceID string, issueID *string, parentIssueID *string) (pgtype.UUID, error) {
	if parentIssueID == nil || strings.TrimSpace(*parentIssueID) == "" {
		return pgtype.UUID{}, nil
	}

	parentIssue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          parseUUID(*parentIssueID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("parent issue not found")
	}

	if issueID == nil || strings.TrimSpace(*issueID) == "" {
		return parentIssue.ID, nil
	}

	currentIssueID := strings.TrimSpace(*issueID)
	if currentIssueID == uuidToString(parentIssue.ID) {
		return pgtype.UUID{}, fmt.Errorf("issue cannot be its own parent")
	}

	nextParentID := parentIssue.ParentIssueID
	for nextParentID.Valid {
		if uuidToString(nextParentID) == currentIssueID {
			return pgtype.UUID{}, fmt.Errorf("parent issue would create a cycle")
		}

		ancestorIssue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          nextParentID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			break
		}
		nextParentID = ancestorIssue.ParentIssueID
	}

	return parentIssue.ID, nil
}

func (h *Handler) publishIssueSnapshot(ctx context.Context, workspaceID, actorType, actorID string, issueID pgtype.UUID) {
	if !issueID.Valid {
		return
	}

	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		return
	}

	resp, err := h.buildIssueDetailResponse(ctx, issue)
	if err != nil {
		return
	}

	h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
		"issue": resp,
	})
}

func (h *Handler) publishHierarchyParentSnapshots(ctx context.Context, workspaceID, actorType, actorID string, previousParentID, nextParentID pgtype.UUID) {
	prevParent := uuidToString(previousParentID)
	nextParent := uuidToString(nextParentID)
	if prevParent == nextParent {
		return
	}

	h.publishIssueSnapshot(ctx, workspaceID, actorType, actorID, previousParentID)
	h.publishIssueSnapshot(ctx, workspaceID, actorType, actorID, nextParentID)
}
