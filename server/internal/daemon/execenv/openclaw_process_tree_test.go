//go:build !windows

package execenv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Regression tests for the task-critical half of MUL-5467. prepareOpenclawConfig
// shells out to `openclaw config file` / `openclaw config get ...` while
// preparing a task's execution environment, so both OpenClaw misbehaviours land
// on the path between "task claimed" and "agent started":
//
//   - openclaw forks a long-lived `openclaw-config` helper that inherits
//     stdout/stderr. With cmd.Output(), os/exec waits for those pipes to reach
//     EOF, which never comes while the helper lives — and cancelling the context
//     kills the direct child without unblocking that wait, so openclawCLITimeout
//     could not bound the call.
//   - `openclaw config file` and `openclaw agents list` print the correct answer
//     in ~250ms and then do not exit, so waiting for exit turned two working
//     commands into a task-fatal error.

// writeHelperForkingOpenclaw creates a fake openclaw with the first shape: emit
// JSON on stdout, fork a helper that keeps the inherited stdout/stderr open far
// longer than the test would wait, then exit 0.
func writeHelperForkingOpenclaw(t *testing.T, pidFile string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	body := `#!/bin/sh
( echo $$ > "` + pidFile + `"; sleep 300 ) &
# Make the helper's registration deterministic: without this the parent can
# exit and the group be reaped before the helper runs, leaving the test with
# no pid to assert on.
while [ ! -s "` + pidFile + `" ]; do sleep 0.01; done
echo '{}'
exit 0
`
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}
	return bin
}

func readHelperPid(t *testing.T, pidFile string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("helper never wrote its pid to %s", pidFile)
	return 0
}

func helperGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestExecOpenclawCLIReturnsDespitePipeHoldingHelper is the assertion that
// makes openclawCLITimeout meaningful: the call must come back on the direct
// child's exit, not on the helper's lifetime and not on the deadline.
func TestExecOpenclawCLIReturnsDespitePipeHoldingHelper(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	bin := writeHelperForkingOpenclaw(t, pidFile)

	// A deliberately generous deadline: before the fix this hung past it, so a
	// tight ctx would have hidden the bug behind a plausible-looking timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	out, err := execOpenclawCLI(ctx, bin, "config", "get", "--json")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("execOpenclawCLI: %v", err)
	}
	if strings.TrimSpace(out) != "{}" {
		t.Errorf("stdout = %q, want {}", out)
	}
	if elapsed > 15*time.Second {
		t.Errorf("execOpenclawCLI took %v — it waited on the helper instead "+
			"of returning once openclaw itself exited", elapsed)
	}
}

// TestExecOpenclawCLIReapsForkedHelper pins the cleanup half. Task preparation
// runs per task, and this is where the orphan `openclaw-config` processes came
// from. It is also what the reverted cmd.WaitDelay backstop could not do.
func TestExecOpenclawCLIReapsForkedHelper(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	bin := writeHelperForkingOpenclaw(t, pidFile)

	if _, err := execOpenclawCLI(context.Background(), bin, "config", "file"); err != nil {
		t.Fatalf("execOpenclawCLI: %v", err)
	}

	pid := readHelperPid(t, pidFile)
	if !helperGone(pid, 5*time.Second) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("forked helper (pid %d) survived execOpenclawCLI — the "+
			"orphan leak is back", pid)
	}
}

// TestExecOpenclawCLIDoesNotSalvagePartialJSON pins that a `--json` subcommand
// still streaming when the deadline arrives is an error, not a truncated success.
func TestExecOpenclawCLIDoesNotSalvagePartialJSON(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	body := "#!/bin/sh\nprintf '{\"agents\":['\n" +
		"while :; do printf '{\"id\":\"a\"},'; sleep 0.12; done\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	out, err := execOpenclawCLI(ctx, bin, "config", "get", "--json")
	if err == nil {
		t.Fatalf("partial JSON reported as success: %q", out)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) must hold\ngot: %v", err)
	}
}

