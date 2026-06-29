package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsTraecliBackend(t *testing.T) {
	t.Parallel()
	b, err := New("traecli", Config{ExecutablePath: "/nonexistent/trae-cli"})
	if err != nil {
		t.Fatalf("New(traecli) error: %v", err)
	}
	if _, ok := b.(*traecliBackend); !ok {
		t.Fatalf("expected *traecliBackend, got %T", b)
	}
}

func TestListModelsTraecliReturnsStaticCatalog(t *testing.T) {
	t.Parallel()
	models, err := ListModels(context.Background(), "traecli", "")
	if err != nil {
		t.Fatalf("ListModels(traecli) error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected a non-empty static catalog for traecli")
	}
	var sawDefault bool
	for _, m := range models {
		if m.Provider == "" {
			t.Errorf("model %q has no provider tag", m.ID)
		}
		gotProvider, gotModel := splitTraecliModel(m.ID)
		if gotProvider != m.Provider {
			t.Errorf("model %q: ID provider %q != Provider field %q", m.ID, gotProvider, m.Provider)
		}
		if gotModel == "" {
			t.Errorf("model %q split to an empty model name", m.ID)
		}
		if m.Default {
			sawDefault = true
		}
	}
	if !sawDefault {
		t.Error("expected a default model in the traecli catalog")
	}
}

func TestSplitTraecliModel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in           string
		wantProvider string
		wantModel    string
	}{
		{"", "", ""},
		{"  ", "", ""},
		{"gpt-4o", "", "gpt-4o"},
		{"openai/gpt-4o", "openai", "gpt-4o"},
		{"doubao/doubao-seed-1.6", "doubao", "doubao-seed-1.6"},
		// OpenRouter model IDs themselves contain a slash; only the FIRST
		// separator splits provider from model.
		{"openrouter/anthropic/claude-3-5-sonnet", "openrouter", "anthropic/claude-3-5-sonnet"},
		// A leading slash has no provider before it — treat as bare model.
		{"/weird", "", "/weird"},
	}
	for _, tc := range cases {
		gotP, gotM := splitTraecliModel(tc.in)
		if gotP != tc.wantProvider || gotM != tc.wantModel {
			t.Errorf("splitTraecliModel(%q) = (%q, %q), want (%q, %q)", tc.in, gotP, gotM, tc.wantProvider, tc.wantModel)
		}
	}
}

func TestBuildTraecliArgs(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	args := buildTraecliArgs("do the thing", "/tmp/traj.json", ExecOptions{
		Cwd:   cwd,
		Model: "doubao/doubao-seed-1.6",
	}, slog.Default())

	// Must start with the hardcoded subcommand + task + simple console.
	if len(args) < 4 || args[0] != "run" || args[1] != "do the thing" {
		t.Fatalf("unexpected arg prefix: %q", args)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--console-type simple",
		"--working-dir " + cwd,
		"--trajectory-file /tmp/traj.json",
		"--provider doubao",
		"--model doubao-seed-1.6",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q; got %q", want, joined)
		}
	}
}

func TestBuildTraecliArgsBareModelOmitsProvider(t *testing.T) {
	t.Parallel()
	args := buildTraecliArgs("task", "/tmp/t.json", ExecOptions{Model: "gpt-4o"}, slog.Default())
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--provider") {
		t.Errorf("bare model must not emit --provider, got %q", joined)
	}
	if !strings.Contains(joined, "--model gpt-4o") {
		t.Errorf("expected --model gpt-4o, got %q", joined)
	}
}

