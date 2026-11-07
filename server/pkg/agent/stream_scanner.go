package agent

import (
	"bufio"
	"errors"
	"io"
)

// agentStreamMaxLineBytes bounds a single line read from an agent CLI's
// event stream. Agent transports are line-delimited JSON, and one line can
// carry a whole conversation: Codex app-server serializes an entire thread
// into the single `thread/resume` response, and providers that re-send a
// growing message partial on every delta (Pi) or embed a large tool result
// in one event grow the same way. Crossing this bound makes scanner.Scan()
// return false with bufio.ErrTooLong, which the backends can only report as
// a transport failure — the session itself is fine, we just cannot read it.
//
// 32 MiB was picked as the follow-up to GH#4520, where the previous 10 MiB
// bound broke `thread/resume` for long Codex threads (MUL-5722). It is
// headroom, not a guarantee: a thread can still outgrow any fixed cap, so
// the recovery path matters more than the number.
const agentStreamMaxLineBytes = 32 * 1024 * 1024

// agentStreamInitialBufferBytes is the scanner's starting allocation. Lines
// above it still grow up to agentStreamMaxLineBytes; this only decides how
// much is reserved before the first grow, so ordinary events never realloc.
const agentStreamInitialBufferBytes = 1024 * 1024

// newAgentStreamScanner returns a bufio.Scanner sized for agent event
// streams. Every backend reading a line-delimited agent transport must go
// through this constructor: the bound used to be copy-pasted per backend and
// silently drifted, which is how GH#4520's fix reached only one of them.
func newAgentStreamScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, agentStreamInitialBufferBytes), agentStreamMaxLineBytes)
	return scanner
}

// readAgentStreamLine reads the next newline-delimited record from r and
// returns it without the newline (a single trailing '\r' is dropped, like
// bufio.Scanner's line splitting). It is the log-walking companion to
// newAgentStreamScanner for scans that read a session's persisted stream
// after the fact: where the Scanner ends its scan for good once a line
// passes agentStreamMaxLineBytes, readAgentStreamLine consumes and discards
// that record whole and returns bufio.ErrTooLong with r positioned after it,
// so later records stay readable — an oversized record must not hide the
// records behind it. A final unterminated line (a live log's partial-write
// tail) is returned whole, like Scanner does. The returned line is non-nil
// only when err is nil; an exhausted reader returns io.EOF. Any other read
// error is returned as-is.
func readAgentStreamLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	discard := false
	for {
		chunk, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			if !discard {
				line = append(line, chunk...)
				if discard = len(line) > agentStreamMaxLineBytes; discard {
					line = nil // one record must not pin the bound in memory
				}
			}
			continue
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, err
			}
			if discard {
				return nil, bufio.ErrTooLong
			}
			if tail := append(line, chunk...); len(tail) > 0 {
				if len(tail) > agentStreamMaxLineBytes {
					return nil, bufio.ErrTooLong
				}
				return tail, nil
			}
			return nil, io.EOF
		}
		if discard {
			return nil, bufio.ErrTooLong
		}
		line = append(line, chunk[:len(chunk)-1]...)
		if len(line) > agentStreamMaxLineBytes {
			return nil, bufio.ErrTooLong
		}
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		return line, nil
	}
}
