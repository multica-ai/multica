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
	"time"
)

// Exercise the real completion entry point: the provider returns terminal text
// without posting a comment, and only upload plus completion may deliver it.
func TestLongReplyDelivery(t *testing.T) {
	t.Setenv("MULTICA_LONG_REPLY_ATTACHMENTS", "true")
	t.Setenv("MULTICA_LONG_REPLY_THRESHOLD", "")
	for _, provider := range []string{"hermes", "codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			var mutations []string
			var output string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/upload-file":
					mutations = append(mutations, "upload")
					_ = json.NewEncoder(w).Encode(map[string]string{"id": "attachment-1", "markdown_url": "https://example.com/reply"})
				case "/api/daemon/tasks/task-1/progress":
					w.WriteHeader(http.StatusOK)
				case "/api/daemon/tasks/task-1/status":
					_ = json.NewEncoder(w).Encode(map[string]string{"status": "running"})
				case "/api/daemon/tasks/task-1/complete":
					mutations = append(mutations, "complete")
					var payload struct {
						Output string `json:"output"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					output = payload.Output
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()
			d := &Daemon{
				cfg:                Config{ServerBaseURL: srv.URL, WorkspacesRoot: t.TempDir()},
				client:             NewClient(srv.URL),
				logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
				runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: provider}},
				cancelPollInterval: time.Hour,
			}
			d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
				return TaskResult{Status: "completed", Comment: strings.Repeat("界", 8001)}, nil
			})
			d.handleTask(context.Background(), Task{ID: "task-1", RuntimeID: "rt-1", IssueID: "issue-1", WorkspaceID: "ws-1", AuthToken: "mat_test"}, 0)
			if !reflect.DeepEqual(mutations, []string{"upload", "complete"}) {
				t.Fatalf("delivery requests = %v, want one attachment followed by completion, never a comment or task creation", mutations)
			}
			if !strings.Contains(output, "!file[reply-task-1.md](https://example.com/reply)") {
				t.Fatal("completion did not include the attachment card")
			}
		})
	}
}
