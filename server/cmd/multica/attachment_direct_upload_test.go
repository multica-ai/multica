package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// uploadRecorder is a fake server that answers the presign -> PUT -> complete
// sequence and records what each entry point sent. It lets the four --attachment
// call sites (attachment upload, issue create, issue update, comment add) be
// asserted against one observable contract instead of each one's internals.
type uploadRecorder struct {
	t *testing.T

	mu            sync.Mutex
	presignBodies []map[string]any
	putBodies     [][]byte
	putAuth       []string
	completeCalls int

	// presignStatus, when set, makes presign fail with that status and body,
	// which is how the fallback and no-fallback cases are driven.
	presignStatus int
	presignBody   string

	// requestLog is every path the server saw, in order.
	requestLog []string

	attachmentID string
}

func newUploadRecorder(t *testing.T) (*uploadRecorder, *httptest.Server) {
	t.Helper()
	recorder := &uploadRecorder{t: t, attachmentID: "01a1cf5b-0000-7000-8000-000000000001"}
	server := httptest.NewServer(recorder)
	t.Cleanup(server.Close)
	return recorder, server
}

func (u *uploadRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.requestLog = append(u.requestLog, r.Method+" "+r.URL.Path)
	presignStatus := u.presignStatus
	presignBody := u.presignBody
	attachmentID := u.attachmentID
	u.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/attachments/presign-upload":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		u.mu.Lock()
		u.presignBodies = append(u.presignBodies, body)
		u.mu.Unlock()
		if presignStatus != 0 {
			w.WriteHeader(presignStatus)
			_, _ = io.WriteString(w, presignBody)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"attachment_id": attachmentID,
			"upload_url":    "http://" + r.Host + "/storage/" + attachmentID,
			"method":        http.MethodPut,
			"headers":       map[string]string{"Content-Type": "text/plain"},
			"expires_in":    900,
		})
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/storage/"):
		body, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.putBodies = append(u.putBodies, body)
		u.putAuth = append(u.putAuth, r.Header.Get("Authorization"))
		u.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete-upload"):
		u.mu.Lock()
		u.completeCalls++
		u.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           attachmentID,
			"filename":     "file.txt",
			"content_type": "text/plain; charset=utf-8",
			"size_bytes":   float64(8),
			"markdown_url": "/api/attachments/" + attachmentID + "/download",
			"download_url": "/api/attachments/" + attachmentID + "/download",
		})
	case r.Method == http.MethodPost && r.URL.Path == "/api/upload-file":
		// The legacy path, reached only by a fallback.
		body, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.putBodies = append(u.putBodies, body)
		u.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "legacy-attachment",
			"url":          "https://cdn.example.com/legacy",
			"filename":     "file.txt",
			"content_type": "text/plain",
			"markdown_url": "/api/attachments/legacy-attachment/download",
		})
	case r.Method == http.MethodPost && r.URL.Path == "/api/issues":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "01a1cf5b-0000-7000-8000-0000000000aa", "identifier": "TST-1", "number": 1,
			"title": "created", "status": "todo", "description": "",
		})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/issues/"):
		// issue update / comment add resolve the issue first
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "01a1cf5b-0000-7000-8000-0000000000aa", "identifier": "TST-1",
			"title": "issue", "status": "todo", "description": "",
		})
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/issues/"):
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "01a1cf5b-0000-7000-8000-0000000000aa", "identifier": "TST-1",
			"title": "issue", "status": "todo", "description": "",
		})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments"):
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "01a1cf5b-0000-7000-8000-0000000000bb", "content": "posted",
		})
	case r.Method == http.MethodGet && r.URL.Path == "/api/config":
		_ = json.NewEncoder(w).Encode(map[string]any{})
	default:
		u.t.Logf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (u *uploadRecorder) uploadCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.presignBodies)
}

func (u *uploadRecorder) sawPath(suffix string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, entry := range u.requestLog {
		if strings.HasSuffix(entry, suffix) {
			return true
		}
	}
	return false
}

// chdirTemp moves the process into a fresh temp dir for one test, which is what
// makes the --attachment workdir guard accept the fixture paths.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	// t.TempDir on macOS returns /var/... while the resolved cwd is /private/var,
	// so hand back the resolved path the guard will compare against.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return resolved
}

