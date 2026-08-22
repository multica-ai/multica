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

// TestExecOpenclawCLIWaitsForThePathAfterDoctorBanner is the regression for the
// review finding on #6275: "output arrived and went quiet" is not the same as
// "the answer arrived".
//
// `openclaw config file` can print Doctor warning UI before the path (MUL-3136,
// which is why openclawParseActiveConfigPath takes the last non-empty line). If
// the early return fired on idle output alone, a pause between the banner and the
// path would hand the banner back as the config path — and expandOpenclawPath
// would dutifully turn it into an absolute path, so nothing downstream would
// catch it.
func TestExecOpenclawCLIWaitsForThePathAfterDoctorBanner(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	// A real, statable path rather than the literal path the host printed:
	// openclawParseActiveConfigPath stats what it parses and tolerates only
	// os.ErrNotExist, so a hard-coded /root/... path reads as ENOENT on a
	// developer machine but as EACCES on a Linux CI runner — failing the test for
	// a reason that has nothing to do with the boundary under test.
	cfgPath := filepath.Join(dir, "openclaw.json")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write fake config: %v", err)
	}
	body := "#!/bin/sh\n" +
		"echo '┌───────────────────────────────┐'\n" +
		"echo '│ warning: run openclaw doctor  │'\n" +
		"echo '└───────────────────────────────┘'\n" +
		"sleep 1\n" +
		"printf '%s\\n' '" + cfgPath + "'\n" +
		"sleep 300\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := execOpenclawCLI(ctx, bin, "config", "file")
	if err != nil {
		t.Fatalf("execOpenclawCLI: %v", err)
	}
	got := strings.TrimSpace(out)
	if !strings.HasSuffix(got, cfgPath) {
		t.Fatalf("output = %q — returned before the path arrived, so the Doctor "+
			"banner would be parsed as the config path", got)
	}
	// And the parse the daemon actually performs must land on the path.
	path, _, perr := openclawParseActiveConfigPath(out)
	if perr != nil {
		t.Fatalf("openclawParseActiveConfigPath: %v", perr)
	}
	if path != cfgPath {
		t.Errorf("parsed path = %q, want the real config path", path)
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
// that an unknown shape gets none — which makes RunCollectQuiet wait for exit
// rather than guess.
func TestOpenclawOutputCompleteRules(t *testing.T) {
	banner := []byte("┌────┐\n│ hi │\n└────┘\n")
	pathOut := []byte(string(banner) + "/root/.openclaw/openclaw.json\n")
	partialJSON := []byte(`{"agents":[{"id":"a"},`)
	fullJSON := []byte("{\"agents\":[]}\n")

	// `config file` prints the path with the $HOME prefix collapsed, so the shape
	// depends on the environment rather than on where the file is: the same file was
	// reported as `~/.openclaw/openclaw.json` under one HOME and as
	// `/root/.openclaw/openclaw.json` under another (openclaw 2026.5.27). The daemon
	// normally shares openclaw's HOME, so the tilde form is the common one.
	//
	// With OPENCLAW_HOME set, upstream prints that variable's name instead (see
	// openclawHomeRest), so whether such a line is a finished answer depends on the
	// environment too — env carries the value under test for those rows.
	cases := []struct {
		name string
		env  map[string]string
		args []string
		out  []byte
		want bool
	}{
		{name: "config file, banner only", args: []string{"config", "file"}, out: banner, want: false},
		{name: "config file, path arrived", args: []string{"config", "file"}, out: pathOut, want: true},
		{name: "config file, tilde path", args: []string{"config", "file"}, out: []byte("~/.openclaw/openclaw.json\n"), want: true},
		{name: "config file, Windows tilde path", args: []string{"config", "file"}, out: []byte("~\\.openclaw\\openclaw.json\r\n"), want: true},
		// The hand-off from #7310: the parser understands this shape now, so the
		// rule may accept it and these hosts get the early return.
		{
			name: "config file, OPENCLAW_HOME path",
			env:  map[string]string{"OPENCLAW_HOME": string(filepath.Separator) + "srv" + string(filepath.Separator) + "openclaw"},
			args: []string{"config", "file"},
			out:  []byte("$OPENCLAW_HOME\\.openclaw\\openclaw.json\r\n"),
			want: true,
		},
		// A tilde-valued OPENCLAW_HOME resolves to an absolute directory, so the
		// answer is finished. Judging the raw value with filepath.IsAbs would say
		// false here and withhold the early return on exactly the configuration
		// #7310's review was about.
		{
			name: "config file, tilde-valued OPENCLAW_HOME",
			env:  map[string]string{"OPENCLAW_HOME": "~/svc"},
			args: []string{"config", "file"},
			out:  []byte("$OPENCLAW_HOME/.openclaw/openclaw.json\n"),
			want: true,
		},
		// Nothing to resolve it to: not an answer, so the call waits for exit and
		// the parser reports the real error rather than this rule guessing.
		{
			name: "config file, OPENCLAW_HOME unset",
			env:  map[string]string{"OPENCLAW_HOME": ""},
			args: []string{"config", "file"},
			out:  []byte("$OPENCLAW_HOME/.openclaw/openclaw.json\n"),
			want: false,
		},
		{name: "config file, empty", args: []string{"config", "file"}, out: nil, want: false},
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

	if rule := openclawOutputComplete([]string{"doctor"}); rule != nil {
		t.Error("an unrecognised subcommand must have no rule, so the runner " +
			"waits for exit instead of judging output it does not understand")
	}
}

// TestExecOpenclawCLIToleratesNonExitingCLI covers the second failure mode.
// Measured on the host, `openclaw config file` printed the path in ~250ms and
// then hung until killed, which reached the user as
//
//	agent_error.process_failure (prepare execution environment: execenv:
//	prepare openclaw config: locate openclaw active config:
//	openclaw config file: context deadline exceeded (process: signal: killed))
//
// while the answer had been on stdout the whole time.
func TestExecOpenclawCLIToleratesNonExitingCLI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	// Statable for the same reason as above, even though this test only inspects
	// the raw stdout: a hard-coded /root/... path here would be a trap for
	// whoever next adds a parse to this test.
	cfgPath := filepath.Join(dir, "openclaw.json")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write fake config: %v", err)
	}
	body := "#!/bin/sh\n" +
		"printf '%s\\n' '" + cfgPath + "'\n" +
		"sleep 300\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	out, err := execOpenclawCLI(ctx, bin, "config", "file")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("execOpenclawCLI: %v", err)
	}
	if strings.TrimSpace(out) != cfgPath {
		t.Errorf("stdout = %q, want the printed path", out)
	}
	// Loose on purpose: only has to sit far below the 60s ctx and the stub's 300s
	// sleep, either of which a broken mechanism would take.
	if elapsed > 10*time.Second {
		t.Errorf("took %v — waited for an exit that never comes", elapsed)
	}
}
