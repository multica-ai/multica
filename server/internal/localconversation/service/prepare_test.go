package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/codex"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// codexProcesses starts a new fake app-server per process, as a runner that
// restarts or resumes needs.
type codexProcesses struct {
	mu      sync.Mutex
	servers []*fakeCodex
	setup   func(i int, s *fakeCodex)
}

func (p *codexProcesses) start(_ context.Context, h codex.Handler, _ []string) (CodexServer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := &fakeCodex{params: map[string]map[string]any{}, done: make(chan struct{}), handler: h}
	if p.setup != nil {
		p.setup(len(p.servers), s)
	}
	p.servers = append(p.servers, s)
	return s, nil
}

func (p *codexProcesses) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.servers)
}

func (p *codexProcesses) server(i int) *fakeCodex {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.servers[i]
}

func newCodexPrepareFixture(t *testing.T, session CodexSession, setup func(i int, s *fakeCodex)) (*runnerFixture, *CodexRunner, *codexProcesses) {
	t.Helper()
	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	processes := &codexProcesses{setup: setup}
	if session.Cwd == "" {
		session.Cwd = "/repo"
	}
	r := NewCodexRunner(CodexRunnerOptions{
		ConversationID: "conv-1", ScopeID: f.scopeID, Files: f.store, Session: session,
		Batcher: f.batcher, Limiter: f.limiter, ContextMCPName: "multica", Start: processes.start,
	})
	return f, r, processes
}

func completeCodexTurn(t *testing.T, s *fakeCodex) {
	t.Helper()
	s.notify(t, "turn/completed", `{"threadId":"th-1","turn":{"id":"tu-1","status":"completed"}}`)
}

