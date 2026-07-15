package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildPiArgsNoToolAllowlist(t *testing.T) {
	// Extension tools registered via Pi's registerTool() must not be
	// filtered out by a hardcoded --tools allowlist. Omitting --tools
	// lets Pi use its full tool registry. See #2379.
	args := buildPiArgs("test prompt", "/tmp/session.jsonl", ExecOptions{}, slog.Default())
	for i, arg := range args {
		if arg == "--tools" {
			t.Errorf("buildPiArgs emits --tools %q; should not restrict tool registry (see #2379)", args[i+1])
		}
	}
}

func TestBuildPiArgsBasicFlags(t *testing.T) {
	args := buildPiArgs("hello world", "/tmp/s.jsonl", ExecOptions{
		Model:        "anthropic/claude-sonnet-4-20250514",
		SystemPrompt: "be helpful",
	}, slog.Default())

	joined := strings.Join(args, " ")
	for _, want := range []string{"-p", "--mode json", "--session /tmp/s.jsonl", "--provider anthropic", "--model claude-sonnet-4-20250514", "--append-system-prompt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in args, got: %v", want, args)
		}
	}

	// Prompt must be the last positional argument.
	if args[len(args)-1] != "hello world" {
		t.Errorf("prompt should be last arg, got %q", args[len(args)-1])
	}
}

func TestBuildPiArgsCustomArgsAppended(t *testing.T) {
	// Users can still restrict tools via custom_args if desired.
	args := buildPiArgs("prompt", "/tmp/s.jsonl", ExecOptions{
		CustomArgs: []string{"--tools", "read,bash"},
	}, slog.Default())

	found := false
	for i, arg := range args {
		if arg == "--tools" && i+1 < len(args) && args[i+1] == "read,bash" {
			found = true
		}
	}
	if !found {
		t.Errorf("custom --tools should pass through via custom_args, got: %v", args)
	}
}

// TestPiExecuteAttachesStdinPipe verifies that the Pi backend spawns the
// child with an explicit stdin pipe (FIFO) instead of leaving cmd.Stdin
// nil. Without an explicit pipe, Pi has been observed to block under
// systemd waiting for stdin events (#2188); attaching and immediately
// closing a pipe delivers a clean EOF on a FIFO and unblocks Pi.
//
// The probe is structural rather than behavioral: a shell script in
// place of `pi` inspects /proc/self/fd/0 and only emits a valid event
// stream if stdin is a FIFO. If the fix regresses (stdin nil → /dev/null
// char device), the fake exits non-zero and the test fails.
func TestPiExecuteAttachesStdinPipe(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		// /proc/self/fd/0 is Linux-specific; skipping elsewhere keeps
		// the assertion portable without losing CI coverage.
		t.Skip("stdin fd inspection relies on /proc/self/fd/0")
	}

	fakePath := filepath.Join(t.TempDir(), "pi")
	script := "#!/bin/sh\n" +
		"kind=$(stat -c '%F' -L /proc/self/fd/0 2>/dev/null || echo unknown)\n" +
		"case \"$kind\" in\n" +
		"  fifo|*pipe*)\n" +
		"    printf '%s\\n' '{\"type\":\"agent_start\"}'\n" +
		"    printf '%s\\n' '{\"type\":\"turn_end\",\"message\":{\"role\":\"assistant\",\"model\":\"test\",\"usage\":{\"input\":1,\"output\":1,\"cacheRead\":0,\"cacheWrite\":0,\"totalTokens\":2}}}'\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"esac\n" +
		"printf 'stdin was %s; expected fifo\\n' \"$kind\" >&2\n" +
		"exit 1\n"
	writeTestExecutable(t, fakePath, []byte(script))

	cfg, cwd := providerCommandTestConfig(t, fakePath, slog.Default())
	backend, err := New("pi", cfg)
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:             cwd,
		Timeout:         5 * time.Second,
		ResumeSessionID: "0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed (stdin attached as fifo), got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// piEventStreamScript builds a sh script that prints each JSON event on
// its own stdout line. Fixtures must not contain single quotes.
func piEventStreamScript(events []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, e := range events {
		b.WriteString("printf '%s\\n' '")
		b.WriteString(e)
		b.WriteString("'\n")
	}
	return b.String()
}

func newPiSessionSecurityBackend(t *testing.T) (Backend, Config, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("secure Pi session descriptor handoff is currently Linux-only")
	}

	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript([]string{
		`{"type":"agent_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1}}}`,
	})))
	cfg, cwd := providerCommandTestConfig(t, fakePath, slog.Default())
	backend, err := New("pi", cfg)
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}
	return backend, cfg, cwd
}

