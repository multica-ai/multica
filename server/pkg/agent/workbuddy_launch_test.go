package agent

// WorkBuddy ships CodeBuddy's CLI as a bundled Node script rather than a
// native binary, so a task launch has to reach the OS as
// `<node> <cli script> -p ...`: the interpreter and the script are two
// separate argv entries, and WorkBuddy's identity lives in the second one.
// Losing the prefix there turns a WorkBuddy task into a bare `node -p ...` —
// the exact drop the launch-prefix plumbing exists to prevent, and one that
// argv-construction tests cannot see because the loss happens at the exec
// boundary.
//
// The fake interpreter is this test binary re-executed, the same trick
// claude_deadlock_test.go's TestMain uses for its CLI fixtures, so the real
// os/exec path runs on every platform, Windows included.

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	// workbuddyFakeNodeEnv makes the re-executed test binary act as the bundled
	// Node interpreter; workbuddyFakeNodeArgvEnv is where that fake records what
	// it was spawned with, because only the launch boundary can observe it.
	workbuddyFakeNodeEnv     = "MULTICA_FAKE_WORKBUDDY_NODE"
	workbuddyFakeNodeArgvEnv = "MULTICA_FAKE_WORKBUDDY_NODE_ARGV"
)

func TestWorkBuddyTaskLaunchKeepsBundledCliPrefix(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "task-launch-argv.txt")
	// A script, exactly as WorkBuddy stages the CLI beside its private Node
	// build: the daemon spawns the interpreter and passes this path as the first
	// argument.
	cliScript := filepath.Join(dir, "codebuddy-cli.js")
	if err := os.WriteFile(cliScript, []byte("#!/usr/bin/env node\n"), 0o644); err != nil {
		t.Fatalf("write bundled CLI script: %v", err)
	}

	backend, err := ResolveBackend("workbuddy", Config{
		ExecutablePath: self,
		LaunchPrefix:   []string{cliScript},
		Logger:         slog.Default(),
		Env: map[string]string{
			workbuddyFakeNodeEnv:     "1",
			workbuddyFakeNodeArgvEnv: argvPath,
		},
	})
	if err != nil {
		t.Fatalf("ResolveBackend(workbuddy): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "summarize the failing test", ExecOptions{Timeout: 20 * time.Second, Model: "hy3"})
	if err != nil {
		t.Fatalf("execute workbuddy task: %v", err)
	}

	var gotText bool
	for msg := range session.Messages {
		if msg.Type == MessageText && msg.Content == "workbuddy task ok" {
			gotText = true
		}
	}
	result := <-session.Result

	// Assert the launch shape before the outcome: a dropped prefix still
	// produces a Result, just one from `node -p ...` instead of WorkBuddy.
	raw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read recorded launch argv: %v", err)
	}
	argv := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(argv) == 0 || argv[0] != cliScript {
		t.Fatalf("launched argv = %#v, want the bundled CLI script %q as the first argument after the interpreter", argv, cliScript)
	}
	if len(argv) < 2 || argv[1] != "-p" {
		t.Fatalf("launched argv = %#v, want the task prompt flag directly after the CLI script", argv)
	}
	for _, want := range []string{"--output-format", "stream-json", "--input-format", "--permission-mode", "bypassPermissions"} {
		if !slices.Contains(argv, want) {
			t.Fatalf("launched argv = %#v, want CodeBuddy's headless stream-json flags (missing %q)", argv, want)
		}
	}

	if result.Status != "completed" {
		t.Fatalf("task status = %q (error=%q), want completed", result.Status, result.Error)
	}
	if !gotText {
		t.Fatal("expected the assistant text message from the fake bundled CLI")
	}
	if result.SessionID != "sess-wb-task" {
		t.Fatalf("session id = %q, want sess-wb-task", result.SessionID)
	}
}

// runFakeWorkBuddyNode plays the bundled Node interpreter: it records the argv
// the backend actually spawned, answers the version probe runtime registration
// uses, and replays the stream-json a completed CodeBuddy turn emits. Dispatch
// lives in TestMain because the backend launches it with the CLI's own flags,
// which the testing package would otherwise try to parse.
func runFakeWorkBuddyNode() {
	if path := os.Getenv(workbuddyFakeNodeArgvEnv); path != "" {
		if err := os.WriteFile(path, []byte(strings.Join(os.Args[1:], "\n")), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "record argv: %v\n", err)
			os.Exit(31)
		}
	}
	// os.Args[1] is the CLI script (the WorkBuddy launch prefix) and os.Args[2]
	// the flag the interpreter was invoked with.
	if len(os.Args) > 2 && os.Args[2] == "--version" {
		fmt.Println("22.10.0")
		return
	}
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		fmt.Fprintf(os.Stderr, "read prompt: %v\n", err)
		os.Exit(32)
	}
	fmt.Println(`{"type":"system","session_id":"sess-wb-task"}`)
	fmt.Println(`{"type":"assistant","message":{"role":"assistant","model":"hy3","content":[{"type":"text","text":"workbuddy task ok"}]}}`)
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-wb-task","result":"workbuddy task ok","modelUsage":{"hy3":{"inputTokens":10,"outputTokens":2}}}`)
}
