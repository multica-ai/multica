//go:build !windows

package terminal

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func helperCommand(t *testing.T, mode string) Command {
	t.Helper()
	return Command{
		Path: os.Args[0],
		Args: []string{"-test.run=TestPTYHelperProcess", "--", mode},
		Dir:  t.TempDir(),
		Env:  append(os.Environ(), "GO_WANT_PTY_HELPER=1"),
	}
}

func TestPTYHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_PTY_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "echo":
		var line string
		_, _ = os.Stdout.WriteString("ready\n")
		_, _ = fmtFscanln(os.Stdin, &line)
		_, _ = os.Stdout.WriteString("echo:" + line + "\n")
	case "burst":
		for i := 0; i < 20; i++ {
			_, _ = os.Stdout.WriteString(strings.Repeat("x", 128))
			time.Sleep(2 * time.Millisecond)
		}
	case "sleep":
		for {
			time.Sleep(time.Second)
		}
	}
	os.Exit(0)
}

func fmtFscanln(file *os.File, value *string) (int, error) {
	buf := make([]byte, 128)
	n, err := file.Read(buf)
	if n > 0 {
		*value = strings.TrimSpace(string(buf[:n]))
		return 1, nil
	}
	return 0, err
}

func TestManagerInputReplayResizeAndExit(t *testing.T) {
	events := make(chan Event, 32)
	m := NewManager(Options{RingBytes: 1024, OnEvent: func(event Event) { events <- event }})
	s, err := m.Start(context.Background(), StartRequest{
		SessionID: uuid.New(), TaskID: "task", WorkspaceID: "workspace", RuntimeID: "runtime", Provider: "fake", Command: helperCommand(t, "echo"),
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, events, "ready")
	if err := s.Resize(1, 999); err != nil {
		t.Fatal(err)
	}
	meta := s.Metadata()
	if meta.Cols != MinCols || meta.Rows != MaxRows {
		t.Fatalf("clamped size = %dx%d", meta.Cols, meta.Rows)
	}
	if err := s.WriteInput(7, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteInput(7, []byte("duplicate\n")); !errors.Is(err, ErrDuplicateInput) {
		t.Fatalf("duplicate input error = %v", err)
	}
	waitForOutput(t, events, "echo:hello")
	exit := s.Wait()
	if exit.Code != 0 {
		t.Fatalf("exit = %#v", exit)
	}
	s.SetProviderSessionID("019fe469-33bc-75c2-9492-ca640a1788a4")
	if got := s.Metadata().ProviderSessionID; got != "019fe469-33bc-75c2-9492-ca640a1788a4" {
		t.Fatalf("provider session id = %q", got)
	}
	foundProviderSession := false
	for len(events) > 0 {
		event := <-events
		if event.Type == "state" && event.ProviderSessionID == "019fe469-33bc-75c2-9492-ca640a1788a4" && event.Status == "exited" {
			foundProviderSession = true
		}
	}
	if !foundProviderSession {
		t.Fatal("provider session state event was not emitted")
	}
	replay := s.Replay(0)
	if replay.LatestSeq == 0 || len(replay.Chunks) == 0 {
		t.Fatalf("replay = %#v", replay)
	}
}

func TestManagerStartEmitsSessionRegistrationBeforeOutput(t *testing.T) {
	events := make(chan Event, 32)
	m := NewManager(Options{OnEvent: func(event Event) { events <- event }})
	sessionID := uuid.New()
	s, err := m.Start(context.Background(), StartRequest{
		SessionID: sessionID, TaskID: "task", WorkspaceID: "workspace", RuntimeID: "runtime", Provider: "fake", Command: helperCommand(t, "sleep"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	select {
	case event := <-events:
		if event.Type != "session" || event.SessionID != sessionID || event.Status != "running" {
			t.Fatalf("first event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for session registration")
	}
}

func TestManagerStopTerminatesProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY is unsupported")
	}
	m := NewManager(Options{StopGrace: 20 * time.Millisecond})
	s, err := m.Start(context.Background(), StartRequest{SessionID: uuid.New(), TaskID: "task", WorkspaceID: "workspace", RuntimeID: "runtime", Provider: "fake", Command: helperCommand(t, "sleep")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("process was not reaped")
	}
}

func TestManagerReplayReportsGapAfterRingOverwrite(t *testing.T) {
	events := make(chan Event, 32)
	m := NewManager(Options{RingBytes: 64, OnEvent: func(event Event) { events <- event }})
	s, err := m.Start(context.Background(), StartRequest{SessionID: uuid.New(), TaskID: "task", WorkspaceID: "workspace", RuntimeID: "runtime", Provider: "fake", Command: helperCommand(t, "burst")})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Wait()
	replay := s.Replay(0)
	if !replay.Gap || replay.OldestSeq <= 1 || replay.LatestSeq < replay.OldestSeq {
		t.Fatalf("replay gap = %#v", replay)
	}
}

func waitForOutput(t *testing.T, events <-chan Event, substring string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == "output" && strings.Contains(string(event.Payload), substring) {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q", substring)
		}
	}
}
