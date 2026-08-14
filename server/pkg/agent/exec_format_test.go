package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// enoexecError is the error shape os/exec produces when execve refuses a file
// that is executable but not a runnable program (no shebang, no native
// header). Built directly so the pure-function assertions run on every
// platform, including Windows, where the OS reports a different errno.
func enoexecError(path string) error {
	return &fs.PathError{Op: "fork/exec", Path: path, Err: syscall.ENOEXEC}
}

// writePlaceholderPackage stages the layout an npm package with a native
// binary has when its postinstall never ran: a bin/ entry that is executable
// but is really the shipped text stub, next to a manifest declaring the
// postinstall that was supposed to replace it.
func writePlaceholderPackage(t *testing.T, pkgName, postinstall string) (root, execPath string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "claude-code")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	manifest := `{"name":"` + pkgName + `","scripts":{"postinstall":"` + postinstall + `"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	execPath = filepath.Join(binDir, "claude.exe")
	stub := "echo \"Error: claude native binary not installed.\" >&2\nexit 1\n"
	if err := os.WriteFile(execPath, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return root, execPath
}

func TestExplainExecErrorNamesPackageAndRepairCommand(t *testing.T) {
	root, execPath := writePlaceholderPackage(t, "@anthropic-ai/claude-code", "node install.cjs")

	explained := ExplainExecError(enoexecError(execPath))
	msg := explained.Error()

	if !errors.Is(explained, syscall.ENOEXEC) {
		t.Fatalf("ENOEXEC must survive wrapping so callers can still branch on it; got %v", explained)
	}
	for _, want := range []string{
		execPath,
		"@anthropic-ai/claude-code",
		"postinstall",
		"cd '" + root + "' && node install.cjs",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("diagnosis is missing %q:\n%s", want, msg)
		}
	}
}

// The repair command comes from the package's own manifest, so a provider with
// a differently named installer needs no code change here.
func TestExplainExecErrorReadsRepairCommandFromManifest(t *testing.T) {
	root, execPath := writePlaceholderPackage(t, "opencode-ai", "node ./postinstall.mjs")

	msg := ExplainExecError(enoexecError(execPath)).Error()

	if !strings.Contains(msg, "opencode-ai") {
		t.Errorf("diagnosis should name the owning package:\n%s", msg)
	}
	if want := "cd '" + root + "' && node ./postinstall.mjs"; !strings.Contains(msg, want) {
		t.Errorf("diagnosis is missing %q:\n%s", want, msg)
	}
}

// A binary that is not an npm bin entry (native installer, app bundle) still
// gets the generic guidance instead of a bare "exec format error".
func TestExplainExecErrorFallsBackWithoutManifest(t *testing.T) {
	execPath := filepath.Join(t.TempDir(), "claude")

	msg := ExplainExecError(enoexecError(execPath)).Error()

	if !strings.Contains(msg, execPath) {
		t.Errorf("diagnosis should name the path:\n%s", msg)
	}
	if !strings.Contains(msg, "install scripts enabled") {
		t.Errorf("diagnosis should still say how to repair the install:\n%s", msg)
	}
	if strings.Contains(msg, "cd '") {
		t.Errorf("no manifest means no concrete repair command to offer:\n%s", msg)
	}
}

// Boundaries nest: a backend preflight that already explained the error hands
// it to the daemon's launch boundary, which explains errors too. The second
// pass must not append a second copy of the same paragraph.
func TestExplainExecErrorIsIdempotentAcrossNestedBoundaries(t *testing.T) {
	_, execPath := writePlaceholderPackage(t, "@anthropic-ai/claude-code", "node install.cjs")

	inner := ExplainExecError(enoexecError(execPath))
	// The inner boundary wraps with %w on its way out, exactly as a backend
	// does before returning to the daemon.
	wrapped := fmt.Errorf("openclaw --version failed: %w", inner)
	outer := ExplainExecError(wrapped)

	if got := strings.Count(outer.Error(), "is not a runnable executable on"); got != 1 {
		t.Errorf("diagnosis should appear exactly once, got %d:\n%s", got, outer.Error())
	}
	if outer.Error() != wrapped.Error() {
		t.Errorf("re-explaining must leave the message untouched:\n got %s\nwant %s", outer.Error(), wrapped.Error())
	}
	if !errors.Is(outer, syscall.ENOEXEC) {
		t.Errorf("ENOEXEC must survive both passes, got %v", outer)
	}
}

func TestExplainExecErrorQuotesPathsForTheShell(t *testing.T) {
	root := filepath.Join(t.TempDir(), "it's a dir")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	manifest := `{"name":"pkg","scripts":{"postinstall":"node install.cjs"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	msg := ExplainExecError(enoexecError(filepath.Join(binDir, "cli"))).Error()

	// A single quote inside the path has to be closed, escaped, and reopened,
	// or the command the user pastes runs against a different directory.
	want := `cd '` + strings.ReplaceAll(root, "'", `'\''`) + `' && node install.cjs`
	if !strings.Contains(msg, want) {
		t.Errorf("repair command is not shell-safe, want %q in:\n%s", want, msg)
	}
}

func TestExplainExecErrorPassesThroughOtherErrors(t *testing.T) {
	if got := ExplainExecError(nil); got != nil {
		t.Errorf("nil must stay nil, got %v", got)
	}
	other := errors.New("exit status 1")
	if got := ExplainExecError(other); got != other {
		t.Errorf("a non-ENOEXEC error must be returned unchanged, got %v", got)
	}
	// Missing executables have their own reason and their own message; this
	// diagnosis must not hijack them.
	notFound := &exec.Error{Name: "claude", Err: exec.ErrNotFound}
	if got := ExplainExecError(notFound); got != error(notFound) {
		t.Errorf("a lookup failure must be returned unchanged, got %v", got)
	}
}

// The synthetic error above has to match what the OS actually produces —
// otherwise every assertion here tests a shape that never occurs in the field.
func TestExplainExecErrorMatchesRealExecFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows rejects a non-executable file with ERROR_BAD_EXE_FORMAT, not ENOEXEC")
	}
	_, execPath := writePlaceholderPackage(t, "@anthropic-ai/claude-code", "node install.cjs")

	// The stub is executable, so it passes the same check the daemon uses to
	// decide the CLI is installed.
	if _, err := exec.LookPath(execPath); err != nil {
		t.Fatalf("placeholder stub should pass LookPath, got %v", err)
	}
	err := exec.Command(execPath, "--version").Start()
	if !errors.Is(err, syscall.ENOEXEC) {
		t.Fatalf("expected the OS to reject the stub with ENOEXEC, got %v", err)
	}
	if !strings.Contains(ExplainExecError(err).Error(), "node install.cjs") {
		t.Errorf("a real launch failure should carry the repair command: %v", ExplainExecError(err))
	}
}