// gateRequest blocks a method's request until open is closed or its context ends.
func gateRequest(entered chan<- struct{}, open <-chan struct{}, method string) func(ctx context.Context, m string) error {
	return func(ctx context.Context, m string) error {
		if m != method {
			return nil
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-open:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func timingLog(t *testing.T) (*timing.Log, func() []timing.Record) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "timing.jsonl")
	log := timing.Open(path, timing.Options{})
	t.Cleanup(log.Close)
	return log, func() []timing.Record {
		log.Flush()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var out []timing.Record
		for _, line := range bytes.Split(raw, []byte("\n")) {
			var r timing.Record
			if len(line) > 0 && json.Unmarshal(line, &r) == nil {
				out = append(out, r)
			}
		}
		return out
	}
}

func TestCodexPrepareLoadsTheThreadWithoutATurn(t *testing.T) {
	t.Parallel()

	f, r, p := newCodexPrepareFixture(t, CodexSession{ThreadID: "th-1"}, nil)
	if err := r.Prepare(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(p.server(0).sent()); got != "[config/read model/list thread/resume]" {
		t.Fatalf("requests = %s", got)
	}
	if f.limiter.Running() != 0 {
		t.Fatal("prepare took a turn slot")
	}
	idle, ok := r.IdleProcess()
	if !ok || !idle.preheated {
		t.Fatalf("idle = %+v, %v", idle, ok)
	}
	// Already ready: nothing to do.
	if err := r.Prepare(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := len(p.server(0).sent()); got != 3 || p.count() != 1 {
		t.Fatalf("second prepare did work: %d requests, %d processes", got, p.count())
	}
	codexSend(t, r, "t1")
	if got := fmt.Sprint(p.server(0).sent()); got != "[config/read model/list thread/resume thread/resume turn/start]" {
		t.Fatalf("requests after send = %s", got)
	}
	if idle, _ := r.IdleProcess(); idle.preheated {
		t.Fatal("a running process still reads as preheated")
	}
}

func TestCodexPrepareSkipsAConversationWithoutAThread(t *testing.T) {
	t.Parallel()

	_, r, p := newCodexPrepareFixture(t, CodexSession{}, nil)
	if err := r.Prepare(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if p.count() != 0 {
		t.Fatal("prepare started a process for a conversation with no thread")
	}
}

func TestCodexSendJoinsARunningPrepare(t *testing.T) {
	t.Parallel()

	entered, open := make(chan struct{}, 1), make(chan struct{})
	f, r, p := newCodexPrepareFixture(t, CodexSession{ThreadID: "th-1"}, func(_ int, s *fakeCodex) {
		s.intercept = gateRequest(entered, open, "thread/resume")
	})
	log, records := timingLog(t)
	preheat := timing.NewTrace(log, timing.TraceOptions{ConversationID: "conv-1"})
	prepared := make(chan error, 1)
	go func() { prepared <- r.Prepare(context.Background(), preheat) }()
	<-entered

	sent := make(chan error, 1)
	go func() {
		sent <- r.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "req-t1", MessageID: "msg-t1", Text: "go",
			Trace: timing.NewTrace(log, timing.TraceOptions{ConversationID: "conv-1", TurnID: "t1"})})
	}()
	// Another preheat while a turn waits does nothing.
	eventually(t, "condition", func() bool { return r.Running() })
	if err := r.Prepare(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	close(open)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	if p.count() != 1 {
		t.Fatalf("%d processes started", p.count())
	}
	starts := 0
	for _, m := range p.server(0).sent() {
		if m == "turn/start" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("requests = %v", p.server(0).sent())
	}
	completeCodexTurn(t, p.server(0))
	f.turnEvents(t, "t1")
	all := records()
	var ids = map[string]bool{}
	for _, rec := range all {
		if rec.TurnID == "t1" && rec.Stage == timing.StagePrepareWait {
			if rec.PrepareKind != timing.PreparePreheated {
				t.Errorf("send prepare kind = %q", rec.PrepareKind)
			}
			ids[rec.PrepareID] = true
		}
		if rec.TurnID == "" && rec.Stage == timing.StageThreadPrepare {
			ids[rec.PrepareID] = true
		}
	}
	if len(ids) != 1 || ids[""] {
		t.Errorf("prepare ids = %v", ids)
	}
}

func TestCodexCloseDuringPrepareStopsTheProcess(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 1)
	_, r, p := newCodexPrepareFixture(t, CodexSession{ThreadID: "th-1"}, func(_ int, s *fakeCodex) {
		s.intercept = gateRequest(entered, make(chan struct{}), "config/read")
	})
	prepared := make(chan error, 1)
	go func() { prepared <- r.Prepare(context.Background(), nil) }()
	<-entered
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-prepared; !errors.Is(err, context.Canceled) {
		t.Fatalf("prepare err = %v", err)
	}
	select {
	case <-p.server(0).Done():
	default:
		t.Fatal("the process was left running")
	}
	if _, ok := r.IdleProcess(); ok {
		t.Fatal("a closed runner kept a process")
	}
	if err := r.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "r", MessageID: "m", Text: "x"}); !errors.Is(err, ErrClosing) {
		t.Fatalf("send after close = %v", err)
	}
}

func TestCodexPrepareFinishingAfterCloseIsNotInstalled(t *testing.T) {
	t.Parallel()

	// config/read ignores cancellation, as a process answering late does.
	entered, open := make(chan struct{}, 1), make(chan struct{})
	_, r, p := newCodexPrepareFixture(t, CodexSession{ThreadID: "th-1"}, func(_ int, s *fakeCodex) {
		s.intercept = func(_ context.Context, m string) error {
			if m == "config/read" {
				entered <- struct{}{}
				<-open
			}
			return nil
		}
	})
	prepared := make(chan error, 1)
	go func() { prepared <- r.Prepare(context.Background(), nil) }()
	<-entered
	closing, cancel := context.WithCancel(context.Background())
	cancel()
	_ = r.Close(closing)
	close(open)
	if err := <-prepared; !errors.Is(err, ErrClosing) {
		t.Fatalf("prepare err = %v", err)
	}
	select {
	case <-p.server(0).Done():
	default:
		t.Fatal("the finishing prepare left its process running")
	}
}

