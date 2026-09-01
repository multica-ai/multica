package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestTaskPhaseRecorderRecordsElapsedDurations(t *testing.T) {
	times := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, 20*time.Millisecond.Nanoseconds()),
		time.Unix(0, 55*time.Millisecond.Nanoseconds()),
	}
	recorder := newTaskPhaseRecorder(testTaskPhaseLogger(), func() time.Time {
		now := times[0]
		times = times[1:]
		return now
	})

	if got, want := recorder.Mark(taskPhaseClaimed), (taskPhaseSample{
		Phase: taskPhaseClaimed, PhaseElapsed: 20 * time.Millisecond, TotalElapsed: 20 * time.Millisecond,
	}); got != want {
		t.Fatalf("Mark(%q) = %#v, want %#v", taskPhaseClaimed, got, want)
	}
	if got, want := recorder.Mark(taskPhasePrepareStarted), (taskPhaseSample{
		Phase: taskPhasePrepareStarted, PhaseElapsed: 35 * time.Millisecond, TotalElapsed: 55 * time.Millisecond,
	}); got != want {
		t.Fatalf("Mark(%q) = %#v, want %#v", taskPhasePrepareStarted, got, want)
	}
}

func TestTaskPhaseRecorderLogsEachPhaseOnlyOnce(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil)).With("task_id", "task-1", "runtime_id", "runtime-1")
	recorder := newTaskPhaseRecorder(logger, func() time.Time { return time.Unix(0, 0) })

	recorder.Mark(taskPhaseClaimed)
	recorder.Mark(taskPhaseClaimed)

	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatalf("decode log record: %v", err)
	}
	if got, want := record["task_phase"], string(taskPhaseClaimed); got != want {
		t.Fatalf("task_phase = %#v, want %#v", got, want)
	}
	for _, field := range []string{"phase_elapsed_ms", "total_elapsed_ms", "task_id", "runtime_id"} {
		if _, ok := record[field]; !ok {
			t.Fatalf("missing stable log field %q in %#v", field, record)
		}
	}
}

func TestTaskPhaseRecorderClassifiesVisibleOutput(t *testing.T) {
	cases := []struct {
		name string
		msg  agent.Message
		want bool
	}{
		{name: "status progress", msg: agent.Message{Type: agent.MessageStatus, Status: "running"}, want: false},
		{name: "empty text", msg: agent.Message{Type: agent.MessageText}, want: false},
		{name: "text", msg: agent.Message{Type: agent.MessageText, Content: "working"}, want: true},
		{name: "tool use", msg: agent.Message{Type: agent.MessageToolUse, Tool: "shell"}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTaskVisibleOutput(tc.msg); got != tc.want {
				t.Fatalf("isTaskVisibleOutput(%#v) = %t, want %t", tc.msg, got, tc.want)
			}
		})
	}
}

func TestTaskPhaseRecorderClassifiesToolUse(t *testing.T) {
	cases := []struct {
		name string
		msg  agent.Message
		want bool
	}{
		{name: "status progress", msg: agent.Message{Type: agent.MessageStatus, Status: "running"}, want: false},
		{name: "text progress", msg: agent.Message{Type: agent.MessageText, Content: "Working on it"}, want: false},
		{name: "tool use", msg: agent.Message{Type: agent.MessageToolUse, Tool: "shell"}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTaskToolUse(tc.msg); got != tc.want {
				t.Fatalf("isTaskToolUse(%#v) = %t, want %t", tc.msg, got, tc.want)
			}
		})
	}
}

func TestTaskPhaseRecorderRecordsDrainLifecycleInOrder(t *testing.T) {
	capture := newTaskPhaseCaptureHandler()
	recorder := newTaskPhaseRecorder(slog.New(capture), time.Now)
	ctx := withTaskPhaseRecorder(context.Background(), recorder)
	d := newTestDaemon(t)

	messages := make(chan agent.Message, 3)
	messages <- agent.Message{Type: agent.MessageStatus, Status: "running"}
	messages <- agent.Message{Type: agent.MessageText, Content: "working"}
	messages <- agent.Message{Type: agent.MessageToolUse, Tool: "shell"}
	close(messages)
	results := make(chan agent.Result, 1)
	results <- agent.Result{Status: "completed"}
	close(results)

	_, _, err := d.executeAndDrain(ctx, taskPhaseTimingBackend{
		session: &agent.Session{Messages: messages, Result: results},
	}, "prompt", agent.ExecOptions{}, testTaskPhaseLogger(), "task-1", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("executeAndDrain() error = %v", err)
	}

	want := []taskPhase{
		taskPhaseRuntimeStarted,
		taskPhaseFirstVisibleOutput,
		taskPhaseFirstToolUse,
		taskPhaseTurnCompleted,
	}
	if got := capture.phasesSnapshot(); !slices.Equal(got, want) {
		t.Fatalf("phase order = %v, want %v", got, want)
	}
}

func TestTaskPhaseRecorderHandleTaskMarksBoundaryPhases(t *testing.T) {
	capture := newTaskPhaseCaptureHandler()
	d := newTestDaemon(t)
	d.logger = slog.New(capture)
	d.runtimeIndex = map[string]Runtime{"runtime-1": {ID: "runtime-1", Provider: "codex"}}
	d.cancelPollInterval = time.Hour
	d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
		return TaskResult{Status: "completed"}, nil
	})

	d.handleTask(context.Background(), Task{ID: "task-1", RuntimeID: "runtime-1"}, 0)

	want := []taskPhase{taskPhaseClaimed, taskPhaseFinished}
	if got := capture.phasesSnapshot(); !slices.Equal(got, want) {
		t.Fatalf("phase order = %v, want %v", got, want)
	}
}

type taskPhaseTimingBackend struct {
	session *agent.Session
}

func (b taskPhaseTimingBackend) Execute(context.Context, string, agent.ExecOptions) (*agent.Session, error) {
	return b.session, nil
}

type taskPhaseCaptureHandler struct {
	mu     sync.Mutex
	phases []taskPhase
}

func newTaskPhaseCaptureHandler() *taskPhaseCaptureHandler { return &taskPhaseCaptureHandler{} }

func (h *taskPhaseCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *taskPhaseCaptureHandler) Handle(_ context.Context, record slog.Record) error {
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "task_phase" {
			h.mu.Lock()
			h.phases = append(h.phases, taskPhase(attr.Value.String()))
			h.mu.Unlock()
		}
		return true
	})
	return nil
}

func (h *taskPhaseCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *taskPhaseCaptureHandler) WithGroup(string) slog.Handler      { return h }

func (h *taskPhaseCaptureHandler) phasesSnapshot() []taskPhase {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]taskPhase(nil), h.phases...)
}

func testTaskPhaseLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