func TestPiExecuteStoresNewSessionUnderTaskTempDir(t *testing.T) {
	backend, cfg, cwd := newPiSessionSecurityBackend(t)
	ownerHome := t.TempDir()
	t.Setenv("HOME", ownerHome)

	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Cwd: cwd, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if filepath.IsAbs(result.SessionID) || strings.ContainsAny(result.SessionID, `/\\`) {
		t.Fatalf("SessionID = %q, want opaque path-free identifier", result.SessionID)
	}
	wantPath := filepath.Join(cfg.TaskStateDir, "pi-sessions", result.SessionID+".jsonl")
	info, err := os.Lstat(wantPath)
	if err != nil {
		t.Fatalf("task-private session file %q: %v", wantPath, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("task-private session mode = %v, want regular file", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(ownerHome, ".multica", "pi-sessions")); !os.IsNotExist(err) {
		t.Fatalf("daemon-owner Pi session directory must not be created, stat err=%v", err)
	}
}

func TestPiExecuteRejectsCallerSelectedSessionPathsBeforeCreation(t *testing.T) {
	tests := map[string]func(root, taskTemp string) string{
		"absolute outside task root": func(root, _ string) string {
			return filepath.Join(root, "owner-session.jsonl")
		},
		"parent traversal": func(root, taskState string) string {
			return filepath.Join(taskState, "..", filepath.Base(root), "traversal-session.jsonl")
		},
	}
	for name, sessionID := range tests {
		t.Run(name, func(t *testing.T) {
			backend, cfg, cwd := newPiSessionSecurityBackend(t)
			outside := t.TempDir()
			requestedPath := sessionID(outside, cfg.TaskStateDir)
			session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
				Cwd:             cwd,
				Timeout:         5 * time.Second,
				ResumeSessionID: requestedPath,
			})
			if err == nil {
				if session != nil {
					go func() {
						for range session.Messages {
						}
						<-session.Result
					}()
				}
				t.Fatalf("Execute accepted caller-selected session path %q", requestedPath)
			}
			if _, statErr := os.Lstat(requestedPath); !os.IsNotExist(statErr) {
				t.Fatalf("caller-selected path %q was touched, stat err=%v", requestedPath, statErr)
			}
		})
	}
}

func TestPiExecuteRejectsSymlinkSessionBeforeProcessStart(t *testing.T) {
	backend, cfg, cwd := newPiSessionSecurityBackend(t)
	sessionDir := filepath.Join(cfg.TaskStateDir, "pi-sessions")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("create session dir: %v", err)
	}
	target := filepath.Join(t.TempDir(), "owner-secret")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(sessionDir, "0123456789abcdef0123456789abcdef.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
		Cwd:             cwd,
		Timeout:         5 * time.Second,
		ResumeSessionID: "0123456789abcdef0123456789abcdef",
	})
	if err == nil {
		if session != nil {
			go func() {
				for range session.Messages {
				}
				<-session.Result
			}()
		}
		t.Fatal("Execute accepted symlink session file")
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil || string(got) != "unchanged" {
		t.Fatalf("symlink target changed: bytes=%q err=%v", got, readErr)
	}
}

func TestPiExecuteRequiresTaskStateDirBeforeProcessStart(t *testing.T) {
	backend, cfg, cwd := newPiSessionSecurityBackend(t)
	outsidePath := filepath.Join(t.TempDir(), "owner-session.jsonl")
	pi := backend.(*piBackend)
	pi.cfg.TaskStateDir = ""

	session, err := pi.Execute(context.Background(), "prompt", ExecOptions{
		Cwd:             cwd,
		Timeout:         5 * time.Second,
		ResumeSessionID: outsidePath,
	})
	if err == nil {
		if session != nil {
			go func() {
				for range session.Messages {
				}
				<-session.Result
			}()
		}
		t.Fatal("Execute succeeded without TaskStateDir")
	}
	if _, statErr := os.Lstat(outsidePath); !os.IsNotExist(statErr) {
		t.Fatalf("path was touched without TaskStateDir, stat err=%v (original state root=%q)", statErr, cfg.TaskStateDir)
	}
}

func TestPiSessionRejectsHardLinkedFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("secure Pi session descriptor handoff is currently Linux-only")
	}
	root := t.TempDir()
	sessionDir := filepath.Join(root, "pi-sessions")
	if err := os.Mkdir(sessionDir, 0o700); err != nil {
		t.Fatalf("create session directory: %v", err)
	}
	sessionID := "0123456789abcdef0123456789abcdef"
	path := filepath.Join(sessionDir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if err := os.Link(path, filepath.Join(t.TempDir(), "foreign-link")); err != nil {
		t.Fatalf("create hard link: %v", err)
	}

	if _, _, err := preparePiSession(root, sessionID); err == nil || !strings.Contains(err.Error(), "link count") {
		t.Fatalf("preparePiSession hard-linked file error = %v, want link-count rejection", err)
	}
}