func TestCodexHungPrepareEndsAtItsDeadline(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 1)
	_, r, p := newCodexPrepareFixture(t, CodexSession{ThreadID: "th-1"}, func(_ int, s *fakeCodex) {
		s.intercept = gateRequest(entered, make(chan struct{}), "config/read")
	})
	r.gate.deadline = 10 * time.Millisecond
	if err := r.Prepare(context.Background(), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("prepare err = %v", err)
	}
	select {
	case <-p.server(0).Done():
	default:
		t.Fatal("the hung process was left running")
	}
}

func TestCodexStopDuringPrepareEndsTheTurnWithoutDispatch(t *testing.T) {
	t.Parallel()

	entered, open := make(chan struct{}, 1), make(chan struct{})
	f, r, p := newCodexPrepareFixture(t, CodexSession{ThreadID: "th-1"}, func(_ int, s *fakeCodex) {
		s.intercept = gateRequest(entered, open, "thread/resume")
	})
	sent := make(chan error, 1)
	go func() {
		sent <- r.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "req-t1", MessageID: "msg-t1", Text: "go"})
	}()
	<-entered
	if err := r.Interrupt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	got := f.turnEvents(t, "t1")
	if got[len(got)-1] != "turn.completed  cancelled" {
		t.Fatalf("events = %v", got)
	}
	close(open)
	eventually(t, "condition", func() bool { _, ok := r.IdleProcess(); return ok })
	for _, m := range p.server(0).sent() {
		if m == "turn/start" {
			t.Fatal("the prompt was dispatched after stop")
		}
	}
	if f.limiter.Running() != 0 {
		t.Fatal("limiter slot leaked")
	}
}

func TestCodexSettingsChangedDuringPrepareRestartBeforeTheTurn(t *testing.T) {
	t.Parallel()

	entered, open := make(chan struct{}, 1), make(chan struct{})
	_, r, p := newCodexPrepareFixture(t, CodexSession{ThreadID: "th-1", PermissionMode: "full-access"}, func(i int, s *fakeCodex) {
		if i == 0 {
			s.intercept = gateRequest(entered, open, "thread/resume")
		}
	})
	prepared := make(chan error, 1)
	go func() { prepared <- r.Prepare(context.Background(), nil) }()
	<-entered
	// The loaded thread keeps its permissions: only a new process applies this.
	if err := r.ApplySettings(context.Background(), store.ConversationSettings{PermissionMode: "default"}); err != nil {
		t.Fatal(err)
	}
	close(open)
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	codexSend(t, r, "t1")
	if p.count() != 2 {
		t.Fatalf("%d processes started", p.count())
	}
	resume := p.server(1).paramsOf("thread/resume")
	if resume["sandbox"] != "workspace-write" {
		t.Fatalf("resume after the change = %v", resume)
	}
	if fmt.Sprint(p.server(0).sent()) != "[config/read model/list thread/resume]" {
		t.Fatalf("old process requests = %v", p.server(0).sent())
	}
}

func TestCodexFailedPreheatDoesNotFailAWaitingSend(t *testing.T) {
	t.Parallel()

	entered, open := make(chan struct{}, 1), make(chan struct{})
	_, r, _ := newCodexPrepareFixture(t, CodexSession{ThreadID: "th-1"}, func(i int, s *fakeCodex) {
		if i == 0 {
			inner := gateRequest(entered, open, "thread/resume")
			s.intercept = func(ctx context.Context, m string) error {
				if err := inner(ctx, m); err != nil {
					return err
				}
				if m == "thread/resume" {
					return errors.New("resume failed")
				}
				return nil
			}
		}
	})
	prepared := make(chan error, 1)
	go func() { prepared <- r.Prepare(context.Background(), nil) }()
	<-entered
	sent := make(chan error, 1)
	go func() {
		sent <- r.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "req-t1", MessageID: "msg-t1", Text: "go"})
	}()
	eventually(t, "condition", func() bool { return r.Running() })
	close(open)
	if err := <-prepared; err == nil {
		t.Fatal("preheat should report its failure")
	}
	if err := <-sent; err != nil {
		t.Fatalf("send = %v", err)
	}
}

