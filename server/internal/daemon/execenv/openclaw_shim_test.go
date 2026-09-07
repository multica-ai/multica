package execenv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestIsOpenclawShimPath locks the shim-detection surface. Case-insensitivity
// matters because Windows PATH resolution is case-insensitive and npm/PATHEXT
// can hand back `OPENCLAW.CMD`; paths containing spaces and non-ASCII segments
// are included because those are the Windows install locations most likely to
// be mis-parsed, and #6061's open questions called them out explicitly.
func TestIsOpenclawShimPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		bin  string
		want bool
	}{
		{"npm cmd shim", `C:\Users\dev\AppData\Roaming\npm\openclaw.cmd`, true},
		{"uppercase extension", `C:\npm\OPENCLAW.CMD`, true},
		{"mixed case extension", `C:\npm\openclaw.Cmd`, true},
		{"legacy bat shim", `C:\npm\openclaw.bat`, true},
		{"path with spaces", `C:\Program Files\node modules\openclaw.cmd`, true},
		{"path with unicode segment", `C:\用户\开发\npm\openclaw.cmd`, true},
		{"surrounding whitespace", "  C:\\npm\\openclaw.cmd  ", true},
		{"real executable", `C:\npm\openclaw.exe`, false},
		{"powershell shim is not a batch shim", `C:\npm\openclaw.ps1`, false},
		{"unix binary without extension", "/usr/local/bin/openclaw", false},
		{"unix path with dotted directory", "/opt/openclaw.cmd.d/openclaw", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isOpenclawShimPath(tc.bin); got != tc.want {
				t.Fatalf("isOpenclawShimPath(%q) = %v, want %v", tc.bin, got, tc.want)
			}
		})
	}
}

// exitError produces a real *exec.ExitError so the diagnostic's errors.As gate
// is exercised against the same type production sees, not a stand-in.
//
// The interpreter is invoked by absolute path on purpose: callers stub PATH to
// control the interpreter lookup, and a PATH-dependent helper would break
// depending on the order those two happen in.
func exitError(t *testing.T) error {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		shell := os.Getenv("ComSpec")
		if shell == "" {
			shell = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
		}
		cmd = exec.Command(shell, "/c", "exit 1")
	} else {
		cmd = exec.Command("/bin/sh", "-c", "exit 1")
	}
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError, got %T (%v)", err, err)
	}
	return err
}

// pathWithout points PATH at an empty directory so the interpreter cannot
// resolve. Setting PATH rather than clearing it keeps LookPath on its normal
// code path instead of its empty-PATH special case.
func pathWithout(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// writeFakeInterpreter drops an executable named like the interpreter into dir.
// It only has to be resolvable — the diagnostic reports lookup results and
// never runs it.
func writeFakeInterpreter(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake interpreter: %v", err)
	}
	return p
}

// pathWithFakeNode puts a resolvable interpreter on PATH and nowhere else.
func pathWithFakeNode(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	name := openclawShimInterpreter
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	writeFakeInterpreter(t, dir, name)
	t.Setenv("PATH", dir)
}

// TestOpenclawShimDiagnosticNamesUnreachableInterpreter is the core #6061
// regression: a silent shim exit must be reported as an unreachable
// interpreter, with an actionable next step, instead of a bare exit code.
func TestOpenclawShimDiagnosticNamesUnreachableInterpreter(t *testing.T) {
	pathWithout(t)
	shim := filepath.Join(t.TempDir(), "openclaw.cmd")
	got := openclawShimDiagnostic(shim, exitError(t))
	if got == "" {
		t.Fatal("expected a diagnostic for a silent .cmd shim failure, got none")
	}
	for _, want := range []string{
		"resolves neither alongside the shim nor on the daemon PATH",
		openclawShimInterpreter,
		"openclaw.cmd",
		"install Node.js",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostic missing %q\ngot: %s", want, got)
		}
	}
}

// TestOpenclawShimDiagnosticFindsColocatedInterpreter is Sol-Boy's must-fix 2.
// npm's cmd-shim template checks `%dp0%\node.exe` BEFORE falling back to PATH:
//
//	IF EXIST "%dp0%\node.exe" ( SET "_prog=%dp0%\node.exe" ) ELSE ( SET "_prog=node" )
//
// So an install whose Node sits next to the shim runs fine with nothing on
// PATH. Reporting "node is not resolvable" there would be confidently wrong,
// which is worse than staying quiet.
func TestOpenclawShimDiagnosticFindsColocatedInterpreter(t *testing.T) {
	pathWithout(t) // nothing on PATH — only the co-located copy can be found
	dir := t.TempDir()
	shim := filepath.Join(dir, "openclaw.cmd")
	name := openclawShimInterpreter
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	writeFakeInterpreter(t, dir, name)

	got := openclawShimDiagnostic(shim, exitError(t))
	if got == "" {
		t.Fatal("expected a diagnostic, got none")
	}
	if !strings.Contains(got, "alongside the shim") {
		t.Errorf("diagnostic should credit the co-located interpreter\ngot: %s", got)
	}
	if strings.Contains(got, "resolves neither") {
		t.Errorf("diagnostic must not claim the interpreter is unreachable\ngot: %s", got)
	}
}

