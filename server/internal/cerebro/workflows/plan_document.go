package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	planDocumentRootFolder = "Agents"
	planDocumentAreaFolder = "Workflow"
	planDocumentFolderKind = "document"
)

type PlanDocumentService struct {
	q *db.Queries
}

type PlanDocumentEnsureParams struct {
	WorkspaceID   pgtype.UUID
	IssueID       pgtype.UUID
	WorkflowID    pgtype.UUID
	WorkflowName  string
	AuthorID      pgtype.UUID
	AuthorType    string
	RequesterID   pgtype.UUID
	InitialStatus string
}

func NewPlanDocumentService(q *db.Queries) *PlanDocumentService {
	return &PlanDocumentService{q: q}
}

func (s *PlanDocumentService) Ensure(ctx context.Context, p PlanDocumentEnsureParams) (db.Artifact, error) {
	if s == nil || s.q == nil {
		return db.Artifact{}, fmt.Errorf("plan document service is not wired")
	}
	planName := cleanPlanDocumentName(p.WorkflowName)
	root, err := s.ensureFolder(ctx, p.WorkspaceID, pgtype.UUID{}, planDocumentRootFolder, p.RequesterID)
	if err != nil {
		return db.Artifact{}, err
	}
	workflow, err := s.ensureFolder(ctx, p.WorkspaceID, root.ID, planDocumentAreaFolder, p.RequesterID)
	if err != nil {
		return db.Artifact{}, err
	}
	planFolder, err := s.ensureFolder(ctx, p.WorkspaceID, workflow.ID, planName, p.RequesterID)
	if err != nil {
		return db.Artifact{}, err
	}

	meta := planDocumentMetadata(p.WorkflowID, p.IssueID)
	existing, ok, err := s.findExisting(ctx, p.WorkspaceID, p.IssueID, p.WorkflowID)
	if err != nil {
		return db.Artifact{}, err
	}
	if ok {
		body := appendPlanDocumentEntry(existing.Body, p.InitialStatus)
		if body == existing.Body && existing.FolderID == planFolder.ID {
			return existing, nil
		}
		if existing.FolderID != planFolder.ID {
			if _, err := s.q.MoveArtifactToFolder(ctx, db.MoveArtifactToFolderParams{
				ID:          existing.ID,
				WorkspaceID: p.WorkspaceID,
				FolderID:    planFolder.ID,
			}); err != nil {
				return db.Artifact{}, fmt.Errorf("move plan document: %w", err)
			}
		}
		updated, err := s.q.UpdateArtifact(ctx, db.UpdateArtifactParams{
			ID:          existing.ID,
			WorkspaceID: p.WorkspaceID,
			Title:       existing.Title,
			Body:        body,
			Metadata:    meta,
		})
		if err != nil {
			return db.Artifact{}, fmt.Errorf("update plan document: %w", err)
		}
		return updated, nil
	}

	id, err := uuid.NewV7()
	if err != nil {
		return db.Artifact{}, fmt.Errorf("new artifact id: %w", err)
	}
	title := "Workflow plan: " + planName
	artifact, err := s.q.CreateArtifact(ctx, db.CreateArtifactParams{
		ID:              pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID:     p.WorkspaceID,
		IssueID:         p.IssueID,
		FolderID:        planFolder.ID,
		Kind:            "plan",
		Format:          "md",
		Title:           title,
		Body:            initialPlanDocumentBody(planName, p.InitialStatus),
		Metadata:        meta,
		AuthorType:      p.AuthorType,
		AuthorID:        p.AuthorID,
		RequesterUserID: p.RequesterID,
	})
	if err != nil {
		return db.Artifact{}, fmt.Errorf("create plan document: %w", err)
	}
	if _, err := s.q.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     p.IssueID,
		WorkspaceID: p.WorkspaceID,
		AuthorType:  p.AuthorType,
		AuthorID:    p.AuthorID,
		Content:     fmt.Sprintf("Workflow plan document: [%s](mention://artifact/%s)", artifact.Title, uuidString(artifact.ID)),
		Type:        "comment",
		ParentID:    pgtype.UUID{},
	}); err != nil {
		return db.Artifact{}, fmt.Errorf("create plan document comment: %w", err)
	}
	return artifact, nil
}