func TestCodexEvictGuards(t *testing.T) {
	t.Parallel()

	released := 0
	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	p := &codexProcesses{}
	entered, open := make(chan struct{}, 1), make(chan struct{})
	p.setup = func(i int, s *fakeCodex) {
		if i == 0 {
			s.intercept = gateRequest(entered, open, "thread/resume")
		}
	}
	r := NewCodexRunner(CodexRunnerOptions{ConversationID: "conv-1", ScopeID: f.scopeID, Files: f.store, Session: CodexSession{Cwd: "/repo", ThreadID: "th-1"}, Batcher: f.batcher, Limiter: f.limiter, ContextMCPName: "multica", Start: p.start,
		Context: func(context.Context, string, func() map[string]any) (BoundContext, func(), error) {
			return BoundContext{}, func() { released++ }, nil
		}})
	if r.Evict(context.Background()) {
		t.Fatal("evicted with no process")
	}
	prepared := make(chan error, 1)
	go func() { prepared <- r.Prepare(context.Background(), nil) }()
	<-entered
	if r.Evict(context.Background()) {
		t.Fatal("evicted during prepare")
	}
	close(open)
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	codexSend(t, r, "t1")
	if r.Evict(context.Background()) {
		t.Fatal("evicted with a running turn")
	}
	completeCodexTurn(t, p.server(0))
	f.turnEvents(t, "t1")
	if !r.Evict(context.Background()) {
		t.Fatal("idle process was not evicted")
	}
	select {
	case <-p.server(0).Done():
	default:
		t.Fatal("process not stopped")
	}
	if released != 1 {
		t.Fatalf("context released %d times", released)
	}
	if _, ok := r.IdleProcess(); ok {
		t.Fatal("evicted runner reports a process")
	}
	// The next send resumes on a new process.
	codexSend(t, r, "t2")
	if p.count() != 2 || p.server(1).paramsOf("thread/resume")["threadId"] != "th-1" {
		t.Fatalf("processes = %d, resume = %v", p.count(), p.server(1).paramsOf("thread/resume"))
	}
}

// --- Claude ---

func TestClaudePrepareStartsTheQueryWithResumeAndNoTurn(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	if err := f.runner.Prepare(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	a := f.adapter(0)
	if fmt.Sprint(a.sent()) != "[start]" || a.fields[0]["resume"] != "native-0" {
		t.Fatalf("requests = %v, fields = %v", a.sent(), a.fields[0])
	}
	if f.limiter.Running() != 0 {
		t.Fatal("prepare took a turn slot")
	}
	// The Query's own start-up output is nobody's turn.
	a.emit(t, `{"type":"assistant","message":{"id":"m0","content":[{"type":"text","text":"hook"}]}}`)
	if f.runner.Running() {
		t.Fatal("a preheated Query's output opened a native turn")
	}
	send(t, f, "t1")
	if fmt.Sprint(a.sent()) != "[start send]" {
		t.Fatalf("requests = %v", a.sent())
	}
	if len(f.adapters) != 1 {
		t.Fatalf("%d adapters", len(f.adapters))
	}
}

func TestClaudePrepareSkipsAConversationWithoutASession(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	f.runner.session.ProviderSessionID = ""
	if err := f.runner.Prepare(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(f.adapters) != 0 {
		t.Fatal("prepare started an adapter for a conversation with no session")
	}
}

// claudeGate blocks the adapter's start until released.
func claudeGate(f *runnerFixture) (entered chan struct{}, open chan struct{}) {
	entered, open = make(chan struct{}, 1), make(chan struct{})
	f.intercept = func(ctx context.Context, typ string) {
		if typ == "start" {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-open:
			case <-ctx.Done():
			}
		}
	}
	return entered, open
}

func TestClaudeSendJoinsARunningPrepare(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	entered, open := claudeGate(f)
	prepared := make(chan error, 1)
	go func() { prepared <- f.runner.Prepare(context.Background(), nil) }()
	<-entered
	sent := make(chan error, 1)
	go func() {
		sent <- f.runner.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "req-t1", MessageID: "msg-t1", Text: "go"})
	}()
	eventually(t, "condition", func() bool { return f.runner.Running() })
	close(open)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	if len(f.adapters) != 1 || fmt.Sprint(f.adapter(0).sent()) != "[start send]" {
		t.Fatalf("%d adapters, requests %v", len(f.adapters), f.adapter(0).sent())
	}
}

