package daemon

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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

// taskLogSourceFiles are the package sources the structural guard below reads.
// Asserting a call count against them keeps a rename or a file move from
// turning the guard into a vacuous pass (the failure mode MUL-5524 hit).
const minTaskLogCalls = 20

// TestTaskLogCallsDoNotRepeatTheBoundTaskField is the structural half of the
// one-line-one-`task=` rule above. The behavioural test can only see the lines
// its own code path emits; most `taskLog` calls live deep inside runTask,
// behind a real agent process. This reads the package source instead and fails
// on any `taskLog.<Level>(...)` that passes a literal "task" key — by
// convention in this package `taskLog` is always the logger handleTask bound
// the full task id to, so such an argument can only be a duplicate.
//
// A string literal "task" can never be a *value* in these calls, so matching
// the literal anywhere in the argument list needs no position arithmetic.
func TestTaskLogCallsDoNotRepeatTheBoundTaskField(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var calls int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok || recv.Name != "taskLog" {
				return true
			}
			switch sel.Sel.Name {
			case "Debug", "Info", "Warn", "Error":
			default:
				return true
			}
			calls++
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || lit.Value != `"task"` {
					continue
				}
				t.Errorf("%s: taskLog call passes a \"task\" field it is already bound to — the id would print twice on that line", fset.Position(call.Pos()))
			}
			return true
		})
	}

	if calls < minTaskLogCalls {
		t.Fatalf("found only %d taskLog logging calls, want >= %d — the scan stopped matching (renamed logger? moved file?) and would pass vacuously", calls, minTaskLogCalls)
	}
}
