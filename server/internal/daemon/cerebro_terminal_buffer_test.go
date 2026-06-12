// CEREBRO-PATCH(daemon-cerebro-term-buffer): tests for term_stdout retention
// across WS flaps (TECH-3388).
package daemon

import (
	"testing"
)

func newTermBufferTestDaemon() *Daemon {
	return &Daemon{
		cerebroTermBuffers: make(map[string][][]byte),
	}
}

// wsUp installs a buffered writes channel so enqueueCerebroFrame succeeds, and
// returns the channel so the test can assert what was sent.
func wsUp(d *Daemon, cap int) chan []byte {
	ch := make(chan []byte, cap)
	d.wsWrites.Store(&ch)
	return ch
}

func wsDown(d *Daemon) {
	d.wsWrites.Store(nil)
}

func drainChan(ch chan []byte) []string {
	var out []string
	for {
		select {
		case f := <-ch:
			out = append(out, string(f))
		default:
			return out
		}
	}
}

// While the WS is down, output is retained (not dropped); on reconnect it
// flushes in order.
func TestTermBuffer_RetainsWhileDownFlushesOnReconnect(t *testing.T) {
	d := newTermBufferTestDaemon()
	wsDown(d)

	d.sendOrBufferTermStdout("t1", []byte("A"))
	d.sendOrBufferTermStdout("t1", []byte("B"))

	if got := len(d.cerebroTermBuffers["t1"]); got != 2 {
		t.Fatalf("expected 2 buffered frames while WS down, got %d", got)
	}

	ch := wsUp(d, 10)
	d.drainAllCerebroTermBuffers()

	if got := drainChan(ch); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("expected [A B] flushed in order, got %v", got)
	}
	if _, ok := d.cerebroTermBuffers["t1"]; ok {
		t.Fatalf("buffer should be cleared after full flush")
	}
}

// Once a backlog exists, newer output must queue behind it — never jump ahead —
// even if the WS comes back before the drain runs.
func TestTermBuffer_PreservesOrderWithPendingBacklog(t *testing.T) {
	d := newTermBufferTestDaemon()
	wsDown(d)
	d.sendOrBufferTermStdout("t1", []byte("A"))
	d.sendOrBufferTermStdout("t1", []byte("B"))

	// WS is technically up again, but a backlog is pending: C must not overtake.
	ch := wsUp(d, 10)
	d.sendOrBufferTermStdout("t1", []byte("C"))

	if got := drainChan(ch); len(got) != 0 {
		t.Fatalf("C must not be sent ahead of the backlog, got %v", got)
	}
	if got := len(d.cerebroTermBuffers["t1"]); got != 3 {
		t.Fatalf("expected backlog of 3 (A,B,C), got %d", got)
	}

	d.drainCerebroTermBuffer("t1")
	if got := drainChan(ch); len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Fatalf("expected [A B C] in order, got %v", got)
	}
}

// When the WS is up and no backlog exists, output goes straight through.
func TestTermBuffer_DirectSendWhenHealthy(t *testing.T) {
	d := newTermBufferTestDaemon()
	ch := wsUp(d, 10)

	d.sendOrBufferTermStdout("t1", []byte("A"))

	if got := drainChan(ch); len(got) != 1 || got[0] != "A" {
		t.Fatalf("expected [A] sent live, got %v", got)
	}
	if len(d.cerebroTermBuffers["t1"]) != 0 {
		t.Fatalf("nothing should be buffered on the healthy path")
	}
}

// A finished task's backlog is dropped and cannot be re-sent.
func TestTermBuffer_RemovedOnTaskEnd(t *testing.T) {
	d := newTermBufferTestDaemon()
	wsDown(d)
	d.sendOrBufferTermStdout("t1", []byte("A"))

	d.removeCerebroTermBuffer("t1")

	ch := wsUp(d, 10)
	d.drainAllCerebroTermBuffers()
	if got := drainChan(ch); len(got) != 0 {
		t.Fatalf("removed task's backlog must not flush, got %v", got)
	}
}

// A prolonged outage cannot grow the backlog without bound: oldest frames are
// dropped first once the byte cap is exceeded.
func TestTermBuffer_BoundDropsOldest(t *testing.T) {
	d := newTermBufferTestDaemon()
	wsDown(d)

	frame := make([]byte, 200*1024) // 200 KB each; 4 frames = 800 KB > 512 KB cap
	labels := []byte("WXYZ")
	for i := 0; i < 4; i++ {
		f := make([]byte, len(frame))
		f[0] = labels[i] // tag the first byte so we can identify which survived
		d.sendOrBufferTermStdout("t1", f)
	}

	buf := d.cerebroTermBuffers["t1"]
	total := 0
	for _, f := range buf {
		total += len(f)
	}
	if total > cerebroTermBufferMaxBytes {
		t.Fatalf("backlog %d exceeds cap %d", total, cerebroTermBufferMaxBytes)
	}
	// 800 KB down to <=512 KB keeps the 2 most-recent (Y, Z).
	if len(buf) != 2 || buf[0][0] != 'Y' || buf[1][0] != 'Z' {
		t.Fatalf("expected newest two frames [Y Z], got len=%d first=%q", len(buf), firstBytes(buf))
	}
}

func firstBytes(buf [][]byte) string {
	out := make([]byte, 0, len(buf))
	for _, f := range buf {
		if len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return string(out)
}