func TestClaudeProcessThatServedAJoinedSendIsNotPreheated(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	entered, open := claudeGate(f)
	prepared := make(chan error, 1)
	go func() { prepared <- f.runner.Prepare(context.Background(), nil) }()
	<-entered
	sent := make(chan error, 1)
	go func() {
		sent <- f.runner.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "req-t1", MessageID: "msg-t1", Text: "go"})
	}()
	eventually(t, "condition", func() bool { return f.runner.Running() })
	close(open)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	f.adapter(0).emit(t, `{"type":"result","subtype":"success","is_error":false,"result":""}`)
	f.turnEvents(t, "t1")
	idle, ok := f.runner.IdleProcess()
	if !ok || idle.preheated {
		t.Fatalf("idle = %+v, %v", idle, ok)
	}
}

func TestClaudeCloseDuringPrepareStopsTheAdapter(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	entered, _ := claudeGate(f)
	prepared := make(chan error, 1)
	go func() { prepared <- f.runner.Prepare(context.Background(), nil) }()
	<-entered
	if err := f.runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-prepared; err == nil {
		t.Fatal("prepare after close should fail")
	}
	select {
	case <-f.adapter(0).Done():
	default:
		t.Fatal("the adapter was left running")
	}
	if _, ok := f.runner.IdleProcess(); ok {
		t.Fatal("a closed runner kept a process")
	}
}

func TestClaudeStopDuringPrepareEndsTheTurnWithoutDispatch(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	entered, open := claudeGate(f)
	sent := make(chan error, 1)
	go func() {
		sent <- f.runner.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "req-t1", MessageID: "msg-t1", Text: "go"})
	}()
	<-entered
	if err := f.runner.Interrupt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	got := f.turnEvents(t, "t1")
	if got[len(got)-1] != "turn.completed  cancelled" {
		t.Fatalf("events = %v", got)
	}
	close(open)
	eventually(t, "condition", func() bool { _, ok := f.runner.IdleProcess(); return ok })
	if fmt.Sprint(f.adapter(0).sent()) != "[start]" {
		t.Fatalf("requests = %v", f.adapter(0).sent())
	}
}

func TestClaudeSettingsChangedDuringPrepareAreAppliedOnceReady(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	entered, open := claudeGate(f)
	prepared := make(chan error, 1)
	go func() { prepared <- f.runner.Prepare(context.Background(), nil) }()
	<-entered
	model := "claude-opus-5-5"
	if err := f.runner.ApplySettings(context.Background(), store.ConversationSettings{Model: &model, PermissionMode: "default"}); err != nil {
		t.Fatal(err)
	}
	close(open)
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(f.adapter(0).sent()); got != "[start set_permission_mode set_model apply_flag_settings]" {
		t.Fatalf("requests = %s", got)
	}
}

func TestClaudeSettingsThatCannotBeAppliedFailThePrepare(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), func(typ string) (json.RawMessage, error) {
		if typ == "set_permission_mode" {
			return nil, errors.New("refused")
		}
		return json.RawMessage(`{}`), nil
	})
	entered, open := claudeGate(f)
	prepared := make(chan error, 1)
	go func() { prepared <- f.runner.Prepare(context.Background(), nil) }()
	<-entered
	if err := f.runner.ApplySettings(context.Background(), store.ConversationSettings{PermissionMode: "default"}); err != nil {
		t.Fatal(err)
	}
	close(open)
	if err := <-prepared; err == nil {
		t.Fatal("prepare kept an adapter with outdated settings")
	}
	if _, ok := f.runner.IdleProcess(); ok {
		t.Fatal("the adapter stayed installed")
	}
}

