package daemon

import (
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"
)

func silentDaemonLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(noopWriter{}, nil))
}

// TestSignalTaskWakeup_RecordsRuntimeBeforeSend pins refinement 1 (#7452): the
// woken runtime must be recorded in the force-recheck set BEFORE the channel
// send is attempted, not only on the full-channel default branch. If recording
// trailed the send, a poller that consumed the already-queued nudge and drained
// between the failed send and the note would strand the id in a set nobody
// drains, with no nudge left to drive a claim.
//
// The observable proof is a channel with room: on the pre-fix ordering the send
// succeeds and the producer never records the runtime (it relied on the
// consumer noting it after receipt), so the set is empty right after the call.
// Recording first makes the runtime present regardless of the send outcome.
func TestSignalTaskWakeup_RecordsRuntimeBeforeSend(t *testing.T) {
	d := &Daemon{logger: silentDaemonLogger()}
	taskWakeups := make(chan taskWakeup, 1) // room available, so the send succeeds

	d.signalTaskWakeup(taskWakeups, "rt-1")

	if !d.hasPendingForceRecheck() {
		t.Fatal("runtime not recorded before the send; a racing drain could strand it")
	}
	got := d.drainWokenRuntimes()
	if len(got) != 1 || got[0] != "rt-1" {
		t.Fatalf("woken set = %v, want [rt-1]", got)
	}
}

// TestSignalTaskWakeup_ConcurrentDrainNeverStrands stresses the drop-on-full
// path against a concurrent drainer (#7452): the runtime id must always end up
// either drained by the racer or still pending for the next drain — never lost.
func TestSignalTaskWakeup_ConcurrentDrainNeverStrands(t *testing.T) {
	logger := silentDaemonLogger()
	for i := 0; i < 100_000; i++ {
		d := &Daemon{logger: logger}
		full := make(chan taskWakeup) // unbuffered, no receiver: every send hits default

		var wg sync.WaitGroup
		wg.Add(1)
		var raced []string
		go func() {
			defer wg.Done()
			raced = d.drainWokenRuntimes()
		}()
		d.signalTaskWakeup(full, "rt-burst")
		wg.Wait()

		drainedByRacer := len(raced) == 1 && raced[0] == "rt-burst"
		if !drainedByRacer && !d.hasPendingForceRecheck() {
			t.Fatalf("iteration %d: runtime neither drained (%v) nor pending — lost", i, raced)
		}
	}
}

// TestForceRecheckFiltersDeletedRuntimeBeforeClaim covers refinement 2 (#7452,
// #7969): a runtime woken and then deleted before the claim is intersected out
// by filterLiveRuntimes, so its id never reaches the request. Without the
// filter it would be sent every cycle forever, because the server filters the
// request down to authorized runtimes and can never acknowledge a deleted one.
func TestForceRecheckFiltersDeletedRuntimeBeforeClaim(t *testing.T) {
	d := &Daemon{
		logger:     silentDaemonLogger(),
		workspaces: map[string]*workspaceState{"ws-1": {runtimeIDs: []string{"rt-live"}}},
	}
	d.noteWokenRuntime("rt-live")
	d.noteWokenRuntime("rt-gone") // woken, then its runtime was removed

	forceRecheck := d.filterLiveRuntimes(d.drainWokenRuntimes())
	if len(forceRecheck) != 1 || forceRecheck[0] != "rt-live" {
		t.Fatalf("force-recheck set = %v, want only the live runtime [rt-live]", forceRecheck)
	}
}

// TestForceRecheckFiltersDeletionRacingInFlightClaim covers the interleaving
// half of refinement 2 (#7452, #7969): a runtime still live when the claim goes
// out but deleted while it is in flight must be dropped from the re-note, not
// re-forced next cycle. The poller re-filters against the live set at consume
// time, so the deletion is honoured even mid-request.
func TestForceRecheckFiltersDeletionRacingInFlightClaim(t *testing.T) {
	ws := &workspaceState{runtimeIDs: []string{"rt-live", "rt-gone"}}
	d := &Daemon{
		logger:     silentDaemonLogger(),
		workspaces: map[string]*workspaceState{"ws-1": ws},
	}
	d.noteWokenRuntime("rt-live")
	d.noteWokenRuntime("rt-gone")

	// Both runtimes are live when the claim is issued.
	forceRecheck := d.filterLiveRuntimes(d.drainWokenRuntimes())
	sort.Strings(forceRecheck)
	if len(forceRecheck) != 2 {
		t.Fatalf("force-recheck set at claim time = %v, want both runtimes", forceRecheck)
	}

	// rt-gone is deleted while the claim is in flight; the server acknowledged
	// nothing (present-but-empty), so every unacknowledged runtime would normally
	// be re-forced.
	ws.runtimeIDs = []string{"rt-live"}
	d.consumeForceRecheckHints(d.filterLiveRuntimes(forceRecheck), &[]string{})

	remaining := d.drainWokenRuntimes()
	if len(remaining) != 1 || remaining[0] != "rt-live" {
		t.Fatalf("re-forced set = %v, want only [rt-live] (rt-gone dropped after deletion)", remaining)
	}
}

// TestTaskClaimPollInterval_PendingForceKeepsNormalCadence pins refinement 3 of
// the rebase (#7452 vs #7983): a claim over a healthy WS with poll hints would
// otherwise take the long (~3-minute) safety interval, but a still-pending
// force-recheck hint must keep the poller on the normal PollInterval so the next
// same-agent task is not stranded for the exact window this fix targets.
func TestTaskClaimPollInterval_PendingForceKeepsNormalCadence(t *testing.T) {
	d := New(Config{
		PollInterval:        30 * time.Second,
		WSClaimPollInterval: 3 * time.Minute,
		MaxConcurrentTasks:  1,
	}, silentDaemonLogger())
	gen := d.wsRPC.attach(func([]byte) (*wsOutbound, error) { return nil, nil })
	d.wsRPC.markRPCV1Supported(gen)

	result := claimTasksResult{ClaimedOverWS: true, ClaimPollHintSupported: true}

	// No pending force: the healthy-WS path takes the long jittered interval,
	// which sits well above the normal poll.
	if got := d.taskClaimPollInterval(result); got <= d.cfg.PollInterval {
		t.Fatalf("healthy-WS interval = %v, want the long safety interval above PollInterval %v", got, d.cfg.PollInterval)
	}

	// A pending force hint must collapse the interval back to the normal poll.
	d.noteWokenRuntime("rt-forced")
	if got := d.taskClaimPollInterval(result); got != d.cfg.PollInterval {
		t.Fatalf("interval with pending force = %v, want normal PollInterval %v", got, d.cfg.PollInterval)
	}
}
