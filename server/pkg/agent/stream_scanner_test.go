package agent

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestAgentStreamScannerReadsPastOldTenMiBCap is the regression for MUL-5722 /
// GH#4520: Codex serializes a whole thread into the single `thread/resume`
// response line, and the previous 10 MiB bound turned any thread past that
// size into a permanent "bufio.Scanner: token too long" resume failure.
func TestAgentStreamScannerReadsPastOldTenMiBCap(t *testing.T) {
	t.Parallel()

	const oldCap = 10 * 1024 * 1024
	if agentStreamMaxLineBytes <= oldCap {
		t.Fatalf("agentStreamMaxLineBytes must exceed the old %d-byte cap, got %d",
			oldCap, agentStreamMaxLineBytes)
	}

	line := strings.Repeat("x", oldCap+1024*1024)
	scanner := newAgentStreamScanner(strings.NewReader(line + "\n"))

	if !scanner.Scan() {
		t.Fatalf("expected a %d-byte line to scan, got err=%v", len(line), scanner.Err())
	}
	if got := len(scanner.Bytes()); got != len(line) {
		t.Fatalf("expected %d bytes, got %d", len(line), got)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("unexpected scanner error: %v", err)
	}
}

// TestAgentStreamScannerStillFailsClosedAboveCap keeps the bound a bound: the
// backends' overflow handling (fail the turn rather than silently truncate an
// event) depends on Scan reporting bufio.ErrTooLong, so raising the cap must
// not mean removing it.
func TestAgentStreamScannerStillFailsClosedAboveCap(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	buf.Grow(agentStreamMaxLineBytes + 2)
	for i := 0; i < agentStreamMaxLineBytes+1; i++ {
		buf.WriteByte('x')
	}
	buf.WriteByte('\n')

	scanner := newAgentStreamScanner(&buf)

	if scanner.Scan() {
		t.Fatalf("expected an over-cap line to fail, scanned %d bytes", len(scanner.Bytes()))
	}
	if err := scanner.Err(); !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("expected bufio.ErrTooLong, got %v", err)
	}
}
