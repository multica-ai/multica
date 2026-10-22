package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOpencodeUsesV2Contract pins which version strings switch the backend onto
// the 2.x argv. The shapes matter: extractVersionLine keeps the whole matched
// line, and 2.x reports `opencode v2.0.10` where 1.x reports a bare `1.18.31`,
// so a prefix test against "2." would miss every real 2.x runtime.
func TestOpencodeUsesV2Contract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		version string
		builtin bool
		want    bool
	}{
		{"v2 as reported by the real CLI", "opencode v2.0.10", true, true},
		{"v2 bare semver", "2.0.10", true, true},
		{"v2 with leading v", "v2.1.0", true, true},
		{"future major", "opencode v3.0.0", true, true},
		{"v1 bare semver as reported by the real CLI", "1.18.31", true, false},
		{"v1 oldest supported", "1.1.54", true, false},
		{"unknown version fails closed onto 1.x", "", true, false},
		{"unparsable version fails closed onto 1.x", "opencode nightly", true, false},
		// A custom runtime profile wraps a binary this package did not choose,
		// so its version string does not establish the usage convention. Acting
		// on it would break working 1.x profiles on a guess.
		{"custom runtime never switches", "opencode v2.0.10", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := Config{CLIVersion: tc.version, BuiltinRuntime: tc.builtin}
			if got := opencodeUsesV2Contract(cfg); got != tc.want {
				t.Fatalf("opencodeUsesV2Contract(%q, builtin=%v) = %v, want %v",
					tc.version, tc.builtin, got, tc.want)
			}
		})
	}
}

// TestOpencodeModelArg covers folding the thinking level into the model string,
// which is how 2.x expresses what 1.x passed as `--variant`.
func TestOpencodeModelArg(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		model         string
		thinkingLevel string
		wantModel     string
		wantOK        bool
	}{
		{"no thinking level leaves the model alone", "anthropic/claude-sonnet-4-5", "", "anthropic/claude-sonnet-4-5", true},
		{"level folds onto the model", "anthropic/claude-sonnet-4-5", "high", "anthropic/claude-sonnet-4-5#high", true},
		{"neither set", "", "", "", true},
		// 2.x resolves the default model server-side, so there is no model
		// string to attach the variant to and the level cannot be expressed.
		{"level without a model is not representable", "", "high", "", false},
		// An explicitly pinned variant wins rather than growing a second '#',
		// which OpenCode would reject.
		{"explicit variant on the model wins", "anthropic/claude-sonnet-4-5#max", "high", "anthropic/claude-sonnet-4-5#max", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotModel, gotOK := opencodeModelArg(tc.model, tc.thinkingLevel)
			if gotModel != tc.wantModel || gotOK != tc.wantOK {
				t.Fatalf("opencodeModelArg(%q, %q) = (%q, %v), want (%q, %v)",
					tc.model, tc.thinkingLevel, gotModel, gotOK, tc.wantModel, tc.wantOK)
			}
		})
	}
}

const testMCPConfig = `{"mcpServers":{"probe":{"command":"node","args":["probe.js"]}}}`

