package workflows

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestPlanDocumentServiceEnsureCreatesIdempotentFolderAndArtifact(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	f := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, f, "Plan document target", "todo", 31, pgtype.UUID{})

	svc := NewPlanDocumentService(db.New(pool))
	first, err := svc.Ensure(ctx, PlanDocumentEnsureParams{
		WorkspaceID:   f.workspaceID,
		IssueID:       issueID,
		WorkflowID:    mustUUID("11111111-2222-3333-4444-555555555555"),
		WorkflowName:  "Claude plans - Opus builds",
		AuthorID:      f.userID,
		AuthorType:    "member",
		RequesterID:   f.userID,
		InitialStatus: "Workflow activated. Plan will start next.",
	})
	if err != nil {
		t.Fatalf("ensure first: %v", err)
	}
	second, err := svc.Ensure(ctx, PlanDocumentEnsureParams{
		WorkspaceID:   f.workspaceID,
		IssueID:       issueID,
		WorkflowID:    mustUUID("11111111-2222-3333-4444-555555555555"),
		WorkflowName:  "Claude plans - Opus builds",
		AuthorID:      f.userID,
		AuthorType:    "member",
		RequesterID:   f.userID,
		InitialStatus: "Workflow activated. Plan will start next.",
	})
	if err != nil {
		t.Fatalf("ensure second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("ensure created duplicate artifact: first=%s second=%s", uuidString(first.ID), uuidString(second.ID))
	}

	q := db.New(pool)
	roots, err := q.ListArtifactFoldersByParent(ctx, db.ListArtifactFoldersByParentParams{
		WorkspaceID: f.workspaceID,
		ParentID:    pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("list root folders: %v", err)
	}
	agents := folderByName(roots, "Agents")
	if !agents.ID.Valid {
		t.Fatalf("Agents folder missing: %+v", roots)
	}
	workflow := folderByName(mustListChildFolders(t, q, f.workspaceID, agents.ID), "Workflow")
	if !workflow.ID.Valid {
		t.Fatal("Workflow child folder missing")
	}
	plan := folderByName(mustListChildFolders(t, q, f.workspaceID, workflow.ID), "Claude plans - Opus builds")
	if !plan.ID.Valid {
		t.Fatal("plan_name child folder missing")
	}

	artifacts, err := q.ListArtifactsByIssue(ctx, db.ListArtifactsByIssueParams{
		IssueID:     issueID,
		WorkspaceID: f.workspaceID,
	})
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	var matched []db.Artifact
	for _, art := range artifacts {
		if art.FolderID == plan.ID && art.Kind == "plan" {
			matched = append(matched, art)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("plan artifacts in folder = %d, want 1", len(matched))
	}
	if !strings.Contains(matched[0].Body, "Workflow activated. Plan will start next.") {
		t.Fatalf("artifact body missing status entry: %q", matched[0].Body)
	}
	var meta map[string]any
	if err := json.Unmarshal(matched[0].Metadata, &meta); err != nil {
		t.Fatalf("artifact metadata is not JSON: %v", err)
	}
	if meta["workflow_id"] != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("metadata workflow_id = %v", meta["workflow_id"])
	}
	if meta["issue_id"] != uuidString(issueID) {
		t.Fatalf("metadata issue_id = %v", meta["issue_id"])
	}
	comments, err := q.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issueID,
		WorkspaceID: f.workspaceID,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comment count = %d, want 1", len(comments))
	}
	if !strings.Contains(comments[0].Content, "Workflow plan document: [Workflow plan: Claude plans - Opus builds](mention://artifact/") {
		t.Fatalf("comment does not link plan artifact: %q", comments[0].Content)
	}
}

func folderByName(folders []db.ArtifactFolder, name string) db.ArtifactFolder {
	for _, folder := range folders {
		if folder.Name == name {
			return folder
		}
	}
	return db.ArtifactFolder{}
}

func mustListChildFolders(t *testing.T, q *db.Queries, workspaceID, parentID pgtype.UUID) []db.ArtifactFolder {
	t.Helper()
	folders, err := q.ListArtifactFoldersByParent(context.Background(), db.ListArtifactFoldersByParentParams{
		WorkspaceID: workspaceID,
		ParentID:    parentID,
	})
	if err != nil {
		t.Fatalf("list child folders: %v", err)
	}
	return folders
}
