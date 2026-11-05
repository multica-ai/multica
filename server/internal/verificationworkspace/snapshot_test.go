//go:build verificationdev

package verificationworkspace

import (
	"testing"

	"github.com/google/uuid"
)

func TestValidateGraphBeforeImport(t *testing.T) {
	ws, parent, child, status := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	content := Content{SchemaVersion: 1, Workspace: map[string]any{"id": ws}, Statuses: []map[string]any{{"id": status, "key": "todo"}}, Issues: []map[string]any{{"id": child, "title": "child", "status": "todo", "parent_issue_id": parent}, {"id": parent, "title": "parent", "status": "todo"}}}
	if err := content.Validate(); err != nil {
		t.Fatal(err)
	}
	if Text(content.Issues[0], "id") != parent {
		t.Fatal("parent must precede child")
	}
	content.Issues[0]["parent_issue_id"] = child
	if err := content.Validate(); err == nil {
		t.Fatal("cyclic graph accepted")
	}
	delete(content.Issues[0], "parent_issue_id")
	content.Issues[0]["workspace_id"] = uuid.NewString()
	if err := content.Validate(); err == nil {
		t.Fatal("cross-workspace graph accepted")
	}
}
func TestAttachmentIntegrityBeforeMutation(t *testing.T) {
	content := Content{SchemaVersion: 1, Workspace: map[string]any{"id": uuid.NewString()}, Attachments: []Attachment{{ID: uuid.NewString(), Data: "aGVsbG8=", SizeBytes: 5, SHA256: "wrong"}}}
	if err := content.Validate(); err == nil {
		t.Fatal("corrupt attachment accepted")
	}
}
