package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"
)

// TestConsumeForceRecheckHints_ReNotesEverythingUnacknowledged pins the
// acknowledgement contract (#7452). The server acknowledges only the forced
// runtimes it observed genuinely idle; every other drained runtime stays forced
// so the next claim forces it again.
func TestConsumeForceRecheckHints_ReNotesEverythingUnacknowledged(t *testing.T) {
	// A partial acknowledgement: rt-1's scan came back empty, rt-2's did not
	// (either it found candidates or it was never scanned). rt-2 stays forced.
	d := &Daemon{}
	d.consumeForceRecheckHints([]string{"rt-1", "rt-2"}, &[]string{"rt-1"})
	got := d.drainWokenRuntimes()
	sort.Strings(got)
	if len(got) != 1 || got[0] != "rt-2" {
		t.Fatalf("re-noted set = %v, want [rt-2] (only the unacknowledged runtime)", got)
	}

	// A full acknowledgement: every drained runtime was confirmed idle, so
	// nothing stays forced.
	d = &Daemon{}
	d.consumeForceRecheckHints([]string{"rt-1", "rt-2"}, &[]string{"rt-1", "rt-2"})
	if got := d.drainWokenRuntimes(); got != nil {
		t.Fatalf("full acknowledgement re-noted %v, want nothing", got)
	}
}

// TestConsumeForceRecheckHints_PresentButEmptyKeepsAllForced covers refinement 2
// (#7452): a PRESENT but empty acknowledged set means an upgraded server
// explicitly acknowledged nothing — reclaim filled the batch before the scan, or
// every forced scan found candidates — so every drained runtime stays forced.
func TestConsumeForceRecheckHints_PresentButEmptyKeepsAllForced(t *testing.T) {
	d := &Daemon{}
	drained := []string{"rt-1", "rt-2", "rt-3"}
	d.consumeForceRecheckHints(drained, &[]string{})
	got := d.drainWokenRuntimes()
	sort.Strings(got)
	want := []string{"rt-1", "rt-2", "rt-3"}
	if len(got) != len(want) {
		t.Fatalf("re-noted set = %v, want all drained %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("re-noted set = %v, want all drained %v", got, want)
		}
	}
}

// TestConsumeForceRecheckHints_AbsentFieldConsumesHints is the other half of
// refinement 2 (#7452): an ABSENT acknowledged set means a pre-#7452 server that
// cannot act on the hint at all. Re-forcing it forever would be pointless churn,
// so the drained hints are consumed and the empty-claim TTL stays the outer
// bound — exactly the pre-#7452 behaviour.
func TestConsumeForceRecheckHints_AbsentFieldConsumesHints(t *testing.T) {
	d := &Daemon{}
	d.consumeForceRecheckHints([]string{"rt-1", "rt-2"}, nil)
	if got := d.drainWokenRuntimes(); got != nil {
		t.Fatalf("absent acknowledgement re-noted %v, want nothing (legacy server)", got)
	}
}

// TestClaimTasksWSFirst_UncertainCooldownAcknowledgesNothing is the "targeted
// wakeup during the uncertain-claim cooldown still forces a re-check next cycle"
// regression (#7452). While the send-nothing cooldown is open ClaimTasksWSFirst
// scans nothing, so it must return a PRESENT but empty acknowledgement —
// driving the daemon to keep the whole drained set forced — even though a frame
// went out on the earlier uncertain attempt.
func TestClaimTasksWSFirst_UncertainCooldownAcknowledgesNothing(t *testing.T) {
	originalDelay := wsClaimUncertainFallbackDelay
	wsClaimUncertainFallbackDelay = time.Hour // keep the cooldown open for the test
	t.Cleanup(func() { wsClaimUncertainFallbackDelay = originalDelay })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[]}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))

	// Drive one uncertain sent-frame outcome so the HTTP-fallback cooldown opens.
	var mu sync.Mutex
	var item *wsOutbound
	frameQueued := make(chan struct{})
	generation := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		close(frameQueued)
		return item, nil
	})
	d.wsRPC.markRPCV1Supported(generation)

	done := make(chan struct{})
	go func() {
		d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2, "rt1")
		close(done)
	}()
	select {
	case <-frameQueued:
	case <-time.After(time.Second):
		t.Fatal("WS claim frame was not queued")
	}
	mu.Lock()
	item.beginWrite()
	mu.Unlock()
	d.wsRPC.attach(nil)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("uncertain ClaimTasksWSFirst did not return")
	}

	// The cooldown is now open. A claim that names a woken runtime scans nothing
	// and must echo an empty force-rechecked set.
	drained := []string{"rt1"}
	tasks, acknowledged, err := d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2, drained...)
	if err != nil {
		t.Fatalf("cooldown ClaimTasksWSFirst: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("cooldown claim returned %d tasks, want 0", len(tasks))
	}
	if acknowledged == nil {
		t.Fatal("cooldown acknowledgement is absent, want present but empty so the force set is kept")
	}
	if len(*acknowledged) != 0 {
		t.Fatalf("cooldown acknowledgement = %v, want empty so the daemon keeps the force set", *acknowledged)
	}

	// End to end: feeding that empty acknowledgement back keeps the runtime forced.
	d.consumeForceRecheckHints(drained, acknowledged)
	if got := d.drainWokenRuntimes(); len(got) != 1 || got[0] != "rt1" {
		t.Fatalf("re-noted set after cooldown = %v, want [rt1]", got)
	}
}

