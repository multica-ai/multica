package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The child is a controlled ACP peer, never an installed agent or account.
func TestHermesNativePeerProcess(t *testing.T) {
	if os.Getenv("MULTICA_NATIVE_PEER") != "1" {
		t.Skip("subprocess helper")
	}
	mode := os.Getenv("MULTICA_NATIVE_CASE")
	var mu sync.Mutex
	write := func(v any) { mu.Lock(); defer mu.Unlock(); b, _ := json.Marshal(v); fmt.Fprintln(os.Stdout, string(b)) }
	response := func(id int, result any) { write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}) }
	update := func(sid, kind, text string) {
		write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": sid, "update": map[string]any{"sessionUpdate": kind, "content": map[string]string{"type": "text", "text": text}}}})
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(2)
		}
		switch req.Method {
		case "initialize":
			response(req.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
		case "session/new":
			response(req.ID, map[string]string{"sessionId": "native-session"})
			commands := []map[string]string{}
			if mode != "unsupported" {
				commands = append(commands, map[string]string{"name": "steer", "description": "guidance"})
			}
			sid := "native-session"
			if mode == "wrong-session" {
				sid = "other"
			}
			write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": sid, "update": map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": commands}}})
		case "session/prompt":
			text := req.Params.Prompt[0].Text
			if strings.HasPrefix(text, "/steer ") {
				f, err := os.OpenFile(os.Getenv("MULTICA_NATIVE_TRACE"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					os.Exit(3)
				}
				fmt.Fprintln(f, text)
				f.Close()
				switch mode {
				case "refused":
					write(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32603, "message": "unsupported_control"}})
				case "lost": // No reply: the SDK must not retry this command.
				case "cancelled":
					response(req.ID, map[string]string{"stopReason": "cancelled"})
				default:
					preview := []rune(strings.TrimPrefix(text, "/steer "))
					suffix := ""
					if len(preview) > 80 {
						preview = preview[:80]
						suffix = "..."
					}
					update("native-session", "agent_message_chunk", "⏩ Steer queued for the active turn: "+string(preview)+suffix+"\n")
					response(req.ID, map[string]any{"stopReason": "end_turn", "usage": map[string]int{"inputTokens": 999, "outputTokens": 999}})
				}
			} else {
				update("native-session", "agent_thought_chunk", "working")
				go func(id int) {
					deadline := time.Now().Add(15 * time.Second)
					for time.Now().Before(deadline) {
						if _, err := os.Stat(os.Getenv("MULTICA_NATIVE_GATE")); err == nil {
							break
						}
						time.Sleep(5 * time.Millisecond)
					}
					update("native-session", "agent_message_chunk", "primary answer")
					response(id, map[string]any{"stopReason": "end_turn", "usage": map[string]int{"inputTokens": 17, "outputTokens": 8}, "_meta": map[string]string{"modelId": "native-model"}})
				}(req.ID)
			}
		default:
			response(req.ID, map[string]any{})
		}
	}
	os.Exit(0)
}

func TestHermesNativeSteerProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process shim")
	}
	for _, mode := range []string{"normal", "refused", "cancelled", "lost", "unsupported", "wrong-session", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			gate := filepath.Join(dir, "release")
			trace := filepath.Join(dir, "trace")
			defer os.WriteFile(gate, []byte("release"), 0600)
			executable := filepath.Join(dir, "native-peer")
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
			writeTestExecutable(t, executable, []byte("#!/bin/sh\nexec "+quote(binary)+" -test.run='^TestHermesNativePeerProcess$'\n"))
			backend := &hermesBackend{cfg: Config{ExecutablePath: executable, Logger: slog.Default(), Env: map[string]string{"HERMES_HOME": dir, "MULTICA_NATIVE_PEER": "1", "MULTICA_NATIVE_CASE": mode, "MULTICA_NATIVE_GATE": gate, "MULTICA_NATIVE_TRACE": trace}}}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "original objective", ExecOptions{Cwd: dir, EnableTaskSupplement: mode != "disabled"})
			if err != nil {
				t.Fatal(err)
			}
			drained := make(chan struct{})
			var messages []Message
			go func() {
				defer close(drained)
				for m := range session.Messages {
					messages = append(messages, m)
				}
			}()
			supported := mode != "unsupported" && mode != "wrong-session" && mode != "disabled"
			if supported {
				deadline := time.NewTimer(5 * time.Second)
				defer deadline.Stop()
				for !session.SupplementReady() {
					select {
					case <-deadline.C:
						t.Fatal("native steering never ready")
					case <-time.After(time.Millisecond):
					}
				}
				err = session.Supplement(ctx, "literal input")
				if (mode == "normal") != (err == nil) {
					t.Fatalf("mode=%s steer=%v", mode, err)
				}
				select {
				case result := <-session.Result:
					t.Fatalf("control completed primary: %+v", result)
				default:
				}
			} else {
				// The primary stays alive until its own gate is released.
				time.Sleep(30 * time.Millisecond)
				if session.SupplementReady != nil && session.SupplementReady() {
					t.Fatal("unsupported/disabled peer enabled steer")
				}
			}
			if err := os.WriteFile(gate, []byte("release"), 0600); err != nil {
				t.Fatal(err)
			}
			var result Result
			select {
			case result = <-session.Result:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			<-drained
			if result.Status != "completed" || result.Output != "primary answer" {
				t.Fatalf("primary=%+v", result)
			}
			for _, usage := range result.Usage {
				if usage.InputTokens != 17 || usage.OutputTokens != 8 {
					t.Fatalf("control changed usage: %+v", result.Usage)
				}
			}
			for _, m := range messages {
				if strings.Contains(m.Content, "Steer queued for") {
					t.Fatal("command acknowledgement leaked into transcript")
				}
			}
			if supported {
				b, err := os.ReadFile(trace)
				if err != nil || string(b) != "/steer literal input\n" {
					t.Fatalf("native command replayed or missing: %s %v", b, err)
				}
			}
		})
	}
}
