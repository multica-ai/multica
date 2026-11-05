//go:build verificationdev

package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/verificationworkspace"
)

func TestVerificationWorkspaceImportIsIdempotentAndIsolated(t *testing.T) {
	t.Setenv("MULTICA_VERIFICATION_BACKEND", uuid.NewString())
	t.Setenv("MULTICA_VERIFICATION_TOKEN", strings.Repeat("a", 64))
	t.Setenv("MULTICA_VERIFICATION_DIRECTORY", t.TempDir())
	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	local := *testHandler
	local.Storage = storage.NewLocalStorageFromEnv()
	router := chi.NewRouter()
	local.RegisterVerificationRoutes(router)
	call := func(path string, body any) *testutil.Response {
		req := newRequest(http.MethodPost, path+"?workspace_id="+testWorkspaceID, body)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("X-Multica-Verification-Token", strings.Repeat("a", 64))
		return testutil.Call(t, router.ServeHTTP, req)
	}
	var catalog struct {
		Statuses []map[string]any `json:"statuses"`
	}
	testutil.Call(t, testHandler.ListIssueStatuses, newRequest("GET", "/api/issue-statuses?workspace_id="+testWorkspaceID, nil)).Want(200).JSON(&catalog)
	project, issue, comment := uuid.NewString(), uuid.NewString(), uuid.NewString()
	content := verificationWorkspaceContent{SchemaVersion: 1, Workspace: map[string]any{"id": uuid.NewString()}, Statuses: catalog.Statuses,
		Projects: []map[string]any{{"id": project, "title": "Copied project", "status": "planned", "priority": "none"}},
		Issues:   []map[string]any{{"id": issue, "title": "Copied issue", "status": "todo", "priority": "medium", "project_id": project}},
		Comments: []map[string]any{{"id": comment, "issue_id": issue, "content": "Copied comment", "type": "comment", "created_at": "2026-01-01T00:00:00Z"}},
	}
	// Source DTOs retain their original workspace identity, not the test target.
	for _, status := range content.Statuses {
		delete(status, "workspace_id")
	}
	workflow, label, parent, attachment := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	content.Workflows = []map[string]any{{"id": workflow, "name": "Copied workflow", "steps": []any{map[string]any{"status_key": "todo", "handler": map[string]any{"type": "agent", "id": uuid.NewString()}, "instructions": "Do not execute"}, map[string]any{"status_key": "done"}}}}
	content.Projects[0]["workflow_id"] = workflow
	content.Labels = []map[string]any{{"id": label, "name": strings.Repeat("L", 32), "color": "#123456"}}
	content.Issues[0]["labels"] = []any{map[string]any{"id": label}}
	content.Issues[0]["parent_issue_id"] = parent
	content.Issues = append(content.Issues, map[string]any{"id": parent, "title": "Parent issue", "status": "backlog", "priority": "none", "project_id": project})
	bytes := []byte("actual attachment bytes")
	sum := sha256.Sum256(bytes)
	content.Attachments = []verificationworkspace.Attachment{{ID: attachment, Filename: "notes.txt", ContentType: "text/plain", SizeBytes: len(bytes), IssueID: &issue, CommentID: &comment, Data: base64.StdEncoding.EncodeToString(bytes), SHA256: hex.EncodeToString(sum[:])}}
	content.Comments[0]["author_id"] = uuid.NewString()
	unrelated := dbfx.Issue(t, "Unrelated test content", testutil.Cols{"id": testUserID, "number": 1000000})
	input := verificationWorkspaceRequest{RestoreID: uuid.NewString() + ":1", Content: content}
	var first, second struct {
		Mapping map[string]string `json:"mapping"`
	}
	call("/api/verification/import", input).Want(200).JSON(&first)
	t.Cleanup(func() { call("/api/verification/reset", map[string]string{"restoreId": input.RestoreID}).Want(200) })
	call("/api/verification/import", input).Want(200).JSON(&second)
	if first.Mapping[issue] == issue || first.Mapping[issue] != second.Mapping[issue] {
		t.Fatalf("import mapping changed or retained source ID: %#v", second.Mapping)
	}
	var gotProject, title string
	var assignee *string
	dbfx.QueryRow(t, "SELECT project_id::text,title,assignee_id::text FROM issue WHERE id=$1", first.Mapping[issue]).Scan(&gotProject, &title, &assignee)
	if gotProject != first.Mapping[project] || title != "Copied issue" || assignee != nil {
		t.Fatalf("incorrect copy: %s %s %v", gotProject, title, assignee)
	}
	var count int
	dbfx.QueryRow(t, "SELECT count(*) FROM comment WHERE id=$1", first.Mapping[comment]).Scan(&count)
	if count != 1 {
		t.Fatalf("comments=%d", count)
	}
	var copiedParent, objectURL string
	var steps []byte
	dbfx.QueryRow(t, "SELECT parent_issue_id::text FROM issue WHERE id=$1", first.Mapping[issue]).Scan(&copiedParent)
	if copiedParent != first.Mapping[parent] {
		t.Fatal("parent mapping missing")
	}
	dbfx.QueryRow(t, "SELECT steps FROM issue_workflow WHERE id=$1", first.Mapping[workflow]).Scan(&steps)
	if strings.Contains(string(steps), `"type": "agent"`) {
		t.Fatal("workflow handoff remained executable")
	}
	dbfx.QueryRow(t, "SELECT url FROM attachment WHERE id=$1", first.Mapping[attachment]).Scan(&objectURL)
	reader, err := local.Storage.GetReader(t.Context(), local.Storage.KeyFromURL(objectURL))
	if err != nil {
		t.Fatal(err)
	}
	copied, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(copied) != string(bytes) {
		t.Fatal("attachment bytes differ")
	}
	dbfx.QueryRow(t, "SELECT count(*) FROM issue_to_label WHERE issue_id=$1 AND label_id=$2", first.Mapping[issue], first.Mapping[label]).Scan(&count)
	if count != 1 {
		t.Fatal("label mapping missing")
	}
	input.Content.Issues[0]["title"] = "Conflicting payload"
	call("/api/verification/import", input).Want(409)
	outsideChild := dbfx.Issue(t, "Additional reference", testutil.Cols{"number": 1000001, "parent_issue_id": first.Mapping[issue]})
	call("/api/verification/reset", map[string]string{"restoreId": input.RestoreID}).Want(409)
	dbfx.QueryRow(t, "SELECT count(*) FROM issue WHERE id=$1", first.Mapping[issue]).Scan(&count)
	if count != 1 {
		t.Fatal("rejected reset changed the imported graph")
	}
	dbfx.Exec(t, "UPDATE issue SET parent_issue_id=NULL WHERE id=$1", outsideChild)
	call("/api/verification/reset", map[string]string{"restoreId": input.RestoreID}).Want(200)
	dbfx.QueryRow(t, "SELECT count(*) FROM attachment WHERE id=$1", first.Mapping[attachment]).Scan(&count)
	if count != 0 {
		t.Fatal("reset retained an owned attachment row")
	}
	dbfx.QueryRow(t, "SELECT count(*) FROM issue WHERE id=$1", unrelated).Scan(&count)
	if count != 1 {
		t.Fatal("reset removed a resource outside the imported graph")
	}
}
func TestVerificationBackendRequiresLoopbackCapability(t *testing.T) {
	t.Setenv("MULTICA_VERIFICATION_BACKEND", uuid.NewString())
	t.Setenv("MULTICA_VERIFICATION_TOKEN", strings.Repeat("a", 64))
	t.Setenv("MULTICA_VERIFICATION_DIRECTORY", t.TempDir())
	router := chi.NewRouter()
	testHandler.RegisterVerificationRoutes(router)
	for _, test := range []struct {
		address, token string
		want           int
	}{{"127.0.0.1:1", "", 403}, {"192.0.2.1:1", strings.Repeat("a", 64), 403}, {"127.0.0.1:1", strings.Repeat("a", 64), 200}} {
		req := newRequest("GET", "/api/verification/capabilities", nil)
		req.RemoteAddr = test.address
		req.Header.Set("X-Multica-Verification-Token", test.token)
		response := testutil.Call(t, router.ServeHTTP, req).Want(test.want)
		if test.want == 200 {
			var value map[string]any
			if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
				t.Fatal(err)
			}
			if value["environmentId"] != os.Getenv("MULTICA_VERIFICATION_BACKEND") {
				t.Fatal("wrong environment")
			}
		}
	}
}