// TestOpenclawOutputCompleteRules pins which rule each subcommand shape gets, and
// that everything else gets none — which makes RunCollectQuiet wait for exit
// rather than guess.
func TestOpenclawOutputCompleteRules(t *testing.T) {
	banner := []byte("┌────┐\n│ hi │\n└────┘\n")
	partialJSON := []byte(`{"agents":[{"id":"a"},`)
	fullJSON := []byte("{\"agents\":[]}\n")

	// Only `--json` shapes get a rule. A JSON document has to parse as a whole, so
	// no pause mid-write can be mistaken for a finished answer, and upstream keeps
	// incidental output off a `--json` stdout entirely.
	cases := []struct {
		name string
		env  map[string]string
		args []string
		out  []byte
		want bool
	}{
		{name: "json, banner before the document never parses", args: []string{"config", "validate", "--json"}, out: append(append([]byte{}, banner...), fullJSON...), want: false},
		{name: "json, validate payload", args: []string{"config", "validate", "--json"}, out: []byte(`{"valid":true,"path":"/home/u/.openclaw/openclaw.json"}`), want: true},
		{name: "json, partial", args: []string{"config", "get", "--json"}, out: partialJSON, want: false},
		{name: "json, complete", args: []string{"config", "get", "--json"}, out: fullJSON, want: true},
		{name: "json, null is a real answer", args: []string{"config", "get", "agents.list", "--json"}, out: []byte("null\n"), want: true},
		{name: "json, empty", args: []string{"agents", "list", "--json"}, out: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			rule := openclawOutputComplete(tc.args)
			if rule == nil {
				t.Fatalf("no completeness rule for %v", tc.args)
			}
			if got := rule(tc.out); got != tc.want {
				t.Errorf("rule(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}

	for _, args := range [][]string{
		{"doctor"},
		// `config file` deliberately has no rule any more. Its answer is the last
		// line of a stdout it shares with Doctor and plugin warnings, so a rule
		// could only ask "does the last line look like a path" — and review broke
		// exactly that with a warning line that was one. See
		// openclawActiveConfigPath, which prefers `config validate --json`.
		{"config", "file"},
	} {
		if rule := openclawOutputComplete(args); rule != nil {
			t.Errorf("%v must have no completeness rule, so the runner waits for exit "+
				"instead of judging output whose shape cannot identify the answer", args)
		}
	}
}

// TestExecOpenclawCLIToleratesNonExitingCLI covers the second failure mode:
// a CLI that prints its answer and then never exits.
//
// Measured on a 2026.5.27 host, `openclaw config file` printed the path in ~250ms
// and then hung until killed, which reached the user as
//
//	agent_error.process_failure (prepare execution environment: execenv:
//	prepare openclaw config: locate openclaw active config:
//	openclaw config file: context deadline exceeded (process: signal: killed))
//
// while the answer had been on stdout the whole time.
//
// Exercised on a `--json` subcommand, because that is where the tolerance lives
// now: a JSON document is recognisable as finished, so waiting is provably
// unnecessary. `config file` gave up its early return — see
// TestOpenclawActiveConfigPathFailsClosedWhenConfigFileNeverExits for what it
// does instead.
func TestExecOpenclawCLIToleratesNonExitingCLI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	body := "#!/bin/sh\n" +
		"printf '{\"mcp\":{}}\\n'\n" +
		"sleep 300\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	out, err := execOpenclawCLI(ctx, bin, "config", "get", "mcp", "--json")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("execOpenclawCLI: %v", err)
	}
	if strings.TrimSpace(out) != `{"mcp":{}}` {
		t.Errorf("stdout = %q, want the printed document", out)
	}
	// Loose on purpose: only has to sit far below the 60s ctx and the stub's 300s
	// sleep, either of which a broken mechanism would take.
	if elapsed > 10*time.Second {
		t.Errorf("took %v — waited for an exit that never comes", elapsed)
	}
}

// writeOpenclawConfigStub writes a stub CLI that answers `config validate --json`
// with validateOut, exiting validateExit, and `config file` with fileOut, appending
// trailing to the `config file` branch (for stubs that must linger or delay).
//
// validateExit is a parameter because a non-zero exit is the *normal* case for two
// of the three states measured on a real host: upstream exits 1 both for a missing
// config file and for an invalid one, and carries the path in the payload either
// way. A stub that could only exit 0 would test the rarest branch.
func writeOpenclawConfigStub(t *testing.T, validateOut string, validateExit int, fileOut, fileTrailing string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = \"config\" ] && [ \"$2\" = \"validate\" ]; then\n" +
		validateOut +
		"  exit " + strconv.Itoa(validateExit) + "\n" +
		"fi\n" +
		"if [ \"$1\" = \"config\" ] && [ \"$2\" = \"file\" ]; then\n" +
		fileOut +
		fileTrailing +
		"  exit 0\n" +
		"fi\n" +
		"exit 9\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}
	return bin
}

// TestOpenclawActiveConfigPathIgnoresAPathShapedWarning is the regression for the
// second review finding on #6275, and the reason `config file` is no longer the
// primary source of the config path.
//
// The reproduction from that review: a CLI whose last warning line is itself an
// existing path, followed by a pause longer than any fixed grace, followed by the
// real path. Judging `config file`'s stdout by shape returned the warning path
// after 5.57s — before the real answer existed — and expandOpenclawPath then
// turned it into a confident absolute path, so nothing downstream caught it.
//
// `config validate --json` reports the path in a named field, so the warning
// cannot be mistaken for it however long the CLI pauses. This test keeps the
// hostile `config file` branch in the stub precisely so that a future change that
// reinstates shape-based parsing as the primary path fails here.
func TestOpenclawActiveConfigPathIgnoresAPathShapedWarning(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "openclaw.json")
	warnPath := filepath.Join(dir, "plugin-cache.json")
	for _, p := range []string{realPath, warnPath} {
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	bin := writeOpenclawConfigStub(t,
		"  printf '{\"valid\":true,\"path\":\"%s\"}\\n' '"+realPath+"'\n", 0,
		"  printf 'plugin cache written to:\\n'\n  printf '%s\\n' '"+warnPath+"'\n",
		"  sleep 6\n  printf '%s\\n' '"+realPath+"'\n")

	start := time.Now()
	got, exists, err := openclawActiveConfigPath(bin, 30*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("openclawActiveConfigPath: %v", err)
	}
	if got != realPath {
		t.Errorf("path = %q, want %q — a path-shaped warning line was accepted as the answer",
			got, realPath)
	}
	if !exists {
		t.Error("exists = false for a file that is on disk")
	}
	// The validate answer arrives immediately, so this must not have paid the
	// `config file` branch's 6s pause at all.
	if elapsed > 5*time.Second {
		t.Errorf("took %v — the JSON answer was not preferred", elapsed)
	}
}

// TestOpenclawActiveConfigPathFallsBackToConfigFile keeps the fallback honest: a
// CLI whose `config validate --json` is unusable (too old, or a shape we do not
// recognise) must still resolve through `config file`.
func TestOpenclawActiveConfigPathFallsBackToConfigFile(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "openclaw.json")
	if err := os.WriteFile(realPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	for _, tc := range []struct {
		name         string
		validateOut  string
		validateExit int
	}{
		{"validate prints nothing", "  :\n", 0},
		{"validate prints non-JSON", "  echo 'Unknown command: validate' >&2\n", 1},
		{"validate omits the path field", "  printf '{\"valid\":true}\\n'\n", 0},
		{"validate reports a relative path", "  printf '{\"valid\":false,\"path\":\"openclaw.json\"}\\n'\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := writeOpenclawConfigStub(t, tc.validateOut, tc.validateExit,
				"  printf '%s\\n' '"+realPath+"'\n", "")
			got, exists, err := openclawActiveConfigPath(bin, 30*time.Second)
			if err != nil {
				t.Fatalf("openclawActiveConfigPath: %v", err)
			}
			if got != realPath || !exists {
				t.Errorf("path = %q exists = %v, want %q true", got, exists, realPath)
			}
		})
	}
}

