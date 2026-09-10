package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func commentAt(created time.Time) db.Comment {
	return db.Comment{
		AuthorType: "system",
		Content:    "task failed",
		Type:       "system",
		CreatedAt:  pgtype.Timestamptz{Time: created, Valid: true},
	}
}

// A comment:created broadcast must carry the SAME created_at string the REST
// timeline returns for that comment. pgx decodes timestamptz into the process
// location, so formatting with a literal "Z" suffix published local wall-clock
// digits under a UTC label: on an Asia/Shanghai deployment every WS-delivered
// agent/system comment landed 8 hours from its real instant, then jumped back
// into place once a refetch replaced it with the REST value.
func TestCommentEventFieldsCreatedAtCarriesRealOffset(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*60*60)
	created := time.Date(2026, 8, 24, 17, 7, 5, 0, shanghai)

	got, _ := commentEventFields(commentAt(created))["created_at"].(string)
	if want := "2026-08-24T17:07:05+08:00"; got != want {
		t.Fatalf("created_at = %q, want %q", got, want)
	}
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("parse created_at %q: %v", got, err)
	}
	if !parsed.Equal(created) {
		t.Fatalf("created_at %q decodes to %s, want %s", got, parsed.UTC(), created.UTC())
	}
}

// The overwhelmingly common deployment runs the server process in UTC, where
// the old literal-"Z" format was already correct. Pin that those clients see a
// byte-identical payload so the fix carries no migration for them.
func TestCommentEventFieldsCreatedAtUTCIsUnchanged(t *testing.T) {
	created := time.Date(2026, 8, 24, 9, 7, 5, 0, time.UTC)

	got, _ := commentEventFields(commentAt(created))["created_at"].(string)
	if want := "2026-08-24T09:07:05Z"; got != want {
		t.Fatalf("created_at = %q, want %q", got, want)
	}
}

// An unset timestamp must not render as a zero-value instant that would sort
// to the beginning of every timeline.
func TestCommentEventFieldsCreatedAtInvalidIsEmpty(t *testing.T) {
	got, ok := commentEventFields(db.Comment{})["created_at"].(string)
	if !ok || got != "" {
		t.Fatalf("created_at = %q (string=%v), want empty string", got, ok)
	}
}
