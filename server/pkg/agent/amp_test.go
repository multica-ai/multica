package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAmpHelperProcess(t *testing.T) {
	if os.Getenv("AMP_HELPER_PROCESS") != "1" {
		return
	}
	input, _ := io.ReadAll(os.Stdin)
	if capture := os.Getenv("AMP_CAPTURE"); capture != "" {
		cwd, _ := os.Getwd()
		data, _ := json.Marshal(map[string]any{"args": os.Args[1:], "prompt": string(input), "cwd": cwd, "cli_path": os.Getenv("MULTICA_CLI_PATH")})
		_ = os.WriteFile(capture, data, 0o600)
	}
	if os.Getenv("AMP_HANG") == "1" {
		fmt.Println(`{"type":"system","subtype":"init","session_id":"ready"}`)
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Getenv("AMP_OVERSIZED") == "1" {
		fmt.Print(strings.Repeat("x", agentStreamMaxLineBytes+1))
		for {
			time.Sleep(time.Hour)
		}
	}
	fmt.Print(os.Getenv("AMP_STREAM"))
	if streamFile := os.Getenv("AMP_STREAM_FILE"); streamFile != "" {
		stream, err := os.ReadFile(streamFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		_, _ = os.Stdout.Write(stream)
	}
	fmt.Fprint(os.Stderr, os.Getenv("AMP_STDERR"))
	if delay, err := time.ParseDuration(os.Getenv("AMP_EXIT_DELAY")); err == nil {
		time.Sleep(delay)
	}
	if os.Getenv("AMP_HANG_AFTER_STREAM") == "1" {
		fmt.Println(`{"type":"system","subtype":"init","session_id":"awaiting-exit"}`)
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Getenv("AMP_FAIL") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

func newFakeAmp(t *testing.T, env map[string]string, logs io.Writer) Backend {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env["AMP_HELPER_PROCESS"] = "1"
	if _, ok := env["AMP_API_KEY"]; !ok {
		env["AMP_API_KEY"] = "sgamp_fixture-not-a-real-token"
	}
	backend, err := New("amp", Config{
		ExecutablePath: executable, LaunchPrefix: []string{"-test.run=^TestAmpHelperProcess$", "amp-test"},
		Env: env, Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func collectAmp(t *testing.T, session *Session) ([]Message, Result) {
	t.Helper()
	var messages []Message
	var result Result
	var gotResult bool
	msgCh, resCh := session.Messages, session.Result
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for msgCh != nil || resCh != nil {
		select {
		case m, ok := <-msgCh:
			if !ok {
				msgCh = nil
			} else {
				messages = append(messages, m)
			}
		case r, ok := <-resCh:
			if ok {
				result, gotResult = r, true
			} else {
				resCh = nil
			}
		case <-timer.C:
			t.Fatal("Amp session did not terminate")
		}
	}
	if !gotResult {
		t.Fatal("Amp session returned no result")
	}
	return messages, result
}

func TestAmpArguments(t *testing.T) {
	args, err := buildAmpArgs(ExecOptions{Model: "high", ResumeSessionID: "T-saved", CustomArgs: []string{
		"--executor", "orb", "--execute=wrong prompt", "--stream-json-input", "--mode=low", "--features", "fast",
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"threads", "continue", "T-saved", "--execute", "--stream-json", "--executor", "local", "--mode", "high", "--features", "fast"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %q, want %q", args, want)
	}
	prefix := FilterLaunchPrefix("amp", []string{"wrapper", "-xprivate", "-mlow", "--executor=orb"}, nil)
	if !reflect.DeepEqual(prefix, []string{"wrapper"}) {
		t.Fatalf("managed flags survived prefix: %q", prefix)
	}
	for _, custom := range [][]string{{"threads", "delete"}, {"replacement prompt"}, {"-ox"}, {"-xsecret"}, {"-mhigh"}, {"--features"}, {"--fast=false"}, {"--unverified"}} {
		_, err := buildAmpArgs(ExecOptions{CustomArgs: custom}, nil)
		// A filtered standalone flag is harmless; every unverified token fails closed.
		if custom[0] != "-ox" && custom[0] != "-xsecret" && custom[0] != "-mhigh" && err == nil {
			t.Errorf("accepted unsafe args %q", custom)
		}
	}
}

func TestAmpBrokenStreamsBounded(t *testing.T) {
	backend := newFakeAmp(t, map[string]string{"AMP_OVERSIZED": "1"}, io.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "task", ExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, result := collectAmp(t, session)
	if result.Status != "failed" || result.Output != "" || result.Error == "" {
		t.Fatalf("broken stream = %+v", result)
	}
}

func TestAmpPostResultCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, delay, want  string
		hang, fail, cancel bool
		timeout            time.Duration
	}{
		{name: "slow clean exit", delay: "3s", want: "completed"},
		{name: "stuck after success", hang: true, want: "completed"},
		{name: "cancel after success", hang: true, cancel: true, want: "completed"},
		{name: "timeout after success", hang: true, timeout: 3 * time.Second, want: "completed"},
		{name: "natural failure after success", delay: "3s", fail: true, want: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			env := map[string]string{"AMP_STREAM": "{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"T-finished\",\"result\":\"Done\"}\n", "AMP_EXIT_DELAY": tc.delay}
			if tc.hang {
				env["AMP_HANG_AFTER_STREAM"] = "1"
			}
			if tc.fail {
				env["AMP_FAIL"] = "1"
			}
			backend := newFakeAmp(t, env, &logs)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, err := backend.Execute(ctx, "task", ExecOptions{Timeout: tc.timeout})
			if err != nil {
				t.Fatal(err)
			}
			if tc.cancel {
				deadline := time.NewTimer(5 * time.Second)
				defer deadline.Stop()
				for !session.TerminalObserved() {
					select {
					case <-deadline.C:
						t.Fatal("terminal boundary was not published before cleanup")
					case <-time.After(10 * time.Millisecond):
					}
				}
				cancel()
			}
			_, result := collectAmp(t, session)
			if result.Status != tc.want || result.SessionID != "T-finished" || !session.TerminalObserved() {
				t.Fatalf("post-result cleanup = %+v", result)
			}
			if tc.want == "completed" && (result.Output != "Done" || result.Error != "") {
				t.Fatalf("successful answer lost = %+v", result)
			}
			if tc.want == "failed" && (result.Output != "" || !strings.Contains(result.Error, "exited")) {
				t.Fatalf("native failure hidden = %+v", result)
			}
			if tc.hang && !strings.Contains(logs.String(), "level=WARN") {
				t.Fatalf("forced cleanup was not logged: %s", logs.String())
			}
		})
	}
}

func TestAmpExecuteProtocol(t *testing.T) {
	const key = "sgamp_test-credential"
	stream := `{"type":"system","subtype":"init","session_id":"T-task"}
{"type":"assistant","message":{"model":"actual-model","content":[{"type":"text","text":"Working sgamp_test-credential"},{"type":"tool_use","id":"tool-1","name":"Read","input":{"path":"file.txt"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":[{"type":"text","text":"file contents"}]}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"intermediate"}]},"stop_reason":"end_turn"}
{"type":"system","subtype":"subagent_finished","tool_use_id":"task-1","status":"completed"}
{"type":"result","subtype":"success","is_error":false,"result":"Done sgamp_test-credential","session_id":"T-task","usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":30,"cache_creation_input_tokens":40}}
`
	var logs bytes.Buffer
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture.json")
	backend := newFakeAmp(t, map[string]string{"AMP_STREAM": stream, "AMP_CAPTURE": capture, "AMP_API_KEY": key, "MULTICA_CLI_PATH": "owned-cli"}, &logs)
	prompt := strings.Repeat("private task\"\n", 20000)
	session, err := backend.Execute(context.Background(), prompt, ExecOptions{Cwd: dir, ResumeSessionID: "T-task", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	messages, result := collectAmp(t, session)
	if result.Status != "completed" || result.Output != "Done [REDACTED]" || result.SessionID != "T-task" || !session.TerminalObserved() || !session.CleanupRequired {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage["actual-model"] != (TokenUsage{InputTokens: 10, OutputTokens: 20, CacheReadTokens: 30, CacheWriteTokens: 40}) {
		t.Fatalf("usage = %+v", result.Usage)
	}
	var sawUse, sawResult bool
	for _, message := range messages {
		sawUse = sawUse || message.Type == MessageToolUse && message.CallID == "tool-1"
		sawResult = sawResult || message.Type == MessageToolResult && message.Output == "file contents"
	}
	if !sawUse || !sawResult {
		t.Fatalf("tool transcript = %+v", messages)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Prompt, Cwd string
		Args        []string
		CLIPath     string `json:"cli_path"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	wantDir, _ := filepath.EvalSymlinks(dir)
	if got.Prompt != prompt || got.Cwd != wantDir || got.CLIPath != "owned-cli" {
		t.Fatalf("input transport lost context: bytes=%d cwd=%q cli=%q", len(got.Prompt), got.Cwd, got.CLIPath)
	}
	encoded, _ := json.Marshal(messages)
	if strings.Contains(string(encoded)+logs.String(), key) || strings.Contains(logs.String(), "private task") || strings.Contains(strings.Join(got.Args, " "), "private task") {
		t.Fatal("credentials or prompt leaked")
	}
}

func TestAmpPermissionDenials(t *testing.T) {
	backend := newFakeAmp(t, map[string]string{
		"AMP_STREAM": `{"type":"result","subtype":"success","result":"Done","permission_denials":["Bash: git push","Bash: /path/to/multica api_key=sgamp_private-token"]}`,
	}, io.Discard)
	session, err := backend.Execute(context.Background(), "task", ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	messages, result := collectAmp(t, session)
	if result.Status != "completed" || result.Output != "Done" {
		t.Fatalf("permission warning replaced the provider result: %+v", result)
	}
	var warning string
	for _, message := range messages {
		if message.Type == MessageError && strings.Contains(message.Content, "Warning: Amp denied tool permissions") {
			warning += message.Content
		}
	}
	if !strings.Contains(warning, "git push") || !strings.Contains(warning, "amp.permissions") || strings.Contains(warning, "sgamp_private-token") {
		t.Fatalf("permission warning = %q", warning)
	}
}

func TestAmpFailurePaths(t *testing.T) {
	for _, tc := range []struct {
		name, stream, stderr, want, detail string
		fail                               bool
	}{
		{name: "missing terminal", stream: `{"type":"assistant","message":{"content":[{"type":"text","text":"partial"}]}}`, want: "failed", detail: "without terminal result"},
		{name: "invalid output", stream: "not json", want: "failed", detail: "without terminal result"},
		{name: "authentication", stderr: "Authentication failed: sgamp_bad-secret", want: "failed", detail: "Authentication", fail: true},
		{name: "error result", stream: `{"type":"result","subtype":"error_during_execution","is_error":true,"error":"Authentication failed sgamp_bad-secret"}`, want: "failed", detail: "Authentication"},
		{name: "error then completion", stream: "{\"type\":\"system\",\"subtype\":\"error_max_turns\",\"error\":\"Turn limit\"}\n{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"partial\"}", want: "failed", detail: "Turn limit"},
		{name: "nonzero after result", stream: `{"type":"result","subtype":"success","result":"partial"}`, want: "failed", detail: "exited", fail: true},
		{name: "empty final", stream: `{"type":"result","subtype":"success","result":""}`, want: "completed"},
		{name: "malformed result", stream: `{"type":"result","result":"partial"}`, want: "failed", detail: "partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			env := map[string]string{"AMP_STREAM": tc.stream, "AMP_STDERR": tc.stderr}
			if tc.fail {
				env["AMP_FAIL"] = "1"
			}
			backend := newFakeAmp(t, env, &logs)
			session, err := backend.Execute(context.Background(), "task", ExecOptions{Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			_, result := collectAmp(t, session)
			if result.Status != tc.want || result.Output != "" || !strings.Contains(result.Error, tc.detail) {
				t.Fatalf("result = %+v", result)
			}
			if strings.Contains(result.Error+logs.String(), "sgamp_bad-secret") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestAmpStartupAndUnsupportedMCP(t *testing.T) {
	backend, err := New("amp", Config{ExecutablePath: filepath.Join(t.TempDir(), "missing")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Execute(context.Background(), "task", ExecOptions{}); err == nil || !strings.Contains(err.Error(), "MULTICA_AMP_PATH") {
		t.Fatalf("missing install = %v", err)
	}
	if _, err := backend.Execute(context.Background(), "task", ExecOptions{McpConfig: json.RawMessage(`{"mcpServers":{"test":{"command":"example"}}}`)}); err == nil || !strings.Contains(err.Error(), "MCP") {
		t.Fatalf("unsupported MCP = %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	backend, err = New("amp", Config{ExecutablePath: executable, Env: map[string]string{"AMP_API_KEY": ""}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Execute(context.Background(), "task", ExecOptions{}); err == nil || !strings.Contains(err.Error(), "requires AMP_API_KEY") {
		t.Fatalf("missing credential = %v", err)
	}
}