// TestOpenclawActiveConfigPathReadsThePathFromANonZeroExit pins the case that is
// *normal* rather than exceptional, and which the first version of these tests
// missed: `config validate --json` exits 1 both for a missing config file and for
// an invalid one, and carries the path in its payload either way.
//
// A fresh install is the missing-file case, so treating a non-zero exit as "no
// answer" would break the most common first-run path. Both payloads below are the
// real shapes measured on OpenClaw 2026.7.1-2, with stderr empty in both.
func TestOpenclawActiveConfigPathReadsThePathFromANonZeroExit(t *testing.T) {
	dir := t.TempDir()
	presentPath := filepath.Join(dir, "openclaw.json")
	if err := os.WriteFile(presentPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	missingPath := filepath.Join(dir, "absent", "openclaw.json")

	for _, tc := range []struct {
		name       string
		payload    string
		want       string
		wantExists bool
	}{
		{
			// {"valid":false,"path":"…","error":"file not found"}, exit 1.
			name:       "file not found",
			payload:    `{"valid":false,"path":"` + missingPath + `","error":"file not found"}`,
			want:       missingPath,
			wantExists: false,
		},
		{
			// {"valid":false,"path":"…","issues":[…]}, exit 1. The user's config is
			// broken, which openclaw itself will report when it runs; all this
			// resolution step owes the caller is where the file is.
			name:       "invalid config",
			payload:    `{"valid":false,"path":"` + presentPath + `","issues":[{"path":"<root>","message":"Invalid input"}]}`,
			want:       presentPath,
			wantExists: true,
		},
		{
			// The healthy case still carries plugin warnings *inside* the document
			// rather than as text before it, which is what keeps stdout a single
			// parseable object.
			name:       "valid with warnings in the payload",
			payload:    `{"valid":true,"path":"` + presentPath + `","warnings":[{"path":"plugins.allow","message":"plugin not found: openclaw-lark"}]}`,
			want:       presentPath,
			wantExists: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exit := 1
			if strings.Contains(tc.payload, `"valid":true`) {
				exit = 0
			}
			bin := writeOpenclawConfigStub(t,
				"  printf '%s\\n' '"+tc.payload+"'\n", exit,
				"  echo 'config file must not be consulted'\n", "")

			got, exists, err := openclawActiveConfigPath(bin, 30*time.Second)
			if err != nil {
				t.Fatalf("openclawActiveConfigPath: %v", err)
			}
			if got != tc.want {
				t.Errorf("path = %q, want %q — the exit status must not decide whether "+
					"the payload carries an answer", got, tc.want)
			}
			if exists != tc.wantExists {
				t.Errorf("exists = %v, want %v", exists, tc.wantExists)
			}
		})
	}
}

// TestOpenclawActiveConfigPathFailsClosedWhenConfigFileNeverExits is the other
// side of dropping the `config file` completeness rule: with no way to recognise
// the answer, a CLI that prints something and then hangs must fail rather than
// have its output guessed at.
//
// This is a deliberate loss of tolerance on that one command, taken because the
// alternative was demonstrated to return a wrong path. The deadline is what makes
// it bounded, and MULTICA_OPENCLAW_CLI_TIMEOUT (#7142) is what makes it tunable.
func TestOpenclawActiveConfigPathFailsClosedWhenConfigFileNeverExits(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "openclaw.json")
	bin := writeOpenclawConfigStub(t,
		"  echo 'validate unsupported' >&2\n", 1,
		"  printf '%s\\n' '"+realPath+"'\n",
		"  sleep 300\n")

	start := time.Now()
	_, _, err := openclawActiveConfigPath(bin, 3*time.Second)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a `config file` that never exits must fail closed, not have its " +
			"stdout accepted on shape")
	}
	if elapsed > 20*time.Second {
		t.Errorf("took %v — the deadline did not bound the call", elapsed)
	}
}