// A SessionStart hook can make Claude answer before the first prompt. While a send
// waits on that start, the answer is still nobody's reply.
func TestClaudeOutputBeforeThePromptIsNotTheWaitingTurnsReply(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	entered, open := claudeGate(f)
	sent := make(chan error, 1)
	go func() {
		sent <- f.runner.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "req-t1", MessageID: "msg-t1", Text: "go"})
	}()
	<-entered
	f.adapter(0).emit(t,
		`{"type":"system","subtype":"init","session_id":"from-hook"}`,
		`{"type":"assistant","message":{"id":"m0","content":[{"type":"text","text":"hook"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"hook"}`,
	)
	if !f.runner.Running() {
		t.Fatal("the hook's result finished the user's turn")
	}
	if got := f.runner.session.ProviderSessionID; got != "native-0" {
		t.Fatalf("session = %q", got)
	}
	close(open)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(f.adapter(0).sent()); got != "[start send]" {
		t.Fatalf("requests = %s: the prompt never reached Claude", got)
	}
	f.adapter(0).emit(t, `{"type":"system","subtype":"init","session_id":"native-1"}`, `{"type":"result","subtype":"success","is_error":false,"result":"ok"}`)
	got := f.turnEvents(t, "t1")
	if got[len(got)-1] != "turn.completed  completed" {
		t.Fatalf("events = %v", got)
	}
	snap, err := f.store.Snapshot(context.Background(), "conv-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Conversation.ProviderSessionID != "native-1" {
		t.Fatalf("stored session = %q", snap.Conversation.ProviderSessionID)
	}
}

// Stopping before the prompt must not leave a resume with no transcript behind.
func TestClaudeStopBeforeThePromptSavesNoSession(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	f.runner.session.ProviderSessionID = ""
	entered, open := claudeGate(f)
	sent := make(chan error, 1)
	go func() {
		sent <- f.runner.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "req-t1", MessageID: "msg-t1", Text: "go"})
	}()
	<-entered
	f.adapter(0).emit(t, `{"type":"system","subtype":"init","session_id":"unrecorded"}`)
	if err := f.runner.Interrupt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	close(open)
	f.turnEvents(t, "t1")
	eventually(t, "process", func() bool { _, ok := f.runner.IdleProcess(); return ok })
	snap, err := f.store.Snapshot(context.Background(), "conv-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Conversation.ProviderSessionID != "" {
		t.Fatalf("stored session = %q", snap.Conversation.ProviderSessionID)
	}
}

// A result arriving for an adapter that already served a turn, in the moment
// between a send taking the turn and its prompt going out, must not end that turn.
func TestClaudeResultDuringASendDoesNotSwallowItsPrompt(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	send(t, f, "t1")
	a := f.adapter(0)
	a.emit(t, `{"type":"result","subtype":"success","is_error":false,"result":""}`)
	f.turnEvents(t, "t1")

	turn := &claudeTurn{id: "t2"}
	f.runner.mu.Lock()
	f.runner.turn = turn
	f.runner.mu.Unlock()
	a.emit(t, `{"type":"result","subtype":"success","is_error":false,"result":"late"}`)
	if !f.runner.Running() {
		t.Fatal("a result ended a turn whose prompt had not gone out")
	}
}

func settingsPushes(a *fakeAdapter) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var modes []string
	for i, typ := range a.requests {
		if typ == "set_permission_mode" {
			modes = append(modes, fmt.Sprint(a.fields[i]["mode"]))
		}
	}
	return modes
}