// TestOpencodeCheckMCPSupport pins the refusal. 2.x has no channel that can
// carry MCP credentials without putting them where the agent can commit them,
// so a configured run fails instead of quietly starting without its servers.
func TestOpencodeCheckMCPSupport(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"no mcp config", "", false},
		{"empty object asks for nothing", `{"mcpServers":{}}`, false},
		{"configured servers are refused", testMCPConfig, true},
		{"native opencode shape is refused too", `{"mcp":{"probe":{"type":"local","command":["x"]}}}`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var raw json.RawMessage
			if tc.raw != "" {
				raw = json.RawMessage(tc.raw)
			}
			err := opencodeCheckMCPSupport(raw)
			if tc.wantErr {
				if !errors.Is(err, ErrOpenCodeV2MCPUnsupported) {
					t.Fatalf("expected ErrOpenCodeV2MCPUnsupported, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

// TestOpencodeV2ExecuteRefusesMCPConfig is the end-to-end half: the run must
// fail before anything starts, and nothing may be written into the workdir —
// that file is the whole reason MCP is refused here.
func TestOpencodeV2ExecuteRefusesMCPConfig(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	marker := filepath.Join(tempDir, "started.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\ntouch \""+marker+"\"\n"))

	workDir := t.TempDir()

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		CLIVersion:     "opencode v2.0.10",
		BuiltinRuntime: true,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, execErr := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:       workDir,
		McpConfig: json.RawMessage(testMCPConfig),
		Timeout:   5 * time.Second,
	})
	if !errors.Is(execErr, ErrOpenCodeV2MCPUnsupported) {
		t.Fatalf("expected the run to be refused, got %v", execErr)
	}

	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("refused run still started the CLI, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("refused run still wrote a config into the workdir, stat err = %v", err)
	}
}

// TestOpencodeV1StillDeliversMCPConfig guards the other side of the refusal:
// 1.x is unaffected and still projects mcp_config through its env channel.
func TestOpencodeV1StillDeliversMCPConfig(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	envFile := filepath.Join(tempDir, "env.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	script := "#!/bin/sh\ncat > /dev/null\nprintf '%s\\n' \"$OPENCODE_CONFIG_CONTENT\" > \"" + envFile + "\"\n" +
		"printf '{\"type\":\"step_start\",\"sessionID\":\"ses\",\"part\":{}}\\n'\n" +
		"printf '{\"type\":\"step_finish\",\"sessionID\":\"ses\",\"part\":{}}\\n'\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		CLIVersion:     "1.18.31",
		BuiltinRuntime: true,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:       t.TempDir(),
		McpConfig: json.RawMessage(testMCPConfig),
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("1.x must still accept mcp_config: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env capture: %v", err)
	}
	if !strings.Contains(string(got), "probe") {
		t.Fatalf("1.x lost its MCP env injection: %q", got)
	}
}

// TestOpencodeV2ArgvOmitsDirAndFoldsVariant is the end-to-end regression for
// GH #8586: on a 2.x runtime the daemon must not pass `--dir` (the CLI rejects
// the unknown flag and the run dies before it starts) and must express the
// thinking level through the model string instead of `--variant`.
func TestOpencodeV2ArgvOmitsDirAndFoldsVariant(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	pwdFile := filepath.Join(tempDir, "pwd.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeScript()))

	workDir := t.TempDir()

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		CLIVersion:     "opencode v2.0.10",
		BuiltinRuntime: true,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_ARGS_FILE": argsFile,
			"OPENCODE_PWD_FILE":  pwdFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:           workDir,
		Model:         "anthropic/claude-sonnet-4-5",
		ThinkingLevel: "high",
		Timeout:       5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")

	if containsString(args, "--dir") {
		t.Fatalf("2.x argv must not carry --dir, got %q", args)
	}
	if containsString(args, "--variant") {
		t.Fatalf("2.x argv must not carry --variant, got %q", args)
	}
	if !containsString(args, "anthropic/claude-sonnet-4-5#high") {
		t.Fatalf("expected the thinking level folded into the model string, got %q", args)
	}

	// Dropping --dir is only safe because cwd still anchors discovery, so the
	// PWD the child sees must remain the task workdir.
	gotPWD, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatalf("read PWD file: %v", err)
	}
	if strings.TrimSpace(string(gotPWD)) != workDir {
		t.Fatalf("expected PWD %q, got %q", workDir, strings.TrimSpace(string(gotPWD)))
	}
}

// TestOpencodeV1ArgvKeepsDirAndVariant is the other half of the contract: the
// 1.x path is what every existing runtime uses and must not shift.
func TestOpencodeV1ArgvKeepsDirAndVariant(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeScript()))

	workDir := t.TempDir()

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		CLIVersion:     "1.18.31",
		BuiltinRuntime: true,
		Logger:         slog.Default(),
		Env:            map[string]string{"OPENCODE_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:           workDir,
		Model:         "anthropic/claude-sonnet-4-5",
		ThinkingLevel: "high",
		Timeout:       5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")

	if !containsString(args, "--dir") {
		t.Fatalf("1.x argv must still carry --dir, got %q", args)
	}
	if !containsString(args, "--variant") {
		t.Fatalf("1.x argv must still carry --variant, got %q", args)
	}
	if containsString(args, "anthropic/claude-sonnet-4-5#high") {
		t.Fatalf("1.x must not fold the variant into the model, got %q", args)
	}
}

func TestOpencodeConnectionFromArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		args           []string
		wantServer     string
		wantStandalone bool
	}{
		{"nothing", []string{"run", "--format", "json"}, "", false},
		{"separate value", []string{"run", "--server", "http://127.0.0.1:4096"}, "http://127.0.0.1:4096", false},
		{"inline value", []string{"run", "--server=http://127.0.0.1:4096"}, "http://127.0.0.1:4096", false},
		{"standalone", []string{"run", "--standalone"}, "", true},
		{"trailing --server with no value is ignored", []string{"run", "--server"}, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, standalone := opencodeConnectionFromArgs(tc.args)
			if server != tc.wantServer || standalone != tc.wantStandalone {
				t.Fatalf("opencodeConnectionFromArgs(%q) = (%q, %v), want (%q, %v)",
					tc.args, server, standalone, tc.wantServer, tc.wantStandalone)
			}
		})
	}
}

// TestOpencodeSessionTracker covers the handoff the cancellation path depends
// on, including the nil receiver every direct-processEvents test relies on.
func TestOpencodeSessionTracker(t *testing.T) {
	t.Parallel()

	var nilTracker *opencodeSessionTracker
	nilTracker.set("ses_ignored") // must not panic
	if got := nilTracker.get(); got != "" {
		t.Fatalf("nil tracker should report no session, got %q", got)
	}

	tracker := &opencodeSessionTracker{}
	tracker.set("")
	if got := tracker.get(); got != "" {
		t.Fatalf("empty session id must be ignored, got %q", got)
	}
	tracker.set("ses_first")
	tracker.set("ses_second")
	if got := tracker.get(); got != "ses_first" {
		t.Fatalf("tracker should keep the first session id, got %q", got)
	}
}

// TestOpencodeProcessEventsPublishesSession pins that the scanner hands the
// session id to the tracker. Without it the cancellation path has no id to
// interrupt and a cancelled 2.x run keeps going server-side.
func TestOpencodeProcessEventsPublishesSession(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}, session: &opencodeSessionTracker{}}
	ch := make(chan Message, 8)
	lines := `{"type":"step_start","timestamp":1,"sessionID":"ses_abc","part":{}}` + "\n" +
		`{"type":"step_finish","timestamp":2,"sessionID":"ses_abc","part":{}}` + "\n"

	go func() {
		b.processEvents(strings.NewReader(lines), ch)
		close(ch)
	}()
	for range ch {
	}

	if got := b.session.get(); got != "ses_abc" {
		t.Fatalf("expected the scanner to publish the session id, got %q", got)
	}
}

func TestOpencodeInterruptSessionWithoutSessionIsNoop(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	marker := filepath.Join(tempDir, "called.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\ntouch \""+marker+"\"\n"))

	opencodeInterruptSession(opencodeRunConnection{cmd: NewCommand(fakePath, nil)}, "", slog.Default())

	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("interrupt must not run the CLI without a session id, stat err = %v", err)
	}
}

// TestOpencodeInterruptSessionStandaloneIsNoop: a private server is a child of
// the client, so the process-group signalling already stops it. Interrupting the
// default service here would signal an unrelated session.
func TestOpencodeInterruptSessionStandaloneIsNoop(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	marker := filepath.Join(tempDir, "called.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\ntouch \""+marker+"\"\n"))

	conn := opencodeRunConnection{cmd: NewCommand(fakePath, nil), standalone: true}
	opencodeInterruptSession(conn, "ses_abc", slog.Default())

	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("interrupt must not run for a standalone run, stat err = %v", err)
	}
}

