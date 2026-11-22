package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestTaskSupplementCommentDescriptionAnchorsDefault(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	const content = "Ordinary supplemental instruction"
	row, err := testHandler.Queries.CreateTaskSupplement(t.Context(), db.CreateTaskSupplementParams{
		TaskID:          parseUUID(f.taskID),
		IssueID:         parseUUID(f.issueID),
		WorkspaceID:     parseUUID(testWorkspaceID),
		AuthorID:        parseUUID(testUserID),
		Content:         content,
		ClientRequestID: parseUUID("0199a4e8-22ce-7b01-bba5-444444444444"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the generated SQL and row.Scan against the migrated schema.
	// Supplements do not attach description selections and retain the default.
	if string(row.DescriptionAnchors) != "[]" {
		t.Fatalf("supplement anchors = %s, want []", row.DescriptionAnchors)
	}
	if row.Content != content || row.ParentID != parseUUID(f.triggerID) ||
		row.SupplementTaskID != parseUUID(f.taskID) || row.SupplementStatus != "pending" {
		t.Fatalf("supplement row lost its content, parent or receipt: %+v", row)
	}
	stored, err := testHandler.Queries.GetComment(t.Context(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.DescriptionAnchors) != "[]" || stored.Content != content {
		t.Fatalf("persisted supplemental comment = %+v", stored)
	}
	req := withURLParam(newRequest(http.MethodGet, "/api/issues/"+f.issueID+"/comments", nil), "id", f.issueID)
	var comments []CommentResponse
	testutil.Call(t, testHandler.ListComments, req).Want(http.StatusOK).JSON(&comments)
	for _, comment := range comments {
		if comment.ID == uuidToString(row.ID) {
			if comment.Content != content || len(comment.DescriptionAnchors) != 0 {
				t.Fatalf("ordinary supplement response changed: %+v", comment)
			}
			return
		}
	}
	t.Fatal("supplemental comment missing from ordinary comment list")
}