func TestClaudeSettingsPushOutlivesItsRequestAndStartsWithPermission(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	var cancelled atomic.Bool
	f.intercept = func(ctx context.Context, typ string) {
		if ctx.Err() != nil && strings.HasPrefix(typ, "set_") {
			cancelled.Store(true)
		}
	}
	send(t, f, "t1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	model := "claude-opus-5-5"
	if err := f.runner.ApplySettings(ctx, store.ConversationSettings{Model: &model, PermissionMode: "default"}); err != nil {
		t.Fatal(err)
	}
	if cancelled.Load() {
		t.Fatal("the push used the caller's cancelled context")
	}
	if got := fmt.Sprint(f.adapter(0).sent()); got != "[start send set_permission_mode set_model apply_flag_settings]" {
		t.Fatalf("requests = %s", got)
	}
}

func TestClaudeFailedPushRetiresAnIdleAdapter(t *testing.T) {
	t.Parallel()

	var failing atomic.Bool
	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), func(typ string) (json.RawMessage, error) {
		if typ == "set_model" && failing.Load() {
			return nil, errors.New("refused")
		}
		return json.RawMessage(`{}`), nil
	})
	if err := f.runner.Prepare(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	failing.Store(true)
	// The stored settings hold and the next process starts with them.
	if err := f.runner.ApplySettings(context.Background(), store.ConversationSettings{PermissionMode: "default"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "adapter retired", func() bool { return isDone(f.adapter(0).Done()) })
	if _, ok := f.runner.IdleProcess(); ok {
		t.Fatal("an adapter on unknown settings stayed")
	}
	failing.Store(false)
	send(t, f, "t1")
	if len(f.adapters) != 2 || f.adapter(1).fields[0]["permission_mode"] != "default" {
		t.Fatalf("%d adapters", len(f.adapters))
	}
}

func TestClaudeFailedPushDuringATurnRestartsAtItsEnd(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), func(typ string) (json.RawMessage, error) {
		if typ == "set_permission_mode" {
			return nil, errors.New("refused")
		}
		return json.RawMessage(`{}`), nil
	})
	send(t, f, "t1")
	a := f.adapter(0)
	if err := f.runner.ApplySettings(context.Background(), store.ConversationSettings{PermissionMode: "default"}); err != nil {
		t.Fatal(err)
	}
	if isDone(a.Done()) || !f.runner.Running() {
		t.Fatal("the running turn was disturbed")
	}
	a.emit(t, `{"type":"result","subtype":"success","is_error":false,"result":""}`)
	f.turnEvents(t, "t1")
	eventually(t, "adapter retired at turn end", func() bool { return isDone(a.Done()) })
	send(t, f, "t2")
	if len(f.adapters) != 2 || f.adapter(1).fields[0]["permission_mode"] != "default" {
		t.Fatalf("%d adapters", len(f.adapters))
	}
}

// Pushes are serialized and the newest settings land last, whatever order the
// callers came in, and a prompt never overtakes a push under way.
func TestClaudeSettingsPushesLandInOrderBeforeThePrompt(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	entered, open := make(chan struct{}, 1), make(chan struct{})
	var first atomic.Bool
	first.Store(true)
	f.intercept = func(_ context.Context, typ string) {
		if typ == "set_permission_mode" && first.Swap(false) {
			entered <- struct{}{}
			<-open
		}
	}
	if err := f.runner.Prepare(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	applied := make(chan error, 2)
	go func() {
		applied <- f.runner.ApplySettings(context.Background(), store.ConversationSettings{PermissionMode: "auto"})
	}()
	<-entered
	go func() {
		applied <- f.runner.ApplySettings(context.Background(), store.ConversationSettings{PermissionMode: "default"})
	}()
	eventually(t, "second settings version", func() bool {
		f.runner.mu.Lock()
		defer f.runner.mu.Unlock()
		return f.runner.settingsVersion == 2
	})
	sent := make(chan error, 1)
	go func() {
		sent <- f.runner.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "req-t1", MessageID: "msg-t1", Text: "go"})
	}()
	eventually(t, "the send to wait", func() bool { return f.runner.Running() })
	time.Sleep(10 * time.Millisecond)
	if slices.Contains(f.adapter(0).sent(), "send") {
		t.Fatal("the prompt overtook a settings push")
	}
	close(open)
	for range 2 {
		if err := <-applied; err != nil {
			t.Fatal(err)
		}
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(settingsPushes(f.adapter(0))); got != "[auto default]" {
		t.Fatalf("pushes = %s", got)
	}
	requests := f.adapter(0).sent()
	if requests[len(requests)-1] != "send" {
		t.Fatalf("requests = %v", requests)
	}
}

