package channels

// FIR-2791 — OnlyArchivedChannels backs `GET /api/channels?archived_only=true`
// (the Archived block/view in the inbox). It must return exactly the rows the
// caller archived, per user, and stay the inverse of FilterArchivedChannels.
// Runs against the local dev DB like the rest of this suite (see service_test.go).

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func listChannelsFor(t *testing.T, userID pgtype.UUID) []db.ListChannelsForUserRow {
	t.Helper()
	rows, err := db.New(testPool).ListChannelsForUser(context.Background(), db.ListChannelsForUserParams{
		WorkspaceID: testWorkspaceID,
		UserID:      userID,
	})
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	return rows
}

func rowIDs(rows []db.ListChannelsForUserRow) map[string]bool {
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[uuidString(r.ID)] = true
	}
	return out
}

func TestOnlyArchivedChannels(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	userA := createTestUserAndMember(t, "archived-only-a")
	userB := createTestUserAndMember(t, "archived-only-b")
	dm1 := createTestDM(t, userA, userB)
	dm2 := createTestDM(t, userA, userB)

	rowsA := listChannelsFor(t, userA)
	if len(rowsA) < 2 {
		t.Fatalf("expected at least 2 channels for userA, got %d", len(rowsA))
	}

	// Nothing archived yet → archived-only returns nothing.
	if got := testSvc.OnlyArchivedChannels(ctx, userA, rowsA); len(got) != 0 {
		t.Fatalf("expected no archived channels, got %d", len(got))
	}

	// userA archives dm1.
	if err := testSvc.CerebroQueries.ArchiveChannelForUser(ctx, cerebrodb.ArchiveChannelForUserParams{
		ChannelID: dm1.ID,
		UserID:    userA,
	}); err != nil {
		t.Fatalf("archive dm1: %v", err)
	}
	t.Cleanup(func() {
		_ = testSvc.CerebroQueries.UnarchiveChannelForUser(context.Background(), cerebrodb.UnarchiveChannelForUserParams{
			ChannelID: dm1.ID, UserID: userA,
		})
	})

	// archived-only returns exactly dm1 for userA.
	got := testSvc.OnlyArchivedChannels(ctx, userA, rowsA)
	ids := rowIDs(got)
	if len(got) != 1 || !ids[uuidString(dm1.ID)] {
		t.Fatalf("expected exactly dm1 archived, got %d rows (%v)", len(got), ids)
	}

	// FilterArchivedChannels stays the inverse: dm1 hidden, dm2 kept.
	filtered := rowIDs(testSvc.FilterArchivedChannels(ctx, userA, listChannelsFor(t, userA), false))
	if filtered[uuidString(dm1.ID)] {
		t.Fatal("dm1 should be hidden from the non-archived list")
	}
	if !filtered[uuidString(dm2.ID)] {
		t.Fatal("dm2 should remain in the non-archived list")
	}

	// The archive is per-user: userB archived nothing.
	if got := testSvc.OnlyArchivedChannels(ctx, userB, listChannelsFor(t, userB)); len(got) != 0 {
		t.Fatalf("userB should have no archived channels, got %d", len(got))
	}
}
