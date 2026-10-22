package agent

import (
	"bufio"
	"errors"
	"io"
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

	line := strings.Repeat("x", agentStreamMaxLineBytes+1)
	scanner := newAgentStreamScanner(strings.NewReader(line + "\n"))

	if scanner.Scan() {
		t.Fatalf("expected an over-cap line to fail, scanned %d bytes", len(scanner.Bytes()))
	}
	if err := scanner.Err(); !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("expected bufio.ErrTooLong, got %v", err)
	}
}

// TestReadAgentStreamLineSkipsOversizedAndContinues covers the review on
// #9057: the log-walking companion to newAgentStreamScanner must do what
// the Scanner cannot — discard a record beyond agentStreamMaxLineBytes and
// keep the later records readable.
func TestReadAgentStreamLineSkipsOversizedAndContinues(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("x", agentStreamMaxLineBytes+16)
	r := bufio.NewReaderSize(strings.NewReader(oversized+"\nafter\n"), agentStreamInitialBufferBytes)

	line, err := readAgentStreamLine(r)
	if line != nil || !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("expected ErrTooLong with no line, got %d bytes err=%v", len(line), err)
	}
	line, err = readAgentStreamLine(r)
	if err != nil || string(line) != "after" {
		t.Fatalf("expected the record after the oversized one, got %q err=%v", line, err)
	}
	if _, err := readAgentStreamLine(r); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF after the last record, got %v", err)
	}
}

// TestReadAgentStreamLineReturnsUnterminatedTail: a live log can end
// mid-write; the final unterminated line is still returned, matching
// bufio.Scanner's behavior, and an exhausted reader reports io.EOF.
func TestReadAgentStreamLineReturnsUnterminatedTail(t *testing.T) {
	t.Parallel()

	r := bufio.NewReaderSize(strings.NewReader("first\npartial-tail"), agentStreamInitialBufferBytes)
	if line, err := readAgentStreamLine(r); err != nil || string(line) != "first" {
		t.Fatalf("expected the first line, got %q err=%v", line, err)
	}
	if line, err := readAgentStreamLine(r); err != nil || string(line) != "partial-tail" {
		t.Fatalf("expected the unterminated tail, got %q err=%v", line, err)
	}
	if _, err := readAgentStreamLine(r); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}
