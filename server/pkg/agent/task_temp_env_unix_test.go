//go:build unix

package agent

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordChildEnv spawns backend `provider` against a fake CLI that dumps its
// own environment to a file, and returns what the child actually saw. The fake
// emits no protocol stream, so the run is cancelled as soon as the dump lands
// rather than waiting out each backend's handshake timeout: the assertion here
// is about the environment the process is handed, which is fixed at spawn.
func recordChildEnv(t *testing.T, provider string, cfgEnv map[string]string) map[string]string {
	t.Helper()

	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.txt")

	// Write to a temp name and rename, so the reader below can never observe a
	// half-written dump and mistake a missing key for a dropped variable.
	script := fmt.Sprintf("#!/bin/sh\nenv > %[1]q.partial\nmv %[1]q.partial %[1]q\ncat > /dev/null\n", envPath)
	fakePath := filepath.Join(dir, provider)
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New(provider, Config{
		ExecutablePath: fakePath,
		Env:            cfgEnv,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("New(%s): %v", provider, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	session, err := backend.Execute(ctx, "prompt", ExecOptions{
		Timeout: 30 * time.Second,
		Model:   "lanz/Lanz-Medium",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, statErr := os.Stat(envPath); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child never recorded its environment at %s", envPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-session.Result

	f, err := os.Open(envPath)
	if err != nil {
		t.Fatalf("read recorded environment: %v", err)
	}
	defer func() { _ = f.Close() }()

	got := map[string]string{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if key, value, ok := strings.Cut(scanner.Text(), "="); ok {
			got[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read recorded environment: %v", err)
	}
	return got
}

// TestAgentExecutePassesTaskTempEnvToChild pins the per-task temp variables to
// the process that actually consumes them.
//
// The daemon gives every task a private temp directory and points TMPDIR, TMP
// and TEMP at it (taskMulticaEnvironment in server/internal/daemon/daemon.go),
// then removes that directory when the task ends. That isolation is what bounds
// a class of agent-CLI temp litter we do not own and cannot safely delete by
// filename in a shared /tmp: OpenCode is compiled by Bun into a single-file
// executable, and on every successful run its embedded runtime extracts a
// native module into $TMPDIR under a fresh non-content-addressed name and never
// deletes it — one 4-8 MB module per run, surviving clean exit and SIGKILL,
// measured across OpenCode 1.1.49 through 1.18.30 on macOS/arm64 and Linux.
//
// This is NOT the regression for #8392. That report's cause was upstream:
// OpenCode <= 1.1.53 ignored these variables outright and wrote into the shared
// /tmp no matter what the daemon exported (fixed upstream in 1.1.54). No test
// here could have caught that. What this pins is our half of the contract, on
// which the isolation depends for every CLI that does honor the variables.
//
// TestOpencodeBackendOmitsMCPEnvWhenEmpty already fails if a backend drops
// Config.Env wholesale, so that much was covered. The gap this closes is
// narrower and was genuinely uncovered: these three keys specifically, and
// their precedence over the same names inherited from the daemon's own
// environment — see TestAgentExecuteTaskTempEnvOverridesInheritedValue, which
// is the case the MCP test passes straight through.
func TestAgentExecutePassesTaskTempEnvToChild(t *testing.T) {
	t.Parallel()

	// The providers the daemon spawns through this package's exec path, on
	// Unix. Not an exhaustive registry walk: a newly added backend has to be
	// listed here to be covered.
	for _, provider := range []string{"claude", "codex", "opencode"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			taskTemp := t.TempDir()
			got := recordChildEnv(t, provider, map[string]string{
				"TMPDIR": taskTemp,
				"TMP":    taskTemp,
				"TEMP":   taskTemp,
			})

			for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
				if got[key] != taskTemp {
					t.Errorf("child %s = %q, want the per-task temp dir %q — "+
						"agent temp files would land in the shared system temp dir instead",
						key, got[key], taskTemp)
				}
			}
		})
	}
}

// TestAgentExecuteTaskTempEnvOverridesInheritedValue guards the merge order.
// The daemon process has its own TMPDIR, inherited by every child through
// os.Environ(); the per-task value has to win over it. A merge that appended
// the daemon's environment last, or a de-duplication that kept the first
// occurrence, would leave the child writing to the shared system temp dir
// while the per-task directory sits empty and is removed at task end.
func TestAgentExecuteTaskTempEnvOverridesInheritedValue(t *testing.T) {
	inherited := t.TempDir()
	taskTemp := t.TempDir()

	// Not parallel: t.Setenv forbids it, and the inherited value is the point.
	t.Setenv("TMPDIR", inherited)
	t.Setenv("TMP", inherited)
	t.Setenv("TEMP", inherited)

	got := recordChildEnv(t, "opencode", map[string]string{
		"TMPDIR": taskTemp,
		"TMP":    taskTemp,
		"TEMP":   taskTemp,
	})

	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if got[key] != taskTemp {
			t.Errorf("child %s = %q, want per-task %q (daemon's own value was %q)",
				key, got[key], taskTemp, inherited)
		}
	}
}