// TestOpenclawShimDiagnosticReportsInterpreterOnPath guards the other
// direction, which is the evidence that actually discriminates between the
// competing #6061 hypotheses. If the interpreter resolves, the diagnostic must
// say so rather than blaming PATH, otherwise the next bug report gets steered
// toward the wrong root cause.
func TestOpenclawShimDiagnosticReportsInterpreterOnPath(t *testing.T) {
	pathWithFakeNode(t)
	shim := filepath.Join(t.TempDir(), "openclaw.cmd")
	got := openclawShimDiagnostic(shim, exitError(t))
	if got == "" {
		t.Fatal("expected a diagnostic, got none")
	}
	if !strings.Contains(got, "on the daemon PATH") || !strings.Contains(got, "the interpreter is reachable") {
		t.Errorf("diagnostic should clear PATH of blame\ngot: %s", got)
	}
	if strings.Contains(got, "resolves neither") {
		t.Errorf("diagnostic must not claim the interpreter is unreachable\ngot: %s", got)
	}
}

// TestOpenclawShimDiagnosticIsPhrasedConditionally is the rest of must-fix 2. A
// batch extension does not prove npm authorship — an operator can point
// MULTICA_OPENCLAW_PATH at any batch file — so the text must not assert npm
// shim semantics as established fact for whatever failed.
func TestOpenclawShimDiagnosticIsPhrasedConditionally(t *testing.T) {
	pathWithout(t)
	shim := filepath.Join(t.TempDir(), "custom-wrapper.cmd")
	got := openclawShimDiagnostic(shim, exitError(t))
	if got == "" {
		t.Fatal("expected a diagnostic, got none")
	}
	if !strings.Contains(got, "if custom-wrapper.cmd is an npm-generated shim") {
		t.Errorf("diagnostic should be conditional about npm authorship\ngot: %s", got)
	}
}

// TestOpenclawShimDiagnosticRedactsLocalPaths is Sol-Boy's must-fix 3, and the
// reason it matters is the blast radius: on prep failure this text is not
// log-local. It travels reportTerminalTask → Client.FailTask and is persisted
// server-side as the task error, so an absolute Windows shim path would upload
// the account name and install layout.
func TestOpenclawShimDiagnosticRedactsLocalPaths(t *testing.T) {
	secretDir := filepath.Join(t.TempDir(), "Users", "a-real-person", "AppData", "Roaming", "npm")
	if err := os.MkdirAll(secretDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	shim := filepath.Join(secretDir, "openclaw.cmd")
	pathDir := filepath.Join(t.TempDir(), "another-private-location")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	name := openclawShimInterpreter
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	interpreter := writeFakeInterpreter(t, pathDir, name)
	t.Setenv("PATH", pathDir)

	got := openclawShimDiagnostic(shim, exitError(t))
	if got == "" {
		t.Fatal("expected a diagnostic, got none")
	}
	for _, leak := range []string{secretDir, shim, pathDir, interpreter, "a-real-person", "another-private-location"} {
		if strings.Contains(got, leak) {
			t.Errorf("diagnostic leaked local path detail %q\ngot: %s", leak, got)
		}
	}
	if !strings.Contains(got, "openclaw.cmd") {
		t.Errorf("diagnostic should still name the shim's base name\ngot: %s", got)
	}
	if !strings.Contains(got, "1 entry") {
		t.Errorf("diagnostic should summarise PATH as a count\ngot: %s", got)
	}
}

// TestOpenclawShimDiagnosticStaysSilentOutOfScope pins the no-op cases. A
// diagnostic attached to a missing binary or a normal native executable would
// be actively misleading.
func TestOpenclawShimDiagnosticStaysSilentOutOfScope(t *testing.T) {
	pathWithout(t)
	realExit := exitError(t)
	cases := []struct {
		name string
		bin  string
		err  error
	}{
		{"native executable", `C:\npm\openclaw.exe`, realExit},
		{"unix binary", "/usr/local/bin/openclaw", realExit},
		{"binary not found", `C:\npm\openclaw.cmd`, exec.ErrNotFound},
		{"nil error", `C:\npm\openclaw.cmd`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := openclawShimDiagnostic(tc.bin, tc.err); got != "" {
				t.Fatalf("expected no diagnostic, got: %s", got)
			}
		})
	}
}

