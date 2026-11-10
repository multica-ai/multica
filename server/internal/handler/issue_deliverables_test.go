package handler

import (
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"testing"
	"time"
)

func TestIssueDeliverableEntries(t *testing.T) {
	id := parseUUID("00000000-0000-0000-0000-000000000001")
	gone := parseUUID("00000000-0000-0000-0000-000000000002")
	parent := parseUUID("00000000-0000-0000-0000-000000000003")
	comments := []db.Comment{
		{ID: id, AuthorType: "agent", AuthorID: parent, ParentID: parent, Content: "large historical body", CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}},
		{ID: gone, DeletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}},
	}
	files := map[string][]AttachmentResponse{uuidToString(id): {{ID: "old-chart", ContentType: "image/png"}, {ID: "same-name-new-version", ContentType: "image/png"}}, uuidToString(gone): {{ID: "deleted-image"}}, "orphan": {{ID: "orphan-image"}}}
	out := issueDeliverableEntries(comments, files)
	if len(out) != 1 || len(out[0].Attachments) != 2 {
		t.Fatalf("must retain all live uploads, omit deleted/orphan sources: %#v", out)
	}
	if out[0].Content != nil || out[0].ActorID != uuidToString(parent) || out[0].ParentID == nil {
		t.Fatal("return lightweight origin metadata without comment bodies")
	}
	if len(issueDeliverableEntries(nil, files)) != 0 {
		t.Fatal("an unbound attachment is not an output")
	}
}
