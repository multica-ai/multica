package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestTomcatSessionConfigFailureDoesNotRejectResume(t *testing.T) {
	err := &acpRPCError{Method: "session/load", Code: -32603, Message: "session_config_failed"}
	if isACPResumeRejected(err) {
		t.Fatal("a startup configuration failure must not discard the existing session")
	}
}

// The current test executable is the fake ACP peer; never discover a user's CLI.
func TestTomcatHelperProcess(t *testing.T) {
	mode := os.Getenv("MULTICA_TOMCAT_TEST_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "parse-failure":
		_, _ = fmt.Fprintln(os.Stderr, "error: unexpected argument '--fixture-unknown'")
		os.Exit(2)
	case "output-then-exit-2":
		_, _ = fmt.Fprintln(os.Stdout, "non-protocol output")
		os.Exit(2)
	case "silent-exit-1":
		os.Exit(1)
	}
	root := os.Getenv("TOMCAT_TEST_ROOT")
	if mode == "child" {
		for {
			f, _ := os.OpenFile(filepath.Join(root, "ticks"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if f != nil {
				_, _ = f.WriteString("tick\n")
				_ = f.Close()
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	trace, err := os.OpenFile(filepath.Join(root, "trace.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(90)
	}
	enc := json.NewEncoder(os.Stdout)
	log := json.NewEncoder(trace)
	cwd, _ := os.Getwd()
	_ = log.Encode(map[string]any{"cwd": cwd, "args": os.Args, "marker": os.Getenv("TOMCAT_TEST_MARKER")})
	effort, selectedModel := "high", "configured-model"
	options := func(model string) []any {
		out := []any{map[string]any{"id": "model", "currentValue": model, "type": "select", "options": []any{map[string]any{"value": model, "name": model}}}}
		if strings.HasPrefix(mode, "effort-") {
			choices := []any{map[string]any{"value": "default", "name": "Default"}, map[string]any{"value": "low", "name": "Low"}}
			if model != "limited-model" {
				choices = append(choices, map[string]any{"value": "high", "name": "High"})
			}
			out = append(out, map[string]any{"id": "effort", "currentValue": effort, "type": "select", "category": "thought_level", "options": choices})
		}
		return out
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(91)
		}
		_ = log.Encode(req)
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			response["result"] = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]any{"http": true, "sse": true}}}
		case "session/new":
			response["result"] = map[string]any{"sessionId": "tomcat-fixture", "configOptions": options("configured-model")}
		case "session/load":
			if mode == "missing" || mode == "busy" {
				message := "unknown_session"
				if mode == "busy" {
					message = "session_in_use"
				}
				response["error"] = map[string]any{"code": -32603, "message": message}
				break
			}
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "tomcat-fixture", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "old history"}}}})
			response["result"] = map[string]any{"configOptions": options("configured-model")}
		case "session/set_config_option":
			if mode == "bad-model" {
				response["error"] = map[string]any{"code": -32602, "message": "unknown_model"}
			} else if req.Params["configId"] == "effort" {
				if mode == "effort-reject" {
					response["error"] = map[string]any{"code": -32602, "message": "invalid_thinking_level"}
				} else {
					if mode != "effort-unconfirmed" {
						effort = fmt.Sprint(req.Params["value"])
					}
					response["result"] = map[string]any{"configOptions": options(selectedModel)}
				}
			} else {
				selectedModel = fmt.Sprint(req.Params["value"])
				response["result"] = map[string]any{"configOptions": options(selectedModel)}
			}
		case "session/prompt":
			_ = os.WriteFile(filepath.Join(root, "prompted"), []byte("yes"), 0600)
			if mode == "wait" || mode == "hang" || mode == "cancel-shutdown-failure" {
				if mode == "hang" {
					self, _ := os.Executable()
					child := exec.Command(self, "-test.run=^TestTomcatHelperProcess$")
					child.Env = append(os.Environ(), "MULTICA_TOMCAT_TEST_HELPER=child")
					child.Stdout, child.Stderr = os.Stdout, os.Stderr
					if child.Start() != nil {
						os.Exit(92)
					}
				}
				continue
			}
			if mode == "failure" {
				response["error"] = map[string]any{"code": -32603, "message": "model unavailable"}
				break
			}
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "tomcat-fixture", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "new answer"}}}})
			stop := "end_turn"
			if mode == "cancelled" {
				stop = "cancelled"
			}
			if mode == "invalid" {
				stop = ""
			}
			response["result"] = map[string]any{"stopReason": stop, "usage": map[string]any{"inputTokens": 5, "outputTokens": 3}}
		default:
			os.Exit(93)
		}
		_ = enc.Encode(response)
	}
	if mode == "hang" {
		for {
			time.Sleep(time.Hour)
		}
	}
	time.Sleep(100 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(root, "stopped"), []byte("yes"), 0600)
	if mode == "shutdown-failure" || mode == "cancel-shutdown-failure" {
		os.Exit(94)
	}
	os.Exit(0)
}

