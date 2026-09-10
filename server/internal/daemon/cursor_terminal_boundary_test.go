package daemon

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// The ordering both tests below pin is the one that loses a completed run:
//
//  1. the tool watchdog enters the background-cleanup callback,
//  2. Cursor reads its authoritative terminal result and blocks behind the same
//     cleanup lock, so nothing it does is visible yet,
//  3. cleanup fails, leaving native accounting and its timestamp untouched,
//  4. no transcript message arrives to rescue the tick.
//
// Before the terminal boundary existed, the watchdog then cancelled the run and
// executeAndDrain re-tagged Cursor's completed result as idle_watchdog.

// TestIdleWatchdogYieldsToTerminalObservedDuringCleanup drives that exact
// interleaving deterministically: the cleanup callback itself publishes the
// terminal observation, so step 2 provably happens while step 1 is in progress.
func TestIdleWatchdogYieldsToTerminalObservedDuringCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var last, threshold atomic.Int64
		var fired atomic.Bool
		var terminal atomic.Bool
		last.Store(time.Now().UnixNano())

		// A held background tool: in flight, and its activity never moves.
		tools := func() int32 { return 1 }
		interrupts := 0
		interrupt := func() bool {
			interrupts++
			// Cursor read its terminal result while this call held the lock.
			terminal.Store(true)
			// Ownership could not be confirmed, so nothing is released.
			return false
		}

		go new(Daemon).runIdleWatchdog(ctx, time.Minute, time.Minute, &last, tools, &fired, &threshold,
			cancel, make(chan agent.Message), interrupt, terminal.Load, slog.Default())

		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()

		if interrupts == 0 {
			t.Fatal("never reached the cleanup boundary; the interleaving was not exercised")
		}
		if fired.Load() || ctx.Err() != nil {
			t.Fatalf("a decided outcome was force-stopped: fired=%v err=%v", fired.Load(), ctx.Err())
		}

		// And it stays decided: further budgets must not revive the hang verdict.
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if fired.Load() || ctx.Err() != nil {
			t.Fatalf("terminal boundary expired: fired=%v err=%v", fired.Load(), ctx.Err())
		}
	})
}

// terminalRaceBackend reproduces the same ordering through executeAndDrain, so
// the re-tagging half of the defect is covered too. Ordering is enforced by
// channel handshakes; only the watchdog budget itself uses time.
type terminalRaceBackend struct {
	interrupted chan struct{}
	terminal    atomic.Bool
	interrupts  atomic.Int32
}

func (b *terminalRaceBackend) Execute(ctx context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	messages := make(chan agent.Message, 4)
	results := make(chan agent.Result, 1)
	var nativeCount atomic.Int32
	var nativeActivity atomic.Int64
	nativeCount.Store(1)
	nativeActivity.Store(time.Now().UnixNano())
	messages <- agent.Message{Type: agent.MessageToolUse, Tool: "shell", CallID: "bg"}

	go func() {
		defer close(messages)
		defer close(results)
		select {
		case <-b.interrupted:
			// Cleanup already failed and the terminal observation is published;
			// Cursor now finishes normally with its authoritative result.
			results <- agent.Result{Status: "completed", Output: "Cursor terminal result"}
		case <-ctx.Done():
			results <- agent.Result{Status: "aborted"}
		}
	}()

	return &agent.Session{
		Messages:         messages,
		Result:           results,
		ToolActivity:     func() (int32, time.Time) { return nativeCount.Load(), time.Unix(0, nativeActivity.Load()) },
		TerminalObserved: b.terminal.Load,
		InterruptBackgroundTools: func() bool {
			b.interrupts.Add(1)
			// Step 2: the terminal result becomes decided while cleanup holds
			// the lock. Native accounting deliberately stays untouched.
			b.terminal.Store(true)
			select {
			case <-b.interrupted:
			default:
				close(b.interrupted)
			}
			// Step 3: ownership could not be confirmed.
			return false
		},
	}, nil
}

func TestExecuteAndDrainKeepsTerminalResultObservedDuringCleanup(t *testing.T) {
	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond
	d.cfg.AgentToolWatchdog = 50 * time.Millisecond

	backend := &terminalRaceBackend{interrupted: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, _, err := d.executeAndDrain(ctx, backend, "test", agent.ExecOptions{}, slog.Default(), "terminal-race", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}
	if backend.interrupts.Load() == 0 {
		t.Fatal("never reached the cleanup boundary; the interleaving was not exercised")
	}
	if result.Status != "completed" || result.Output != "Cursor terminal result" {
		t.Fatalf("Cursor's authoritative result was rewritten: %+v", result)
	}
}

// handoffProbe both proves and sequences the branch under test. The daemon logs
// once, on entering the post-force-stop hand-off, and that record is the only
// externally visible evidence of which arm executeAndDrain took. Closing the
// gate from the handler is what keeps Result unavailable until then, so the
// outer select provably cannot satisfy itself on the Result arm instead.
type handoffProbe struct {
	slog.Handler
	gate   chan struct{}
	once   sync.Once
	seen   atomic.Bool
	prefix string
}

func (h *handoffProbe) Handle(ctx context.Context, r slog.Record) error {
	if strings.HasPrefix(r.Message, h.prefix) {
		h.seen.Store(true)
		h.once.Do(func() { close(h.gate) })
	}
	return nil
}

func (h *handoffProbe) Enabled(context.Context, slog.Level) bool { return true }
func (h *handoffProbe) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *handoffProbe) WithGroup(string) slog.Handler            { return h }

