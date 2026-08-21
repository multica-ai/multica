//go:build windows

package execenv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This file is the Windows half of the MUL-5422 / #6061 regression. The
// cross-platform file proves the diagnostic's logic; only a real cmd.exe host
// can prove how a batch shim actually behaves, which is where #6061's central
// claim lived ("the error goes to the .cmd layer and Go's stderr pipe misses
// it"). The first CI run of this job disproved that claim, and the assertions
// below now encode the observed behaviour instead of the reported guess.
//
// Run via the ci.yml `windows-execenv` job, which scopes -run patterns to the
// Windows-safe tests in this package.

// npmCmdShimBody reproduces npm's actual generated cmd-shim, not a
// hand-simplified `node ...` one-liner. Faithfulness matters here: the real
// template resolves a co-located `node.exe` BEFORE falling back to PATH, and a
// simplified shim would hide exactly the case Sol-Boy's must-fix 2 called out.
//
// Source: https://github.com/npm/cmd-shim/blob/main/lib/index.js (writeShim_),
// with longProg="%dp0%\node.exe", prog="node", target="%dp0%\openclaw.mjs".
func npmCmdShimBody() string {
	return "@ECHO off\r\n" +
		"GOTO start\r\n" +
		":find_dp0\r\n" +
		"SET dp0=%~dp0\r\n" +
		"EXIT /b\r\n" +
		":start\r\n" +
		"SETLOCAL\r\n" +
		"CALL :find_dp0\r\n" +
		"\r\n" +
		"IF EXIST \"%dp0%\\node.exe\" (\r\n" +
		"  SET \"_prog=%dp0%\\node.exe\"\r\n" +
		") ELSE (\r\n" +
		"  SET \"_prog=node\"\r\n" +
		"  SET PATHEXT=%PATHEXT:;.JS;=;%\r\n" +
		")\r\n" +
		"\r\n" +
		"endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & " +
		"set PATHEXT=%PATHEXT:;.JS;=;% & \"%_prog%\" \"%dp0%\\openclaw.mjs\" %*\r\n"
}

// writeNpmStyleShim writes npm's real shim plus the JS entrypoint it re-execs.
func writeNpmStyleShim(t *testing.T, dir string) string {
	t.Helper()
	entry := filepath.Join(dir, "openclaw.mjs")
	if err := os.WriteFile(entry, []byte("console.log(String.raw`C:\\openclaw\\config.json`);\n"), 0o644); err != nil {
		t.Fatalf("write entrypoint: %v", err)
	}
	shim := filepath.Join(dir, "openclaw.cmd")
	if err := os.WriteFile(shim, []byte(npmCmdShimBody()), 0o644); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	return shim
}

// nodeDir returns the directory holding a real node.exe, skipping the test when
// the runner has no Node installed.
func nodeDir(t *testing.T) string {
	t.Helper()
	resolved, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("no node on PATH, cannot exercise a real npm shim: %v", err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		t.Fatalf("resolve node path: %v", err)
	}
	return filepath.Dir(abs)
}

// systemPath is the minimum PATH a batch shim needs to run at all (cmd.exe and
// friends live there), without any Node directory on it.
func systemPath(t *testing.T) string {
	t.Helper()
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return strings.Join([]string{filepath.Join(root, "System32"), root}, string(os.PathListSeparator))
}