func tomcatTestBackend(t *testing.T, mode string) (Backend, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	backend, err := New("tomcat", Config{BuiltinRuntime: true, ExecutablePath: self, LaunchPrefix: []string{"-test.run=^TestTomcatHelperProcess$"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Env: map[string]string{"MULTICA_TOMCAT_TEST_HELPER": mode, "TOMCAT_TEST_ROOT": root, "TOMCAT_TEST_MARKER": "host-only"}})
	if err != nil {
		t.Fatal(err)
	}
	return backend, root
}

func tomcatResult(t *testing.T, session *Session) Result {
	t.Helper()
	select {
	case result := <-session.Result:
		return result
	case <-time.After(tomcatShutdownGrace + 10*time.Second):
		t.Fatal("Tomcat backend did not return")
		return Result{}
	}
}

func TestTomcatExecutionAndResume(t *testing.T) {
	for _, resume := range []string{"", "tomcat-fixture"} {
		t.Run("resume="+resume, func(t *testing.T) {
			backend, root := tomcatTestBackend(t, "complete")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "only user prompt", ExecOptions{Cwd: root, Model: "custom-model", ResumeSessionID: resume, SystemPrompt: "must-not-be-inlined", CustomArgs: []string{"--help"}, McpConfig: json.RawMessage(`{"mcpServers":{"host":{"type":"http","url":"http://example.test/mcp"}}}`)})
			if err != nil {
				t.Fatal(err)
			}
			result := tomcatResult(t, session)
			if result.Status != "completed" || result.SessionID != "tomcat-fixture" || result.Output != "new answer" {
				t.Fatalf("result: %+v", result)
			}
			if result.Usage["custom-model"].InputTokens != 5 {
				t.Fatalf("usage: %+v", result.Usage)
			}
			if _, err := os.Stat(filepath.Join(root, "stopped")); err != nil {
				t.Fatal("result arrived before peer cleanup", err)
			}
			if !session.TerminalObserved() {
				t.Fatal("missing terminal boundary")
			}
			var messages []Message
			for m := range session.Messages {
				messages = append(messages, m)
			}
			if len(messages) < 2 || messages[0].SessionID != "tomcat-fixture" || messages[0].Type != MessageStatus {
				t.Fatalf("missing early session pin: %+v", messages)
			}
			data, err := os.ReadFile(filepath.Join(root, "trace.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			encodedRoot, err := json.Marshal(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"host-only", string(encodedRoot), "session/set_config_option", "custom-model", "http://example.test/mcp", "only user prompt"} {
				if !strings.Contains(text, want) {
					t.Errorf("trace missing %q: %s", want, text)
				}
			}
			if strings.Contains(text, "--help") || strings.Contains(text, "must-not-be-inlined") {
				t.Errorf("unexpected args/prompt: %s", text)
			}
			if resume != "" && (strings.Contains(text, "session/new") || !strings.Contains(text, "session/load")) {
				t.Errorf("resume forked: %s", text)
			}
		})
	}
}

func TestTomcatEffortSetupOrdersCallsAndNeverPromptsOnFailure(t *testing.T) {
	for _, tc := range []struct{ name, mode, resume, model, level, want string }{
		{"new-high", "effort-ok", "", "custom-model", "high", "completed"},
		{"resume-low", "effort-ok", "tomcat-fixture", "custom-model", "low", "completed"},
		{"resume-default", "effort-ok", "tomcat-fixture", "custom-model", "", "completed"},
		{"implicit-model", "effort-ok", "", "", "low", "completed"},
		{"rejected", "effort-reject", "tomcat-fixture", "custom-model", "low", "failed"},
		{"not-confirmed", "effort-unconfirmed", "", "custom-model", "low", "failed"},
		{"model-narrows-tiers", "effort-ok", "", "limited-model", "high", "failed"},
		{"old-tomcat-requested", "complete", "tomcat-fixture", "custom-model", "high", "failed"},
		{"old-tomcat-default", "complete", "", "custom-model", "", "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend, root := tomcatTestBackend(t, tc.mode)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "effort task", ExecOptions{Cwd: root, ResumeSessionID: tc.resume, Model: tc.model, ThinkingLevel: tc.level})
			if err != nil {
				t.Fatal(err)
			}
			result := tomcatResult(t, session)
			if result.Status != tc.want || result.ResumeRejected {
				t.Fatalf("result: %+v", result)
			}
			data, err := os.ReadFile(filepath.Join(root, "trace.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			var calls []string
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var frame struct {
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if err := json.Unmarshal([]byte(line), &frame); err != nil {
					t.Fatal(err)
				}
				if frame.Method == "" {
					continue
				}
				call := frame.Method
				if call == "session/set_config_option" {
					call += ":" + fmt.Sprint(frame.Params["configId"])
					if frame.Params["configId"] == "effort" {
						want := tc.level
						if want == "" {
							want = "default"
						}
						if frame.Params["value"] != want || frame.Params["sessionId"] != "tomcat-fixture" {
							t.Fatalf("wrong effort params: %+v", frame.Params)
						}
					}
				}
				calls = append(calls, call)
			}
			opening := "session/new"
			if tc.resume != "" {
				opening = "session/load"
			}
			want := []string{"initialize", opening}
			if tc.model != "" {
				want = append(want, "session/set_config_option:model")
			}
			if strings.HasPrefix(tc.mode, "effort-") && tc.model != "limited-model" {
				want = append(want, "session/set_config_option:effort")
			}
			if tc.want == "completed" {
				want = append(want, "session/prompt")
			}
			if strings.Join(calls, ",") != strings.Join(want, ",") {
				t.Fatalf("setup order %v, want %v", calls, want)
			}
			if tc.want == "failed" {
				if result.Error == "" {
					t.Fatal("setup failed without an error")
				}
				for message := range session.Messages {
					t.Errorf("message before successful setup: %+v", message)
				}
				if tc.resume != "" && result.SessionID != tc.resume {
					t.Fatalf("lost resumable session: %+v", result)
				}
			}
		})
	}
}

func TestTomcatCompletedTurnSurvivesLateCancellation(t *testing.T) {
	backend, root := tomcatTestBackend(t, "complete")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "task", ExecOptions{Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !session.TerminalObserved() {
		if time.Now().After(deadline) {
			t.Fatal("no terminal response")
		}
		time.Sleep(time.Millisecond)
	}
	cancel() // The protocol completed, but the peer is still cleaning up.
	result := tomcatResult(t, session)
	if result.Status != "completed" {
		t.Fatalf("late cancellation changed a completed turn: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "stopped")); err != nil {
		t.Fatal(err)
	}
}

func TestTomcatArgumentParseFailure(t *testing.T) {
	for _, tc := range []struct {
		name, mode, status string
		builtin            bool
	}{
		{"native-parse-failure", "parse-failure", "failed", true},
		{"custom-parse-failure", "parse-failure", "cleanup_unconfirmed", false},
		{"output-then-exit-2", "output-then-exit-2", "cleanup_unconfirmed", true},
		{"silent-exit-1", "silent-exit-1", "cleanup_unconfirmed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend, root := tomcatTestBackend(t, tc.mode)
			tomcat := backend.(*tomcatBackend)
			tomcat.cfg.BuiltinRuntime = tc.builtin
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "task", ExecOptions{Cwd: root, ResumeSessionID: "tomcat-fixture"})
			if err != nil {
				t.Fatal(err)
			}
			result := tomcatResult(t, session)
			if result.Status != tc.status || result.SessionID != "tomcat-fixture" || result.ResumeRejected {
				t.Fatalf("result: %+v", result)
			}
			if session.TerminalObserved() || !session.CleanupRequired {
				t.Fatalf("terminal=%v cleanupRequired=%v", session.TerminalObserved(), session.CleanupRequired)
			}
			for message := range session.Messages {
				t.Errorf("unexpected message before initialization: %+v", message)
			}
			if tc.status == "failed" {
				for _, want := range []string{tomcat.cfg.ExecutablePath, "exit status 2", "--fixture-unknown", "acp --help"} {
					if !strings.Contains(result.Error, want) {
						t.Errorf("error missing %q: %s", want, result.Error)
					}
				}
				if reason := taskfailure.Classify(result.Error); reason != taskfailure.ReasonAgentProcessFailure {
					t.Errorf("failure reason=%s: %s", reason, result.Error)
				}
			}
		})
	}
}

func TestTomcatFailuresAndResumeClassification(t *testing.T) {
	for _, tc := range []struct {
		mode, status string
		rejected     bool
	}{{"missing", "failed", true}, {"busy", "failed", false}, {"failure", "failed", false}, {"cancelled", "aborted", false}, {"invalid", "failed", false}, {"shutdown-failure", "cleanup_unconfirmed", false}, {"bad-model", "failed", false}} {
		t.Run(tc.mode, func(t *testing.T) {
			backend, root := tomcatTestBackend(t, tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "task", ExecOptions{Cwd: root, ResumeSessionID: "tomcat-fixture", Model: "custom-model"})
			if err != nil {
				t.Fatal(err)
			}
			result := tomcatResult(t, session)
			if result.Status != tc.status || result.ResumeRejected != tc.rejected || result.Error == "" {
				t.Fatalf("result: %+v", result)
			}
			wantTerminal := tc.mode == "cancelled" || tc.mode == "shutdown-failure"
			if session.TerminalObserved() != wantTerminal || !session.CleanupRequired {
				t.Fatalf("terminal=%v want=%v cleanupRequired=%v", session.TerminalObserved(), wantTerminal, session.CleanupRequired)
			}
			if _, err := os.Stat(filepath.Join(root, "stopped")); err != nil {
				t.Fatal("result before shutdown", err)
			}
		})
	}
}

func TestTomcatCancellationWaitsForEOFExitAndKillsUnresponsiveTree(t *testing.T) {
	for _, mode := range []string{"wait", "hang", "cancel-shutdown-failure"} {
		t.Run(mode, func(t *testing.T) {
			backend, root := tomcatTestBackend(t, mode)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, err := backend.Execute(ctx, "task", ExecOptions{Cwd: root})
			if err != nil {
				t.Fatal(err)
			}
			ready := "prompted"
			if mode == "hang" {
				ready = "ticks"
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(root, ready)); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("peer did not become ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			started := time.Now()
			cancel()
			result := tomcatResult(t, session)
			wantStatus := "aborted"
			if mode != "wait" {
				wantStatus = "cleanup_unconfirmed"
			}
			if result.Status != wantStatus || session.TerminalObserved() || !session.CleanupRequired {
				t.Fatalf("result=%+v terminal=%v cleanupRequired=%v", result, session.TerminalObserved(), session.CleanupRequired)
			}
			if mode == "wait" {
				if _, err := os.Stat(filepath.Join(root, "stopped")); err != nil {
					t.Fatal("peer was killed rather than allowed to finish", err)
				}
			} else if mode == "hang" {
				if time.Since(started) < tomcatShutdownGrace {
					t.Fatal("killed before grace expired")
				}
				if !strings.Contains(result.Error, "cleanup was not acknowledged") {
					t.Fatalf("dishonest cleanup result: %+v", result)
				}
				before, _ := os.ReadFile(filepath.Join(root, "ticks"))
				time.Sleep(120 * time.Millisecond)
				after, _ := os.ReadFile(filepath.Join(root, "ticks"))
				if string(before) != string(after) {
					t.Fatal("descendant still writing after result")
				}
			}
		})
	}
}

func TestTomcatLocalTimeoutDoesNotClaimAnAuthoritativeTerminal(t *testing.T) {
	backend, root := tomcatTestBackend(t, "wait")
	session, err := backend.Execute(t.Context(), "task", ExecOptions{Cwd: root, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	result := tomcatResult(t, session)
	if result.Status != "timeout" || session.TerminalObserved() || !session.CleanupRequired {
		t.Fatalf("result=%+v terminal=%v cleanupRequired=%v", result, session.TerminalObserved(), session.CleanupRequired)
	}
}

func TestTomcatRegistrationAndInputValidation(t *testing.T) {
	backend, err := New("tomcat", Config{ExecutablePath: filepath.Join(t.TempDir(), "missing")})
	if err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{"", ".", filepath.Join(t.TempDir(), "missing")} {
		if _, err := backend.Execute(context.Background(), "task", ExecOptions{Cwd: cwd}); err == nil {
			t.Fatalf("accepted cwd %q", cwd)
		}
	}
	catalog, err := ListModels(context.Background(), "tomcat", NewCommand(filepath.Join(t.TempDir(), "missing-tomcat"), nil))
	if err == nil || len(catalog.Models) != 0 || !ModelSelectionSupported("tomcat") {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	if LaunchHeader("tomcat") != "tomcat acp" {
		t.Fatal("wrong launch header")
	}
	for _, tc := range []struct {
		version string
		valid   bool
	}{{"0.1.80", false}, {"0.1.96", false}, {"0.1.97", true}, {"0.2.0", true}, {"not-a-version", false}} {
		if err := CheckMinVersion("tomcat", tc.version); (err == nil) != tc.valid {
			t.Errorf("version %s: %v", tc.version, err)
		}
	}
}
