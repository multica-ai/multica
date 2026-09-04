package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two task ids from the field report on #7326. They differ from the fourth hex
// char of the second group onward and share the leading eight — which is not a
// coincidence worth guarding against once: task ids are UUIDv7, whose first 32
// bits are the high half of a millisecond timestamp, so every pair of tasks
// born inside the same ~65s window shares those eight chars. Truncating an id
// for a log field therefore merges concurrent runs into one indistinguishable
// stream exactly when telling them apart matters.
const (
	collidingTaskIDA = "01a05ec1-8413-76e0-82e3-fd427ee315fd"
	collidingTaskIDB = "01a05ec1-841d-7b0d-a60b-849f777505df"
)

// lockedBuffer is a slog sink safe to read while the task's cancellation
// watcher goroutine may still be alive.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fieldValues returns every value the text handler emitted for `key=`.
func fieldValues(logs, key string) []string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `=(\S+)`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(logs, -1) {
		out = append(out, strings.Trim(m[1], `"`))
	}
	return out
}

// TestHandleTask_LogsFullTaskIDSoConcurrentRunsStayDistinct is the log-side
// half of #7326. The workdir fix made two same-prefix tasks run in separate
// roots; this asserts their log lines can still be told apart afterwards, so
// scheduling/provider/workdir behaviour stays diagnosable per run.
func TestHandleTask_LogsFullTaskIDSoConcurrentRunsStayDistinct(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	logs := &lockedBuffer{}
	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		cancelPollInterval: time.Hour,
		cfg:                Config{WorkspacesRoot: t.TempDir()},
	}
	// The runner's injected per-task logger is the one that carries `task=`
	// through the whole run, so make it emit a line we can inspect.
	d.runner = taskRunnerFunc(func(_ context.Context, _ Task, _ string, _ int, log *slog.Logger) (TaskResult, error) {
		log.Info("runner reached")
		return TaskResult{Status: "completed"}, nil
	})

	for _, id := range []string{collidingTaskIDA, collidingTaskIDB} {
		d.handleTask(context.Background(), Task{
			ID:        id,
			RuntimeID: "rt-1",
			IssueID:   "issue-log-correlation",
			Agent:     &AgentData{Name: "test-agent"},
		}, 0)
	}

	out := logs.String()
	for _, id := range []string{collidingTaskIDA, collidingTaskIDB} {
		if !strings.Contains(out, "task="+id) {
			t.Errorf("no log line carried task=%s; logs:\n%s", id, out)
		}
	}
	for _, got := range fieldValues(out, "task") {
		if got != collidingTaskIDA && got != collidingTaskIDB {
			t.Errorf("task=%q is neither full task id — a truncated id cannot be joined to the task JSON, the env-root ownership manifest, or the other run's lines", got)
		}
	}
	// The per-task logger must reach the runner intact: everything the agent
	// run emits inherits this field.
	if !strings.Contains(out, "runner reached") {
		t.Fatal("runner never ran; the assertions above proved nothing about the per-task logger")
	}
	// One line, one `task=`. A call that re-passes the field its logger is
	// already bound to prints the id twice, which is 72 chars of noise now
	// that the value is a whole UUID.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if n := len(fieldValues(line, "task")); n > 1 {
			t.Errorf("log line carries %d task fields, want at most 1: %s", n, line)
		}
	}
}

// TestHandleTask_LogsFullChatSessionID covers the sibling correlation key on
// the chat path — a chat session id is a UUIDv7 too, and truncating it merges
// concurrent sessions the same way.
func TestHandleTask_LogsFullChatSessionID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	const chatSessionID = "01a05ec1-9d02-7f11-9c3a-11c0de5510ab"

	logs := &lockedBuffer{}
	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		cancelPollInterval: time.Hour,
		cfg:                Config{WorkspacesRoot: t.TempDir()},
	}
	d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
		return TaskResult{Status: "completed"}, nil
	})

	d.handleTask(context.Background(), Task{
		ID:            collidingTaskIDA,
		RuntimeID:     "rt-1",
		ChatSessionID: chatSessionID,
		Agent:         &AgentData{Name: "test-agent"},
	}, 0)

	out := logs.String()
	if !strings.Contains(out, "chat_session="+chatSessionID) {
		t.Errorf("no log line carried chat_session=%s; logs:\n%s", chatSessionID, out)
	}
}

// TestHandleTask_UntrackedRuntimeLogsFullTaskID guards the failure path, which
// logs before the per-task logger exists and so has its own `task=` argument.
func TestHandleTask_UntrackedRuntimeLogsFullTaskID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	logs := &lockedBuffer{}
	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		runtimeIndex:       map[string]Runtime{},
		cancelPollInterval: time.Hour,
	}

	d.handleTask(context.Background(), Task{ID: collidingTaskIDB, RuntimeID: "rt-demoted"}, 0)

	if out := logs.String(); !strings.Contains(out, "task="+collidingTaskIDB) {
		t.Errorf("runtime-offline warning did not carry the full task id; logs:\n%s", out)
	}
}
