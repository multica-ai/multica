package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestHelperProcess is a fake adapter. It runs only when re-executed by
// helperCommand, never as a normal test and never as a real Claude CLI.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("CLAUDE_ADAPTER_HELPER") != "1" {
		return
	}
	mode := os.Getenv("CLAUDE_ADAPTER_HELPER_MODE")
	write := func(v any) {
		data, _ := json.Marshal(v)
		fmt.Fprintln(os.Stdout, string(data))
	}
	if mode == "never-ready" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	write(map[string]any{"version": 1, "type": "ready", "sdk_version": "0.3.310"})
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &req)
		switch req["type"] {
		case "send":
			write(map[string]any{"version": 1, "type": "sdk_message", "query_generation": 1, "message": map[string]any{"type": "assistant"}})
			write(map[string]any{"version": 1, "type": "result", "id": req["id"]})
		case "crash":
			os.Exit(7)
		case "close":
			if mode == "ignore-close" {
				continue
			}
			write(map[string]any{"version": 1, "type": "result", "id": req["id"]})
			os.Exit(0)
		}
	}
	os.Exit(0)
}

func helperCommand(mode string) Command {
	return Command{
		Path: os.Args[0],
		Args: []string{"-test.run=^TestHelperProcess$"},
		Env:  append(os.Environ(), "CLAUDE_ADAPTER_HELPER=1", "CLAUDE_ADAPTER_HELPER_MODE="+mode),
	}
}

func TestAdapterCommandRunsElectronAsNodeWithoutDaemonElectronEnv(t *testing.T) {
	t.Parallel()

	cmd := AdapterCommand("/Applications/Multica.app/Contents/MacOS/Multica", "/res/claude-host.js", []string{"PATH=/usr/bin", "ELECTRON_ENABLE_LOGGING=1"})
	if cmd.Path != "/Applications/Multica.app/Contents/MacOS/Multica" || len(cmd.Args) != 1 || cmd.Args[0] != "/res/claude-host.js" {
		t.Fatalf("command = %+v", cmd)
	}
	want := []string{"ELECTRON_RUN_AS_NODE=1", "PATH=/usr/bin"}
	if fmt.Sprint(cmd.Env) != fmt.Sprint(want) {
		t.Fatalf("env = %v, want %v", cmd.Env, want)
	}
}

func TestProcessStartsRequestsAndCloses(t *testing.T) {
	t.Parallel()

	got := &outputs{}
	p, err := Start(context.Background(), helperCommand(""), got.add, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if p.Ready.SDKVersion != "0.3.310" {
		t.Fatalf("ready = %+v", p.Ready)
	}
	if _, err := p.Request(context.Background(), "send", map[string]any{"request_id": "r", "content": []any{}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if outs := got.wait(t, 1); outs[0].Type != "sdk_message" {
		t.Fatalf("output = %+v", outs[0])
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-p.Exited():
	case <-time.After(5 * time.Second):
		t.Fatal("adapter did not exit")
	}
}

func TestProcessReportsHowLongItsStartTook(t *testing.T) {
	t.Parallel()

	p, err := Start(context.Background(), helperCommand(""), func(Output) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	if spawn, ready := p.StartDurations(); spawn <= 0 || ready <= 0 {
		t.Fatalf("spawn = %v, ready = %v", spawn, ready)
	}
}

func TestParseStartTiming(t *testing.T) {
	t.Parallel()

	got := ParseStartTiming(json.RawMessage(`{"models":[],"timing":{"sdk_load_ms":1.5,"query_init_ms":2}}`))
	if got.SDKLoad == nil || *got.SDKLoad != 1500*time.Microsecond || got.QueryInit == nil || *got.QueryInit != 2*time.Millisecond {
		t.Fatalf("got %+v", got)
	}
	for _, raw := range []string{`{"models":[]}`, `{}`, `not json`, `{"timing":{"sdk_load_ms":"soon","query_init_ms":2}}`, `{"timing":{"sdk_load_ms":-1}}`} {
		if got := ParseStartTiming(json.RawMessage(raw)); got.SDKLoad != nil {
			t.Fatalf("%s: got %+v", raw, got)
		}
	}
}

func TestProcessExitFailsPendingRequests(t *testing.T) {
	t.Parallel()

	p, err := Start(context.Background(), helperCommand(""), func(Output) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Request(context.Background(), "crash", nil)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	<-p.Exited()
	if p.ExitErr() == nil {
		t.Fatal("exit status not recorded")
	}
}

func TestProcessCloseKillsAnAdapterThatIgnoresClose(t *testing.T) {
	t.Parallel()

	p, err := Start(context.Background(), helperCommand("ignore-close"), func(Output) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = p.Close(ctx)
	select {
	case <-p.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("adapter still running after Close")
	}
}

func TestStartFailsWhenAdapterNeverBecomesReady(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := Start(ctx, helperCommand("never-ready"), func(Output) {}, nil); err == nil {
		t.Fatal("Start succeeded without ready")
	}
}
