package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type acpTerminalResponseRecorder struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	changed chan struct{}
}

func (r *acpTerminalResponseRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	n, err := r.buffer.Write(p)
	r.mu.Unlock()
	select {
	case r.changed <- struct{}{}:
	default:
	}
	return n, err
}

func (r *acpTerminalResponseRecorder) containsID(id int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Contains(r.buffer.String(), `"id":`+strconv.Itoa(id))
}

func TestACPManagedTerminalRunsAndReturnsOutput(t *testing.T) {
	t.Parallel()

	command := "printf 'hello from terminal'"
	if runtime.GOOS == "windows" {
		command = "echo hello from terminal"
	}
	c := &hermesClient{
		terminalCtx: cxtBackground(),
		terminalCwd: t.TempDir(),
		terminalEnv: os.Environ(),
		terminals:   make(map[string]*acpTerminal),
	}

	created, err := c.acpTerminalCreate(json.RawMessage(`{"command":` + jsonString(command) + `}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id, ok := created["terminalId"].(string)
	if !ok || id == "" {
		t.Fatalf("create result terminalId = %#v", created["terminalId"])
	}

	waited, err := c.acpTerminalResponse("terminal/wait_for_exit", json.RawMessage(`{"terminalId":`+jsonString(id)+`}`))
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if got := waited["exitCode"]; got != float64(0) && got != 0 {
		t.Fatalf("exitCode = %#v, want 0", got)
	}

	output, err := c.acpTerminalResponse("terminal/output", json.RawMessage(`{"terminalId":`+jsonString(id)+`}`))
	if err != nil {
		t.Fatalf("output: %v", err)
	}
	if !strings.Contains(output["output"].(string), "hello from terminal") {
		t.Fatalf("output = %#v", output["output"])
	}
	if output["truncated"] != false {
		t.Fatalf("truncated = %#v, want false", output["truncated"])
	}

	if err := c.acpTerminalRelease(id); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestACPManagedTerminalBoundsOutputToTail(t *testing.T) {
	t.Parallel()

	command := "printf '0123456789'"
	if runtime.GOOS == "windows" {
		command = "echo 0123456789"
	}
	c := &hermesClient{
		terminalCtx: cxtBackground(),
		terminalCwd: t.TempDir(),
		terminalEnv: os.Environ(),
		terminals:   make(map[string]*acpTerminal),
	}
	created, err := c.acpTerminalCreate(json.RawMessage(`{"command":` + jsonString(command) + `,"outputByteLimit":4}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created["terminalId"].(string)
	if _, err := c.acpTerminalResponse("terminal/wait_for_exit", json.RawMessage(`{"terminalId":`+jsonString(id)+`}`)); err != nil {
		t.Fatalf("wait: %v", err)
	}
	output, err := c.acpTerminalResponse("terminal/output", json.RawMessage(`{"terminalId":`+jsonString(id)+`}`))
	if err != nil {
		t.Fatalf("output: %v", err)
	}
	if output["truncated"] != true {
		t.Fatalf("truncated = %#v, want true", output["truncated"])
	}
	if len(output["output"].(string)) != 4 {
		t.Fatalf("output length = %d, want 4", len(output["output"].(string)))
	}
	_ = c.acpTerminalRelease(id)
}

func TestACPManagedTerminalTruncatesAtUTF8Boundary(t *testing.T) {
	t.Parallel()

	terminal := &acpTerminal{limit: 4}
	writer := acpTerminalOutputWriter{terminal: terminal}
	if _, err := writer.Write([]byte("界界")); err != nil {
		t.Fatal(err)
	}
	output, truncated, _ := terminal.snapshot()
	if !truncated {
		t.Fatal("truncated = false, want true")
	}
	if !utf8.ValidString(output) {
		t.Fatalf("output is invalid UTF-8: %x", []byte(output))
	}
	if output != "界" {
		t.Fatalf("output = %q, want one complete trailing rune", output)
	}
}

func TestACPManagedTerminalHidesIncompleteUTF8Suffix(t *testing.T) {
	t.Parallel()

	terminal := &acpTerminal{limit: 10}
	writer := acpTerminalOutputWriter{terminal: terminal}
	encoded := []byte("界")
	if _, err := writer.Write(encoded[:2]); err != nil {
		t.Fatal(err)
	}
	if output, _, _ := terminal.snapshot(); output != "" {
		t.Fatalf("partial-rune output = %q, want empty", output)
	}
	if _, err := writer.Write(encoded[2:]); err != nil {
		t.Fatal(err)
	}
	if output, _, _ := terminal.snapshot(); output != "界" {
		t.Fatalf("completed-rune output = %q, want 界", output)
	}
}

func TestACPManagedTerminalWaitKeepsReaderResponsive(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	closed := false
	defer func() {
		if !closed {
			close(done)
		}
	}()
	zero := uint32(0)
	recorder := &acpTerminalResponseRecorder{changed: make(chan struct{}, 4)}
	c := &hermesClient{
		stdin:           recorder,
		terminalEnabled: true,
		terminals: map[string]*acpTerminal{
			"term": {
				done:       done,
				exitStatus: &acpTerminalExitStatus{exitCode: &zero},
			},
		},
	}

	dispatched := make(chan struct{})
	go func() {
		c.handleLine(`{"jsonrpc":"2.0","id":1,"method":"terminal/wait_for_exit","params":{"sessionId":"s","terminalId":"term"}}`)
		c.handleLine(`{"jsonrpc":"2.0","id":2,"method":"terminal/output","params":{"sessionId":"s","terminalId":"term"}}`)
		close(dispatched)
	}()

	waitForACPResponseID(t, recorder, 2, time.Second)
	close(done)
	closed = true
	waitForACPResponseID(t, recorder, 1, time.Second)
	select {
	case <-dispatched:
	case <-time.After(time.Second):
		t.Fatal("ACP reader did not finish dispatching terminal requests")
	}
}

func waitForACPResponseID(t *testing.T, recorder *acpTerminalResponseRecorder, id int, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for !recorder.containsID(id) {
		select {
		case <-recorder.changed:
		case <-deadline.C:
			t.Fatalf("timeout waiting for ACP response id %d", id)
		}
	}
}

// These tiny helpers keep the test JSON readable without introducing a
// second JSON encoder abstraction into the production ACP transport.
func cxtBackground() context.Context { return context.Background() }

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