// TestWindowsOpenclawShimMissingNodeSurfacesCmdStderr records what actually
// happens on Windows when a real npm shim cannot find its interpreter.
//
// The first CI run of this job showed cmd.exe's "'node' is not recognized"
// reaching Go's stderr pipe, which refutes #6061's premise and means this case
// takes execOpenclawCLI's stderr branch — the shim diagnostic is NOT involved.
// Asserting that directly (rather than "either branch is fine") is deliberate:
// the earlier disjunction let the diagnostic go completely unexercised on
// Windows while still reporting a pass. If this ever flips, this test fails
// loudly and TestWindowsOpenclawShimSilentFailureIsDiagnosed still proves the
// fallback works.
func TestWindowsOpenclawShimMissingNodeSurfacesCmdStderr(t *testing.T) {
	nodeDir(t) // skip early if the runner has no Node to remove from PATH
	shim := writeNpmStyleShim(t, t.TempDir())
	t.Setenv("PATH", systemPath(t))

	out, err := execOpenclawCLI(context.Background(), shim, "config", "file")
	if err == nil {
		t.Fatalf("shim must fail without node on PATH, got output %q", out)
	}
	msg := err.Error()
	t.Logf("observed error: %s", msg)

	if !strings.Contains(strings.ToLower(msg), "not recognized") {
		t.Errorf("expected cmd.exe's own stderr to reach Go's pipe; if this now fails, "+
			"the platform behaviour changed and the shim diagnostic is carrying this case instead\ngot: %s", msg)
	}
	if !strings.Contains(msg, "openclaw config file") {
		t.Errorf("error should name the failing subcommand\ngot: %s", msg)
	}
}

// TestWindowsOpenclawShimSilentFailureIsDiagnosed is Sol-Boy's must-fix 4: prove
// the new fallback actually executes on Windows. The missing-node case above
// never reaches it because cmd.exe supplies stderr, so a shim that exits
// non-zero while writing nothing at all is the only way to cover this branch —
// and that silent shape is precisely what #6061's daemon log reported.
func TestWindowsOpenclawShimSilentFailureIsDiagnosed(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "openclaw.cmd")
	// No output on either stream, non-zero exit.
	if err := os.WriteFile(shim, []byte("@ECHO off\r\nexit /b 1\r\n"), 0o644); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	t.Setenv("PATH", systemPath(t)) // no Node anywhere, and none co-located

	_, err := execOpenclawCLI(context.Background(), shim, "config", "file")
	if err == nil {
		t.Fatal("expected the silent shim to fail")
	}
	msg := err.Error()
	t.Logf("observed error: %s", msg)
	if !strings.Contains(msg, "resolves neither alongside the shim nor on the daemon PATH") {
		t.Fatalf("the shim diagnostic must carry a silent failure\ngot: %s", msg)
	}
	// Redaction holds on the platform where the path is actually sensitive.
	if strings.Contains(msg, dir) {
		t.Errorf("diagnostic leaked the absolute shim path\ngot: %s", msg)
	}
	if !strings.Contains(msg, "openclaw.cmd") {
		t.Errorf("diagnostic should name the shim's base name\ngot: %s", msg)
	}
}

// TestWindowsOpenclawShimColocatedNodeIsCredited covers must-fix 2 on the real
// platform. npm's template runs `%dp0%\node.exe` when it exists, so an install
// with a co-located interpreter is healthy even with nothing on PATH. The
// diagnostic must credit that instead of claiming Node is unreachable.
//
// A placeholder file is enough: this asserts our resolution order, and the file
// is only ever stat'd, never executed.
func TestWindowsOpenclawShimColocatedNodeIsCredited(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "openclaw.cmd")
	if err := os.WriteFile(shim, []byte("@ECHO off\r\nexit /b 1\r\n"), 0o644); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node.exe"), []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write co-located node.exe: %v", err)
	}
	t.Setenv("PATH", systemPath(t)) // nothing Node-ish on PATH

	_, err := execOpenclawCLI(context.Background(), shim, "config", "file")
	if err == nil {
		t.Fatal("expected the silent shim to fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "alongside the shim") {
		t.Errorf("diagnostic should credit the co-located interpreter\ngot: %s", msg)
	}
	if strings.Contains(msg, "resolves neither") {
		t.Errorf("diagnostic must not claim Node is unreachable\ngot: %s", msg)
	}
}

