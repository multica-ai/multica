package agent

import (
	"io"
	"sync"
	"time"
)

// openclawResultIdleGrace is how long stdout must stay silent *after* the
// buffer already parses as a complete openclaw result before readOpenclawStdout
// treats the run as finished.
//
// Deliberately generous. The cheap check — does the buffer parse as a complete
// result? — is the real gate; this only guards against declaring victory
// mid-write if a future openclaw flushes a result and then appends more. 2s is
// far longer than the gap inside one flush, and it costs nothing in the normal
// case because a CLI that exits reaches EOF and never consults it.
const openclawResultIdleGrace = 2 * time.Second

// openclawStdoutPoll is how often the reader re-evaluates its exit conditions.
const openclawStdoutPoll = 100 * time.Millisecond

// readOpenclawStdout drains r and returns the bytes read. It stops at EOF, or
// early once the buffer parses as a complete openclaw result AND stdout has
// been idle for idleGrace — reporting cutShort=true in that second case so the
// caller can cancel the run before waiting on the process.
//
// # Why not io.ReadAll
//
// io.ReadAll returns only at EOF, and EOF requires every write end of the pipe
// to be closed. Observed in production: `openclaw agent --local --json` printed
// its complete result blob — the agent's reply was fully generated — and then
// did not exit. The pipe stayed open, so the read never returned, the goroutine
// never reached cmd.Wait, and the finished reply sat in the daemon's buffer
// while the task held its execution slot:
//
//	T+0s     openclaw started
//	T+24s    the complete result blob was written to stdout
//	T+8min   process still alive, slot still held, user saw nothing
//
// # Why a complete result is the right boundary
//
// This exact hazard is already handled for cursor-agent, whose adapter notes
// that current versions "can emit the terminal result event but keep a worker
// process alive" and therefore treats result as the protocol boundary and
// cancels (see the "result" case in cursor.go). openclaw parses one
// whole-buffer blob rather than line events, so the equivalent condition is
// "the buffer is a complete result" instead of "a result line arrived".
//
// Both conditions are required. Idle alone is not enough — an agent may pause
// for minutes while thinking, and cutting off a partial buffer would discard
// work it has already done, which is worse than the hang. Parseable alone is
// not enough either, since more output may still be coming.
//
// Nothing is cut short before any output appears: idle time is measured from
// the last byte received, so a silent agent remains governed purely by the
// caller's context, exactly as before.
//
// On the cutShort path the internal read goroutine is still blocked in
// r.Read. It exits when the caller's cancellation closes r, which openclaw's
// Execute already arranges. Callers that do not close r on cancellation must
// not use this function.
func readOpenclawStdout(r io.Reader, idleGrace time.Duration) (buf []byte, cutShort bool, err error) {
	if idleGrace <= 0 {
		idleGrace = openclawResultIdleGrace
	}

	var (
		mu       sync.Mutex
		acc      []byte
		lastByte time.Time
		readErr  error
		atEOF    bool
	)

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		chunk := make([]byte, 32*1024)
		for {
			n, rerr := r.Read(chunk)
			// A terminating Read may carry data and its error at once:
			// io.Reader permits returning n > 0 with io.EOF, and this function
			// accepts any io.Reader. Publishing the bytes and the end of the
			// stream in one critical section keeps that a single observation —
			// split across two locks, a poll landing in between sees a buffer
			// that has just become parseable while atEOF is still false, and
			// reports a stream that in fact ended cleanly as cut short.
			//
			// os/exec's StdoutPipe is not one of those readers: *os.File
			// reports the final bytes and the EOF as two adjacent reads, since
			// only a zero-byte read is turned into io.EOF. Production
			// therefore reaches this seam through the wider gap between those
			// two reads, which still has to outlast idleGrace to fool the
			// poll. The narrower race that does not need starvation at all is
			// the one in the tick branch below.
			if n > 0 || rerr != nil {
				mu.Lock()
				if n > 0 {
					acc = append(acc, chunk[:n]...)
					lastByte = time.Now()
				}
				if rerr != nil {
					if rerr != io.EOF {
						readErr = rerr
					}
					atEOF = true
				}
				mu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(openclawStdoutPoll)
	defer ticker.Stop()

	for {
		select {
		case <-finished:
			// The stream ended on its own: the pre-existing behaviour, with the
			// same buffer io.ReadAll would have produced.
			mu.Lock()
			out, rerr := acc, readErr
			mu.Unlock()
			return out, false, rerr

		case <-ticker.C:
			// Decide and copy in one critical section: the bytes returned must be
			// the same bytes the silence check was made about. Snapshotting the
			// conditions, releasing the lock and re-reading acc would let output
			// that arrived in between complete the buffer, so a stream that had
			// just ended cleanly would be reported as cut short.
			//
			// The cheap conditions are still checked first and the buffer copied
			// only once the silence threshold is met. Copying on every tick would
			// allocate the whole accumulated result ~20 times per wait window,
			// which for a large result is pure waste.
			mu.Lock()
			var out []byte
			// atEOF: let the <-finished branch report the final state.
			if !atEOF && len(acc) > 0 && time.Since(lastByte) >= idleGrace {
				out = append([]byte(nil), acc...)
			}
			mu.Unlock()

			if out == nil {
				continue
			}
			if _, ok := parseWholeBufferOpenclawResult(out); !ok {
				continue
			}
			return out, true, nil
		}
	}
}