// The repair command is meant to be pasted into a terminal on the machine that
// reported it, so it has to be written for THAT machine's shell. Windows was
// getting POSIX syntax: `cd 'C:\\...' && node install.cjs` is wrong twice over
// — cmd.exe treats the quotes as literal characters, and PowerShell 5.1, which
// still ships with Windows, has no `&&`.
func TestRepairCommandIsWrittenForTheHostShell(t *testing.T) {
	posix := repairCommand("/usr/lib/node_modules/@anthropic-ai/claude-code", "node install.cjs", shellBash)
	if posix != "cd '/usr/lib/node_modules/@anthropic-ai/claude-code' && node install.cjs" {
		t.Errorf("posix repair = %q", posix)
	}

	windows := repairCommand(`C:\Program Files\nodejs\node_modules\@anthropic-ai\claude-code`, "node install.cjs", shellPowerShell)
	want := "Set-Location 'C:\\Program Files\\nodejs\\node_modules\\@anthropic-ai\\claude-code'\nnode install.cjs"
	if windows != want {
		t.Errorf("windows repair =\n%q\nwant\n%q", windows, want)
	}
	// No `&&` anywhere: PowerShell 5.1 would reject the whole line.
	if strings.Contains(windows, "&&") {
		t.Errorf("windows repair uses &&, which PowerShell 5.1 cannot run: %q", windows)
	}
}

// Paths with a quote in them are rare and catastrophic: the pasted command
// silently targets a different directory.
func TestRepairCommandQuotesPathsPerShell(t *testing.T) {
	if got := repairCommand("/home/it's mine/pkg", "node install.cjs", shellBash); got != `cd '/home/it'\''s mine/pkg' && node install.cjs` {
		t.Errorf("posix quoting = %q", got)
	}
	if got := repairCommand(`C:\Users\it's mine\pkg`, "node install.cjs", shellPowerShell); got != "Set-Location 'C:\\Users\\it''s mine\\pkg'\nnode install.cjs" {
		t.Errorf("powershell quoting = %q", got)
	}
}

// The shell travels with the command so whoever renders it can label the code
// block truthfully instead of guessing.
func TestExecFormatRepairReportsItsShell(t *testing.T) {
	_, execPath := writePlaceholderPackage(t, "@anthropic-ai/claude-code", "node install.cjs")

	repair, ok := execFormatRepairForShell(execPath, shellPowerShell)
	if !ok {
		t.Fatal("expected a repair for a staged npm package")
	}
	if repair.Shell != shellPowerShell {
		t.Errorf("repair shell = %q, want %q", repair.Shell, shellPowerShell)
	}
	if !strings.HasPrefix(repair.Command, "Set-Location ") {
		t.Errorf("repair command is not PowerShell: %q", repair.Command)
	}
	// And the host default still resolves to something coherent.
	host, ok := ExecFormatRepairFor(execPath)
	if !ok || host.Shell == "" || host.Command == "" {
		t.Errorf("host repair is incomplete: %+v", host)
	}
}
