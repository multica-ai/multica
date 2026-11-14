package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleTask_ShutdownErrorDisposition(t *testing.T) {
	for _, tc := range []struct {
		name         string
		shutdown     bool
		result       TaskResult
		wantEndpoint string
		wantReason   string
		wantError    string
		runErr       error
		serverStatus string
	}{
		{"shutdown cancellation", true, TaskResult{Status: "cancelled"}, "/fail", "runtime_recovery", "daemon shut down while task was in flight", nil, "running"},
		{"ordinary cancellation", false, TaskResult{Status: "cancelled"}, "/fail", "cancelled", "", nil, "running"},
		{"completion wins", true, TaskResult{Status: "completed", Comment: "done"}, "/complete", "", "", nil, "running"},
		{"cleanup uncertainty wins", true, TaskResult{Status: "blocked", Comment: "cleanup uncertain", FailureReason: "cleanup_unconfirmed"}, "/fail", "cleanup_unconfirmed", "cleanup uncertain", nil, "running"},
		{"real failure wins", true, TaskResult{Status: "blocked", Comment: "access denied", FailureReason: "agent_error.provider_auth_or_access"}, "/fail", "agent_error.provider_auth_or_access", "access denied", nil, "running"},

		{"snapshot wait cancellation", true, TaskResult{}, "/fail", "runtime_recovery", "daemon shut down while task was in flight: local_directory worktree: wait for snapshot: context canceled", fmt.Errorf("local_directory worktree: wait for snapshot: %w", context.Canceled), "running"},
		{"prepare cancellation", true, TaskResult{}, "/fail", "runtime_recovery", "daemon shut down while task was in flight: prepare execution environment: context canceled", asEnvironmentSetupFailure(fmt.Errorf("prepare execution environment: %w", context.Canceled)), "running"},
		{"reuse cancellation", true, TaskResult{}, "/fail", "runtime_recovery", "daemon shut down while task was in flight: reuse execution environment: context canceled", asEnvironmentSetupFailure(fmt.Errorf("reuse execution environment: %w", context.Canceled)), "running"},
		{"joined cancellations", true, TaskResult{}, "/fail", "runtime_recovery", "daemon shut down while task was in flight: context canceled\ncontext canceled", errors.Join(context.Canceled, context.Canceled), "running"},
		{"cancellation text is not structural evidence", true, TaskResult{}, "/fail", "environment_prepare_failed", "context canceled", asEnvironmentSetupFailure(errors.New("context canceled")), "running"},
		{"prepare timeout during shutdown", true, TaskResult{}, "/fail", "timeout", errTaskPrepareTimeout.Error(), errTaskPrepareTimeout, "running"},
		{"live parent preparation cancellation", false, TaskResult{}, "/fail", "environment_prepare_failed", "context canceled", asEnvironmentSetupFailure(context.Canceled), "running"},
		{"real setup error during shutdown", true, TaskResult{}, "/fail", "environment_prepare_failed", "filesystem failure", asEnvironmentSetupFailure(errors.New("filesystem failure")), "running"},
		{"joined real error during shutdown", true, TaskResult{}, "/fail", "environment_prepare_failed", "context canceled\nfilesystem failure", errors.Join(context.Canceled, asEnvironmentSetupFailure(errors.New("filesystem failure"))), "running"},
		{"finalization cancellation keeps classification", true, TaskResult{Status: "completed"}, "/fail", "local_directory_error", "context canceled", &worktreePreservedError{err: context.Canceled}, "running"},
		{"cancellation joined with finalization", true, TaskResult{}, "/fail", "agent_error.unknown", "context canceled\npreserved worktree /work", errors.Join(context.Canceled, &worktreePreservedError{err: errors.New("preserved worktree /work")}), "running"},
		{"cleanup uncertainty outranks cancellation error", true, TaskResult{Status: "blocked", Comment: "cleanup uncertain", FailureReason: "cleanup_unconfirmed"}, "/fail", "cleanup_unconfirmed", "cleanup uncertain; context canceled", context.Canceled, "running"},
		{"cleanup uncertainty outranks finalization", true, TaskResult{Status: "blocked", Comment: "cleanup uncertain", FailureReason: "cleanup_unconfirmed"}, "/fail", "cleanup_unconfirmed", "cleanup uncertain; preserved worktree /work", &worktreePreservedError{err: errors.New("preserved worktree /work")}, "running"},
		{"late server cancellation with setup error", true, TaskResult{}, "/cancel-ack", "", "", asEnvironmentSetupFailure(context.Canceled), "cancelled"},
		{"late server completion with setup error", true, TaskResult{}, "/cancel-ack", "", "", asEnvironmentSetupFailure(context.Canceled), "completed"},
		{"late server deletion with setup error", true, TaskResult{}, "/cancel-ack", "", "", asEnvironmentSetupFailure(context.Canceled), "deleted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type report struct {
				endpoint string
				payload  map[string]any
			}
			reports := make(chan report, 2)
			var statusChecks atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/fail") || strings.HasSuffix(r.URL.Path, "/complete") || strings.HasSuffix(r.URL.Path, "/cancel-ack") {
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					reports <- report{r.URL.Path, payload}
				} else if strings.HasSuffix(r.URL.Path, "/status") {
					statusChecks.Add(1)
					if tc.serverStatus == "deleted" {
						http.Error(w, "task not found", http.StatusNotFound)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"status": tc.serverStatus})
				}
			}))
			defer srv.Close()
			d := &Daemon{
				client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				runtimeIndex:       map[string]Runtime{"rt": {ID: "rt", Provider: "claude"}},
				cancelPollInterval: time.Hour,
			}
			d.runner = taskRunnerFunc(func(runCtx context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
				if tc.shutdown {
					cancel()
					<-runCtx.Done()
				}
				result := tc.result
				result.SessionID, result.WorkDir, result.BranchName = "session", "/work/task", "work-branch"
				return result, tc.runErr
			})
			d.handleTask(ctx, Task{ID: "task", RuntimeID: "rt"}, 0)
			select {
			case got := <-reports:
				t.Logf("endpoint=%s failure_reason=%v error=%v final_status_checks=%d", got.endpoint, got.payload["failure_reason"], got.payload["error"], statusChecks.Load())
				if !strings.HasSuffix(got.endpoint, tc.wantEndpoint) {
					t.Errorf("endpoint = %s, want %s", got.endpoint, tc.wantEndpoint)
				}
				for key, want := range map[string]string{"failure_reason": tc.wantReason, "error": tc.wantError} {
					value, _ := got.payload[key].(string)
					if value != want {
						t.Errorf("%s = %q, want %q", key, value, want)
					}
				}
				metadata := map[string]string{"branch_name": "work-branch"}
				if tc.wantEndpoint != "/cancel-ack" {
					metadata["work_dir"] = "/work/task"
				}
				if tc.runErr == nil {
					metadata["session_id"] = "session"
				}
				for key, want := range metadata {
					if got.payload[key] != want {
						t.Errorf("%s = %v, want %s", key, got.payload[key], want)
					}
				}
			default:
				t.Fatal("no terminal callback delivered")
			}
			if len(reports) != 0 {
				t.Fatal("duplicate terminal report")
			}
		})
	}
}

func TestHandleTask_ServerCancelBeforeShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var terminalCalls, ackCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cancel-ack"):
			ackCalls.Add(1)
		case strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = io.WriteString(w, `{"status":"cancelled"}`)
		case strings.HasSuffix(r.URL.Path, "/fail"), strings.HasSuffix(r.URL.Path, "/complete"):
			terminalCalls.Add(1)
		}
	}))
	defer srv.Close()
	d := &Daemon{
		client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		runtimeIndex:       map[string]Runtime{"rt": {ID: "rt", Provider: "claude"}},
		cancelPollInterval: time.Millisecond,
	}
	d.runner = taskRunnerFunc(func(runCtx context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		<-runCtx.Done()
		if ctx.Err() != nil {
			t.Fatal("server cancellation did not interrupt the runner")
		}
		cancel()
		return TaskResult{Status: "cancelled"}, nil
	})
	d.handleTask(ctx, Task{ID: "task", RuntimeID: "rt"}, 0)
	if got := ackCalls.Load(); got != 1 {
		t.Errorf("cancel-ack calls = %d, want 1", got)
	}
	if got := terminalCalls.Load(); got != 0 {
		t.Fatalf("server-cancelled task received %d terminal callbacks during shutdown", got)
	}
}
