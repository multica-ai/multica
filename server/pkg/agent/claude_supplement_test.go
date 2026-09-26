package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func (s *claudeSupplementSession) handleHook(msg claudeSDKMessage, w io.Writer) (bool, error) {
	reply, handled := s.prepareHook(msg)
	if !handled {
		return false, nil
	}
	return true, reply(w)
}

func claudeSupplementHook(event, agentID string) claudeSDKMessage {
	request, _ := json.Marshal(map[string]any{
		"subtype": "hook_callback", "callback_id": "multica-supplement-" + event,
		"input": map[string]any{"hook_event_name": event, "agent_id": agentID},
	})
	return claudeSDKMessage{Type: "control_request", RequestID: "hook-request", Request: request}
}

func activeClaudeSupplementSession(t *testing.T) *claudeSupplementSession {
	t.Helper()
	s := newClaudeSupplementSession(t.Context())
	if s.ready() {
		t.Fatal("supplements ready before provider hook")
	}
	if _, err := s.handleHook(claudeSupplementHook("UserPromptSubmit", ""), io.Discard); err != nil {
		t.Fatal(err)
	}
	if !s.ready() {
		t.Fatal("provider hook did not enable supplements")
	}
	t.Cleanup(s.end)
	return s
}

func queueClaudeSupplement(t *testing.T, s *claudeSupplementSession, ctx context.Context, text string, count int) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.supplement(ctx, text) }()
	deadline := time.After(3 * time.Second)
	for {
		s.mu.Lock()
		queued := len(s.pending)
		s.mu.Unlock()
		if queued == count {
			return done
		}
		select {
		case err := <-done:
			t.Fatalf("supplement returned before provider accepted it: %v", err)
		case <-deadline:
			t.Fatal("supplement was not queued")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestClaudeSupplementDeliveredAtProviderBoundary(t *testing.T) {
	for _, event := range claudeSupplementEvents {
		t.Run(event, func(t *testing.T) {
			s := activeClaudeSupplementSession(t)
			first := queueClaudeSupplement(t, s, t.Context(), "Keep the original goal; add A.", 1)
			second := queueClaudeSupplement(t, s, t.Context(), "Also add B.", 2)
			select {
			case <-first:
				t.Fatal("local queueing must not acknowledge delivery")
			default:
			}
			var wire bytes.Buffer
			if handled, err := s.handleHook(claudeSupplementHook(event, ""), &wire); !handled || err != nil {
				t.Fatalf("hook: handled=%v err=%v", handled, err)
			}
			for _, done := range []<-chan error{first, second} {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			var frame struct {
				Type     string `json:"type"`
				Response struct {
					RequestID string `json:"request_id"`
					Response  struct {
						Decision string `json:"decision"`
						Reason   string `json:"reason"`
						Hook     struct {
							Context string `json:"additionalContext"`
						} `json:"hookSpecificOutput"`
					} `json:"response"`
				} `json:"response"`
			}
			if err := json.Unmarshal(wire.Bytes(), &frame); err != nil {
				t.Fatal(err)
			}
			if frame.Type != "control_response" || frame.Response.RequestID != "hook-request" {
				t.Fatalf("must reply to the provider hook, not send a new prompt: %s", wire.Bytes())
			}
			text := frame.Response.Response.Hook.Context
			if event == "Stop" {
				if frame.Response.Response.Decision != "block" {
					t.Fatal("pending input must keep the current loop running")
				}
				text = frame.Response.Response.Reason
			}
			if text != "Keep the original goal; add A.\n\nAlso add B." {
				t.Fatalf("delivered instructions = %q", text)
			}
			if !s.ready() {
				t.Fatal("delivery must allow subsequent supplements")
			}
		})
	}
}

func TestClaudeSupplementRejectsAfterStopAndEnd(t *testing.T) {
	s := activeClaudeSupplementSession(t)
	_, _ = s.handleHook(claudeSupplementHook("Stop", ""), io.Discard)
	if s.ready() {
		t.Fatal("empty Stop must close admission before result arrives")
	}
	if err := s.supplement(t.Context(), "too late"); err == nil {
		t.Fatal("supplement accepted after Stop")
	}
	s.end()
	_, _ = s.handleHook(claudeSupplementHook("PreToolUse", ""), io.Discard)
	if s.ready() {
		t.Fatal("late events reopened a completed run")
	}
}

func TestClaudeSupplementRacingStopNeverStartsAnotherTurn(t *testing.T) {
	for i := 0; i < 100; i++ {
		s := activeClaudeSupplementSession(t)
		start := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			<-start
			done <- s.supplement(t.Context(), "racing input")
		}()
		var wire bytes.Buffer
		close(start)
		_, err := s.handleHook(claudeSupplementHook("Stop", ""), &wire)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if strings.Contains(wire.String(), "racing input") {
				if err != nil || !strings.Contains(wire.String(), `"decision":"block"`) {
					t.Fatalf("accepted input must continue this loop: err=%v response=%s", err, wire.Bytes())
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("Stop won but input was not rejected: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Stop left supplement waiting")
		}
		s.end()
	}
}

func TestClaudeSupplementDoesNotReachSubagent(t *testing.T) {
	s := activeClaudeSupplementSession(t)
	done := queueClaudeSupplement(t, s, t.Context(), "main task only", 1)
	for _, event := range []string{"PreToolUse", "Stop"} {
		var wire bytes.Buffer
		_, _ = s.handleHook(claudeSupplementHook(event, "child-agent"), &wire)
		if strings.Contains(wire.String(), "main task only") {
			t.Fatal("subagent consumed main task supplement")
		}
	}
	select {
	case <-done:
		t.Fatal("subagent acknowledged main task supplement")
	default:
	}
	if !s.ready() {
		t.Fatal("subagent Stop closed the main run")
	}
	_, _ = s.handleHook(claudeSupplementHook("Stop", ""), io.Discard)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestClaudeSupplementCancellationCannotDeliverLater(t *testing.T) {
	s := activeClaudeSupplementSession(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := queueClaudeSupplement(t, s, ctx, "cancelled input", 1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	var wire bytes.Buffer
	_, _ = s.handleHook(claudeSupplementHook("Stop", ""), &wire)
	if strings.Contains(wire.String(), "cancelled input") || strings.Contains(wire.String(), "block") {
		t.Fatalf("cancelled input was delivered: %s", wire.Bytes())
	}
}

func TestClaudeSupplementRunEndReleasesPending(t *testing.T) {
	s := activeClaudeSupplementSession(t)
	done := queueClaudeSupplement(t, s, t.Context(), "pending input", 1)
	s.end()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run end: %v", err)
	}
}

type failedClaudeInput struct{}

func (failedClaudeInput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestClaudeSupplementWriteFailureIsNotDelivery(t *testing.T) {
	s := activeClaudeSupplementSession(t)
	done := queueClaudeSupplement(t, s, t.Context(), "input", 1)
	_, err := s.handleHook(claudeSupplementHook("PostToolUse", ""), failedClaudeInput{})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write: %v", err)
	}
	if err := <-done; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("delivery: %v", err)
	}
}

// This fake CLI implements the official SDK initialize/hook_callback exchange.
// It accepts exactly one user prompt; every later input must reply to a hook.
func runFakeClaudeSupplement() {
	reader := bufio.NewReader(os.Stdin)
	read := func() map[string]any {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(10)
		}
		var frame map[string]any
		if json.Unmarshal(line, &frame) != nil {
			os.Exit(11)
		}
		return frame
	}
	init := read()
	if init["type"] != "control_request" {
		os.Exit(12)
	}
	request := init["request"].(map[string]any)
	hooks := request["hooks"].(map[string]any)
	for _, event := range []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "Stop"} {
		if hooks[event] == nil {
			os.Exit(13)
		}
	}
	if failure := os.Getenv("CLAUDE_SUPPLEMENT_INIT_FAILURE"); failure != "" {
		if failure == "reject" {
			_ = writeClaudeFrame(os.Stdout, map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "error", "request_id": init["request_id"], "error": "hooks unsupported",
			}})
		}
		// Unsupported hooks disable supplements, but the original goal still runs.
		if prompt := read(); prompt["type"] != "user" {
			os.Exit(19)
		}
		fmt.Println(`{"type":"result","subtype":"success","result":"original goal completed"}`)
		if _, err := reader.ReadBytes('\n'); err != io.EOF {
			os.Exit(19)
		}
		return
	}
	_ = writeClaudeFrame(os.Stdout, map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": init["request_id"], "response": map[string]any{}}})
	prompt := read()
	if prompt["type"] != "user" {
		os.Exit(14)
	}
	hook := func(event string) map[string]any {
		_ = json.NewEncoder(os.Stdout).Encode(claudeSupplementHook(event, ""))
		if event == "PostToolUse" {
			// Fill stdout before reading a hook reply larger than stdin's pipe
			// buffer. A synchronous reply on the stdout reader deadlocks here.
			for range 8 {
				_ = writeClaudeFrame(os.Stdout, map[string]any{"type": "system", "subtype": "backpressure", "padding": strings.Repeat("x", 32*1024)})
			}
		}
		frame := read()
		if frame["type"] != "control_response" {
			os.Exit(15)
		}
		return frame["response"].(map[string]any)["response"].(map[string]any)
	}
	hook("UserPromptSubmit")
	fmt.Println(`{"type":"system","subtype":"init","session_id":"claude-supplement"}`)
	for {
		out := hook("PostToolUse")
		if specific, ok := out["hookSpecificOutput"].(map[string]any); ok {
			if specific["additionalContext"] != strings.Repeat("context ", 8192) {
				os.Exit(16)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if out := hook("Stop"); len(out) != 0 {
		os.Exit(17)
	}
	fmt.Println(`{"type":"result","subtype":"success","session_id":"claude-supplement","result":"original goal and tests completed"}`)
	if _, err := reader.ReadBytes('\n'); err != io.EOF {
		os.Exit(18)
	}
}

func TestClaudeSupplementExecuteUsesSamePromptUnderBackpressure(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b := &claudeBackend{cfg: Config{ExecutablePath: self, Logger: slog.Default(), Env: map[string]string{"CLAUDE_FAKE_MODE": "supplement", "IS_SANDBOX": "1"}}}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "original goal", ExecOptions{EnableTaskSupplement: true})
	if err != nil {
		t.Fatal(err)
	}
	if session.Supplement == nil || session.SupplementReady == nil {
		t.Fatal("Claude supplement interface missing")
	}
	for !session.SupplementReady() {
		select {
		case result := <-session.Result:
			t.Fatalf("ended before ready: %+v", result)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if err := session.Supplement(ctx, strings.Repeat("context ", 8192)); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result:
		if result.Status != "completed" || result.Output != "original goal and tests completed" {
			t.Fatalf("result: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if session.SupplementReady() {
		t.Fatal("completed process still accepts supplements")
	}
	if err := session.Supplement(ctx, "late input"); err == nil {
		t.Fatal("late input accepted")
	}
}

func TestClaudeSupplementInitializeFailure(t *testing.T) {
	for _, failure := range []string{"reject", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			b := &claudeBackend{cfg: Config{ExecutablePath: self, Logger: slog.Default(), Env: map[string]string{
				"CLAUDE_FAKE_MODE": "supplement", "CLAUDE_SUPPLEMENT_INIT_FAILURE": failure, "IS_SANDBOX": "1",
			}}}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			session, err := b.Execute(ctx, "original goal", ExecOptions{EnableTaskSupplement: true, HandshakeTimeout: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-session.Result:
				if result.Status != "completed" || result.Output != "original goal completed" {
					t.Fatalf("handshake failure must preserve ordinary execution: %+v", result)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if session.SupplementReady() {
				t.Fatal("failed handshake enabled supplements")
			}
		})
	}
}
