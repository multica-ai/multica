package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestInboxRowToResponseUsesCurrentIssueTitle(t *testing.T) {
	resp := inboxRowToResponse(db.ListInboxItemsRow{
		Title:      "title captured when the notification was created",
		IssueTitle: pgtype.Text{String: "current issue title", Valid: true},
	})

	if resp.Title != "current issue title" {
		t.Fatalf("expected current issue title, got %q", resp.Title)
	}
}

func TestInboxRowToResponseFallsBackToStoredTitleWithoutIssue(t *testing.T) {
	resp := inboxRowToResponse(db.ListInboxItemsRow{
		Title: "standalone inbox title",
	})

	if resp.Title != "standalone inbox title" {
		t.Fatalf("expected stored title fallback, got %q", resp.Title)
	}
}