func TestTraecliBlockedArgsFiltering(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "trae-cli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliRunScript(fakeTraecliOpts{argsFile: argsFile, success: true, exitCode: 0, writeTraj: true})))

	backend, err := New("traecli", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"TRAECLI_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "the task", ExecOptions{
		Timeout: 5 * time.Second,
		// Users must not be able to inject a second subcommand, override the
		// model/provider/console-type, or hijack the trajectory file.
		CustomArgs: []string{"interactive", "--model", "evil", "--trajectory-file", "/tmp/hijack", "--max-steps", "42"},
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
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	joined := strings.Join(lines, " ")

	if lines[0] != "run" {
		t.Fatalf("arg[0] = %q, want run", lines[0])
	}
	// Blocked custom args must be filtered out.
	for _, blocked := range []string{"interactive", "/tmp/hijack", "evil"} {
		for _, got := range lines {
			if got == blocked {
				t.Errorf("blocked custom arg %q survived filtering: %q", blocked, lines)
			}
		}
	}
	// The hardcoded trajectory file must appear exactly once and not be the
	// user's hijack path.
	if strings.Count(joined, "--trajectory-file") != 1 {
		t.Errorf("expected exactly one --trajectory-file, got %q", joined)
	}
	// An allowed custom arg must survive.
	if !strings.Contains(joined, "--max-steps 42") {
		t.Errorf("expected allowed custom arg --max-steps 42 to survive, got %q", joined)
	}
}

func TestTraecliSuccessfulExecution(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "trae-cli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliRunScript(fakeTraecliOpts{success: true, exitCode: 0, writeTraj: true})))

	backend, err := New("traecli", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "hello", ExecOptions{
		Model:   "doubao/doubao-seed-1.6",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var sawText bool
	messagesDone := make(chan struct{})
	go func() {
		defer close(messagesDone)
		for msg := range session.Messages {
			if msg.Type == MessageText && strings.Contains(msg.Content, "Hello from Trae") {
				sawText = true
			}
		}
	}()

	result := <-session.Result
	<-messagesDone

	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if !sawText {
		t.Error("expected a streamed MessageText containing the assistant output")
	}
	// Stateless CLI must not advertise a resumable session id.
	if result.SessionID != "" {
		t.Errorf("expected empty SessionID for stateless trae-cli, got %q", result.SessionID)
	}
	// Usage must be parsed from the trajectory and keyed by the requested model.
	u, ok := result.Usage["doubao/doubao-seed-1.6"]
	if !ok {
		t.Fatalf("expected usage keyed by requested model, got %v", result.Usage)
	}
	if u.InputTokens != 150 || u.OutputTokens != 75 || u.CacheReadTokens != 10 {
		t.Errorf("unexpected usage totals: %+v", u)
	}
}

func TestTraecliAgentFailureExitsZeroSuccessFalse(t *testing.T) {
	t.Parallel()
	// The important real-world failure path: trae-cli run exits 0 even when
	// the agent did not complete the task (bad API key, max-steps). The only
	// reliable signal is trajectory.success=false. Verified against trae-cli
	// v0.1.0, which exited 0 with success=false on an invalid key.
	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "trae-cli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliRunScript(fakeTraecliOpts{success: false, exitCode: 0, writeTraj: true})))

	backend, err := New("traecli", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("exit 0 + success=false must map to failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "success=false") {
		t.Errorf("expected error to mention success=false, got %q", result.Error)
	}
}

func TestTraecliStartupFailureExitsNonZeroNoTrajectory(t *testing.T) {
	t.Parallel()
	// Startup/config failure: exit 1 with NO trajectory written (verified:
	// real trae-cli exits 1 before writing a trajectory when no config file
	// resolves).
	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "trae-cli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliRunScript(fakeTraecliOpts{success: false, exitCode: 1, writeTraj: false})))

	backend, err := New("traecli", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed on non-zero exit, got %q", result.Status)
	}
	if result.Error == "" {
		t.Error("expected a non-empty error message on failure")
	}
}

func TestTraecliConfigMissingAddsActionableHint(t *testing.T) {
	t.Parallel()
	// When the startup failure is the "Config file not found" case, the
	// backend should append an actionable TRAE_CONFIG_FILE hint.
	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "trae-cli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliRunScript(fakeTraecliOpts{
		success:    false,
		exitCode:   1,
		writeTraj:  false,
		stderrLine: "Error: Config file not found. Please specify a valid config file",
	})))

	backend, err := New("traecli", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "TRAE_CONFIG_FILE") {
		t.Errorf("expected an actionable TRAE_CONFIG_FILE hint, got %q", result.Error)
	}
}

func TestTraecliReadTrajectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "traj.json")
	body := `{
  "provider": "doubao",
  "model": "doubao-seed-1.6",
  "success": true,
  "final_result": "done",
  "execution_time": 1.5,
  "llm_interactions": [
    {"response": {"usage": {"input_tokens": 10, "output_tokens": 5, "cache_read_input_tokens": 2}}},
    {"response": {"usage": {"input_tokens": 20, "output_tokens": 7, "cache_read_input_tokens": 3}}}
  ]
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write traj: %v", err)
	}
	traj, ok := readTraecliTrajectory(path)
	if !ok {
		t.Fatal("expected trajectory to parse")
	}
	if !traj.Success || traj.FinalResult != "done" {
		t.Errorf("unexpected trajectory fields: %+v", traj)
	}
	u := traecliUsageFromTrajectory(traj)
	if u.InputTokens != 30 || u.OutputTokens != 12 || u.CacheReadTokens != 5 {
		t.Errorf("unexpected summed usage: %+v", u)
	}

	// Missing / unparseable files degrade to (nil, false).
	if _, ok := readTraecliTrajectory(filepath.Join(dir, "nope.json")); ok {
		t.Error("expected missing trajectory to return ok=false")
	}
}

// fakeTraecliOpts parameterizes the fake `trae-cli` so tests can reproduce the
// real binary's distinct outcomes (verified against trae-cli v0.1.0):
//   - success run:        exit 0, trajectory success=true
//   - agent failure:      exit 0, trajectory success=false (bad key / max-steps)
//   - startup failure:    exit 1, NO trajectory (e.g. missing trae_config.yaml)
type fakeTraecliOpts struct {
	argsFile   string
	success    bool   // value written to trajectory.success
	exitCode   int    // process exit code
	writeTraj  bool   // whether a trajectory file is written
	stderrLine string // optional line emitted to stderr
}

// fakeTraecliRunScript impersonates `trae-cli run` for unit tests. It logs
// received argv to TRAECLI_ARGS_FILE (when set), optionally writes a trajectory
// JSON to the path following --trajectory-file, prints assistant text to
// stdout, optionally emits a stderr line, and exits with the configured code.
func fakeTraecliRunScript(o fakeTraecliOpts) string {
	argsLog := ""
	if o.argsFile != "" {
		argsLog = `
if [ -n "$TRAECLI_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$TRAECLI_ARGS_FILE"
  done
fi`
	}
	successJSON := "false"
	if o.success {
		successJSON = "true"
	}
	stderrEmit := ""
	if o.stderrLine != "" {
		stderrEmit = "\necho " + shQuote(o.stderrLine) + " 1>&2"
	}
	trajBlock := ""
	if o.writeTraj {
		trajBlock = `
if [ -n "$traj" ]; then
  cat > "$traj" <<'JSON'
{
  "provider": "doubao",
  "model": "doubao-seed-1.6",
  "success": ` + successJSON + `,
  "final_result": "task complete",
  "execution_time": 2.0,
  "llm_interactions": [
    {"response": {"usage": {"input_tokens": 100, "output_tokens": 50, "cache_read_input_tokens": 10}}},
    {"response": {"usage": {"input_tokens": 50, "output_tokens": 25, "cache_read_input_tokens": 0}}}
  ]
}
JSON
fi`
	}
	return `#!/bin/sh` + argsLog + `
# Find the value following --trajectory-file.
traj=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--trajectory-file" ]; then
    traj="$arg"
  fi
  prev="$arg"
done

echo "Hello from Trae CLI"
echo "ran a tool"` + stderrEmit + trajBlock + `

exit ` + fmt.Sprintf("%d", o.exitCode) + `
`
}

// shQuote single-quotes a string for safe embedding in the generated /bin/sh.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
