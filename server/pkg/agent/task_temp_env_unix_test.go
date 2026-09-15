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

// TestAgentExecutePassesTaskTempEnvToChild is the regression for #8392.
//
// The daemon gives every task a private temp directory and points TMPDIR, TMP
// and TEMP at it (taskMulticaEnvironment in server/internal/daemon/daemon.go),
// then removes that directory when the task ends. That isolation is the only
// thing bounding a class of agent-CLI temp litter we do not own: OpenCode is
// compiled by Bun into a single-file executable, and on every successful run
// its embedded runtime extracts a native module into $TMPDIR under a fresh
// non-content-addressed name and never deletes it. Reproduced on macOS/arm64
// and Linux (amd64 and arm64) across OpenCode 1.15.0 through 1.18.30: one
// 4-8 MB module per successful `opencode run`, surviving both clean exit and
// SIGKILL. #8392 is what happens when those land in the shared system temp
// directory instead — ~2,960 files, 11.16 GiB, root filesystem at 99%.
//
// Existing coverage stops at the map taskMulticaEnvironment returns and at the
// custom_env blocklist that keeps an agent from overriding these keys. Neither
// notices if a backend builds its child environment without Config.Env — the
// variables would still be in the map, correct in every unit test, and simply
// absent from the process that creates the files. This asserts the value the
// spawned process actually receives.
func TestAgentExecutePassesTaskTempEnvToChild(t *testing.T) {
	t.Parallel()

	// Every provider the daemon can spawn through this package's exec path.
	// A backend added without wiring Config.Env into its child fails here.
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
						"agent temp files would land in the shared system temp dir (#8392)",
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
