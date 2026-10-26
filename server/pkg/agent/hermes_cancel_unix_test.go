//go:build unix

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This is a deterministic fake ACP peer, not an installed Hermes integration.
// The test-created executable re-execs this helper with an empty disposable home.
func TestHermesCancelFakePeer(t *testing.T) {
	if os.Getenv("MULTICA_HERMES_CANCEL_FAKE") != "1" {
		t.Skip("fake ACP subprocess only")
	}
	log, err := os.OpenFile(os.Getenv("HERMES_CANCEL_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	if err := os.WriteFile(os.Getenv("HERMES_CANCEL_LOG")+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	record := func(s string) { _, _ = fmt.Fprintln(log, s) }
	send := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	mode := os.Getenv("HERMES_CANCEL_MODE")
	var promptID json.RawMessage
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		line := scanner.Text()
		record(line)
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatal(err)
		}
		respond := func(result any) {
			send(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": result})
		}
		switch frame.Method {
		case "initialize":
			if mode == "setup" {
				continue
			}
			respond(map[string]any{"protocolVersion": 1})
			if mode == "blocked_setup" {
				record(`{"fake":"not_reading"}`)
				time.Sleep(30 * time.Second)
			}
		case "session/new":
			if mode == "waiting_refresh" || mode == "blocked_refresh" {
				respond(map[string]any{
					"sessionId": "ses_cancel_target",
					"configOptions": []map[string]any{{
						"id": "model", "currentValue": "initial",
						"options": []map[string]any{{"value": "initial"}, {"value": "other-model"}},
					}},
				})
			} else {
				respond(map[string]any{"sessionId": "ses_cancel_target"})
			}
			if mode == "blocked_prompt" {
				record(`{"fake":"not_reading"}`)
				time.Sleep(30 * time.Second)
			}
		case "session/set_model":
			respond(map[string]any{})
			if mode == "blocked_refresh" {
				record(`{"fake":"not_reading_refresh"}`)
				time.Sleep(30 * time.Second)
			}
		case "session/prompt":
			promptID = frame.ID
			if mode == "permission_normal" {
				sendHermesCancelPermission(send)
			} else if mode == "normal" || mode == "drain" {
				respond(map[string]any{"stopReason": "end_turn"})
				// A notification follows the response so cancelling on this marker
				// exercises post-response drain, not the active prompt.
				send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "ses_cancel_target", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "response done"}}}})
			} else if mode == "exit" {
				os.Exit(0)
			} else if mode == "blocked_reply" {
				// The reply echoes this unknown method in its error message. Its
				// size fills stdin while we deliberately stop reading that pipe.
				send(map[string]any{"jsonrpc": "2.0", "id": 999, "method": strings.Repeat("x", 1<<20)})
				record(`{"fake":"not_reading"}`)
				time.Sleep(30 * time.Second)
			}
		case "session/cancel":
			if mode == "permission_cancel" {
				// Ask only after the caller/timeout cancellation reached the wire,
				// while the original prompt is still pending in the grace period.
				time.Sleep(150 * time.Millisecond)
				sendHermesCancelPermission(send)
			} else if mode != "ignore" {
				send(map[string]any{"jsonrpc": "2.0", "id": promptID, "result": map[string]any{"stopReason": "cancelled", "usage": map[string]any{"inputTokens": 7, "outputTokens": 3}}})
			}
		default:
			if string(frame.ID) == "9001" {
				var reply struct {
					Result struct {
						Outcome struct {
							Outcome  string `json:"outcome"`
							OptionID string `json:"optionId"`
						} `json:"outcome"`
					} `json:"result"`
				}
				if err := json.Unmarshal([]byte(line), &reply); err != nil {
					t.Fatal(err)
				}
				outcome := reply.Result.Outcome
				if outcome.Outcome == "selected" && (outcome.OptionID == "allow_once" || outcome.OptionID == "allow_always") {
					if err := os.WriteFile(os.Getenv("HERMES_CANCEL_LOG")+".effect", []byte("permission granted"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				stopReason := "cancelled"
				if mode == "permission_normal" {
					stopReason = "end_turn"
				}
				send(map[string]any{"jsonrpc": "2.0", "id": promptID, "result": map[string]any{"stopReason": stopReason, "usage": map[string]any{"inputTokens": 7, "outputTokens": 3}}})
			}
		}
	}
	record("EOF")
	if mode == "normal" || mode == "drain" {
		time.Sleep(50 * time.Millisecond)
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "ses_cancel_target", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": " late final"}}}})
	}
	if mode == "ignore" {
		time.Sleep(30 * time.Second)
	}
	// Avoid the Go test runner's PASS line entering the ACP stream.
	os.Exit(0)
}

