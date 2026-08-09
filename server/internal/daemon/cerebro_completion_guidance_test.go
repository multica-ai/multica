package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReportTaskResult_ReturnsCompletionGuidanceWithoutFailing(t *testing.T) {
	t.Parallel()

	var completeCalls, failCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/complete"):
			completeCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"workflow_gate_rejected","requirement":"Create a wakeup","alternatives":["Create a wakeup","Ask a member"],"attempt":1}`))
		case strings.HasSuffix(req.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	guidance := d.reportTaskResult(context.Background(), "task-guide", TaskResult{
		Status:    "completed",
		Comment:   "I will follow up later.",
		SessionID: "ses-guide",
		WorkDir:   "/tmp/guide",
	}, slog.Default())

	if guidance == nil || guidance.Requirement != "Create a wakeup" || guidance.Attempt != 1 {
		t.Fatalf("guidance = %#v", guidance)
	}
	if completeCalls.Load() != 1 || failCalls.Load() != 0 {
		t.Fatalf("complete calls=%d fail calls=%d", completeCalls.Load(), failCalls.Load())
	}
}

func TestHandleTask_GuidesCompletionOnceAndPreservesAnswer(t *testing.T) {
	t.Parallel()

	var completeCalls atomic.Int32
	var failCalls atomic.Int32
	var completedOutputs []string
	var completedAttempts []int
	var payloadMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/status"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"running"}`))
		case strings.HasSuffix(req.URL.Path, "/complete"):
			var payload map[string]any
			_ = json.NewDecoder(req.Body).Decode(&payload)
			payloadMu.Lock()
			completedOutputs = append(completedOutputs, payload["output"].(string))
			attempt, _ := payload["completion_attempt"].(float64)
			completedAttempts = append(completedAttempts, int(attempt))
			payloadMu.Unlock()
			if completeCalls.Add(1) == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"code":"workflow_gate_rejected","requirement":"Create a wakeup","alternatives":["Create a wakeup","Ask a member","Set the issue to blocked"],"attempt":1}`))
				return
			}
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(req.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		cancelPollInterval: time.Hour,
	}
	var runCalls atomic.Int32
	d.runner = taskRunnerFunc(func(_ context.Context, task Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		if runCalls.Add(1) == 1 {
			return TaskResult{Status: "completed", Comment: "I will follow up later.", SessionID: "ses-1", WorkDir: "/tmp/work"}, nil
		}
		if task.CompletionGuidance == nil || task.PriorSessionID != "ses-1" || task.PriorWorkDir != "/tmp/work" || task.CompletionOriginalAnswer != "I will follow up later." {
			t.Fatalf("guided task = %#v", task)
		}
		return TaskResult{Status: "completed", Comment: "", SessionID: "ses-1", WorkDir: "/tmp/work"}, errors.New("provider stopped during guidance")
	})

	d.handleTask(context.Background(), Task{
		ID: "task-guide", RuntimeID: "rt-1", WorkspaceID: "workspace-1", IssueID: "issue-1",
		Agent: &AgentData{Name: "test-agent"},
	}, 0)

	if runCalls.Load() != 2 || completeCalls.Load() != 2 || failCalls.Load() != 0 {
		t.Fatalf("runner calls=%d complete calls=%d fail calls=%d", runCalls.Load(), completeCalls.Load(), failCalls.Load())
	}
	payloadMu.Lock()
	defer payloadMu.Unlock()
	if len(completedOutputs) != 2 || completedOutputs[0] != "I will follow up later." || completedOutputs[1] != "I will follow up later." {
		t.Fatalf("completed outputs = %#v", completedOutputs)
	}
	if len(completedAttempts) != 2 || completedAttempts[0] != 0 || completedAttempts[1] != 2 {
		t.Fatalf("completion attempts = %#v", completedAttempts)
	}
}

func TestGuideTaskCompletionOnce_NonCompletedTurnPreservesOriginal(t *testing.T) {
	t.Parallel()

	var completeOutput string
	var failCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/complete"):
			var payload map[string]any
			_ = json.NewDecoder(req.Body).Decode(&payload)
			completeOutput, _ = payload["output"].(string)
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(req.URL.Path, "/fail"):
			failCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
		return TaskResult{Status: "blocked", Comment: "iteration limit"}, nil
	})
	original := TaskResult{Status: "completed", Comment: "original answer", SessionID: "ses-1", WorkDir: "/tmp/work"}
	got := d.guideTaskCompletionOnce(context.Background(), context.Background(), Task{ID: "task-guide"}, "claude", 0, original, &CompletionGuidance{
		Code: "workflow_gate_rejected", Requirement: "Create a wakeup", Attempt: 1,
	}, slog.Default())

	if got.Comment != original.Comment || completeOutput != original.Comment || failCalls.Load() != 0 {
		t.Fatalf("result=%#v complete output=%q fail calls=%d", got, completeOutput, failCalls.Load())
	}
}

func TestBuildPromptCompletionGuidanceReturnsOnlyCorrectiveTurn(t *testing.T) {
	t.Parallel()

	out := BuildPrompt(Task{
		IssueID: "issue-1",
		CompletionGuidance: &CompletionGuidance{
			Code:         "workflow_gate_rejected",
			Requirement:  "Create a wakeup",
			Alternatives: []string{"Create a wakeup", "Ask a member", "Set the issue to blocked"},
			Attempt:      1,
		},
		CompletionOriginalAnswer: "I will follow up later.",
	}, "claude")

	for _, want := range []string{
		"The Workflow hook rejected your first attempt to stop",
		"Create a wakeup",
		"Ask a member",
		"Set the issue to blocked",
		"I will follow up later.",
		"Return a complete final answer",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("completion guidance prompt missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Start by running `multica issue get") {
		t.Fatalf("completion guidance must be a focused corrective turn:\n%s", out)
	}
}
