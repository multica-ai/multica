package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The issue attachment list labels each comment file with its comment's
// author and time, so the file inventory needs no second request and no
// comment bodies. Description files and files of a deleted comment carry none.
func TestListAttachmentsCarriesCommentOrigins(t *testing.T) {
	issueID := dbfx.Issue(t, "attachment origins")
	agentID := dbfx.Agent(t, "attachment origin agent", testRuntimeID)
	// Uploaded by the member, posted in the agent's comment: the label is the
	// comment's author, not the uploader.
	commentID := dbfx.Comment(t, issueID, "delivered chart", testutil.Cols{"author_type": "agent", "author_id": agentID})
	goneID := dbfx.Comment(t, issueID, "deleted", testutil.Cols{"deleted_at": testutil.Raw("now()")})
	attachment := func(name string, cols testutil.Cols) string {
		base := testutil.Cols{
			"workspace_id": testWorkspaceID, "issue_id": issueID,
			"uploader_type": "member", "uploader_id": testUserID,
			"filename": name, "url": "https://files.example.test/" + name,
			"content_type": "image/png", "size_bytes": 1,
		}
		for k, v := range cols {
			base[k] = v
		}
		return dbfx.Insert(t, "attachment", base)
	}
	inComment := attachment("chart.png", testutil.Cols{"comment_id": commentID})
	inDescription := attachment("description.png", nil)
	inDeleted := attachment("gone.png", testutil.Cols{"comment_id": goneID})

	req := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+issueID+"/attachments", nil), testUserID), "id", issueID)
	var got []AttachmentResponse
	testutil.Call(t, testHandler.ListAttachments, req).Want(http.StatusOK).JSON(&got)
	byID := map[string]AttachmentResponse{}
	for _, a := range got {
		byID[a.ID] = a
	}
	if len(byID) != 3 {
		t.Fatalf("attachments = %+v, want 3", got)
	}
	c := byID[inComment]
	if c.CommentAuthorType == nil || *c.CommentAuthorType != "agent" || c.CommentAuthorID == nil || *c.CommentAuthorID != agentID || c.CommentCreatedAt == nil || *c.CommentCreatedAt == "" {
		t.Fatalf("comment file origin = %+v, want agent %s", c, agentID)
	}
	for _, id := range []string{inDescription, inDeleted} {
		if a := byID[id]; a.CommentAuthorType != nil || a.CommentAuthorID != nil || a.CommentCreatedAt != nil {
			t.Fatalf("file %s must carry no comment origin: %+v", a.Filename, a)
		}
	}
}
