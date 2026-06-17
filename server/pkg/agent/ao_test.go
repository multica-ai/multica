package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildAOArgs(t *testing.T) {
	t.Parallel()
	args := buildAOArgs("hello", ExecOptions{
		Model:        "codex",
		SystemPrompt: "system",
		CustomArgs:   []string{"--open", "--prompt", "evil", "--agent", "claude-code", "--claim-pr", "12"},
	}, slog.Default())
	got := strings.Join(args, "\x00")
	if !strings.HasPrefix(got, "spawn\x00--prompt\x00hello") {
		t.Fatalf("unexpected prefix: %#v", args)
	}
	if strings.Contains(got, "evil") || strings.Contains(got, "--open") {
		t.Fatalf("blocked args leaked into argv: %#v", args)
	}
	if !containsString(args, "--agent") || !containsString(args, "claude-code") {
		t.Fatalf("custom --agent should be preserved and suppress model injection: %#v", args)
	}
	if !containsString(args, "--claim-pr") || !containsString(args, "12") {
		t.Fatalf("custom allowed args missing: %#v", args)
	}
}

func TestBuildAOSendArgs(t *testing.T) {
	t.Parallel()
	args := buildAOSendArgs("follow up", ExecOptions{
		ResumeSessionID: "cg-3",
		CustomArgs:      []string{"--agent", "codex", "--prompt", "evil", "--no-wait", "--timeout", "30"},
	}, slog.Default())
	got := strings.Join(args, "\x00")
	if !strings.HasPrefix(got, "send\x00cg-3\x00") {
		t.Fatalf("unexpected prefix: %#v", args)
	}
	if strings.Contains(got, "codex") || strings.Contains(got, "evil") {
		t.Fatalf("spawn-only args leaked into send argv: %#v", args)
	}
	if !containsString(args, "--no-wait") || !containsString(args, "--timeout") || !containsString(args, "30") {
		t.Fatalf("send-safe custom args missing: %#v", args)
	}
	if args[len(args)-1] != "follow up" {
		t.Fatalf("last arg = %q, want prompt; all=%#v", args[len(args)-1], args)
	}
}