func writeFixture(t *testing.T, dir, name string, size int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// newIssueCreateTestCmd builds the issue-create command wired for the direct
// path. The server URL comes from setCLITestServerEnv.
func newDirectUploadIssueCreateCmd(attachments ...string) *cobra.Command {
	cmd := &cobra.Command{Use: "create", RunE: runIssueCreate}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("description-stdin", "", "")
	cmd.Flags().String("description-file", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("priority", "", "")
	cmd.Flags().String("parent", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().Int("stage", 0, "")
	cmd.Flags().String("start-date", "", "")
	cmd.Flags().String("due-date", "", "")
	cmd.Flags().String("assignee", "", "")
	cmd.Flags().Bool("allow-duplicate", false, "")
	cmd.Flags().Bool("allow-external-file", false, "")
	cmd.Flags().StringArray("attachment", attachments, "")
	cmd.Flags().StringSlice("attachment-id", nil, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("pr-auto-complete", "", "")
	cmd.Flags().StringArray("property", nil, "")
	cmd.Flags().Int("position", 0, "")
	return cmd
}

func TestIssueCreateAttachment_UsesDirectUploadAndAppendsMarkdown(t *testing.T) {
	recorder, server := newUploadRecorder(t)
	setCLITestServerEnv(t, server.URL)
	dir := chdirTemp(t)
	path := writeFixture(t, dir, "chart.txt", 8)

	cmd := newDirectUploadIssueCreateCmd(path)
	mustSet(t, cmd, "title", "with attachment")

	if _, err := captureStdout(t, func() error { return runIssueCreate(cmd, nil) }); err != nil {
		t.Fatalf("issue create: %v", err)
	}

	if recorder.uploadCount() != 1 {
		t.Fatalf("direct uploads = %d, want 1 (request log: %v)", recorder.uploadCount(), recorder.requestLog)
	}
	if !recorder.sawPath("/complete-upload") {
		t.Fatalf("complete-upload was never called: %v", recorder.requestLog)
	}
	if recorder.sawPath("/api/upload-file") {
		t.Fatalf("the create fell back to the multipart endpoint: %v", recorder.requestLog)
	}
	// The issue is created only after the upload, and its description carries
	// the attachment reference.
	if !strings.HasSuffix(recorder.requestLog[len(recorder.requestLog)-1], "/api/issues") {
		t.Fatalf("last request = %q, want the issue create", recorder.requestLog[len(recorder.requestLog)-1])
	}
}

func TestIssueUpdateAttachment_UsesDirectUpload(t *testing.T) {
	recorder, server := newUploadRecorder(t)
	setCLITestServerEnv(t, server.URL)
	dir := chdirTemp(t)
	path := writeFixture(t, dir, "update.txt", 8)

	cmd := newDirectUploadIssueUpdateCmd(path)
	mustSet(t, cmd, "title", "renamed")

	if _, err := captureStdout(t, func() error { return runIssueUpdate(cmd, []string{"TST-1"}) }); err != nil {
		t.Fatalf("issue update: %v", err)
	}
	if recorder.uploadCount() != 1 {
		t.Fatalf("direct uploads = %d, want 1", recorder.uploadCount())
	}
	if recorder.sawPath("/api/upload-file") {
		t.Fatalf("update fell back to the multipart endpoint: %v", recorder.requestLog)
	}
}

func TestCommentAddAttachment_UsesDirectUpload(t *testing.T) {
	recorder, server := newUploadRecorder(t)
	setCLITestServerEnv(t, server.URL)
	dir := chdirTemp(t)
	path := writeFixture(t, dir, "comment.txt", 8)

	cmd := newDirectUploadCommentAddCmd(path)
	mustSet(t, cmd, "content", "see attached")

	if _, err := captureStdout(t, func() error { return runIssueCommentAdd(cmd, []string{"TST-1"}) }); err != nil {
		t.Fatalf("comment add: %v", err)
	}
	if recorder.uploadCount() != 1 {
		t.Fatalf("direct uploads = %d, want 1", recorder.uploadCount())
	}
	if recorder.sawPath("/api/upload-file") {
		t.Fatalf("comment add fell back to the multipart endpoint: %v", recorder.requestLog)
	}
}

func TestAttachmentUploadCommand_UsesDirectUpload(t *testing.T) {
	recorder, server := newUploadRecorder(t)
	setCLITestServerEnv(t, server.URL)
	dir := chdirTemp(t)
	path := writeFixture(t, dir, "reply.txt", 8)

	cmd := newDirectUploadAttachmentCmd()
	mustSet(t, cmd, "task", "01a1cf5b-0000-7000-8000-0000000000cc")

	out, err := captureStdout(t, func() error { return runAttachmentUpload(cmd, []string{path}) })
	if err != nil {
		t.Fatalf("attachment upload: %v", err)
	}
	if recorder.uploadCount() != 1 {
		t.Fatalf("direct uploads = %d, want 1", recorder.uploadCount())
	}
	// The output contract is unchanged: id, filename, markdown_url, markdown.
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	for _, field := range []string{"id", "filename", "markdown_url", "markdown"} {
		if _, ok := result[field]; !ok {
			t.Fatalf("output is missing %q: %s", field, out)
		}
	}
	if !recorder.sawPath("/complete-upload") {
		t.Fatalf("complete-upload was never called: %v", recorder.requestLog)
	}
}

func TestAttachmentEntries_PreflightFailureUploadsNothing(t *testing.T) {
	// Every entry must validate ALL paths before uploading any: a bad second
	// path must not leave the first file uploaded as an orphan.
	dir := chdirTemp(t)
	good := writeFixture(t, dir, "good.txt", 8)
	missing := filepath.Join(dir, "missing.txt")

	entries := map[string]func(t *testing.T, paths ...string) error{
		"issue create": func(t *testing.T, paths ...string) error {
			cmd := newDirectUploadIssueCreateCmd(paths...)
			mustSet(t, cmd, "title", "preflight")
			_, err := captureStdout(t, func() error { return runIssueCreate(cmd, nil) })
			return err
		},
		"issue update": func(t *testing.T, paths ...string) error {
			cmd := newDirectUploadIssueUpdateCmd(paths...)
			mustSet(t, cmd, "title", "preflight")
			_, err := captureStdout(t, func() error { return runIssueUpdate(cmd, []string{"TST-1"}) })
			return err
		},
		"comment add": func(t *testing.T, paths ...string) error {
			cmd := newDirectUploadCommentAddCmd(paths...)
			mustSet(t, cmd, "content", "preflight")
			_, err := captureStdout(t, func() error { return runIssueCommentAdd(cmd, []string{"TST-1"}) })
			return err
		},
	}

	for name, run := range entries {
		t.Run(name, func(t *testing.T) {
			recorder, server := newUploadRecorder(t)
			setCLITestServerEnv(t, server.URL)

			err := run(t, good, missing)
			if err == nil {
				t.Fatalf("%s accepted a missing attachment", name)
			}
			if recorder.uploadCount() != 0 {
				t.Fatalf("%s uploaded %d files before failing preflight", name, recorder.uploadCount())
			}
			if recorder.sawPath("/api/upload-file") {
				t.Fatalf("%s reached the multipart endpoint during preflight", name)
			}
		})
	}
}

func TestAttachmentEntries_RejectPathOutsideWorkdir(t *testing.T) {
	dir := chdirTemp(t)
	outsideDir := t.TempDir()
	outside := writeFixture(t, outsideDir, "outside.txt", 8)

	entries := map[string]func(t *testing.T, paths ...string) error{
		"issue create": func(t *testing.T, paths ...string) error {
			cmd := newDirectUploadIssueCreateCmd(paths...)
			mustSet(t, cmd, "title", "isolation")
			_, err := captureStdout(t, func() error { return runIssueCreate(cmd, nil) })
			return err
		},
		"comment add": func(t *testing.T, paths ...string) error {
			cmd := newDirectUploadCommentAddCmd(paths...)
			mustSet(t, cmd, "content", "isolation")
			_, err := captureStdout(t, func() error { return runIssueCommentAdd(cmd, []string{"TST-1"}) })
			return err
		},
	}
	_ = dir
	for name, run := range entries {
		t.Run(name, func(t *testing.T) {
			recorder, server := newUploadRecorder(t)
			setCLITestServerEnv(t, server.URL)

			err := run(t, outside)
			if err == nil {
				t.Fatalf("%s attached a file from outside the workdir", name)
			}
			if !strings.Contains(err.Error(), "outside the current working directory") {
				t.Fatalf("%s error = %v, want the workdir guard", name, err)
			}
			if recorder.uploadCount() != 0 {
				t.Fatalf("%s uploaded anyway", name)
			}
		})
	}
}

func TestAttachmentEntries_FallBackWhenServerLacksDirectUpload(t *testing.T) {
	// A pre-endpoint server answers 404; the upload must still succeed through
	// the legacy multipart route, and the output contract must not change.
	dir := chdirTemp(t)
	path := writeFixture(t, dir, "legacy.txt", 8)

	recorder, server := newUploadRecorder(t)
	recorder.presignStatus = http.StatusNotFound
	recorder.presignBody = `{"error":"not found"}`
	setCLITestServerEnv(t, server.URL)

	cmd := newDirectUploadAttachmentCmd()
	mustSet(t, cmd, "task", "01a1cf5b-0000-7000-8000-0000000000cc")
	out, err := captureStdout(t, func() error { return runAttachmentUpload(cmd, []string{path}) })
	if err != nil {
		t.Fatalf("attachment upload: %v", err)
	}
	if !recorder.sawPath("/api/upload-file") {
		t.Fatalf("no fallback multipart request was made: %v", recorder.requestLog)
	}
	// The legacy body must be a real multipart form carrying the file bytes.
	if len(recorder.putBodies) == 0 || !strings.Contains(string(recorder.putBodies[0]), "legacy.txt") {
		t.Fatalf("fallback body does not carry the file: %q", recorder.putBodies)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if result["id"] != "legacy-attachment" {
		t.Fatalf("id = %v, want the fallback attachment id", result["id"])
	}
}

func TestAttachmentEntries_DoNotFallBackOnAuthFailure(t *testing.T) {
	dir := chdirTemp(t)
	path := writeFixture(t, dir, "denied.txt", 8)

	recorder, server := newUploadRecorder(t)
	recorder.presignStatus = http.StatusForbidden
	recorder.presignBody = `{"error":"forbidden"}`
	setCLITestServerEnv(t, server.URL)

	cmd := newDirectUploadAttachmentCmd()
	mustSet(t, cmd, "task", "01a1cf5b-0000-7000-8000-0000000000cc")
	if _, err := captureStdout(t, func() error { return runAttachmentUpload(cmd, []string{path}) }); err == nil {
		t.Fatal("attachment upload reported success on a 403")
	}
	if recorder.sawPath("/api/upload-file") {
		t.Fatalf("a 403 fell back to the legacy endpoint: %v", recorder.requestLog)
	}
}

func TestCollectLocalAttachments_RecordsSizeWithoutReading(t *testing.T) {
	dir := chdirTemp(t)
	path := writeFixture(t, dir, "sized.txt", 4096)

	cmd := newDirectUploadIssueCreateCmd(path)
	pending, err := collectLocalAttachments(cmd, []string{path})
	if err != nil {
		t.Fatalf("collectLocalAttachments: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	if pending[0].size != 4096 {
		t.Fatalf("recorded size = %d, want 4096", pending[0].size)
	}
	if pending[0].path != path {
		t.Fatalf("path = %q, want %q", pending[0].path, path)
	}
}

func TestCollectLocalAttachments_SkipsURLsWithWarning(t *testing.T) {
	dir := chdirTemp(t)
	path := writeFixture(t, dir, "real.txt", 4)

	cmd := newDirectUploadIssueCreateCmd()
	pending, err := collectLocalAttachments(cmd, []string{"https://example.com/x.png", path})
	if err != nil {
		t.Fatalf("collectLocalAttachments: %v", err)
	}
	if len(pending) != 1 || pending[0].path != path {
		t.Fatalf("pending = %+v, want only the local path", pending)
	}
}

func mustSet(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set --%s: %v", name, err)
	}
}

func newDirectUploadIssueUpdateCmd(attachments ...string) *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("title", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("description-stdin", "", "")
	cmd.Flags().String("description-file", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("priority", "", "")
	cmd.Flags().String("assignee", "", "")
	cmd.Flags().String("parent", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().Int("stage", 0, "")
	cmd.Flags().String("duplicate-of", "", "")
	cmd.Flags().Float64("position", 0, "")
	cmd.Flags().String("start-date", "", "")
	cmd.Flags().String("due-date", "", "")
	cmd.Flags().String("pr-auto-complete", "", "")
	cmd.Flags().Bool("allow-external-file", false, "")
	cmd.Flags().StringArray("attachment", attachments, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newDirectUploadCommentAddCmd(attachments ...string) *cobra.Command {
	cmd := &cobra.Command{Use: "add"}
	cmd.Flags().String("content", "", "")
	cmd.Flags().String("content-stdin", "", "")
	cmd.Flags().String("content-file", "", "")
	cmd.Flags().String("parent", "", "")
	cmd.Flags().Bool("allow-external-file", false, "")
	cmd.Flags().StringArray("attachment", attachments, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().StringArray("steer", nil, "")
	cmd.Flags().Bool("steer-active", false, "")
	cmd.Flags().String("submission-id", "", "")
	cmd.Flags().String("task", "", "")
	return cmd
}

func newDirectUploadAttachmentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "upload"}
	cmd.Flags().String("task", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}