// TestOpenclawShimDiagnosticSurvivesWrappedError confirms the errors.As gate
// still fires when the exit error arrives wrapped, which is how it reaches this
// code once callers have annotated it.
func TestOpenclawShimDiagnosticSurvivesWrappedError(t *testing.T) {
	pathWithout(t)
	wrapped := errors.Join(errors.New("openclaw config file"), exitError(t))
	shim := filepath.Join(t.TempDir(), "openclaw.cmd")
	if got := openclawShimDiagnostic(shim, wrapped); got == "" {
		t.Fatal("expected diagnostic through a wrapped exit error, got none")
	}
}

// writeShim creates an executable named with a `.cmd` extension running body.
//
// On Unix the shebang makes a `.cmd`-named file genuinely executable, so the
// full execOpenclawCLI integration path is provable on the normal test job. The
// real npm-shim reproduction lives in the windows-tagged test file.
func writeShim(t *testing.T, dir, unixBody, windowsBody string) string {
	t.Helper()
	shim := filepath.Join(dir, "openclaw.cmd")
	body := unixBody
	if runtime.GOOS == "windows" {
		body = windowsBody
	}
	if err := os.WriteFile(shim, []byte(body), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	return shim
}

// TestExecOpenclawCLIAnnotatesSilentShimFailure is the end-to-end proof that
// the diagnostic reaches the error the daemon logs and reports. Before this
// change the message stopped at `exit status 1`, which is what left #6061's
// reporter running their own subprocess experiments to find the cause.
func TestExecOpenclawCLIAnnotatesSilentShimFailure(t *testing.T) {
	shim := writeShim(t, t.TempDir(), "#!/bin/sh\nexit 1\n", "@echo off\r\nexit /b 1\r\n")
	// Set PATH after creating the shim: the shim is invoked by absolute path,
	// while the interpreter lookup must miss.
	pathWithout(t)

	_, err := execOpenclawCLI(context.Background(), shim, "config", "file")
	if err == nil {
		t.Fatal("expected the shim failure to surface as an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "openclaw config file") {
		t.Errorf("error should name the failing subcommand\ngot: %s", msg)
	}
	if !strings.Contains(msg, "resolves neither alongside the shim nor on the daemon PATH") {
		t.Errorf("error should carry the shim diagnostic\ngot: %s", msg)
	}
}

// TestExecOpenclawCLITimeoutIsNotMisdiagnosedAsMissingInterpreter is Sol-Boy's
// must-fix 1, exercised through the real code path rather than by handing the
// diagnostic a synthetic context error.
//
// openclawCLITimeout kills the child through CommandContext, and a killed
// process surfaces as *exec.ExitError ("signal: killed") — the same type a
// genuine exit 1 produces. Without checking the context first, a slow or hung
// CLI was reported as "node is not resolvable, install Node.js", pointing the
// user at something that was never broken.
//
// The shim sleeps only briefly on purpose. execOpenclawCLI sets no WaitDelay
// (see openclawCLITimeout's note on why that is left alone), so cmd.Output()
// stays parked until the output pipes os/exec manages for it — stdout AND
// stderr, since both are set to in-memory writers — reach EOF. A long sleep
// would just make this test hostage to that; it would not leak. The `sleep`
// here inherits those write ends and holds them until it exits, so by the time
// this call returns the helper has finished. (That is a property of this
// helper, not a general rule: a process may close its pipes and keep running.)
func TestExecOpenclawCLITimeoutIsNotMisdiagnosedAsMissingInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by TestWindowsOpenclawShimTimeoutIsNotMisdiagnosed with a real cmd.exe host")
	}
	// Resolve the blocking helper BEFORE PATH is stripped and embed it by
	// absolute path. The shim has to keep running with an empty PATH, so it
	// cannot rely on a PATH lookup of its own: `sh` on macOS quietly falls back
	// to a default PATH, but dash on Linux does not, which made a PATH-relative
	// `sleep` pass locally and fail in CI with "sleep: not found".
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep binary available to build a slow shim: %v", err)
	}
	shim := writeShim(t, t.TempDir(), "#!/bin/sh\n"+sleepBin+" 1\n", "")
	pathWithout(t) // an interpreter lookup, if reached, would report "missing"

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = execOpenclawCLI(ctx, shim, "config", "file")
	if err == nil {
		t.Fatal("expected the timed-out invocation to fail")
	}
	msg := err.Error()
	t.Logf("timeout error: %s", msg)

	// The nit from round 2: the context error must be the wrapped cause, so
	// standard cancellation checks work instead of only string matching.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) must hold\ngot: %s", msg)
	}
	for _, forbidden := range []string{"install Node.js", "resolves neither", "the interpreter is reachable"} {
		if strings.Contains(msg, forbidden) {
			t.Errorf("timeout must not be diagnosed as an interpreter problem (found %q)\ngot: %s", forbidden, msg)
		}
	}
}

