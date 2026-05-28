package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewSupportsGrok(t *testing.T) {
	t.Parallel()

	backend, err := New("grok", Config{})
	if err != nil {
		t.Fatalf("New(grok) returned error: %v", err)
	}
	if _, ok := backend.(*grokBackend); !ok {
		t.Fatalf("New(grok) returned %T, want *grokBackend", backend)
	}
}

func TestBuildGrokArgsBaseline(t *testing.T) {
	t.Parallel()

	args := buildGrokArgs("write a haiku", ExecOptions{}, slog.Default())
	want := []string{
		"-p", "write a haiku",
		"--always-approve",
		"--output-format", "streaming-json",
		"--no-alt-screen",
		"--disable-web-search",
	}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d: %v", len(args), len(want), args)
	}
	for i, w := range want {
		if args[i] != w {
			t.Fatalf("args[%d] = %q, want %q (args=%v)", i, args[i], w, args)
		}
	}
}

func TestBuildGrokArgsWithModelResumeAndMaxTurns(t *testing.T) {
	t.Parallel()

	args := buildGrokArgs("hi", ExecOptions{Model: "grok-code-fast-1", ResumeSessionID: "session-123", MaxTurns: 80}, slog.Default())
	joined := "\x00" + strings.Join(args, "\x00") + "\x00"
	for _, want := range []string{"\x00-m\x00grok-code-fast-1\x00", "\x00-r\x00session-123\x00", "\x00--max-turns\x0080\x00"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %v", want, args)
		}
	}
}

func TestBuildGrokArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildGrokArgs("safe", ExecOptions{CustomArgs: []string{"--output-format", "plain", "--sandbox", "workspace-write"}}, slog.Default())
	for i, a := range args {
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "plain" {
			t.Fatalf("blocked --output-format plain should have been filtered: %v", args)
		}
	}
	if args[len(args)-2] != "--sandbox" || args[len(args)-1] != "workspace-write" {
		t.Fatalf("expected custom sandbox args to pass through at end, got %v", args)
	}
}

func TestGrokBackendParsesStreamingJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	dir := t.TempDir()
	fake := filepath.Join(dir, "grok")
	writeTestExecutable(t, fake, []byte(`#!/bin/sh
printf '%s\n' '{"type":"thought","data":"thinking"}'
printf '%s\n' '{"type":"text","data":"hello"}'
printf '%s\n' '{"type":"end","sessionId":"sess-1","stopReason":"EndTurn"}'
`))

	backend := &grokBackend{cfg: Config{ExecutablePath: fake, Logger: slog.Default()}}
	session, err := backend.Execute(context.Background(), "hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var sawThinking, sawText bool
	for msg := range session.Messages {
		switch msg.Type {
		case MessageThinking:
			sawThinking = msg.Content == "thinking"
		case MessageText:
			sawText = msg.Content == "hello"
		}
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("result status = %q error=%q", result.Status, result.Error)
	}
	if result.Output != "hello" {
		t.Fatalf("result output = %q, want hello", result.Output)
	}
	if result.SessionID != "sess-1" {
		t.Fatalf("result session id = %q, want sess-1", result.SessionID)
	}
	if !sawThinking || !sawText {
		t.Fatalf("expected thinking and text messages, sawThinking=%v sawText=%v", sawThinking, sawText)
	}
}