func TestPrepareAOPrompt(t *testing.T) {
	t.Parallel()
	short, err := prepareAOPrompt("prompt", "system", t.TempDir())
	if err != nil {
		t.Fatalf("prepare short prompt: %v", err)
	}
	if short != "system\n\nprompt" {
		t.Fatalf("short prompt = %q", short)
	}

	aoProjectDir := t.TempDir()
	long, err := prepareAOPrompt(strings.Repeat("x", aoInlinePromptLimit+100), "", aoProjectDir)
	if err != nil {
		t.Fatalf("prepare long prompt: %v", err)
	}
	if !strings.Contains(long, ".multica-ao-dispatches") || !strings.Contains(long, "Read the full brief") {
		t.Fatalf("long prompt did not reference dispatch file: %q", long)
	}
	files, err := os.ReadDir(filepath.Join(aoProjectDir, ".multica-ao-dispatches"))
	if err != nil {
		t.Fatalf("read dispatch dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("dispatch files = %d, want 1", len(files))
	}
}

func TestExtractAOSessionID(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Session: cg-123":                             "cg-123",
		"Spawned session cg-rev-7 for project":        "cg-rev-7",
		`{"sessionId":"cg-42","status":"working"}`:    "cg-42",
		`{"data":[{"name":"cg-11","role":"worker"}]}`: "cg-11",
		"no useful session here":                      "",
	}
	for raw, want := range cases {
		if got := extractAOSessionID(raw); got != want {
			t.Fatalf("extractAOSessionID(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestAOBackendExecuteDispatchesSpawn(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	pwdFile := filepath.Join(tempDir, "pwd.txt")
	eventFile := filepath.Join(tempDir, "mymir-event.json")
	aoProjectDir := filepath.Join(tempDir, "ao-project")
	if err := os.MkdirAll(aoProjectDir, 0o755); err != nil {
		t.Fatalf("mkdir ao project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(aoProjectDir, "agent-orchestrator.yaml"), []byte("agents: {}\n"), 0o644); err != nil {
		t.Fatalf("write ao config: %v", err)
	}
	fakePath := filepath.Join(tempDir, "ao")
	writeTestExecutable(t, fakePath, []byte(fakeAOScript()))
	fakeWriter := filepath.Join(tempDir, "mymir-writer")
	writeTestExecutable(t, fakeWriter, []byte("#!/bin/sh\ncp \"$1\" \"$AO_EVENT_FILE\"\n"))

	backend, err := New("ao", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"AO_ARGS_FILE":                   argsFile,
			"AO_PWD_FILE":                    pwdFile,
			"AO_EVENT_FILE":                  eventFile,
			"CLAWGODE_MYMIR_AO_EVENT_WRITER": fakeWriter,
			"MULTICA_AO_WORKDIR":             aoProjectDir,
		},
	})
	if err != nil {
		t.Fatalf("new ao backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "do the thing", ExecOptions{
		Cwd:     filepath.Join(tempDir, "multica-task-workdir"),
		Model:   "codex",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q error=%q", result.Status, result.Error)
	}
	if result.SessionID != "cg-123" {
		t.Fatalf("session id = %q, want cg-123; output=%s", result.SessionID, result.Output)
	}
	if !strings.Contains(result.Output, "AO dispatch accepted") || !strings.Contains(result.Output, "cg-123") {
		t.Fatalf("result output missing dispatch evidence: %q", result.Output)
	}
	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
	wantPrefix := []string{"spawn", "--agent", "codex", "--prompt", "do the thing"}
	for i, want := range wantPrefix {
		if len(args) <= i || args[i] != want {
			t.Fatalf("argv[%d]=%q, want %q; all=%q", i, args, want, args)
		}
	}
	pwdRaw, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatalf("read pwd: %v", err)
	}
	if got := strings.TrimSpace(string(pwdRaw)); got != aoProjectDir {
		t.Fatalf("ao cwd = %q, want %q", got, aoProjectDir)
	}
	rawEvent, err := os.ReadFile(eventFile)
	if err != nil {
		t.Fatalf("read mymir event: %v", err)
	}
	var event aoMyMirEvent
	if err := json.Unmarshal(rawEvent, &event); err != nil {
		t.Fatalf("decode mymir event: %v", err)
	}
	if event.Operation != "spawn" || event.SessionID != "cg-123" || event.Status != "completed" {
		t.Fatalf("unexpected mymir event: %#v", event)
	}
	if event.AOCwd != aoProjectDir {
		t.Fatalf("mymir event ao cwd = %q, want %q", event.AOCwd, aoProjectDir)
	}
}

func TestAOBackendExecuteRoutesResumeToSend(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	aoProjectDir := filepath.Join(tempDir, "ao-project")
	if err := os.MkdirAll(aoProjectDir, 0o755); err != nil {
		t.Fatalf("mkdir ao project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(aoProjectDir, "agent-orchestrator.yaml"), []byte("projects: {}\n"), 0o644); err != nil {
		t.Fatalf("write ao config: %v", err)
	}
	fakePath := filepath.Join(tempDir, "ao")
	writeTestExecutable(t, fakePath, []byte(fakeAOScript()))

	backend, err := New("ao", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"AO_ARGS_FILE":       argsFile,
			"MULTICA_AO_WORKDIR": aoProjectDir,
		},
	})
	if err != nil {
		t.Fatalf("new ao backend: %v", err)
	}

	session, err := backend.Execute(context.Background(), "please adjust", ExecOptions{
		ResumeSessionID: "cg-3",
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q error=%q", result.Status, result.Error)
	}
	if result.SessionID != "cg-3" {
		t.Fatalf("session id = %q, want existing cg-3; output=%s", result.SessionID, result.Output)
	}
	if !strings.Contains(result.Output, "AO feedback routed") || !strings.Contains(result.Output, "cg-3") {
		t.Fatalf("result output missing routed evidence: %q", result.Output)
	}
	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
	wantPrefix := []string{"send", "cg-3", "please adjust"}
	for i, want := range wantPrefix {
		if len(args) <= i || args[i] != want {
			t.Fatalf("argv[%d]=%q, want %q; all=%q", i, args, want, args)
		}
	}
}

func TestAOBackendExecuteSurfacesFailure(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := filepath.Join(t.TempDir(), "ao")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\necho 'boom' >&2\nexit 2\n"))
	backend, err := New("ao", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new ao backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "boom") {
		t.Fatalf("error missing stderr: %q", result.Error)
	}
}

func TestResolveAOWorkdirRejectsBadConfiguredDir(t *testing.T) {
	t.Parallel()
	_, err := resolveAOWorkdir("", map[string]string{"MULTICA_AO_WORKDIR": t.TempDir()})
	if err == nil {
		t.Fatal("expected invalid configured AO workdir to fail")
	}
	if !strings.Contains(err.Error(), "agent-orchestrator.yaml") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func fakeAOScript() string {
	return `#!/bin/sh
if [ -n "$AO_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$AO_ARGS_FILE"
  done
fi
if [ -n "$AO_PWD_FILE" ]; then
  pwd > "$AO_PWD_FILE"
fi
printf 'Spawned session cg-123 for project clawgode\n'
`
}
