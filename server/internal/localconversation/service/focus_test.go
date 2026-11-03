package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// testClock is a clock tests move by hand.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock { return &testClock{t: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type focusFixture struct {
	*serviceFixture
	clock *testClock
}

func newFocusFixture(t *testing.T, opts Options) *focusFixture {
	t.Helper()
	clock := newTestClock()
	opts.Now, opts.maintenanceEvery = clock.Now, time.Hour
	return &focusFixture{serviceFixture: newServiceFixtureWith(t, filepath.Join(t.TempDir(), "c.sqlite"), opts), clock: clock}
}

// old creates a conversation that already has a native session.
func (f *focusFixture) old(t *testing.T, id string) {
	t.Helper()
	if _, err := f.svc.Store().CreateConversation(context.Background(), store.NewConversation{ID: id, ScopeID: f.scopeID, Provider: "claude", ProviderSessionID: "native-" + id, Cwd: "/repo", CreatedAt: f.clock.Now()}); err != nil {
		t.Fatal(err)
	}
}

func (f *focusFixture) focus(t *testing.T, id, client string, focused bool) {
	t.Helper()
	if err := f.svc.SetFocus(context.Background(), f.scopeID, id, client, focused, timing.Caller{}); err != nil {
		t.Fatal(err)
	}
}

func (f *focusFixture) started(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.adapters {
		a.mu.Lock()
		for i, typ := range a.requests {
			if typ == "start" && a.fields[i]["resume"] == "native-"+id {
				n++
			}
		}
		a.mu.Unlock()
	}
	return n
}

func (f *focusFixture) adapterCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.adapters)
}

func (f *focusFixture) idle(id string) bool {
	f.svc.mu.Lock()
	r := f.svc.runners[id]
	f.svc.mu.Unlock()
	if r == nil {
		return false
	}
	_, ok := r.IdleProcess()
	return ok
}

func (f *focusFixture) preheat(t *testing.T, id string) {
	t.Helper()
	f.focus(t, id, "preheater", true)
	eventually(t, "preheat of "+id, func() bool { return f.idle(id) })
}

func TestFocusPreheatsOnceAcrossClients(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	f.old(t, "conv-1")
	f.focus(t, "conv-1", "phone", true)
	eventually(t, "preheat", func() bool { return f.idle("conv-1") })
	f.focus(t, "conv-1", "desktop", true)
	f.focus(t, "conv-1", "phone", true)
	time.Sleep(10 * time.Millisecond)
	if f.adapterCount() != 1 {
		t.Fatalf("%d adapters started", f.adapterCount())
	}
	if f.svc.limiter.Running() != 0 {
		t.Fatal("preheat took a turn slot")
	}
	if got := fmt.Sprint(f.adapters[0].sent()); got != "[start]" {
		t.Fatalf("requests = %s", got)
	}
}

func TestFocusIgnoresAnotherScopesConversation(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	f.old(t, "conv-1")
	if err := f.svc.SetFocus(context.Background(), f.scopeID+1, "conv-1", "phone", true, timing.Caller{}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if f.adapterCount() != 0 {
		t.Fatal("another scope's conversation started a process")
	}
}

func TestFocusDoesNotPreheatWithoutANativeSession(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	f.focus(t, "conv-1", "phone", true)
	f.runTurn(t, "r1", "u1")
	side, err := f.svc.CreateSide(ctx, SideCommand{ScopeID: f.scopeID, SourceID: "conv-1", TurnID: "turn-r1", Quote: "answer r1", RequestID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	before := f.adapterCount()
	f.focus(t, side.ID, "phone", true)
	time.Sleep(10 * time.Millisecond)
	if f.adapterCount() != before {
		t.Fatal("a side conversation without a native session preheated")
	}
	// Its first message still carries the source context.
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: side.ID, RequestID: "side-1", Text: "and then?"}); err != nil {
		t.Fatal(err)
	}
	sends := f.sends(t)
	if !strings.HasPrefix(sends[len(sends)-1], "You are in an independent Multica side conversation.") {
		t.Fatalf("side prompt = %q", sends[len(sends)-1])
	}
}

// gateLaunch holds runner creation inside the launcher until the returned open
// runs, as resolving a CLI does.
func (f *focusFixture) gateLaunch() (entered <-chan struct{}, open func()) {
	in, gate := make(chan struct{}, 1), make(chan struct{})
	f.mu.Lock()
	f.beforeLaunch = func() {
		select {
		case in <- struct{}{}:
		default:
		}
		<-gate
	}
	f.mu.Unlock()
	return in, func() { close(gate) }
}

// A permission downgrade committed while a runner is being created finds no runner
// to apply to, so the runner itself has to pick it up.
func TestSettingsChangedWhileARunnerIsCreatedReachItsProcess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	full, normal := "full-access", "default"
	for name, begin := range map[string]func(t *testing.T, f *focusFixture){
		"preheat": func(t *testing.T, f *focusFixture) { f.focus(t, "conv-1", "phone", true) },
		"send": func(t *testing.T, f *focusFixture) {
			go func() {
				_, _ = f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "go"})
			}()
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFocusFixture(t, Options{})
			f.old(t, "conv-1")
			if err := f.svc.UpdateSettings(ctx, f.scopeID, "conv-1", SettingsUpdate{PermissionMode: &full}); err != nil {
				t.Fatal(err)
			}
			entered, open := f.gateLaunch()
			begin(t, f)
			<-entered
			if err := f.svc.UpdateSettings(ctx, f.scopeID, "conv-1", SettingsUpdate{PermissionMode: &normal}); err != nil {
				t.Fatal(err)
			}
			open()
			eventually(t, "the process", func() bool { return f.adapterCount() == 1 && fieldsOf(f.adapters[0], "start") != nil })
			if got := fieldsOf(f.adapters[0], "start")["permission_mode"]; got != normal {
				t.Fatalf("the process started with %v", got)
			}
		})
	}
}