// TestExecOpenclawCLICancellationIsWrapped pins the same cancellation contract
// for an explicitly cancelled context, not just a deadline, so a caller can
// distinguish "we gave up" from "the CLI failed" without parsing strings.
func TestExecOpenclawCLICancellationIsWrapped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell shim shape is covered by the windows-tagged tests")
	}
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep binary available to build a slow shim: %v", err)
	}
	shim := writeShim(t, t.TempDir(), "#!/bin/sh\n"+sleepBin+" 1\n", "")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	_, err = execOpenclawCLI(ctx, shim, "config", "file")
	if err == nil {
		t.Fatal("expected the cancelled invocation to fail")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) must hold\ngot: %s", err)
	}
}

// TestExecOpenclawCLIPrefersRealStderr guarantees the diagnostic never masks a
// genuine message from the CLI. This is not hypothetical: windows-latest CI
// showed that a missing `node` DOES reach Go's stderr pipe as "'node' is not
// recognized", so on real Windows the missing-interpreter case takes this
// branch and the diagnostic is only a fallback for a truly silent failure.
func TestExecOpenclawCLIPrefersRealStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by the windows-tagged shim tests with a real cmd.exe host")
	}
	shim := writeShim(t, t.TempDir(), "#!/bin/sh\necho 'openclaw doctor says hello' >&2\nexit 1\n", "")
	pathWithout(t)

	_, err := execOpenclawCLI(context.Background(), shim, "config", "file")
	if err == nil {
		t.Fatal("expected the shim failure to surface as an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "openclaw doctor says hello") {
		t.Errorf("real stderr must be preserved\ngot: %s", msg)
	}
	if strings.Contains(msg, "no stderr output") {
		t.Errorf("diagnostic must not fire when stderr is present\ngot: %s", msg)
	}
}

// TestExecOpenclawCLIPreservesFailedStdoutWithoutLeakingIt covers the process
// boundary behind #7130. cmd.Output returns stdout even when the child exits
// non-zero; callers need that value to inspect OpenClaw's JSON error envelope,
// but it must stay out of the error text because other config commands can
// print resolved configuration and secrets there.
func TestExecOpenclawCLIPreservesFailedStdoutWithoutLeakingIt(t *testing.T) {
	const marker = "stdout-only-sensitive-marker"
	shim := writeShim(t, t.TempDir(),
		"#!/bin/sh\necho '"+marker+"'\nexit 1\n",
		"@echo off\r\necho "+marker+"\r\nexit /b 1\r\n",
	)

	out, err := execOpenclawCLI(context.Background(), shim, "config", "get", "agents.list", "--json")
	if err == nil {
		t.Fatalf("expected the shim failure to surface as an error, got output %q", out)
	}
	if !strings.Contains(out, marker) {
		t.Fatalf("failed stdout was discarded; got %q", out)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("failed stdout leaked into the error text: %s", err)
	}
}

// TestExecOpenclawCLIMissingTempDoesNotChangeOutcome pins the root cause #6061
// originally reported and then retracted. The reporter's own follow-up
// experiment showed `{PATH, SystemRoot}` alone succeeds, so TEMP/TMP must not
// be load-bearing for the OpenClaw CLI invocation. Locking that keeps a future
// change from quietly reintroducing a temp-dir dependency and resurrecting a
// root cause we already ruled out.
func TestExecOpenclawCLIMissingTempDoesNotChangeOutcome(t *testing.T) {
	shim := writeShim(t, t.TempDir(),
		"#!/bin/sh\necho '/tmp/openclaw/config.json'\n",
		"@echo off\r\necho C:\\openclaw\\config.json\r\n",
	)
	t.Setenv("TEMP", "")
	t.Setenv("TMP", "")

	out, err := execOpenclawCLI(context.Background(), shim, "config", "file")
	if err != nil {
		t.Fatalf("invocation must not depend on TEMP/TMP: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected the shim's stdout to be returned")
	}
}

// TestExecOpenclawCLIHandlesShimInPathWithSpacesAndUnicode covers the install
// locations #6061's open questions flagged as unverified. A directory
// containing a space or non-ASCII characters must not break invocation or
// mangle the captured output.
func TestExecOpenclawCLIHandlesShimInPathWithSpacesAndUnicode(t *testing.T) {
	for _, segment := range []string{"Program Files", "用户 開發", "café dir"} {
		t.Run(segment, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), segment)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %q: %v", dir, err)
			}
			shim := writeShim(t, dir, "#!/bin/sh\necho 'ok-marker'\n", "@echo off\r\necho ok-marker\r\n")
			out, err := execOpenclawCLI(context.Background(), shim, "config", "file")
			if err != nil {
				t.Fatalf("shim in %q should be invocable: %v", dir, err)
			}
			if !strings.Contains(out, "ok-marker") {
				t.Fatalf("expected shim stdout to survive intact, got %q", out)
			}
		})
	}
}