// TestOpencodeInterruptSessionUsesRunConnection pins that the interrupt reaches
// the same service the run used. A run launched against `--server <url>` whose
// interrupt goes to the default background service reports success while the
// real session keeps working.
func TestOpencodeInterruptSessionUsesRunConnection(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	capture := filepath.Join(tempDir, "capture.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	script := "#!/bin/sh\n" +
		"{ printf 'env=%s\\n' \"$MULTICA_TEST_ENV\"; printf 'cwd=%s\\n' \"$PWD\"; " +
		"for arg in \"$@\"; do printf '%s\\n' \"$arg\"; done; } > \"" + capture + "\"\n"
	writeTestExecutable(t, fakePath, []byte(script))

	workDir := t.TempDir()
	conn := opencodeRunConnection{
		cmd:    NewCommand(fakePath, nil),
		server: "http://127.0.0.1:54321",
		env:    []string{"MULTICA_TEST_ENV=task-marker"},
		dir:    workDir,
	}
	opencodeInterruptSession(conn, "ses_abc", slog.Default())

	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	got := string(raw)

	// The shell reports $PWD with symlinks resolved, which on macOS turns
	// /tmp into /private/tmp.
	wantCwd := workDir
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		wantCwd = resolved
	}

	for _, want := range []string{
		"api", "POST", "/api/session/ses_abc/interrupt",
		"--server", "http://127.0.0.1:54321",
		"env=task-marker",
		"cwd=" + wantCwd,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("interrupt lost %q from the run connection:\n%s", want, got)
		}
	}
}

// TestOpencodeInterruptSessionBoundsPipeWait: CombinedOutput waits for EOF on
// the output pipes, so an `api` process that exits while a descendant still
// holds them would block the termination path behind this call indefinitely.
func TestOpencodeInterruptSessionBoundsPipeWait(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	// Exits immediately but leaves a descendant holding stdout/stderr.
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nsleep 7 &\nexit 0\n"))

	start := time.Now()
	opencodeInterruptSession(opencodeRunConnection{cmd: NewCommand(fakePath, nil)}, "ses_abc", slog.Default())
	if elapsed := time.Since(start); elapsed > opencodeInterruptTimeout+time.Second {
		t.Fatalf("interrupt exceeded its bound waiting for descendant pipe EOF: %s", elapsed)
	}
}

// TestOpencodeInterruptSessionSurvivesFailure keeps the call best-effort: a
// service that is gone or answers non-zero must not stop the caller, because
// the process-group signalling behind it is what the 1.x path always relied on.
func TestOpencodeInterruptSessionSurvivesFailure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\necho 'connection refused' >&2\nexit 1\n"))

	opencodeInterruptSession(opencodeRunConnection{cmd: NewCommand(fakePath, nil)}, "ses_abc", slog.Default())
}

// TestOpencodeV2CancelInterruptsThroughRunConnection is the end-to-end version
// of the connection test: it proves Execute actually threads the run's server
// selection, environment and workdir into the interrupt. A unit test on
// opencodeInterruptSession alone would keep passing if that wiring regressed,
// and the failure mode is quiet — the interrupt reports success against the
// default service while the real session keeps working.
func TestOpencodeV2CancelInterruptsThroughRunConnection(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	capture := filepath.Join(tempDir, "interrupt.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	// `api` invocations record their context; `run` streams one event and then
	// blocks so the test can cancel it mid-flight.
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"api\" ]; then\n" +
		"  { printf 'env=%s\\n' \"$MULTICA_TEST_ENV\"; for arg in \"$@\"; do printf '%s\\n' \"$arg\"; done; } > \"" + capture + "\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"cat > /dev/null\n" +
		"printf '{\"type\":\"text\",\"sessionID\":\"ses_live\",\"part\":{\"type\":\"text\",\"text\":\"ready\"}}\\n'\n" +
		"while :; do sleep 1; done\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		CLIVersion:     "opencode v2.0.10",
		BuiltinRuntime: true,
		Logger:         slog.Default(),
		Env:            map[string]string{"MULTICA_TEST_ENV": "task-marker"},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workDir := t.TempDir()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:        workDir,
		Timeout:    20 * time.Second,
		CustomArgs: []string{"--server", "http://127.0.0.1:54321"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	select {
	case <-session.Messages:
	case <-time.After(10 * time.Second):
		t.Fatal("fake CLI never became ready")
	}

	cancel()
	for range session.Messages {
	}
	<-session.Result

	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("cancelling a 2.x run did not interrupt the session: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		"/api/session/ses_live/interrupt",
		"--server", "http://127.0.0.1:54321",
		"env=task-marker",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("cancel lost %q from the run connection:\n%s", want, got)
		}
	}
}