// TestWindowsOpenclawShimTimeoutIsNotMisdiagnosed is must-fix 1 on Windows: a
// slow shim killed by the context deadline must not be reported as a missing
// interpreter. Go surfaces the kill as *exec.ExitError, so only the explicit
// context check keeps this correct.
//
// The shim waits only briefly: execOpenclawCLI sets no WaitDelay, so
// cmd.Output() stays parked until the output pipes os/exec manages for it
// (stdout and stderr both) reach EOF. `ping` inherits those write ends and
// holds them until it exits, so the ~2s observed in CI is it finishing rather
// than a process left behind; a long wait would only make the test slower.
func TestWindowsOpenclawShimTimeoutIsNotMisdiagnosed(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "openclaw.cmd")
	// ~2s: ping sends 3 packets one second apart.
	body := "@ECHO off\r\nping -n 3 127.0.0.1 >NUL\r\n"
	if err := os.WriteFile(shim, []byte(body), 0o644); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	t.Setenv("PATH", systemPath(t))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := execOpenclawCLI(ctx, shim, "config", "file")
	if err == nil {
		t.Fatal("expected the timed-out invocation to fail")
	}
	msg := err.Error()
	t.Logf("observed error: %s", msg)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) must hold\ngot: %s", msg)
	}
	for _, forbidden := range []string{"install Node.js", "resolves neither", "the interpreter is reachable"} {
		if strings.Contains(msg, forbidden) {
			t.Errorf("timeout must not be diagnosed as an interpreter problem (found %q)\ngot: %s", forbidden, msg)
		}
	}
}

// TestWindowsOpenclawShimSucceedsWithNodeOnPath is the positive control. The
// same real npm shim, same absolute path, differing only by whether Node is
// reachable — this is what makes interpreter resolution the proven
// discriminator rather than an assumed one.
func TestWindowsOpenclawShimSucceedsWithNodeOnPath(t *testing.T) {
	dir := nodeDir(t)
	shim := writeNpmStyleShim(t, t.TempDir())
	t.Setenv("PATH", strings.Join([]string{dir, systemPath(t)}, string(os.PathListSeparator)))

	out, err := execOpenclawCLI(context.Background(), shim, "config", "file")
	if err != nil {
		t.Fatalf("shim should succeed with node on PATH: %v", err)
	}
	if !strings.Contains(out, `C:\openclaw\config.json`) {
		t.Fatalf("expected the entrypoint's stdout, got %q", out)
	}
}

// TestWindowsOpenclawShimSucceedsWithoutTempVars closes out the root cause
// #6061 first reported and then retracted: with Node reachable but TEMP and TMP
// both unset, the shim must still succeed. Node falls back to a system temp
// directory, so these variables are not load-bearing — and pinning that stops a
// future change from reintroducing the dependency.
func TestWindowsOpenclawShimSucceedsWithoutTempVars(t *testing.T) {
	dir := nodeDir(t)
	shim := writeNpmStyleShim(t, t.TempDir())
	t.Setenv("PATH", strings.Join([]string{dir, systemPath(t)}, string(os.PathListSeparator)))
	t.Setenv("TEMP", "")
	t.Setenv("TMP", "")

	out, err := execOpenclawCLI(context.Background(), shim, "config", "file")
	if err != nil {
		t.Fatalf("shim must not depend on TEMP/TMP: %v", err)
	}
	if !strings.Contains(out, `C:\openclaw\config.json`) {
		t.Fatalf("expected the entrypoint's stdout, got %q", out)
	}
}

// TestWindowsOpenclawShimInPathWithSpacesAndUnicode covers the install
// locations #6061's open questions flagged. `%~dp0` expansion and Go's batch
// argument handling both have to survive a directory with a space or non-ASCII
// characters — `C:\Program Files\...` is a completely ordinary install target.
func TestWindowsOpenclawShimInPathWithSpacesAndUnicode(t *testing.T) {
	nodeRoot := nodeDir(t)
	for _, segment := range []string{"Program Files copy", "用户 開發"} {
		t.Run(segment, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), segment)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %q: %v", dir, err)
			}
			shim := writeNpmStyleShim(t, dir)
			t.Setenv("PATH", strings.Join([]string{nodeRoot, systemPath(t)}, string(os.PathListSeparator)))

			out, err := execOpenclawCLI(context.Background(), shim, "config", "file")
			if err != nil {
				t.Fatalf("shim in %q should be invocable: %v", dir, err)
			}
			if !strings.Contains(out, `C:\openclaw\config.json`) {
				t.Fatalf("expected the entrypoint's stdout, got %q", out)
			}
		})
	}
}
