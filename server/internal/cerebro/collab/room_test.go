package collab

import (
	"encoding/json"
	"errors"
	"testing"
)

func step(s string) json.RawMessage { return json.RawMessage(`{"s":"` + s + `"}`) }

func TestSubmitAcceptsStepsAtCurrentVersion(t *testing.T) {
	r := NewRoom("note-1")

	ok, missing, version, err := r.Submit("alice", 0, []json.RawMessage{step("a"), step("b")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected the batch to be accepted at the current version")
	}
	if len(missing) != 0 {
		t.Fatalf("accepted batch must not report missing steps, got %d", len(missing))
	}
	if version != 2 {
		t.Fatalf("version should advance once per step, want 2 got %d", version)
	}
}

// The whole point of the authority: a client that has not seen everyone else's
// steps must not be able to append on top of a stale version, because that is
// exactly how one person's paragraph silently overwrites another's.
func TestSubmitRejectsStaleVersionAndReturnsMissingSteps(t *testing.T) {
	r := NewRoom("note-1")
	if _, _, _, err := r.Submit("alice", 0, []json.RawMessage{step("a")}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	ok, missing, version, err := r.Submit("bob", 0, []json.RawMessage{step("b")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("a submission based on a stale version must be rejected")
	}
	if version != 1 {
		t.Fatalf("rejection must report the room version, want 1 got %d", version)
	}
	if len(missing) != 1 {
		t.Fatalf("client must receive the step it has not seen, got %d", len(missing))
	}
	if missing[0].ClientID != "alice" {
		t.Fatalf("missing step should carry its author, got %q", missing[0].ClientID)
	}
	if r.Version() != 1 {
		t.Fatalf("a rejected batch must not change the room, version is %d", r.Version())
	}
}

func TestBothWritersTextSurvivesRebase(t *testing.T) {
	// Alice and Bob both start from version 0. Bob is rejected, rebases onto
	// the version he is handed, and resubmits — after which BOTH steps are in
	// the log. If this ever regressed to "last writer wins", the log would
	// hold one step, not two.
	r := NewRoom("note-1")
	if _, _, _, err := r.Submit("alice", 0, []json.RawMessage{step("alice-text")}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	ok, _, version, _ := r.Submit("bob", 0, []json.RawMessage{step("bob-text")})
	if ok {
		t.Fatalf("expected rejection before rebase")
	}
	ok, _, _, err := r.Submit("bob", version, []json.RawMessage{step("bob-text")})
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if !ok {
		t.Fatalf("a rebased resubmission at the current version must be accepted")
	}

	all, err := r.StepsSince(0)
	if err != nil {
		t.Fatalf("StepsSince: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("both writers' steps must survive, got %d", len(all))
	}
	authors := map[string]bool{all[0].ClientID: true, all[1].ClientID: true}
	if !authors["alice"] || !authors["bob"] {
		t.Fatalf("expected one step from each writer, got %v", authors)
	}
}

func TestStepsSinceReturnsOnlyUnseenSteps(t *testing.T) {
	r := NewRoom("note-1")
	r.Submit("alice", 0, []json.RawMessage{step("a"), step("b"), step("c")}) //nolint:errcheck

	got, err := r.StepsSince(2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Version != 3 {
		t.Fatalf("want only the third step, got %+v", got)
	}
}

func TestSnapshotCompactsTheStepLog(t *testing.T) {
	r := NewRoom("note-1")
	r.Submit("alice", 0, []json.RawMessage{step("a"), step("b"), step("c")}) //nolint:errcheck

	if !r.SetSnapshot(Snapshot{Version: 2, Doc: json.RawMessage(`{"type":"doc"}`)}) {
		t.Fatalf("a snapshot at or below the room version must be stored")
	}
	got, err := r.StepsSince(2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("steps already contained in the snapshot must be dropped, got %d", len(got))
	}
	if _, err := r.StepsSince(1); !errors.Is(err, ErrSnapshotTooOld) {
		t.Fatalf("a client below the snapshot base must be told to reload, got %v", err)
	}
}

func TestSnapshotFromTheFutureIsIgnored(t *testing.T) {
	r := NewRoom("note-1")
	r.Submit("alice", 0, []json.RawMessage{step("a")}) //nolint:errcheck

	if r.SetSnapshot(Snapshot{Version: 5, Doc: json.RawMessage(`{"type":"doc"}`)}) {
		t.Fatalf("a snapshot ahead of the room version invents history and must be refused")
	}
	if r.Snapshot() != nil {
		t.Fatalf("refused snapshot must not be stored")
	}
}

func TestLateSnapshotCannotRewindTheRoom(t *testing.T) {
	r := NewRoom("note-1")
	r.Submit("alice", 0, []json.RawMessage{step("a"), step("b"), step("c")}) //nolint:errcheck
	r.SetSnapshot(Snapshot{Version: 3, Doc: json.RawMessage(`{"v":3}`)})     //nolint:errcheck

	if r.SetSnapshot(Snapshot{Version: 1, Doc: json.RawMessage(`{"v":1}`)}) {
		t.Fatalf("an older snapshot arriving late must be ignored")
	}
	if snap := r.Snapshot(); snap == nil || snap.Version != 3 {
		t.Fatalf("the newer snapshot must survive, got %+v", snap)
	}
}

func TestJoinAndLeaveTrackWhoIsInTheNote(t *testing.T) {
	r := NewRoom("note-1")
	alice := NewPeer("p1", "user-a", "Alice", true)
	bob := NewPeer("p2", "user-b", "Bob", true)

	if got := r.Join(alice); len(got) != 1 {
		t.Fatalf("first joiner should see themselves, got %d", len(got))
	}
	if got := r.Join(bob); len(got) != 2 {
		t.Fatalf("second joiner should see both, got %d", len(got))
	}
	remaining, empty := r.Leave("p1")
	if empty {
		t.Fatalf("room still has a peer, must not be reported empty")
	}
	if len(remaining) != 1 || remaining[0].ID != "p2" {
		t.Fatalf("wrong peer left behind: %+v", remaining)
	}
	if _, empty = r.Leave("p2"); !empty {
		t.Fatalf("room must be reported empty once the last peer leaves")
	}
}

func TestBroadcastSkipsTheSender(t *testing.T) {
	r := NewRoom("note-1")
	alice := NewPeer("p1", "user-a", "Alice", true)
	bob := NewPeer("p2", "user-b", "Bob", true)
	r.Join(alice)
	r.Join(bob)

	r.Broadcast([]byte(`{"type":"caret"}`), "p1")

	select {
	case <-bob.Outbound():
	default:
		t.Fatalf("the other peer must receive the message")
	}
	select {
	case <-alice.Outbound():
		t.Fatalf("the sender must not receive its own caret back")
	default:
	}
}

func TestBroadcastReportsStalledPeers(t *testing.T) {
	r := NewRoom("note-1")
	slow := NewPeer("p1", "user-a", "Slow", true)
	r.Join(slow)
	for i := 0; i < 200; i++ {
		slow.Send([]byte(`{"type":"noise"}`))
	}

	stalled := r.Broadcast([]byte(`{"type":"caret"}`), "")
	if len(stalled) != 1 || stalled[0] != "p1" {
		t.Fatalf("a peer that stopped reading must be reported, got %v", stalled)
	}
}

func TestNeedsSnapshotTripsAboveTheCap(t *testing.T) {
	r := NewRoom("note-1")
	batch := make([]json.RawMessage, maxBufferedSteps+1)
	for i := range batch {
		batch[i] = step("x")
	}
	if r.NeedsSnapshot() {
		t.Fatalf("a fresh room does not need compaction")
	}
	r.Submit("alice", 0, batch) //nolint:errcheck
	if !r.NeedsSnapshot() {
		t.Fatalf("a room past the step cap must ask for a snapshot")
	}
}

func TestColorForIsStablePerUser(t *testing.T) {
	if ColorFor("user-a") != ColorFor("user-a") {
		t.Fatalf("the same person must keep the same caret colour")
	}
	if ColorFor("") == "" {
		t.Fatalf("colour must always resolve, even without a user id")
	}
}

func TestRegistryCreatesAndReleasesRooms(t *testing.T) {
	reg := NewRegistry()
	r1, created := reg.Acquire("note-1")
	if !created {
		t.Fatalf("first acquire must create the room")
	}
	r2, created := reg.Acquire("note-1")
	if created || r1 != r2 {
		t.Fatalf("second acquire must reuse the same room")
	}
	if reg.Count() != 1 {
		t.Fatalf("want 1 live room, got %d", reg.Count())
	}
	reg.Release("note-1")
	if reg.Count() != 0 || reg.Get("note-1") != nil {
		t.Fatalf("released room must be gone")
	}
}

// A joiner is handed the room's snapshot, which is a document as of its OWN
// version and is normally behind the room. The steps after that version are
// what close the gap; without them the joiner adopts stale text while believing
// it is current, and its next autosave writes that stale text over the others'
// work. This pins the contract the welcome payload depends on.
func TestSnapshotPlusLaterStepsReachesTheCurrentVersion(t *testing.T) {
	r := NewRoom("note-1")
	r.Submit("alice", 0, []json.RawMessage{step("a"), step("b")}) //nolint:errcheck
	r.SetSnapshot(Snapshot{Version: 2, Doc: json.RawMessage(`{"v":2}`)})
	r.Submit("bob", 2, []json.RawMessage{step("c"), step("d"), step("e")}) //nolint:errcheck

	snap := r.Snapshot()
	if snap == nil {
		t.Fatalf("a joiner must be offered the stored snapshot")
	}
	since, err := r.StepsSince(snap.Version)
	if err != nil {
		t.Fatalf("steps since the snapshot must be replayable: %v", err)
	}
	if len(since) != 3 {
		t.Fatalf("every step after the snapshot must be replayed, got %d", len(since))
	}
	if last := since[len(since)-1].Version; last != r.Version() {
		t.Fatalf("snapshot + steps must land on the room version %d, landed on %d", r.Version(), last)
	}
}
