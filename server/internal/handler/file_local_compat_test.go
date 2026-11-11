package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLocalAttachmentResponseCompatibility(t *testing.T) {
	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "https://files.example.test")
	h := &Handler{Storage: storage.NewLocalStorageFromEnv(), cfg: Config{PublicURL: "https://api.example.test"}}
	id := "11111111-2222-3333-4444-555555555555"
	raw := "https://files.example.test/uploads/workspaces/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/" + id + ".png"
	unbound := db.Attachment{ID: parseUUID(id), Url: raw}
	want := "https://api.example.test/api/attachments/" + id + "/download"
	for _, mode := range []attachmentURLMode{attachmentURLModeSigned, attachmentURLModeStable} {
		resp := h.attachmentToResponse(unbound, mode)
		if resp.URL != raw || resp.MarkdownURL != want {
			t.Fatalf("unbound avatar upload must retain storage URL and durable API markdown: %+v", resp)
		}
		for _, binding := range []string{"issue", "comment", "chat_session", "chat_message", "task"} {
			t.Run(binding, func(t *testing.T) {
				bound := unbound
				switch binding {
				case "issue":
					bound.IssueID = parseUUID(id)
				case "comment":
					bound.CommentID = parseUUID(id)
				case "chat_session":
					bound.ChatSessionID = parseUUID(id)
				case "chat_message":
					bound.ChatMessageID = parseUUID(id)
				case "task":
					bound.TaskID = parseUUID(id)
				}
				resp := h.attachmentToResponse(bound, mode)
				if resp.URL != want || resp.MarkdownURL != want || resp.DownloadURL != "/api/attachments/"+id+"/download" {
					t.Fatalf("bound response must use durable authenticated URLs: %+v", resp)
				}
				if bound.Url != raw {
					t.Fatal("response conversion changed the storage record")
				}
			})
		}
	}
}
