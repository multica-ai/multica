package lark

import (
	"bytes"
	"testing"
	"time"
)

func TestChunkAssemblerSingleFramePassthrough(t *testing.T) {
	t.Parallel()
	// sum<=0 means non-chunked event; the connector should never call
	// admit for these, but we still verify the defensive path.
	a := newChunkAssembler(time.Second, nil)
	out, ok := a.admit("msg-1", 0, 0, []byte("not chunked"))
	if ok || out != nil {
		t.Fatalf("admit(sum=0) should return (nil,false); got (%v,%v)", out, ok)
	}
}

func TestChunkAssemblerReassemblesInOrder(t *testing.T) {
	t.Parallel()
	a := newChunkAssembler(time.Second, nil)
	if _, ok := a.admit("om-1", 3, 0, []byte("hello ")); ok {
		t.Fatal("seq=0/3: should be partial")
	}
	if _, ok := a.admit("om-1", 3, 1, []byte("brave ")); ok {
		t.Fatal("seq=1/3: should be partial")
	}
	out, ok := a.admit("om-1", 3, 2, []byte("world"))
	if !ok {
		t.Fatal("seq=2/3: should complete")
	}
	if !bytes.Equal(out, []byte("hello brave world")) {
		t.Errorf("assembled = %q; want %q", string(out), "hello brave world")
	}
	if a.pendingCount() != 0 {
		t.Errorf("pendingCount after complete = %d; want 0", a.pendingCount())
	}
}

func TestChunkAssemblerReassemblesOutOfOrder(t *testing.T) {
	t.Parallel()
	a := newChunkAssembler(time.Second, nil)
	if _, ok := a.admit("om-2", 3, 2, []byte("C")); ok {
		t.Fatal("seq=2/3: should be partial")
	}
	if _, ok := a.admit("om-2", 3, 0, []byte("A")); ok {
		t.Fatal("seq=0/3: should be partial")
	}
	out, ok := a.admit("om-2", 3, 1, []byte("B"))
	if !ok {
		t.Fatal("seq=1/3: should complete")
	}
	if !bytes.Equal(out, []byte("ABC")) {
		t.Errorf("assembled = %q; want ABC", string(out))
	}
}

func TestChunkAssemblerDuplicateSeqIsIdempotent(t *testing.T) {
	t.Parallel()
	a := newChunkAssembler(time.Second, nil)
	if _, ok := a.admit("om-3", 2, 0, []byte("X")); ok {
		t.Fatal("seq=0/2: should be partial")
	}
	// Duplicate seq 0 — must not advance received counter.
	if _, ok := a.admit("om-3", 2, 0, []byte("X")); ok {
		t.Fatal("duplicate seq=0: should still be partial")
	}
	out, ok := a.admit("om-3", 2, 1, []byte("Y"))
	if !ok {
		t.Fatal("seq=1/2: should complete")
	}
	if !bytes.Equal(out, []byte("XY")) {
		t.Errorf("assembled = %q; want XY", string(out))
	}
}

func TestChunkAssemblerMultipleConcurrentMessages(t *testing.T) {
	t.Parallel()
	a := newChunkAssembler(time.Second, nil)
	a.admit("om-A", 2, 0, []byte("a0"))
	a.admit("om-B", 2, 0, []byte("b0"))
	a.admit("om-A", 2, 1, []byte("a1"))
	outA, okA := a.admit("om-A", 2, 1, []byte("a1")) // duplicate complete: already gone
	// "om-A" was already completed and removed; admitting a chunk for a
	// removed entry should treat it as a new entry, which won't complete.
	if okA {
		t.Errorf("re-admit of completed message should not return complete; got %v", string(outA))
	}
	outB, okB := a.admit("om-B", 2, 1, []byte("b1"))
	if !okB || !bytes.Equal(outB, []byte("b0b1")) {
		t.Errorf("om-B assembled = %q ok=%v; want b0b1", string(outB), okB)
	}
}

