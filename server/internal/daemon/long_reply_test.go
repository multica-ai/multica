package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/redact"
)

func TestLongReply(t *testing.T) {
	for _, tc := range []struct {
		name            string
		enabled         string
		threshold       string
		body            string
		status          string
		issue           string
		chat            string
		token           string
		uploadStatus    int
		response        string
		wantUpload      bool
		wantReplacement bool
	}{
		{name: "disabled", body: strings.Repeat("x", 8001)},
		{name: "explicitly disabled", enabled: "false", body: strings.Repeat("x", 8001)},
		{name: "boundary", enabled: "true", body: strings.Repeat("界", 7000)},
		{name: "over boundary", enabled: "true", body: strings.Repeat("界", 7001), wantUpload: true, wantReplacement: true},
		{name: "configured boundary", enabled: "true", threshold: "100", body: strings.Repeat("界", 100)},
		{name: "raised boundary", enabled: "true", threshold: "7500", body: strings.Repeat("界", 7500)},
		{name: "configured over boundary", enabled: "true", threshold: "100", body: strings.Repeat("界", 101), wantUpload: true, wantReplacement: true},
		{name: "invalid threshold", enabled: "true", threshold: "bad", body: strings.Repeat("x", 7001), wantUpload: true, wantReplacement: true},
		{name: "nonpositive threshold", enabled: "true", threshold: "0", body: strings.Repeat("x", 7001), wantUpload: true, wantReplacement: true},
		{name: "negative threshold", enabled: "true", threshold: "-1", body: strings.Repeat("x", 7001), wantUpload: true, wantReplacement: true},
		{name: "escaped paragraphs", enabled: "true", body: strings.Repeat("x", 7001) + `\n\nlast paragraph`, wantUpload: true, wantReplacement: true},
		{name: "leading code fence", enabled: "true", body: "```go\n" + strings.Repeat("x", 8001) + "\n```", wantUpload: true, wantReplacement: true},
		{name: "clamped threshold", enabled: "true", threshold: "9000", body: strings.Repeat("x", 8000), wantUpload: true, wantReplacement: true},
		{name: "upload failure", enabled: "true", body: strings.Repeat("x", 8001), uploadStatus: http.StatusInternalServerError, wantUpload: true},
		{name: "forbidden", enabled: "true", body: strings.Repeat("x", 8001), uploadStatus: http.StatusForbidden, wantUpload: true},
		{name: "invalid response", enabled: "true", body: strings.Repeat("x", 8001), response: "not json", wantUpload: true},
		{name: "missing URL", enabled: "true", body: strings.Repeat("x", 8001), response: `{"id":"attachment-1"}`, wantUpload: true},
		{name: "missing ID", enabled: "true", body: strings.Repeat("x", 8001), response: `{"markdown_url":"https://example.com/reply"}`, wantUpload: true},
		{name: "failed task", enabled: "true", body: strings.Repeat("x", 8001), status: "blocked"},
		{name: "chat", enabled: "true", body: strings.Repeat("x", 8001), chat: "chat-1"},
		{name: "no issue", enabled: "true", body: strings.Repeat("x", 8001), issue: "none"},
		{name: "no task credential", enabled: "true", body: strings.Repeat("x", 8001), token: "invalid"},
		{name: "redacted", enabled: "true", body: strings.Repeat("x", 8001) + " ghp_" + strings.Repeat("a", 36), wantUpload: true, wantReplacement: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_LONG_REPLY_ATTACHMENTS", tc.enabled)
			t.Setenv("MULTICA_LONG_REPLY_THRESHOLD", tc.threshold)
			var paths []string
			var uploaded string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.Method+" "+r.URL.Path)
				if r.URL.Path != "/api/upload-file" {
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if r.Header.Get("Authorization") != "Bearer mat_test" || r.Header.Get("X-Workspace-ID") != "ws-1" {
					t.Error("upload did not use the task credential and workspace")
				}
				file, header, err := r.FormFile("file")
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				defer file.Close()
				data, err := io.ReadAll(file)
				if err != nil {
					t.Error(err)
				}
				uploaded = string(data)
				if r.FormValue("issue_id") != "issue-1" || header.Filename != "reply-task-1.md" {
					t.Error("attachment was not bound to the issue with the expected filename")
				}
				if tc.uploadStatus != 0 {
					w.WriteHeader(tc.uploadStatus)
					return
				}
				if tc.response != "" {
					_, _ = io.WriteString(w, tc.response)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"id": "attachment-1", "markdown_url": "https://example.com/reply"})
			}))
			defer srv.Close()
			d := &Daemon{cfg: Config{ServerBaseURL: srv.URL}}
			task := Task{ID: "task-1", WorkspaceID: "ws-1", IssueID: "issue-1", AuthToken: "mat_test", ChatSessionID: tc.chat}
			if tc.issue == "none" {
				task.IssueID = ""
			}
			if tc.token != "" {
				task.AuthToken = tc.token
			}
			status := tc.status
			if status == "" {
				status = "completed"
			}
			original := TaskResult{Status: status, Comment: tc.body, BranchName: "branch", SessionID: "session", WorkDir: "work"}
			got := d.offloadLongReply(context.Background(), task, original, slog.Default())
			if tc.wantUpload {
				if !reflect.DeepEqual(paths, []string{"POST /api/upload-file"}) {
					t.Fatalf("requests = %v; only an upload is allowed, never comments or task creation", paths)
				}
				if uploaded != redact.Text(util.DecodeEscapedAgentOutput(tc.body)) {
					t.Error("full redacted output was not preserved")
				}
			} else if len(paths) != 0 {
				t.Fatalf("unexpected requests: %v", paths)
			}
			if tc.wantReplacement {
				if !strings.HasPrefix(got.Comment, "> ") || !utf8.ValidString(got.Comment) {
					t.Fatal("excerpt must be quoted, valid UTF-8")
				}
				if !strings.Contains(got.Comment, "!file[reply-task-1.md](https://example.com/reply)") || utf8.RuneCountInString(got.Comment) >= 8000 {
					t.Fatalf("invalid replacement: %q", got.Comment)
				}
				got.Comment = original.Comment
			}
			if !reflect.DeepEqual(got, original) {
				t.Fatal("result fields unexpectedly changed")
			}
		})
	}
}