func startHermesCancelFake(t *testing.T, ctx context.Context, mode string, timeout time.Duration) (*Session, string) {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	hermesHome := filepath.Join(dir, "hermes-home")
	for _, path := range []string{home, hermesHome} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fakePath := filepath.Join(dir, "fake-hermes")
	writeTestExecutable(t, fakePath, []byte(fmt.Sprintf("#!/bin/sh\nexec %q -test.run='^TestHermesCancelFakePeer$'\n", testBinary)))
	logPath := filepath.Join(dir, "peer.jsonl")
	backend, err := New("hermes", Config{ExecutablePath: fakePath, BuiltinRuntime: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Env: map[string]string{
		"HOME": home, "HERMES_HOME": hermesHome, "MULTICA_HERMES_CANCEL_FAKE": "1", "HERMES_CANCEL_LOG": logPath, "HERMES_CANCEL_MODE": mode,
	}})
	if err != nil {
		t.Fatal(err)
	}
	prompt := "test prompt"
	opts := ExecOptions{Cwd: dir, Timeout: timeout}
	if mode == "waiting_refresh" || mode == "blocked_refresh" {
		opts.Model = "other-model"
		opts.ThinkingLevel = "high"
	}
	if mode == "blocked_refresh" {
		opts.Model = strings.Repeat("m", 1<<20)
	}
	if mode == "blocked_setup" {
		opts.Model = strings.Repeat("m", 1<<20)
	}
	if mode == "blocked_prompt" {
		prompt = strings.Repeat("p", 1<<20)
	}
	session, err := backend.Execute(ctx, prompt, opts)
	if err != nil {
		t.Fatal(err)
	}
	if session.Supplement != nil || session.SupplementReady != nil {
		t.Fatal("Hermes unexpectedly supports supplementary prompts")
	}
	t.Cleanup(func() {
		raw, err := os.ReadFile(logPath + ".pid")
		if err != nil {
			t.Error(err)
			return
		}
		pid, err := strconv.Atoi(string(raw))
		if err != nil {
			t.Error(err)
			return
		}
		if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
			t.Errorf("fake viewer pid %d was not reaped: %v", pid, err)
		}
		var stacks strings.Builder
		_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
		if strings.Contains(stacks.String(), "requestHermesTurn.func") {
			t.Errorf("Hermes request/cancel goroutine leaked:\n%s", stacks.String())
		}
	})
	return session, logPath
}

func waitHermesCancelLog(t *testing.T, path, marker string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), marker) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("fake ACP peer did not record %q", marker)
}

func finishHermesCancelFake(t *testing.T, session *Session) Result {
	t.Helper()
	deadline := time.After(7 * time.Second)
	for {
		select {
		case _, ok := <-session.Messages:
			if !ok {
				select {
				case result, ok := <-session.Result:
					if !ok {
						t.Fatal("missing result")
					}
					return result
				case <-deadline:
					t.Fatal("result did not close")
				}
			}
		case <-deadline:
			t.Fatal("unbounded adapter cleanup")
		}
	}
}

func assertHermesCancelWire(t *testing.T, path string, wantCancel, requireEOF bool) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	count, cancelAt, eofAt := 0, -1, -1
	for i, line := range lines {
		if line == "EOF" {
			eofAt = i
			continue
		}
		var frame map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatal(err)
		}
		if string(frame["method"]) != `"session/cancel"` {
			continue
		}
		count++
		cancelAt = i
		if _, ok := frame["id"]; ok {
			t.Error("session/cancel must be a no-id notification")
		}
		var params struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(frame["params"], &params)
		if params.SessionID != "ses_cancel_target" {
			t.Errorf("cross-session cancel: %q", params.SessionID)
		}
	}
	display := strings.ReplaceAll(string(raw), strings.Repeat("m", 1<<20), "<1 MiB model>")
	t.Logf("deterministic fake ACP peer log (large model values abbreviated):\n%s", display)
	if wantCancel {
		if count != 1 || (requireEOF && (eofAt < 0 || cancelAt >= eofAt)) {
			t.Fatalf("want exactly one same-session cancel BEFORE EOF, got count=%d cancelAt=%d eofAt=%d; fake peer log:\n%s", count, cancelAt, eofAt, raw)
		}
	} else if count != 0 {
		t.Fatalf("unexpected cancellation outside active prompt:\n%s", raw)
	}
}

func TestHermesActivePromptCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, mode, status string
		timeout            time.Duration
	}{
		{"parent", "active", "aborted", 0},
		{"timeout", "active", "timeout", 1500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, logPath := startHermesCancelFake(t, ctx, tc.mode, tc.timeout)
			waitHermesCancelLog(t, logPath, `"method":"session/prompt"`)
			if tc.timeout == 0 {
				cancel()
			}
			result := finishHermesCancelFake(t, session)
			if result.Status != tc.status || result.SessionID != "ses_cancel_target" {
				t.Errorf("result = %+v", result)
			}
			if usage := result.Usage["unknown"]; usage.InputTokens != 7 || usage.OutputTokens != 3 {
				t.Errorf("lost cancellation response usage: %+v", result.Usage)
			}
			assertHermesCancelWire(t, logPath, true, true)
		})
	}
}

func TestHermesBlockedTransportCancellation(t *testing.T) {
	for _, mode := range []string{"blocked_setup", "blocked_prompt", "blocked_reply"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, logPath := startHermesCancelFake(t, ctx, mode, 0)
			waitHermesCancelLog(t, logPath, `"fake":"not_reading"`)
			deadline := time.Now().Add(5 * time.Second)
			for {
				var stacks strings.Builder
				_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
				if strings.Contains(stacks.String(), "(*hermesClient).writeLine") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("fixture never blocked the transport writer")
				}
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
			result := finishHermesCancelFake(t, session)
			if mode != "blocked_setup" && result.Status != "aborted" {
				t.Errorf("result = %+v", result)
			}
			// An unread/full pipe cannot promise delivery; it must still close
			// and join both the blocked writer and the cancel sender in bounds.
			assertHermesCancelWire(t, logPath, false, false)
		})
	}
}

// A direct c.request callback can observe cancellation while waiting for a
// response, but cannot interrupt a full pipe while writing the model refresh.
func TestHermesCancellationDuringModelOptionRefresh(t *testing.T) {
	for _, mode := range []string{"waiting_refresh", "blocked_refresh"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, logPath := startHermesCancelFake(t, ctx, mode, 0)
			finished := false
			t.Cleanup(func() {
				if finished {
					return
				}
				// Release the intentionally stalled peer even on a regression, so
				// a failed assertion cannot orphan the fixture or its pipe readers.
				cancel()
				raw, err := os.ReadFile(logPath + ".pid")
				if err == nil {
					if pid, err := strconv.Atoi(string(raw)); err == nil {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
				finishHermesCancelFake(t, session)
			})
			waitHermesCancelLog(t, logPath, `"method":"session/set_model"`)
			if mode == "waiting_refresh" {
				waitHermesCancelLog(t, logPath, `"configId":"model"`)
			} else {
				waitHermesCancelLog(t, logPath, `"fake":"not_reading_refresh"`)
				deadline := time.Now().Add(5 * time.Second)
				for {
					var stacks strings.Builder
					_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
					if strings.Contains(stacks.String(), "(*hermesClient).writeLine") &&
						strings.Contains(stacks.String(), "acpOptionsAfterModelSwitch") {
						t.Log("model refresh reached a blocked transport writer")
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("fixture never blocked the model refresh writer")
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
			cancel()
			finishHermesCancelFake(t, session)
			finished = true
			raw, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), `"method":"session/prompt"`) {
				t.Fatal("prompt sent after cancellation during model refresh")
			}
			assertHermesCancelWire(t, logPath, false, mode == "waiting_refresh")
			var stacks strings.Builder
			_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
			if strings.Contains(stacks.String(), "(*hermesBackend).Execute.func") {
				t.Errorf("Hermes lifecycle/reader/writer goroutine leaked:\n%s", stacks.String())
			}
			t.Log("session channels closed; peer reaping and request cleanup checked")
		})
	}
}

func TestHermesCancelOutsideActivePrompt(t *testing.T) {
	for _, mode := range []string{"normal", "drain", "setup", "exit", "ignore"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, logPath := startHermesCancelFake(t, ctx, mode, 0)
			if mode == "setup" {
				waitHermesCancelLog(t, logPath, `"method":"initialize"`)
				cancel()
			}
			if mode == "ignore" {
				waitHermesCancelLog(t, logPath, `"method":"session/prompt"`)
				cancel()
			}
			if mode == "drain" {
				deadline := time.After(5 * time.Second)
			drainMarker:
				for {
					select {
					case <-deadline:
						t.Fatal("no post-response marker")
					case msg, ok := <-session.Messages:
						if !ok {
							t.Fatal("session closed before post-response marker")
						}
						if msg.Content == "response done" {
							break drainMarker
						}
					}
				}
				cancel()
			}
			result := finishHermesCancelFake(t, session)
			if mode == "normal" || mode == "drain" {
				if result.Status != "completed" || !strings.Contains(result.Output, "late final") {
					t.Errorf("lost normal completion/trailing notification: %+v", result)
				}
			}
			assertHermesCancelWire(t, logPath, mode == "ignore", mode != "ignore")
		})
	}
}
