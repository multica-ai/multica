package inbox

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestEventItemCarriesSection(t *testing.T) {
	for _, tc := range []struct {
		name string
		item db.InboxItem
		want string
	}{
		{"stored update", db.InboxItem{Type: "quick_create_done", Section: pgtype.Text{String: SectionUpdates, Valid: true}}, SectionUpdates},
		{"stored needs you", db.InboxItem{Type: "new_comment", Section: pgtype.Text{String: SectionNeedsYou, Valid: true}}, SectionNeedsYou},
		{"unstored addressed type", db.InboxItem{Type: "mentioned"}, SectionNeedsYou},
		{"unstored update type", db.InboxItem{Type: "issue_subscribed"}, SectionUpdates},
	} {
		got := EventItem(tc.item)
		if got["section"] != tc.want {
			t.Errorf("%s: section = %v, want %q", tc.name, got["section"], tc.want)
		}
		if _, ok := got["snoozed_until"]; !ok {
			t.Errorf("%s: snoozed_until missing", tc.name)
		}
	}
}