func TestPiSessionInheritedFDResistsPathReplacement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("secure Pi session descriptor handoff is currently Linux-only")
	}
	root := t.TempDir()
	sessionID := "0123456789abcdef0123456789abcdef"
	_, sessionFile, err := preparePiSession(root, sessionID)
	if err != nil {
		t.Fatalf("preparePiSession: %v", err)
	}
	defer sessionFile.Close()

	path := filepath.Join(root, "pi-sessions", sessionID+".jsonl")
	originalPath := filepath.Join(root, "pi-sessions", "original-unlinked.jsonl")
	if err := os.Rename(path, originalPath); err != nil {
		t.Fatalf("rename verified session: %v", err)
	}
	if err := os.WriteFile(path, []byte("replacement\n"), 0o600); err != nil {
		t.Fatalf("write replacement session: %v", err)
	}

	cmd := exec.Command("/bin/sh", "-c", `printf 'child-append\n' >> "$1"`, "sh", sessionFile.childPath())
	cmd.ExtraFiles = []*os.File{sessionFile.file}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("append through inherited descriptor: %v, output=%s", err, output)
	}

	original, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("read original inode: %v", err)
	}
	replacement, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replacement path: %v", err)
	}
	if string(original) != "child-append\n" {
		t.Fatalf("original inode bytes = %q, want inherited-FD append", original)
	}
	if string(replacement) != "replacement\n" {
		t.Fatalf("replacement path was modified: %q", replacement)
	}
}

func TestPiExecutionFailsClosedWithoutSecureDescriptorHandoff(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux supports secure Pi session descriptor handoff")
	}
	_, _, err := preparePiSession(t.TempDir(), "0123456789abcdef0123456789abcdef")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("preparePiSession error = %v, want platform fail-closed error", err)
	}
}

// TestPiExecuteRetainsOnlyLastTurnOutput verifies turn_start resets the
// output buffer so Result.Output keeps only the final turn's text.
func TestPiExecuteRetainsOnlyLastTurnOutput(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("secure Pi session descriptor handoff is currently Linux-only")
	}

	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"intermediate"}}`,
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"echo hi"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"hi"}]},"isError":false}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1}}}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"final"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":" "}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"answer"}}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":2,"output":2}}}`,
	}
	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript(events)))

	cfg, cwd := providerCommandTestConfig(t, fakePath, slog.Default())
	backend, err := New("pi", cfg)
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:             cwd,
		Timeout:         5 * time.Second,
		ResumeSessionID: "0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "final answer" {
			t.Fatalf("Output: got %q, want %q", result.Output, "final answer")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestStripPiToolCallMarkup(t *testing.T) {
	tests := map[string]string{
		`before call:bash{command:<|"|>cd repo/path && ls -F<|"|>}<tool_call|> after`:                           "before  after",
		`before call:read{path:<|"|>repo/path/roles/example/verify.yml<|"|>} after`:                             "before  after",
		`before response:bash{command:<|"|>multica issue comment list issue-id --all --output json<|"|>} after`: "before  after",
		`before call:bash{command:<|"|>printf '{"key":"value"}'<|"|>} after`:                                    "before  after",
		`before <|turn>model after`: "before  after",
	}
	for in, want := range tests {
		got := stripPiToolCallMarkup(in)
		if got != want {
			t.Fatalf("unexpected stripped text: %q, want %q", got, want)
		}
	}
}

func TestDrainPiTextBufferSplitToolCall(t *testing.T) {
	chunks := []string{
		"before ca",
		`ll:bash{command:<|"|>ls -R repo/path`,
		`/roles/example<|"|>}`,
		" after",
	}
	var buf strings.Builder
	var got strings.Builder
	for _, chunk := range chunks {
		got.WriteString(drainPiTextBuffer(&buf, chunk))
	}
	got.WriteString(flushPiTextBuffer(&buf))
	if got.String() != "before  after" {
		t.Fatalf("unexpected streamed text: %q", got.String())
	}
}

func TestDrainPiTextBufferSplitControlToken(t *testing.T) {
	chunks := []string{"before <|tu", "rn>model after"}
	var buf strings.Builder
	var got strings.Builder
	for _, chunk := range chunks {
		got.WriteString(drainPiTextBuffer(&buf, chunk))
	}
	got.WriteString(flushPiTextBuffer(&buf))
	if got.String() != "before  after" {
		t.Fatalf("unexpected streamed text: %q", got.String())
	}
}

func TestFlushPiTextBufferKeepsUnmatchedToolPrefixes(t *testing.T) {
	tests := []string{
		"plain response: see below",
		"plain call: see below",
		`plain call:bash{command:<|"|>unterminated`,
	}
	for _, want := range tests {
		var buf strings.Builder
		got := drainPiTextBuffer(&buf, want)
		got += flushPiTextBuffer(&buf)
		if got != want {
			t.Fatalf("unexpected flushed text: %q, want %q", got, want)
		}
	}
}