func TestStopProvidersDuringAPreheatStartsNoProcess(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	f.old(t, "conv-1")
	entered, open := f.gateLaunch()
	f.focus(t, "conv-1", "phone", true)
	<-entered
	if err := f.svc.StopProviders(context.Background()); err != nil {
		t.Fatal(err)
	}
	open()
	eventually(t, "the preheat to end", func() bool {
		f.svc.focus.mu.Lock()
		defer f.svc.focus.mu.Unlock()
		return !f.svc.focus.working
	})
	f.svc.mu.Lock()
	registered := len(f.svc.runners)
	f.svc.mu.Unlock()
	if registered != 0 || f.adapterCount() != 0 {
		t.Fatalf("%d runners, %d processes during an update", registered, f.adapterCount())
	}
	// The refused preheat is not lost if the update is called off.
	if err := f.svc.Undrain(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "preheat after undrain", func() bool { return f.idle("conv-1") })
}

func TestFocusDoesNotPreheatAnArchivedConversation(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	f.old(t, "conv-1")
	archived := true
	if _, err := f.svc.UpdateMetadata(context.Background(), f.scopeID, "conv-1", MetadataUpdate{Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	f.focus(t, "conv-1", "phone", true)
	time.Sleep(10 * time.Millisecond)
	if f.adapterCount() != 0 {
		t.Fatal("an archived conversation preheated")
	}
}

func TestFocusRespectsTheDeviceSetting(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{PreheatOldConversations: func() bool { return false }})
	f.old(t, "conv-1")
	f.focus(t, "conv-1", "phone", true)
	time.Sleep(10 * time.Millisecond)
	if f.adapterCount() != 0 {
		t.Fatal("preheated with the setting off")
	}
}

func TestDrainRejectsPreheatAndUndrainRetries(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	ctx := context.Background()
	f.old(t, "conv-1")
	if _, err := f.svc.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	f.focus(t, "conv-1", "phone", true)
	time.Sleep(10 * time.Millisecond)
	if f.adapterCount() != 0 {
		t.Fatal("preheated while draining")
	}
	if err := f.svc.Undrain(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "preheat after undrain", func() bool { return f.idle("conv-1") })
}

func TestFocusSurvivesAConnectionMoveWithinGrace(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	f.old(t, "conv-1")
	gone := f.svc.ClientConnected("phone")
	f.focus(t, "conv-1", "phone", true)
	eventually(t, "preheat", func() bool { return f.idle("conv-1") })

	gone()
	f.clock.advance(focusGrace - time.Second)
	f.svc.runMaintenance()
	back := f.svc.ClientConnected("phone")
	defer back()
	f.clock.advance(time.Hour)
	f.svc.runMaintenance()
	if !f.idle("conv-1") {
		t.Fatal("a reconnected client lost its focus")
	}
	if f.adapterCount() != 1 {
		t.Fatalf("%d adapters", f.adapterCount())
	}
}

func TestOverlappingConnectionsOfAClientKeepItsFocus(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	f.old(t, "conv-1")
	first := f.svc.ClientConnected("phone")
	f.focus(t, "conv-1", "phone", true)
	eventually(t, "preheat", func() bool { return f.idle("conv-1") })
	second := f.svc.ClientConnected("phone")
	defer second()
	first()
	first() // A repeated close changes nothing.
	f.clock.advance(time.Hour)
	f.svc.runMaintenance()
	if !f.idle("conv-1") {
		t.Fatal("the old connection's close ended the focus")
	}
}

func TestProcessIdleClockStartsWhenTheLastClientLeaves(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	f.old(t, "conv-1")
	phone := f.svc.ClientConnected("phone")
	desktop := f.svc.ClientConnected("desktop")
	defer desktop()
	f.focus(t, "conv-1", "phone", true)
	f.focus(t, "conv-1", "desktop", true)
	eventually(t, "preheat", func() bool { return f.idle("conv-1") })

	// Long past the preheat limit, one client still looks at it.
	f.clock.advance(preheatedIdleLimit * 2)
	phone()
	f.clock.advance(focusGrace)
	f.svc.runMaintenance()
	if !f.idle("conv-1") {
		t.Fatal("evicted while another client was focused")
	}
	f.focus(t, "conv-1", "desktop", false)
	f.clock.advance(preheatedIdleLimit - time.Second)
	f.svc.runMaintenance()
	if !f.idle("conv-1") {
		t.Fatal("evicted before the limit counted from leaving")
	}
	f.clock.advance(time.Second)
	f.svc.runMaintenance()
	if f.idle("conv-1") {
		t.Fatal("not evicted after the limit")
	}
	// The runner stays so the conversation resumes.
	f.svc.mu.Lock()
	r := f.svc.runners["conv-1"]
	f.svc.mu.Unlock()
	if r == nil {
		t.Fatal("eviction dropped the runner")
	}
}

func TestPreheatsRunOneAtATimeAndLeaveTheQueueWhenUnfocused(t *testing.T) {
	t.Parallel()

	entered, open := make(chan struct{}, 8), make(chan struct{})
	f := newFocusFixture(t, Options{})
	f.reply = func(_ *fakeAdapter, typ string) (json.RawMessage, error) {
		if typ == "start" {
			entered <- struct{}{}
			<-open
		}
		return json.RawMessage(`{}`), nil
	}
	for _, id := range []string{"conv-1", "conv-2", "conv-3", "conv-new"} {
		f.old(t, id)
	}
	f.focus(t, "conv-1", "phone", true)
	<-entered
	f.focus(t, "conv-2", "phone", true)
	f.focus(t, "conv-3", "phone", true)
	time.Sleep(10 * time.Millisecond)
	if n := f.adapterCount(); n != 1 {
		t.Fatalf("%d preheats running at once", n)
	}

	// A real send does not wait behind the running preheat.
	f.mu.Lock()
	f.reply = nil
	f.mu.Unlock()
	if _, err := f.svc.Send(context.Background(), SendCommand{ScopeID: f.scopeID, ConversationID: "conv-new", RequestID: "r-new", Text: "go"}); err != nil {
		t.Fatal(err)
	}

	f.focus(t, "conv-2", "phone", false)
	close(open)
	eventually(t, "conv-3 preheat", func() bool { return f.idle("conv-3") })
	if f.started("conv-2") != 0 {
		t.Fatal("an unfocused conversation preheated")
	}
}

func TestIdleLimitsAndLeastRecentlyUsedOrder(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	// Sent processes, oldest first.
	for i := range maxIdleProcesses {
		id := fmt.Sprintf("sent-%d", i)
		f.create(t, id, "claude")
		f.finishTurn(t, id, "r"+id)
		f.clock.advance(time.Minute)
	}
	if got := f.idleCount(); got != maxIdleProcesses {
		t.Fatalf("idle = %d", got)
	}
	f.svc.runMaintenance()
	if got := f.idleCount(); got != maxIdleProcesses {
		t.Fatalf("idle at the limit = %d", got)
	}
	// One preheated process over the limit goes first although it is newest.
	f.old(t, "fresh")
	f.preheat(t, "fresh")
	f.focus(t, "fresh", "preheater", false)
	f.svc.runMaintenance()
	if f.idle("fresh") || f.idleCount() != maxIdleProcesses {
		t.Fatalf("fresh idle = %v, count = %d", f.idle("fresh"), f.idleCount())
	}
	// Then the least recently used sent process.
	f.create(t, "sent-new", "claude")
	f.finishTurn(t, "sent-new", "rnew")
	f.svc.runMaintenance()
	if f.idle("sent-0") || !f.idle("sent-1") || !f.idle("sent-new") {
		t.Fatalf("sent-0 %v sent-1 %v sent-new %v", f.idle("sent-0"), f.idle("sent-1"), f.idle("sent-new"))
	}
	// Sent processes last until their limit.
	f.clock.advance(sentIdleLimit)
	f.svc.runMaintenance()
	if got := f.idleCount(); got != 0 {
		t.Fatalf("idle after the limit = %d", got)
	}
}

func TestEvictionLeavesRunningAndWaitingConversations(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	ctx := context.Background()
	f.create(t, "running", "claude")
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "running", RequestID: "r1", Text: "go"}); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(sentIdleLimit * 2)
	f.svc.runMaintenance()
	f.svc.mu.Lock()
	r := f.svc.runners["running"]
	f.svc.mu.Unlock()
	if !r.Running() || r.Evict(ctx) {
		t.Fatal("a running conversation lost its process")
	}
	f.create(t, "asking", "claude")
	f.finishTurn(t, "asking", "r2")
	f.svc.questions.mu.Lock()
	f.svc.questions.waiting["q"] = &waitingQuestion{conversationID: "asking"}
	f.svc.questions.mu.Unlock()
	f.clock.advance(sentIdleLimit * 2)
	f.svc.runMaintenance()
	if !f.idle("asking") {
		t.Fatal("evicted with a question waiting")
	}
	f.svc.questions.mu.Lock()
	delete(f.svc.questions.waiting, "q")
	f.svc.questions.mu.Unlock()
	f.svc.runMaintenance()
	if f.idle("asking") {
		t.Fatal("not evicted once the question was gone")
	}
}

