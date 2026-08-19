package db

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCreateCommentRowCommentPreservesCanonicalFields(t *testing.T) {
	viaPluginID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	row := CreateCommentRow{
		Content:       "created comment",
		Revision:      7,
		ViaPluginID:   viaPluginID,
		IssueRevision: 11,
	}

	comment := row.Comment()
	if comment.Content != row.Content {
		t.Fatalf("content = %q, want %q", comment.Content, row.Content)
	}
	if comment.Revision != row.Revision {
		t.Fatalf("revision = %d, want %d", comment.Revision, row.Revision)
	}
	if comment.ViaPluginID != viaPluginID {
		t.Fatalf("via_plugin_id = %#v, want %#v", comment.ViaPluginID, viaPluginID)
	}
}