func queryEnded(a *fakeAdapter) {
	a.handler(claude.Output{Type: "error", Error: &claude.AdapterError{Code: "query_ended", Message: "Claude Code ended the session"}})
}

// A Query can end after the start result is written but before the runner holds
// the adapter, so its error frame has no installed adapter to act on.
func TestClaudeQueryEndingBeforeInstallDoesNotInstallADeadAdapter(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	entered, open := claudeGate(f)
	prepared := make(chan error, 1)
	go func() { prepared <- f.runner.Prepare(context.Background(), nil) }()
	<-entered
	queryEnded(f.adapter(0))
	close(open)
	if err := <-prepared; err == nil {
		t.Fatal("prepare kept an adapter whose Query had ended")
	}
	if !isDone(f.adapter(0).Done()) {
		t.Fatal("the adapter was left running")
	}
	if _, ok := f.runner.IdleProcess(); ok {
		t.Fatal("a dead adapter was installed")
	}
	f.intercept = nil
	send(t, f, "t1")
	if len(f.adapters) != 2 || fmt.Sprint(f.adapter(1).sent()) != "[start send]" {
		t.Fatalf("%d adapters, second sent %v", len(f.adapters), f.adapter(1).sent())
	}
}

func TestClaudeNotStartedRetiresTheAdapter(t *testing.T) {
	t.Parallel()

	var refused atomic.Bool
	refused.Store(true)
	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), func(typ string) (json.RawMessage, error) {
		if typ == "send" && refused.Swap(false) {
			return nil, &claude.AdapterError{Code: "not_started", Dispatch: "not_dispatched", Message: "start the conversation before sending"}
		}
		return json.RawMessage(`{}`), nil
	})
	err := f.runner.Send(context.Background(), SendRequest{TurnID: "t1", RequestID: "req-t1", MessageID: "msg-t1", Text: "go"})
	if err == nil {
		t.Fatal("Send succeeded")
	}
	if got := f.turnEvents(t, "t1"); got[len(got)-1] != "turn.completed  failed" {
		t.Fatalf("events = %v", got)
	}
	eventually(t, "adapter retired", func() bool { return isDone(f.adapter(0).Done()) })
	send(t, f, "t2")
	if len(f.adapters) != 2 {
		t.Fatalf("%d adapters: the dead one kept refusing sends", len(f.adapters))
	}
}

func TestClaudeEvictGuards(t *testing.T) {
	t.Parallel()

	f := newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
	send(t, f, "t1")
	a := f.adapter(0)
	if f.runner.Evict(context.Background()) {
		t.Fatal("evicted with a running turn")
	}
	a.emit(t, `{"type":"system","subtype":"task_started","task_id":"bg","description":"build"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":""}`)
	f.turnEvents(t, "t1")
	idle, ok := f.runner.IdleProcess()
	if !ok || idle.evictable {
		t.Fatalf("idle with an open background task = %+v, %v", idle, ok)
	}
	if f.runner.Evict(context.Background()) {
		t.Fatal("evicted with an open background task")
	}
	select {
	case <-a.Done():
		t.Fatal("the adapter was closed")
	default:
	}
	f.runner.mu.Lock()
	f.runner.taskTurns = map[string]string{}
	f.runner.mu.Unlock()
	if !f.runner.Evict(context.Background()) {
		t.Fatal("idle adapter was not evicted")
	}
	select {
	case <-a.Done():
	default:
		t.Fatal("the adapter kept running")
	}
	// The next send resumes on a new adapter.
	send(t, f, "t2")
	if len(f.adapters) != 2 || f.adapter(1).fields[0]["resume"] != "native-0" {
		t.Fatalf("%d adapters, %v", len(f.adapters), f.adapter(1).fields[0])
	}
}
