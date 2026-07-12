package rounds

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FIR-3114 — an unread inbox item on an issue that sits in one of the
// recipient's rounds must not count toward any unread badge (workspace badge,
// OS badge, notifications badge). It still counts for everyone else.
func TestUnreadBadgeCountsExcludeRoundMemberIssues(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	queries := db.New(pool)

	var wsID, ownerID, otherID, issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('Rounds Badge', 'rounds-badge-'||substr(gen_random_uuid()::text,1,8), '', 'RBG') RETURNING id`).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM workspace WHERE id=$1`, wsID)
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Round Owner', 'rounds-badge-owner-'||substr(gen_random_uuid()::text,1,8)||'@test.local') RETURNING id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM "user" WHERE id=$1`, ownerID)
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Bystander', 'rounds-badge-other-'||substr(gen_random_uuid()::text,1,8)||'@test.local') RETURNING id`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM "user" WHERE id=$1`, otherID)
	if err := pool.QueryRow(ctx, `INSERT INTO issue (workspace_id, title, number, creator_type, creator_id) VALUES ($1, 'Round issue', floor(random()*1000000)::int, 'member', $2) RETURNING id`, wsID, ownerID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}

	for _, recipient := range []pgtype.UUID{ownerID, otherID} {
		if _, err := pool.Exec(ctx, `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, issue_id, title, read) VALUES ($1, 'member', $2, 'comment', $3, 'Agent replied', false)`, wsID, recipient, issueID); err != nil {
			t.Fatal(err)
		}
	}

	countFor := func(recipient pgtype.UUID) (int64, int64) {
		t.Helper()
		ws, err := queries.CountUnreadInbox(ctx, db.CountUnreadInboxParams{WorkspaceID: wsID, RecipientType: "member", RecipientID: recipient})
		if err != nil {
			t.Fatal(err)
		}
		all, err := queries.CountUnreadInboxForUserAllWorkspaces(ctx, recipient)
		if err != nil {
			t.Fatal(err)
		}
		return ws, all
	}

	if ws, all := countFor(ownerID); ws != 1 || all != 1 {
		t.Fatalf("owner counts before round = %d/%d, want 1/1", ws, all)
	}

	svc := New(pool, queries, nil)
	round, err := svc.Create(ctx, wsID, ownerID, "Badge", "batch", "", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMember(ctx, wsID, ownerID, mustUUID(t, round.ID), issueID, "member", ownerID); err != nil {
		t.Fatal(err)
	}

	if ws, all := countFor(ownerID); ws != 0 || all != 0 {
		t.Fatalf("owner counts with round membership = %d/%d, want 0/0 (round issues never badge)", ws, all)
	}
	if ws, all := countFor(otherID); ws != 1 || all != 1 {
		t.Fatalf("bystander counts = %d/%d, want 1/1 (exclusion is owner-scoped)", ws, all)
	}
}
