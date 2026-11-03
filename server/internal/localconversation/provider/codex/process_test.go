package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/codexappserver"
)

// TestHelperAppServer is a fake app-server. It runs only when re-executed by
// helperCommand, never as a normal test and never as a real Codex CLI.
func TestHelperAppServer(t *testing.T) {
	if os.Getenv("CODEX_APP_SERVER_HELPER") != "1" {
		return
	}
	write := func(v any) {
		data, _ := json.Marshal(v)
		fmt.Fprintln(os.Stdout, string(data))
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &msg)
		switch msg["method"] {
		case "initialize":
			write(map[string]any{"id": msg["id"], "result": map[string]any{"userAgent": "fake"}})
		case "initialized":
			write(map[string]any{"method": "thread/started", "params": map[string]any{"threadId": "th-1"}})
			write(map[string]any{"id": 99, "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": "th-1"}})
		case "crash":
			os.Exit(3)
		default:
			write(map[string]any{"id": msg["id"], "result": map[string]any{"echo": msg["method"]}})
		}
	}
	os.Exit(0)
}

func helperCommand() Command {
	// The app-server arguments follow "--", so the test binary ignores them.
	return Command{Path: os.Args[0], Env: append(os.Environ(), "CODEX_APP_SERVER_HELPER=1")}
}

type frames struct {
	mu  sync.Mutex
	got []codexappserver.Frame
}

func (f *frames) add(frame codexappserver.Frame) {
	f.mu.Lock()
	f.got = append(f.got, frame)
	f.mu.Unlock()
}

func (f *frames) wait(t *testing.T, n int) []codexappserver.Frame {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if len(f.got) >= n {
			out := append([]codexappserver.Frame(nil), f.got...)
			f.mu.Unlock()
			return out
		}
		f.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("got fewer than %d frames", n)
	return nil
}

func startHelper(t *testing.T, got *frames) *Server {
	t.Helper()
	s, err := startWithArgs(context.Background(), helperCommand(), []string{"-test.run=^TestHelperAppServer$", "--"}, got.add, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return s
}

func TestServerHandshakeRequestsAndNotifications(t *testing.T) {
	t.Parallel()

	got := &frames{}
	s := startHelper(t, got)
	defer s.Stop()
	raw, err := s.Request(context.Background(), "config/read", map[string]any{"cwd": "/repo"})
	if err != nil || string(raw) != `{"echo":"config/read"}` {
		t.Fatalf("config/read = %s, %v", raw, err)
	}
	frames := got.wait(t, 2)
	if frames[0].Kind != codexappserver.FrameNotification || frames[0].Method != "thread/started" {
		t.Fatalf("first frame = %+v", frames[0])
	}
	if frames[1].Kind != codexappserver.FrameServerRequest || frames[1].ID != 99 {
		t.Fatalf("second frame = %+v", frames[1])
	}
}

func TestServerReportsHowLongItsStartTook(t *testing.T) {
	t.Parallel()

	s := startHelper(t, &frames{})
	defer s.Stop()
	if spawn, handshake := s.StartDurations(); spawn <= 0 || handshake <= 0 {
		t.Fatalf("spawn = %v, handshake = %v", spawn, handshake)
	}
}

func TestServerExitFailsPendingRequests(t *testing.T) {
	t.Parallel()

	s := startHelper(t, &frames{})
	if _, err := s.Request(context.Background(), "crash", nil); !IsClosed(err) {
		t.Fatalf("request across exit = %v", err)
	}
	select {
	case <-s.Exited():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("connection not closed")
	}
}