func (s *PlanDocumentService) AppendIssueStatus(ctx context.Context, workspaceID, issueID pgtype.UUID, status string) (db.Artifact, bool, error) {
	if s == nil || s.q == nil {
		return db.Artifact{}, false, nil
	}
	artifacts, err := s.q.ListArtifactsByIssue(ctx, db.ListArtifactsByIssueParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
	})
	if err != nil {
		return db.Artifact{}, false, fmt.Errorf("list issue artifacts: %w", err)
	}
	for _, artifact := range artifacts {
		if !isWorkflowPlanDocument(artifact, issueID) {
			continue
		}
		body := appendPlanDocumentEntry(artifact.Body, status)
		if body == artifact.Body {
			return artifact, true, nil
		}
		updated, err := s.q.UpdateArtifact(ctx, db.UpdateArtifactParams{
			ID:          artifact.ID,
			WorkspaceID: workspaceID,
			Title:       artifact.Title,
			Body:        body,
			Metadata:    artifact.Metadata,
		})
		if err != nil {
			return db.Artifact{}, false, fmt.Errorf("append plan document status: %w", err)
		}
		return updated, true, nil
	}
	return db.Artifact{}, false, nil
}

func (s *PlanDocumentService) ensureFolder(ctx context.Context, workspaceID, parentID pgtype.UUID, name string, ownerID pgtype.UUID) (db.ArtifactFolder, error) {
	folders, err := s.q.ListArtifactFoldersByParent(ctx, db.ListArtifactFoldersByParentParams{
		WorkspaceID: workspaceID,
		ParentID:    parentID,
	})
	if err != nil {
		return db.ArtifactFolder{}, fmt.Errorf("list folders: %w", err)
	}
	for _, folder := range folders {
		if folder.Name == name && folder.Kind == planDocumentFolderKind {
			return folder, nil
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return db.ArtifactFolder{}, fmt.Errorf("new folder id: %w", err)
	}
	folder, err := s.q.CreateArtifactFolder(ctx, db.CreateArtifactFolderParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: workspaceID,
		ParentID:    parentID,
		Name:        name,
		Kind:        planDocumentFolderKind,
		OwnerID:     ownerID,
	})
	if err != nil {
		return db.ArtifactFolder{}, fmt.Errorf("create folder %q: %w", name, err)
	}
	return folder, nil
}

func (s *PlanDocumentService) findExisting(ctx context.Context, workspaceID, issueID, workflowID pgtype.UUID) (db.Artifact, bool, error) {
	artifacts, err := s.q.ListArtifactsByIssue(ctx, db.ListArtifactsByIssueParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
	})
	if err != nil {
		return db.Artifact{}, false, fmt.Errorf("list issue artifacts: %w", err)
	}
	workflowIDStr := uuidString(workflowID)
	issueIDStr := uuidString(issueID)
	for _, artifact := range artifacts {
		if isWorkflowPlanDocument(artifact, issueID) && metadataString(artifact.Metadata, "workflow_id") == workflowIDStr && metadataString(artifact.Metadata, "issue_id") == issueIDStr {
			return artifact, true, nil
		}
	}
	return db.Artifact{}, false, nil
}

func isWorkflowPlanDocument(artifact db.Artifact, issueID pgtype.UUID) bool {
	return metadataBool(artifact.Metadata, "workflow_plan_document") && metadataString(artifact.Metadata, "issue_id") == uuidString(issueID)
}

func metadataBool(raw []byte, key string) bool {
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return false
	}
	value, _ := meta[key].(bool)
	return value
}

func metadataString(raw []byte, key string) string {
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ""
	}
	value, _ := meta[key].(string)
	return value
}

func planDocumentMetadata(workflowID, issueID pgtype.UUID) []byte {
	encoded, _ := json.Marshal(map[string]any{
		"workflow_plan_document": true,
		"workflow_id":            uuidString(workflowID),
		"issue_id":               uuidString(issueID),
	})
	return encoded
}

func cleanPlanDocumentName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Unnamed workflow"
	}
	return name
}

func initialPlanDocumentBody(planName, status string) string {
	body := "# " + planName + "\n\nThis document is the shared plan and status log for the workflow.\n"
	return appendPlanDocumentEntry(body, status)
}

func appendPlanDocumentEntry(body, status string) string {
	status = strings.TrimSpace(status)
	if status == "" || strings.Contains(body, status) {
		return body
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return body + "\n## Status update\n\n- " + time.Now().UTC().Format(time.RFC3339) + " — " + status + "\n"
}