// TestClaimTasksWSFirst_UncertainAfterSendAcknowledgesFullForceSet pins the opposite
// branch (#7452): when a claim frame actually went out and its outcome is
// uncertain, ClaimTasksWSFirst acknowledges the FULL drained force set so the
// daemon re-notes nothing — replaying the hint could double-claim a task the WS frame
// may have already committed.
func TestClaimTasksWSFirst_UncertainAfterSendAcknowledgesFullForceSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tasks":[]}`))
	}))
	defer srv.Close()

	d := New(Config{ServerBaseURL: srv.URL, MaxConcurrentTasks: 4}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))

	var mu sync.Mutex
	var item *wsOutbound
	frameQueued := make(chan struct{})
	generation := d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		mu.Lock()
		defer mu.Unlock()
		item = &wsOutbound{data: frame}
		close(frameQueued)
		return item, nil
	})
	d.wsRPC.markRPCV1Supported(generation)

	drained := []string{"rt1", "rt2"}
	done := make(chan struct{})
	var acknowledged *[]string
	go func() {
		_, acknowledged, _ = d.ClaimTasksWSFirst(context.Background(), "daemon-x", []string{"rt1"}, 2, drained...)
		close(done)
	}()
	select {
	case <-frameQueued:
	case <-time.After(time.Second):
		t.Fatal("WS claim frame was not queued")
	}
	mu.Lock()
	item.beginWrite() // frame on the wire — outcome is genuinely uncertain
	mu.Unlock()
	d.wsRPC.attach(nil)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("uncertain ClaimTasksWSFirst did not return")
	}

	if acknowledged == nil {
		t.Fatal("uncertain-after-send acknowledgement is absent, want the full drained set")
	}
	sort.Strings(*acknowledged)
	if len(*acknowledged) != 2 || (*acknowledged)[0] != "rt1" || (*acknowledged)[1] != "rt2" {
		t.Fatalf("uncertain-after-send acknowledgement = %v, want the full drained set [rt1 rt2]", *acknowledged)
	}
	// Feeding it back re-notes nothing.
	d.consumeForceRecheckHints(drained, acknowledged)
	if got := d.drainWokenRuntimes(); got != nil {
		t.Fatalf("uncertain-after-send re-noted %v, want nothing", got)
	}
}

// TestSignalTaskWakeup_FullChannelCoalescesRuntimeID is the "wakeup channel full
// → runtime id not lost" regression (#7452). signalTaskWakeup drops on a full
// channel, but a targeted wakeup's runtime id is the force-recheck signal, so it
// must be coalesced into the woken set instead of silently lost; the pending
// nudge already queued still drives the next claim.
func TestSignalTaskWakeup_FullChannelCoalescesRuntimeID(t *testing.T) {
	d := New(Config{MaxConcurrentTasks: 1}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))

	// An unbuffered channel with no receiver is always "full" for a non-blocking
	// send, so the coalesce path runs.
	full := make(chan taskWakeup)
	d.signalTaskWakeup(full, "rt-burst")

	got := d.drainWokenRuntimes()
	if len(got) != 1 || got[0] != "rt-burst" {
		t.Fatalf("woken set after dropped wakeup = %v, want [rt-burst]", got)
	}

	// A catch-up wakeup (empty runtime id) has nothing to preserve and must not
	// enter the set.
	d.signalTaskWakeup(full, "")
	if got := d.drainWokenRuntimes(); got != nil {
		t.Fatalf("empty-id wakeup coalesced %v, want nothing", got)
	}
}

// TestClaimTasks_AcknowledgementPresenceOverTheWire is the wire half of
// refinement 2 (#7452): the acknowledged set must survive JSON decoding as
// PRESENT-but-empty when an upgraded server returns an empty array, and as
// ABSENT when an older server omits the field, so the daemon can tell the two
// apart.
func TestClaimTasks_AcknowledgementPresenceOverTheWire(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		present bool
	}{
		{name: "upgraded server acknowledges nothing", body: `{"tasks":[],"force_rechecked_runtime_ids":[]}`, present: true},
		{name: "upgraded server acknowledges one", body: `{"tasks":[],"force_rechecked_runtime_ids":["rt1"]}`, present: true},
		{name: "legacy server omits the field", body: `{"tasks":[]}`, present: false},
		{name: "legacy server sends null", body: `{"tasks":[],"force_rechecked_runtime_ids":null}`, present: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := NewClient(srv.URL)
			c.SetToken("tok")
			result, err := c.claimTasksWithHints(context.Background(), "daemon-x", []string{"rt1"}, 1, "rt1")
			if err != nil {
				t.Fatalf("claimTasksWithHints: %v", err)
			}
			acknowledged := result.ForceRecheckedRuntimeIDs
			if tc.present != (acknowledged != nil) {
				t.Fatalf("acknowledged presence = %v, want %v (body %s)", acknowledged != nil, tc.present, tc.body)
			}

			// The daemon's decision follows from that presence alone: absent consumes
			// the drained hints, present keeps every unacknowledged runtime forced.
			d := &Daemon{}
			d.consumeForceRecheckHints([]string{"rt1", "rt2"}, acknowledged)
			got := d.drainWokenRuntimes()
			sort.Strings(got)
			switch {
			case !tc.present:
				if got != nil {
					t.Fatalf("absent acknowledgement kept %v forced, want nothing", got)
				}
			case len(*acknowledged) == 0:
				if len(got) != 2 {
					t.Fatalf("present-but-empty acknowledgement kept %v forced, want both runtimes", got)
				}
			default:
				if len(got) != 1 || got[0] != "rt2" {
					t.Fatalf("partial acknowledgement kept %v forced, want [rt2]", got)
				}
			}
		})
	}
}
