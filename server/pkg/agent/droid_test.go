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
	"runtime"
	"strings"
	"testing"
	"time"
)

// The test binary is the fake CLI on every platform; default tests never
// discover or execute a user-installed Droid or access a Factory account.
func TestDroidHelperProcess(t *testing.T) {
	if os.Getenv("DROID_HELPER_PROCESS") != "1" {
		return
	}
	input, _ := io.ReadAll(os.Stdin)
	if capture := os.Getenv("DROID_CAPTURE"); capture != "" {
		cwd, _ := os.Getwd()
		var systemPrompt, systemFile string
		var systemMode os.FileMode
		for i, arg := range os.Args {
			if arg == "--append-system-prompt-file" && i+1 < len(os.Args) {
				systemFile = os.Args[i+1]
				content, err := os.ReadFile(systemFile)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(2)
				}
				systemPrompt = string(content)
				info, _ := os.Stat(systemFile)
				systemMode = info.Mode().Perm()
			}
		}
		data, _ := json.Marshal(map[string]any{"args": os.Args[1:], "prompt": string(input), "cwd": cwd,
			"system_prompt": systemPrompt, "system_file": systemFile, "system_mode": systemMode})
		_ = os.WriteFile(capture, data, 0o600)
	}
	if os.Getenv("DROID_HANG") == "1" {
		fmt.Println(`{"type":"system","subtype":"init","session_id":"ready"}`)
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Getenv("DROID_OVERSIZED") == "1" {
		fmt.Print(strings.Repeat("x", agentStreamMaxLineBytes+1))
		for {
			time.Sleep(time.Hour)
		}
	}
	fmt.Print(os.Getenv("DROID_STREAM"))
	if streamFile := os.Getenv("DROID_STREAM_FILE"); streamFile != "" {
		stream, err := os.ReadFile(streamFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		_, _ = os.Stdout.Write(stream)
	}
	fmt.Fprint(os.Stderr, os.Getenv("DROID_STDERR"))
	if os.Getenv("DROID_HANG_AFTER_STREAM") == "1" {
		fmt.Println(`{"type":"system","subtype":"init","session_id":"awaiting-exit"}`)
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Getenv("DROID_FAIL") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

func newFakeDroid(t *testing.T, env map[string]string, logs io.Writer) Backend {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env["DROID_HELPER_PROCESS"] = "1"
	backend, err := New("droid", Config{
		ExecutablePath: executable, LaunchPrefix: []string{"-test.run=^TestDroidHelperProcess$"},
		Env: env, Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func isolateDroidSystemPromptFiles(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
	t.Cleanup(func() {
		files, err := filepath.Glob(filepath.Join(dir, "multica-droid-system-*"))
		if err != nil || len(files) != 0 {
			t.Errorf("system prompt files remain after execution: %v (error: %v)", files, err)
		}
	})
}

func collectDroid(t *testing.T, session *Session) ([]Message, Result) {
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
			t.Fatal("Droid session did not terminate")
		}
	}
	if !gotResult {
		t.Fatal("Droid session returned no result")
	}
	return messages, result
}

func TestDroidArgumentsProtectProtocol(t *testing.T) {
	args, err := buildDroidArgs(ExecOptions{
		Model: "chosen", ResumeSessionID: "saved",
		ExtraArgs:  []string{"--output-format", "text", "--remove-tools", "WebSearch", "-otext", "-fother.md", "-mother", "-sother", "-wother"},
		CustomArgs: []string{"-o=json", "--input-format", "stream-jsonrpc", "--cwd", "/other", "--model=other", "--session-id=other", "-f", "other.md", "--auto", "low", "--skip-permissions-unsafe", "--fork=other", "--worktree", "branch", "--mission", "--worker-model", "other", "--worker-reasoning-effort=high", "--validator-model=other", "--validator-reasoning-effort", "high", "--version", "-v", "--help", "--list-tools", "--reasoning-effort", "high"},
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "--output-format", "stream-json", "--auto", "high", "--model", "chosen", "--session-id", "saved", "--remove-tools", "WebSearch", "--reasoning-effort", "high"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %q, want %q", args, want)
	}
	if prefix := FilterLaunchPrefix("droid", []string{"wrapper", "--file", "override", "--output-format=json"}, nil); !reflect.DeepEqual(prefix, []string{"wrapper"}) {
		t.Fatalf("unsafe launch prefix survived: %v", prefix)
	}
}

func TestDroidArgumentsPreserveNativeConfiguration(t *testing.T) {
	custom := []string{"--reasoning-effort", "high", "-rmedium", "--spec-model=custom:spec", "--spec-reasoning-effort", "low", "--use-spec",
		"--only-tools", "Read,Edit", "--add-tools=WebSearch", "--remove-tools", "Execute", "--settings", "./settings.json",
		"--disable-builtin-skills", "--tag", "nightly", "--tag=review", "--log-group-id", "automation"}
	args, err := buildDroidArgs(ExecOptions{CustomArgs: custom}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args[5:], custom) {
		t.Fatalf("native configuration changed: %q", args)
	}
}

func TestDroidRejectsCustomPromptOverrides(t *testing.T) {
	for _, args := range [][]string{
		{"replace the task"},
		{"--", "replace the task"},
		{"--reasoning-effort", "high", "replace the task"},
		{"--unknown-option", "replace the task"},
		{"--reasoning-effort"},
		{"--settings", "--tag", "nightly"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			for _, useExtra := range []bool{false, true} {
				backend := newFakeDroid(t, map[string]string{"DROID_STREAM": `{"type":"completion","finalText":"wrong task"}`}, io.Discard)
				opts := ExecOptions{CustomArgs: args}
				if useExtra {
					opts.CustomArgs, opts.ExtraArgs = nil, args
				}
				session, err := backend.Execute(context.Background(), "assigned task", opts)
				if err == nil {
					collectDroid(t, session)
					t.Fatal("custom arguments could replace the task prompt")
				}
			}
		})
	}
}

func TestDroidExecuteStreamAndPrompt(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "capture.json")
	stream, err := os.ReadFile("testdata/droid-1.8.0-protocol-example.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	backend := newFakeDroid(t, map[string]string{"DROID_STREAM": string(stream), "DROID_CAPTURE": capture}, io.Discard)
	cwd := t.TempDir()
	prompt := strings.Repeat("Unicode 中文 \"quoted\" $(literal)\n", 10000)
	session, err := backend.Execute(context.Background(), prompt, ExecOptions{Cwd: cwd, SystemPrompt: "instructions", Model: "selected", ResumeSessionID: "saved", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	messages, result := collectDroid(t, session)
	if result.Status != "completed" || result.Output != "Final answer" || result.SessionID != "session-example" {
		t.Fatalf("result = %+v", result)
	}
	if got := result.Usage["actual-model"]; got.InputTokens != 42 || got.OutputTokens != 9 || got.CacheReadTokens != 4 || got.CacheWriteTokens != 3 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	var toolUse, toolResult bool
	for _, m := range messages {
		toolUse = toolUse || m.Type == MessageToolUse && m.CallID == "call-1" && m.Tool == "Read" && m.Input["file_path"] == "README.md"
		toolResult = toolResult || m.Type == MessageToolResult && m.CallID == "call-1" && m.Output == "File contents"
	}
	if !toolUse || !toolResult {
		t.Fatalf("tool events not mapped: %+v", messages)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Args         []string    `json:"args"`
		Prompt       string      `json:"prompt"`
		Cwd          string      `json:"cwd"`
		SystemPrompt string      `json:"system_prompt"`
		SystemFile   string      `json:"system_file"`
		SystemMode   os.FileMode `json:"system_mode"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	wantCwd, _ := filepath.EvalSymlinks(cwd)
	if got.Prompt != prompt || got.SystemPrompt != "instructions" || got.Cwd != wantCwd || strings.Contains(strings.Join(got.Args, " "), "$(literal)") || !strings.Contains(strings.Join(got.Args, " "), "--session-id saved") {
		t.Fatalf("prompt, workdir, or resume was not forwarded correctly")
	}
	if got.SystemFile == "" {
		t.Fatal("system prompt was not passed through Droid's native system prompt file")
	}
	if _, err := os.Stat(got.SystemFile); !os.IsNotExist(err) {
		t.Fatalf("system prompt file remains after execution: %v", err)
	}
	if runtime.GOOS != "windows" && got.SystemMode != 0o600 {
		t.Fatalf("system prompt permissions = %o, want 600", got.SystemMode)
	}
}

func TestDroidFailurePaths(t *testing.T) {
	for _, tc := range []struct{ name, stream, stderr, fail, want string }{
		{"missing completion", `{"type":"message","role":"assistant","text":"partial"}`, "", "", "without terminal result"},
		{"execution error", "{\"type\":\"error\",\"message\":\"Permission rejected\"}\n{\"type\":\"completion\",\"finalText\":\"partial\"}\n", "", "", "Permission rejected"},
		{"error after completion", "{\"type\":\"completion\",\"finalText\":\"partial\"}\n{\"type\":\"error\",\"message\":\"Permission rejected\"}\n", "", "", "Permission rejected"},
		{"authentication", `{"type":"error","source":"cli","message":"Authentication failed. Set FACTORY_API_KEY"}`, "", "1", "Authentication failed"},
		{"crash", `{"type":"completion","finalText":"partial"}`, "Process crashed", "1", "Process crashed"},
		{"invalid protocol", "not json\n", "", "", "without terminal result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateDroidSystemPromptFiles(t)
			b := newFakeDroid(t, map[string]string{"DROID_STREAM": tc.stream, "DROID_STDERR": tc.stderr, "DROID_FAIL": tc.fail}, io.Discard)
			s, err := b.Execute(context.Background(), "task", ExecOptions{SystemPrompt: "private instructions", ResumeSessionID: "keep-session", Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			_, r := collectDroid(t, s)
			if r.Status != "failed" || r.Output != "" || !strings.Contains(r.Error, tc.want) || r.ResumeRejected || r.SessionID != "" {
				t.Fatalf("result = %+v", r)
			}
		})
	}
}

func TestDroidResumeSessionIDRequiresObservedSession(t *testing.T) {
	for _, tc := range []struct{ name, stream, wantSession string }{
		{"not loaded", `{"type":"error","message":"Could not load session"}`, ""},
		{"loaded before failure", "{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"loaded-session\"}\n{\"type\":\"error\",\"message\":\"Provider failed\"}\n", "loaded-session"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := newFakeDroid(t, map[string]string{"DROID_STREAM": tc.stream, "DROID_FAIL": "1"}, io.Discard)
			session, err := backend.Execute(context.Background(), "task", ExecOptions{ResumeSessionID: "stale-session", Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			_, result := collectDroid(t, session)
			if result.Status != "failed" || result.SessionID != tc.wantSession || result.ResumeRejected {
				t.Fatalf("result = %+v, want observed session %q without guessing rejection", result, tc.wantSession)
			}
		})
	}
}

func TestDroidCompletionRequiresProcessExit(t *testing.T) {
	backend := newFakeDroid(t, map[string]string{
		"DROID_STREAM":            "{\"type\":\"completion\",\"finalText\":\"premature\"}\n",
		"DROID_HANG_AFTER_STREAM": "1",
	}, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "task", ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.Messages: // Emitted after completion: the parser has read both.
	case <-time.After(5 * time.Second):
		t.Fatal("fake Droid did not reach completion")
	}
	if session.TerminalObserved() {
		t.Error("completion became authoritative before the process exited")
	}
	cancel()
	_, result := collectDroid(t, session)
	if result.Status != "aborted" || result.Output != "" || session.TerminalObserved() {
		t.Fatalf("completion without a clean process exit was accepted: %+v", result)
	}
}

func TestDroidDrainsLargeFinalOutput(t *testing.T) {
	final := strings.Repeat("final 中文\n", 100000)
	event, err := json.Marshal(map[string]string{"type": "completion", "finalText": final})
	if err != nil {
		t.Fatal(err)
	}
	streamFile := filepath.Join(t.TempDir(), "stream.jsonl")
	if err := os.WriteFile(streamFile, event, 0o600); err != nil {
		t.Fatal(err)
	}
	backend := newFakeDroid(t, map[string]string{"DROID_STREAM_FILE": streamFile}, io.Discard)
	session, err := backend.Execute(context.Background(), "task", ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, result := collectDroid(t, session)
	if result.Status != "completed" || result.Output != final || !session.TerminalObserved() {
		t.Fatalf("final output truncated or misclassified: status=%s bytes=%d error=%s", result.Status, len(result.Output), result.Error)
	}
}

func TestDroidCancellationAndTimeout(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint("timeout=", timeout), func(t *testing.T) {
			isolateDroidSystemPromptFiles(t)
			b := newFakeDroid(t, map[string]string{"DROID_HANG": "1"}, io.Discard)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := ExecOptions{SystemPrompt: "private instructions", Timeout: 5 * time.Second}
			want := "aborted"
			if timeout {
				opts.Timeout = 500 * time.Millisecond
				want = "timeout"
			}
			s, err := b.Execute(ctx, "task", opts)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-s.Messages:
			case <-time.After(3 * time.Second):
				t.Fatal("fake Droid never started")
			}
			if !timeout {
				cancel()
			}
			_, r := collectDroid(t, s)
			if r.Status != want || r.Output != "" || r.Error == "" {
				t.Fatalf("result = %+v", r)
			}
		})
	}
}

func TestDroidSecretsNeverReachResultsOrLogs(t *testing.T) {
	const secret = "opaque-factory-credential"
	const systemPrompt = "private system instructions"
	isolateDroidSystemPromptFiles(t)
	var logs bytes.Buffer
	b := newFakeDroid(t, map[string]string{
		"FACTORY_API_KEY": secret,
		"DROID_STREAM":    "{\"type\":\"message\",\"role\":\"assistant\",\"text\":\"" + secret + "\"}\n{\"type\":\"error\",\"message\":\"Authentication failed: " + secret + " fk-12345678901234567890\"}\n",
		"DROID_STDERR":    secret, "DROID_FAIL": "1",
	}, &logs)
	s, err := b.Execute(context.Background(), "task", ExecOptions{SystemPrompt: systemPrompt, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	messages, r := collectDroid(t, s)
	visible := fmt.Sprint(messages, r, logs.String())
	if strings.Contains(visible, secret) || strings.Contains(visible, systemPrompt) || strings.Contains(visible, "fk-12345678901234567890") || !strings.Contains(visible, "REDACTED") {
		t.Fatalf("credentials not scrubbed: %s", visible)
	}
}

func TestDroidMissingExecutableAndUnsupportedMCP(t *testing.T) {
	b, err := New("droid", Config{ExecutablePath: missingAgentExecutable(t, "droid")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Execute(context.Background(), "task", ExecOptions{}); err == nil || !strings.Contains(err.Error(), "MULTICA_DROID_PATH") {
		t.Fatalf("missing executable error = %v", err)
	}
	if _, err := b.Execute(context.Background(), "task", ExecOptions{McpConfig: json.RawMessage(`{"mcpServers":{"example":{"command":"example"}}}`)}); err == nil || !strings.Contains(err.Error(), "managed MCP") {
		t.Fatalf("MCP error = %v", err)
	}
}

func TestDroidStartFailureAndEmptyCompletion(t *testing.T) {
	isolateDroidSystemPromptFiles(t)
	b := newFakeDroid(t, map[string]string{"DROID_STREAM": "{\"type\":\"message\",\"role\":\"assistant\",\"text\":\"Interim narration\"}\n{\"type\":\"completion\",\"finalText\":\"\"}\n"}, io.Discard)
	if _, err := b.Execute(context.Background(), "task", ExecOptions{SystemPrompt: "private instructions", Cwd: filepath.Join(t.TempDir(), "missing")}); err == nil || !strings.Contains(err.Error(), "start droid") {
		t.Fatalf("start error = %v", err)
	}
	s, err := b.Execute(context.Background(), "task", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, r := collectDroid(t, s)
	if r.Status != "completed" || r.Output != "" {
		t.Fatalf("interim text became the answer: %+v", r)
	}
	if CheckMinVersion("droid", "0.150.1") == nil || CheckMinVersion("droid", "1.8.0") != nil {
		t.Fatal("Droid protocol version gate is not enforced")
	}
	if !ResumeRejectionUndetectable("droid") {
		t.Fatal("Droid must not guess resume rejections")
	}
}

func TestDroidOversizedEventStopsProducer(t *testing.T) {
	b := newFakeDroid(t, map[string]string{"DROID_OVERSIZED": "1"}, io.Discard)
	session, err := b.Execute(context.Background(), "task", ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, result := collectDroid(t, session)
	if result.Status != "failed" || result.Output != "" || !strings.Contains(result.Error, "stdout read error") {
		t.Fatalf("protocol failure was misreported: %+v", result)
	}
}