func TestEvictedConversationResumesOnItsNextSend(t *testing.T) {
	t.Parallel()

	f := newFocusFixture(t, Options{})
	ctx := context.Background()
	f.old(t, "conv-1")
	f.preheat(t, "conv-1")
	f.focus(t, "conv-1", "preheater", false)
	f.clock.advance(preheatedIdleLimit)
	f.svc.runMaintenance()
	if f.idle("conv-1") {
		t.Fatal("not evicted")
	}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "go"}); err != nil {
		t.Fatal(err)
	}
	if f.started("conv-1") != 2 {
		t.Fatalf("%d resumes", f.started("conv-1"))
	}
}

func TestStopProvidersDuringAPreheatStopsItsProcess(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 1)
	f := newFocusFixture(t, Options{})
	f.reply = func(a *fakeAdapter, typ string) (json.RawMessage, error) {
		if typ == "start" {
			entered <- struct{}{}
			<-a.done
		}
		return json.RawMessage(`{}`), nil
	}
	f.old(t, "conv-1")
	f.focus(t, "conv-1", "phone", true)
	<-entered
	f.mu.Lock()
	a := f.adapters[0]
	f.mu.Unlock()
	// The start ignores cancellation until the process is gone, as a stuck
	// adapter does, so stop it once the runner has been told to close.
	closing := make(chan error, 1)
	stopCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	go func() { closing <- f.svc.StopProviders(stopCtx) }()
	time.Sleep(10 * time.Millisecond)
	a.exit(nil)
	if err := <-closing; err != nil {
		t.Fatal(err)
	}
	eventually(t, "no process kept", func() bool { return f.idleCount() == 0 })
	if err := f.svc.acceptTurn(); !errors.Is(err, ErrDraining) {
		t.Fatalf("accept = %v", err)
	}
}

func (f *focusFixture) idleCount() int {
	f.svc.mu.Lock()
	defer f.svc.mu.Unlock()
	n := 0
	for _, r := range f.svc.runners {
		if _, ok := r.IdleProcess(); ok {
			n++
		}
	}
	return n
}

// finishTurn runs one turn to its end on conversation id.
func (f *focusFixture) finishTurn(t *testing.T, id, request string) {
	t.Helper()
	if _, err := f.svc.Send(context.Background(), SendCommand{ScopeID: f.scopeID, ConversationID: id, RequestID: request, Text: "go"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	a := f.adapters[len(f.adapters)-1]
	f.mu.Unlock()
	a.emit(t, `{"type":"result","subtype":"success","is_error":false,"result":""}`)
	eventually(t, "turn end of "+id, func() bool { return f.idle(id) })
}