// lateTerminalBackend hands its result over only once the daemon has entered
// the hand-off, which is the ordering the previous version of this test could
// not guarantee: there the backend sent immediately on cancellation, so the
// outer select could take the Result arm and the drain-timeout classifier was
// never exercised at all.
type lateTerminalBackend struct {
	terminal atomic.Bool
	cancels  atomic.Int32
	gate     <-chan struct{}
}

func (b *lateTerminalBackend) Execute(ctx context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	messages := make(chan agent.Message, 4)
	results := make(chan agent.Result, 1)
	var nativeCount atomic.Int32
	var nativeActivity atomic.Int64
	nativeCount.Store(1)
	nativeActivity.Store(time.Now().UnixNano())
	messages <- agent.Message{Type: agent.MessageToolUse, Tool: "shell", CallID: "bg"}

	go func() {
		defer close(results)
		<-ctx.Done()
		b.cancels.Add(1)
		// Let waitForDrain complete so the hand-off is reached, then stay silent
		// until it is: this is the backend still finalizing, which is why the
		// cancellation surfaces on the drain arm rather than the Result arm.
		close(messages)
		<-b.gate
		// The contract: publish the observation before sending Result, so the
		// daemon's read after delivery cannot lose a race.
		b.terminal.Store(true)
		results <- agent.Result{Status: "completed", Output: "Cursor terminal result"}
	}()

	return &agent.Session{
		Messages:         messages,
		Result:           results,
		ToolActivity:     func() (int32, time.Time) { return nativeCount.Load(), time.Unix(0, nativeActivity.Load()) },
		TerminalObserved: b.terminal.Load,
		// Nothing can be released: cleanup could not confirm ownership.
		InterruptBackgroundTools: func() bool { return false },
	}, nil
}

// TestExecuteAndDrainKeepsTerminalResultHandedOverAfterForceStop pins the arm
// that kept losing: the watchdog fires with the terminal result not yet
// published, so the run reaches the drain-timeout classifier rather than the
// Result arm, and the decided outcome still has to survive. The probe asserts
// that branch really ran instead of inferring it from a passing status.
func TestExecuteAndDrainKeepsTerminalResultHandedOverAfterForceStop(t *testing.T) {
	probe := &handoffProbe{gate: make(chan struct{}), prefix: "idle watchdog fired; waiting"}
	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond
	d.cfg.AgentToolWatchdog = 50 * time.Millisecond

	backend := &lateTerminalBackend{gate: probe.gate}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, _, err := d.executeAndDrain(ctx, backend, "test", agent.ExecOptions{}, slog.New(probe), "late-terminal", "", new(atomic.Int32))
	if err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}
	if backend.cancels.Load() == 0 {
		t.Fatal("the watchdog never cancelled; this test did not exercise the fired path")
	}
	if !probe.seen.Load() {
		t.Fatal("the post-force-stop hand-off never ran; the drain arm was not exercised")
	}
	if result.Status != "completed" || result.Output != "Cursor terminal result" {
		t.Fatalf("a decided outcome was reclassified after the force stop: %+v", result)
	}
}

// wedgedBackend offers no terminal boundary and never finishes: the shape the
// idle watchdog exists for. It must be classified the moment the watchdog
// fires, with no hand-off wait, because a result that could outrank the force
// stop is precisely what this backend cannot produce.
type wedgedBackend struct{}

func (wedgedBackend) Execute(context.Context, string, agent.ExecOptions) (*agent.Session, error) {
	messages := make(chan agent.Message, 1)
	messages <- agent.Message{Type: agent.MessageText, Content: "hello"}
	// Neither closed nor written to again, and no TerminalObserved.
	return &agent.Session{Messages: messages, Result: make(chan agent.Result)}, nil
}

// TestExecuteAndDrainDoesNotDelayBackendsWithoutATerminalBoundary keeps the
// hand-off from becoming a tax on every force stop. Waiting is only justified
// for a backend that can hand back an outcome outranking the watchdog; for the
// rest the point of the watchdog is to free the runtime slot promptly.
func TestExecuteAndDrainDoesNotDelayBackendsWithoutATerminalBoundary(t *testing.T) {
	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	start := time.Now()
	result, _, err := d.executeAndDrain(ctx, wedgedBackend{}, "p", agent.ExecOptions{}, slog.Default(), "wedged", "", new(atomic.Int32))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}
	if result.Status != "idle_watchdog" {
		t.Fatalf("status=%q, want idle_watchdog", result.Status)
	}
	if elapsed > 5*d.cfg.AgentIdleWatchdog {
		t.Fatalf("force stop took %s, want under %s: a backend with no terminal boundary must not wait for a hand-off it cannot make",
			elapsed, 5*d.cfg.AgentIdleWatchdog)
	}
}