func TestChunkAssemblerTTLExpiresPartial(t *testing.T) {
	t.Parallel()
	now := time.Now()
	clock := &fakeClock{now: now}
	a := newChunkAssembler(100*time.Millisecond, clock.Now)
	a.admit("om-stale", 2, 0, []byte("first"))
	if a.pendingCount() != 1 {
		t.Fatalf("pendingCount after first admit = %d; want 1", a.pendingCount())
	}

	// Advance past TTL.
	clock.Advance(200 * time.Millisecond)
	if n := a.gcExpired(); n != 1 {
		t.Errorf("gcExpired = %d; want 1", n)
	}
	if a.pendingCount() != 0 {
		t.Errorf("pendingCount after gc = %d; want 0", a.pendingCount())
	}

	// The follow-up chunk arrives after gc — treated as a brand-new
	// entry, must not auto-complete with stale data.
	out, ok := a.admit("om-stale", 2, 1, []byte("second"))
	if ok {
		t.Errorf("late chunk after TTL should not complete; got %q", string(out))
	}
}

func TestChunkAssemblerSlidingTTLAllowsSteadyProgress(t *testing.T) {
	t.Parallel()
	now := time.Now()
	clock := &fakeClock{now: now}
	a := newChunkAssembler(100*time.Millisecond, clock.Now)

	a.admit("om-slow", 3, 0, []byte("A"))
	clock.Advance(80 * time.Millisecond) // < TTL since last chunk
	a.admit("om-slow", 3, 1, []byte("B"))
	clock.Advance(80 * time.Millisecond) // still < TTL since last chunk
	out, ok := a.admit("om-slow", 3, 2, []byte("C"))
	if !ok || !bytes.Equal(out, []byte("ABC")) {
		t.Errorf("steady progress should complete; got ok=%v out=%q", ok, string(out))
	}
}

func TestChunkAssemblerLazyGCRunsOnAdmit(t *testing.T) {
	t.Parallel()
	now := time.Now()
	clock := &fakeClock{now: now}
	a := newChunkAssembler(50*time.Millisecond, clock.Now)

	// Stash a partial entry that will expire.
	a.admit("om-stale", 4, 0, []byte("x"))
	clock.Advance(200 * time.Millisecond)

	// Admit on an unrelated message — the lazy GC inside admit should
	// reap "om-stale" without us needing to call gcExpired manually.
	a.admit("om-fresh", 2, 0, []byte("a"))
	// Only "om-fresh" should remain.
	if got := a.pendingCount(); got != 1 {
		t.Errorf("pendingCount after lazy gc = %d; want 1", got)
	}
}

func TestChunkAssemblerRejectsBadInputs(t *testing.T) {
	t.Parallel()
	a := newChunkAssembler(time.Second, nil)
	cases := []struct {
		name      string
		messageID string
		sum, seq  int
	}{
		{"empty message_id", "", 2, 0},
		{"seq>=sum", "om-1", 2, 2},
		{"negative seq", "om-1", 2, -1},
		{"sum=-1", "om-1", -1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, ok := a.admit(tc.messageID, tc.sum, tc.seq, []byte("x"))
			if ok || out != nil {
				t.Errorf("admit(%+v) should reject; got (%v,%v)", tc, out, ok)
			}
		})
	}
}

func TestParseChunkHeaders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		headers   []FrameHeader
		wantSum   int
		wantSeq   int
		wantMsgID string
	}{
		{
			name:    "all present",
			headers: []FrameHeader{{Key: "sum", Value: "3"}, {Key: "seq", Value: "1"}, {Key: "message_id", Value: "om-7"}},
			wantSum: 3, wantSeq: 1, wantMsgID: "om-7",
		},
		{
			name:    "missing sum/seq",
			headers: []FrameHeader{{Key: "message_id", Value: "om-8"}},
			wantSum: 0, wantSeq: 0, wantMsgID: "om-8",
		},
		{
			name:    "unparseable sum",
			headers: []FrameHeader{{Key: "sum", Value: "abc"}, {Key: "seq", Value: "0"}, {Key: "message_id", Value: "om-9"}},
			wantSum: 0, wantSeq: 0, wantMsgID: "om-9",
		},
		{
			name:    "nil frame",
			headers: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &Frame{Headers: tc.headers}
			if tc.name == "nil frame" {
				f = nil
			}
			gotSum, gotSeq, gotMsgID := parseChunkHeaders(f)
			if gotSum != tc.wantSum || gotSeq != tc.wantSeq || gotMsgID != tc.wantMsgID {
				t.Errorf("parseChunkHeaders = (%d,%d,%q); want (%d,%d,%q)",
					gotSum, gotSeq, gotMsgID, tc.wantSum, tc.wantSeq, tc.wantMsgID)
			}
		})
	}
}

// fakeClock is defined in http_client_test.go (same package).
