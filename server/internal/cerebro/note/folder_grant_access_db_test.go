package note

// FIR-2595 — folders drive note permissions via the Collections grant model.
// These tests prove the additive bridge added in migration 9116: a
// cerebro_folder_grant on a note's folder (or any ancestor) makes CanUserSeeNote
// return true even when the legacy owner/visibility/share rule would deny the
// viewer — this is the fix for FIR-2589 ("note not found" after granting access
// in the Access dialog). They also prove inheritance (ancestor grant cascades),
// the workspace grant, and group-membership expansion. Legacy access is never
// removed; the grant only ever adds.
//
// Reuses the wave3 fixture harness (TestMain / w3Pool / w3H / w3WsID / w3UserA /
// w3UserB). Skips cleanly when no DB is reachable.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// makeFolder inserts an artifact_folder (default 'workspace' visibility, so the
// legacy folder-chain check never blocks) and returns its id. parent may be a
// zero UUID for a root-level folder.
func makeFolder(t *testing.T, ctx context.Context, name string, parent pgtype.UUID) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := w3Pool.QueryRow(ctx,
		`INSERT INTO artifact_folder (workspace_id, parent_id, name) VALUES ($1, $2, $3) RETURNING id`,
		w3WsID, parent, name,
	).Scan(&id); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	return id
}

// makeNoteInFolder creates a private note (owned by userA) inside the given
// folder. Private means userB is denied by the legacy note-rule, isolating the
// grant path as the only thing that can grant userB access.
func makeNoteInFolder(t *testing.T, ctx context.Context, folder pgtype.UUID) pgtype.UUID {
	t.Helper()
	id, _ := uuid.NewV7()
	art, err := w3H.Upstream.CreateArtifact(ctx, db.CreateArtifactParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: w3WsID,
		Kind:        "note",
		Format:      "md",
		Title:       "grant-test",
		Body:        "body",
		Metadata:    []byte("{}"),
		AuthorType:  "member",
		AuthorID:    w3UserA,
		FolderID:    folder,
	})
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	if _, err := w3H.Cerebro.UpsertNote(ctx, cerebrodb.UpsertNoteParams{
		ArtifactID: art.ID,
		OwnerID:    w3UserA,
		Visibility: "private",
		Pinned:     false,
	}); err != nil {
		t.Fatalf("upsert note: %v", err)
	}
	return art.ID
}

func grantFolder(t *testing.T, ctx context.Context, folder pgtype.UUID, granteeType string, granteeID pgtype.UUID, role string) {
	t.Helper()
	if _, err := w3Pool.Exec(ctx,
		`INSERT INTO cerebro_folder_grant (surface, folder_id, grantee_type, grantee_id, role)
		 VALUES ('artifact', $1, $2, $3, $4)`,
		folder, granteeType, granteeID, role,
	); err != nil {
		t.Fatalf("insert grant: %v", err)
	}
}

func canSee(t *testing.T, ctx context.Context, note, viewer pgtype.UUID) bool {
	t.Helper()
	ok, err := w3H.Cerebro.CanUserSeeNote(ctx, cerebrodb.CanUserSeeNoteParams{
		ArtifactID: note,
		OwnerID:    viewer, // second positional arg = the viewer being checked
	})
	if err != nil {
		t.Fatalf("CanUserSeeNote: %v", err)
	}
	return ok
}

// A direct member grant on the note's own folder opens a note the legacy rule
// denies — the exact FIR-2589 scenario.
func TestFolderGrantOpensPrivateNote(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	folder := makeFolder(t, ctx, "fg-direct", pgtype.UUID{})
	note := makeNoteInFolder(t, ctx, folder)

	if canSee(t, ctx, note, w3UserB) {
		t.Fatal("baseline: userB should NOT see a private note before any grant")
	}
	grantFolder(t, ctx, folder, "member", w3UserB, "viewer")
	if !canSee(t, ctx, note, w3UserB) {
		t.Fatal("after member grant: userB SHOULD see the note")
	}
	// Owner is never affected by the additive change.
	if !canSee(t, ctx, note, w3UserA) {
		t.Fatal("owner must always see their own note")
	}
}

// A grant on an ancestor folder cascades to a note in a descendant folder.
func TestFolderGrantInheritsToChild(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	parent := makeFolder(t, ctx, "fg-parent", pgtype.UUID{})
	child := makeFolder(t, ctx, "fg-child", parent)
	note := makeNoteInFolder(t, ctx, child)

	if canSee(t, ctx, note, w3UserB) {
		t.Fatal("baseline: userB should NOT see the note before any grant")
	}
	grantFolder(t, ctx, parent, "member", w3UserB, "viewer")
	if !canSee(t, ctx, note, w3UserB) {
		t.Fatal("inherited grant on parent SHOULD open a note in the child folder")
	}
}

// A 'workspace' (Whole team) grant opens the note for any member.
func TestFolderWorkspaceGrant(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	folder := makeFolder(t, ctx, "fg-workspace", pgtype.UUID{})
	note := makeNoteInFolder(t, ctx, folder)

	if canSee(t, ctx, note, w3UserB) {
		t.Fatal("baseline: userB should NOT see the note before any grant")
	}
	grantFolder(t, ctx, folder, "workspace", pgtype.UUID{}, "viewer")
	if !canSee(t, ctx, note, w3UserB) {
		t.Fatal("workspace grant SHOULD open the note for every member")
	}
}

// A grant to a group opens the note for members of that group only.
func TestFolderGroupGrant(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	folder := makeFolder(t, ctx, "fg-group", pgtype.UUID{})
	note := makeNoteInFolder(t, ctx, folder)

	var groupID pgtype.UUID
	if err := w3Pool.QueryRow(ctx,
		`INSERT INTO cerebro_group (workspace_id, name) VALUES ($1, $2) RETURNING id`,
		w3WsID, "fg-test-group",
	).Scan(&groupID); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := w3Pool.Exec(ctx,
		`INSERT INTO cerebro_group_member (group_id, user_id) VALUES ($1, $2)`,
		groupID, w3UserB,
	); err != nil {
		t.Fatalf("add group member: %v", err)
	}

	if canSee(t, ctx, note, w3UserB) {
		t.Fatal("baseline: userB should NOT see the note before any grant")
	}
	grantFolder(t, ctx, folder, "group", groupID, "viewer")
	if !canSee(t, ctx, note, w3UserB) {
		t.Fatal("group grant SHOULD open the note for a member of the group")
	}
}
