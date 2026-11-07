package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProjectMoveAttachmentAccess(t *testing.T) {
	if testPool == nil {
		t.Fatal("PostgreSQL required")
	}
	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "")
	store := storage.NewLocalStorageFromEnv()
	h := *testHandler
	h.Storage = store
	source, target, project := moveFixture(t)
	sourceOnly := dbfx.User(t, "Source reader", "move-file-src-"+uuid.NewString()+"@test.invalid")
	targetOnly := dbfx.User(t, "Target reader", "move-file-dst-"+uuid.NewString()+"@test.invalid")
	dbfx.Member(t, source, sourceOnly, "member")
	dbfx.Member(t, target, targetOnly, "member")
	issue := dbfx.Issue(t, "Moved file", testutil.Cols{"workspace_id": source, "project_id": project})
	comment := dbfx.Comment(t, issue, "File comment", testutil.Cols{"workspace_id": source, "author_id": sourceOnly})
	key := "workspaces/" + source + "/" + uuid.NewString() + ".txt"
	url, err := store.Upload(context.Background(), key, []byte("moved bytes"), "text/plain", "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	attachment := dbfx.Insert(t, "attachment", testutil.Cols{"workspace_id": source, "comment_id": comment, "filename": "file.txt", "url": url, "content_type": "text/plain", "size_bytes": 11, "uploader_type": "member", "uploader_id": sourceOnly})
	executeMove(t, source, target, project)
	var storedURL, workspace string
	dbfx.QueryRow(t, "SELECT url,workspace_id FROM attachment WHERE id=$1", attachment).Scan(&storedURL, &workspace)
	if storedURL != url || workspace != target || store.KeyFromURL(storedURL) != key {
		t.Fatal("key changed or attachment not moved")
	}
	for _, tc := range []struct {
		name, user string
		want       int
	}{{"target", targetOnly, 200}, {"source", sourceOnly, 404}} {
		t.Run("authenticated/"+tc.name, func(t *testing.T) {
			req := withURLParam(newRequest("GET", "/api/attachments/"+attachment+"/download", nil), "id", attachment)
			req.Header.Set("X-User-ID", tc.user)
			response := testutil.Call(t, h.DownloadAttachment, req).Want(tc.want)
			if tc.want == 200 && response.Body.String() != "moved bytes" {
				t.Fatal("wrong bytes")
			}
		})
	}
	// MUL-3821/#4477: this static route deliberately remains public.
	for _, tc := range []struct{ name, user string }{{"target", targetOnly}, {"source", sourceOnly}, {"anonymous", ""}} {
		t.Run("public_upload/"+tc.name, func(t *testing.T) {
			req := newRequest("GET", url, nil)
			req.Header.Set("X-User-ID", tc.user)
			response := testutil.Call(t, h.ServeLocalUpload, req).Want(200)
			if response.Body.String() != "moved bytes" {
				t.Fatal("wrong public bytes")
			}
		})
	}
	testutil.Call(t, h.DeleteWorkspace, withURLParam(newRequest("DELETE", "/", nil), "id", source)).Want(204)
	req := withURLParam(newRequest("GET", "/", nil), "id", attachment)
	req.Header.Set("X-User-ID", targetOnly)
	response := testutil.Call(t, h.DownloadAttachment, req).Want(200)
	if response.Body.String() != "moved bytes" {
		t.Fatal("source deletion lost moved object")
	}
}
