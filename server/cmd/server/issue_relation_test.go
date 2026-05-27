package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createRelationTestIssue inserts a throwaway issue in the fixture workspace
// and registers cleanup. Returns the new issue.
func createRelationTestIssue(t *testing.T, queries *db.Queries, title string) db.Issue {
	t.Helper()
	ctx := context.Background()

	number, err := queries.IncrementIssueCounter(ctx, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("IncrementIssueCounter: %v", err)
	}
	issue, err := queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Title:       title,
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   parseUUID(testUserID),
		Number:      number,
	})
	if err != nil {
		t.Fatalf("CreateIssue(%q): %v", title, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue
}

// TestIssueRelationPersistence covers the ITT-237 Phase 1 foundation: a
// directed reference persists, is idempotent, and is readable from both the
// source (forward) and target (backlink) directions.
func TestIssueRelationPersistence(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	source := createRelationTestIssue(t, queries, "relation source")
	target := createRelationTestIssue(t, queries, "relation target")

	arg := db.UpsertIssueRelationParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		SourceIssueID: source.ID,
		TargetIssueID: target.ID,
		RelationType:  "references",
		CreatedByType: "system", // auto-extracted reference, no actor
		CreatedByID:   pgtype.UUID{},
	}

	rel, err := queries.UpsertIssueRelation(ctx, arg)
	if err != nil {
		t.Fatalf("UpsertIssueRelation: %v", err)
	}
	if util.UUIDToString(rel.SourceIssueID) != util.UUIDToString(source.ID) ||
		util.UUIDToString(rel.TargetIssueID) != util.UUIDToString(target.ID) {
		t.Fatalf("relation endpoints mismatch: got src=%s tgt=%s",
			util.UUIDToString(rel.SourceIssueID), util.UUIDToString(rel.TargetIssueID))
	}
	if rel.CreatedByType != "system" || rel.CreatedByID.Valid {
		t.Errorf("expected system relation with NULL actor, got type=%q id.valid=%v", rel.CreatedByType, rel.CreatedByID.Valid)
	}

	// Idempotency: upserting the same (source, target, type) returns the
	// existing row rather than creating a duplicate.
	rel2, err := queries.UpsertIssueRelation(ctx, arg)
	if err != nil {
		t.Fatalf("UpsertIssueRelation (second): %v", err)
	}
	if util.UUIDToString(rel2.ID) != util.UUIDToString(rel.ID) {
		t.Errorf("idempotent upsert returned a new row: %s vs %s",
			util.UUIDToString(rel2.ID), util.UUIDToString(rel.ID))
	}

	// Forward direction: the source lists the relation.
	fromSource, err := queries.ListIssueRelationsBySource(ctx, source.ID)
	if err != nil {
		t.Fatalf("ListIssueRelationsBySource: %v", err)
	}
	if len(fromSource) != 1 || util.UUIDToString(fromSource[0].TargetIssueID) != util.UUIDToString(target.ID) {
		t.Fatalf("expected 1 forward relation to target, got %d: %+v", len(fromSource), fromSource)
	}

	// Backlink direction: the target lists the same relation.
	toTarget, err := queries.ListIssueRelationsByTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("ListIssueRelationsByTarget: %v", err)
	}
	if len(toTarget) != 1 || util.UUIDToString(toTarget[0].SourceIssueID) != util.UUIDToString(source.ID) {
		t.Fatalf("expected 1 backlink from source, got %d: %+v", len(toTarget), toTarget)
	}

	// Explicit delete removes the relation.
	if err := queries.DeleteIssueRelation(ctx, db.DeleteIssueRelationParams{
		SourceIssueID: source.ID,
		TargetIssueID: target.ID,
		RelationType:  "references",
	}); err != nil {
		t.Fatalf("DeleteIssueRelation: %v", err)
	}
	after, err := queries.ListIssueRelationsBySource(ctx, source.ID)
	if err != nil {
		t.Fatalf("ListIssueRelationsBySource (after delete): %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("expected relation removed, still have %d", len(after))
	}
}

// TestIssueRelationCascadeOnIssueDelete verifies relations are cleaned up when
// either endpoint issue is deleted (ON DELETE CASCADE), so no dangling
// backlink rows survive an issue removal.
func TestIssueRelationCascadeOnIssueDelete(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	source := createRelationTestIssue(t, queries, "cascade source")
	target := createRelationTestIssue(t, queries, "cascade target")

	if _, err := queries.UpsertIssueRelation(ctx, db.UpsertIssueRelationParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		SourceIssueID: source.ID,
		TargetIssueID: target.ID,
		RelationType:  "relates_to",
		CreatedByType: "member",
		CreatedByID:   parseUUID(testUserID),
	}); err != nil {
		t.Fatalf("UpsertIssueRelation: %v", err)
	}

	// Deleting the target issue should cascade away the backlink row.
	if _, err := testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, target.ID); err != nil {
		t.Fatalf("delete target issue: %v", err)
	}

	remaining, err := queries.ListIssueRelationsBySource(ctx, source.ID)
	if err != nil {
		t.Fatalf("ListIssueRelationsBySource: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected relation cascade-deleted with target issue, still have %d", len(remaining))
	}
}

// TestIssueRelationRejectsSelfReference confirms the CHECK constraint blocks an
// issue from referencing itself.
func TestIssueRelationRejectsSelfReference(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	issue := createRelationTestIssue(t, queries, "self ref")

	_, err := queries.UpsertIssueRelation(ctx, db.UpsertIssueRelationParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		SourceIssueID: issue.ID,
		TargetIssueID: issue.ID,
		RelationType:  "references",
		CreatedByType: "system",
		CreatedByID:   pgtype.UUID{},
	})
	if err == nil {
		// Clean up in case the constraint was (incorrectly) not enforced.
		testPool.Exec(ctx, `DELETE FROM issue_relation WHERE source_issue_id = $1`, issue.ID)
		t.Fatal("expected self-reference to be rejected by CHECK constraint, got nil error")
	}
}