func TestLongReplyCompletionRetry(t *testing.T) {
	defer noSleepRetry(t)()
	t.Setenv("MULTICA_LONG_REPLY_ATTACHMENTS", "true")
	t.Setenv("MULTICA_LONG_REPLY_THRESHOLD", "")
	var paths []string
	var outputs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/upload-file":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "attachment-1", "markdown_url": "https://example.com/reply"})
		case "/api/daemon/tasks/task-1/complete":
			var payload struct {
				Output string `json:"output"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			outputs = append(outputs, payload.Output)
			if len(outputs) == 1 {
				w.WriteHeader(http.StatusBadGateway)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	d := &Daemon{cfg: Config{ServerBaseURL: srv.URL}, client: NewClient(srv.URL), logger: slog.Default()}
	task := Task{ID: "task-1", WorkspaceID: "ws-1", IssueID: "issue-1", AuthToken: "mat_test"}
	result := d.offloadLongReply(context.Background(), task, TaskResult{Status: "completed", Comment: strings.Repeat("界", 8001)}, slog.Default())
	d.reportTaskResult(context.Background(), task.ID, result, slog.Default())
	want := []string{"POST /api/upload-file", "POST /api/daemon/tasks/task-1/complete", "POST /api/daemon/tasks/task-1/complete"}
	if !reflect.DeepEqual(paths, want) || !reflect.DeepEqual(outputs, []string{result.Comment, result.Comment}) {
		t.Fatalf("requests = %v, outputs = %v", paths, outputs)
	}
}
