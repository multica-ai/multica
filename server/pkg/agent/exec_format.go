package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// ExplainExecError annotates err with an actionable diagnosis when the OS
// refused to run the agent CLI because the file is not a runnable binary for
// this platform (ENOEXEC). Every other error is returned unchanged.
//
// npm-distributed agent CLIs ship a placeholder text stub at their bin entry
// point and rely on a postinstall script to overwrite it with the
// platform-native binary — @anthropic-ai/claude-code (bin/claude.exe, the
// `.exe` name is used on every platform) and opencode-ai (bin/opencode.exe)
// both have this shape. Any install that blocks that script leaves the stub in
// place with its executable bit set: npm 12 blocks install scripts unless they
// are allowlisted, pnpm 10 needs `pnpm approve-builds`, and --ignore-scripts /
// --omit=optional do the same. exec.LookPath accepts the stub (it is an
// executable file) while execve rejects it (no shebang, no native header), so
// the CLI reads as installed everywhere the daemon looks and only fails at the
// moment of truth — with a bare "fork/exec <path>: exec format error" that
// names neither the cause nor the fix (MUL-6164).
//
// The executable path is read back out of the error instead of being passed
// in, so this stays one provider-agnostic call per boundary rather than a wrap
// inside every backend.
//
// Applying it twice to the same error is a no-op. Boundaries nest — a backend's
// preflight error travels out through the daemon's launch boundary — and a
// diagnosis printed twice is worse than the bare errno it replaced.
func ExplainExecError(err error) error {
	if err == nil || !errors.Is(err, syscall.ENOEXEC) {
		return err
	}
	var explained explainedExecError
	if errors.As(err, &explained) {
		return err
	}
	return explainedExecError{fmt.Errorf("%w: %s", err, execFormatDiagnosis(execPathFromError(err)))}
}

// explainedExecError marks an error that already carries the diagnosis, so an
// outer boundary can recognise its own work through any number of %w wraps.
type explainedExecError struct{ error }

func (e explainedExecError) Unwrap() error { return e.error }

// execPathFromError recovers the executable path the OS refused to run.
// os/exec reports a failed launch as *fs.PathError (fork/exec) and a failed
// lookup as *exec.Error; both carry the path, so neither call site has to
// thread it through separately.
func execPathFromError(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Path
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return execErr.Name
	}
	return ""
}

// blockedPostinstallCauses lists the install paths that skip a package's
// postinstall. Kept in the message because the user's own install command is
// the only place this can be fixed, and which one applies is not something the
// daemon can observe after the fact.
const blockedPostinstallCauses = "npm 12 allowScripts, pnpm 10 approve-builds, --ignore-scripts, --omit=optional"

func execFormatDiagnosis(execPath string) string {
	target := execPath
	if target == "" {
		target = "the agent CLI"
	}
	pkg, command, ok := npmPackagePostinstall(execPath)
	if !ok {
		return fmt.Sprintf(
			"%s is not a runnable executable on %s/%s. For an agent CLI installed from npm this usually means its postinstall never ran (%s all block it), leaving the package's placeholder stub at that path. Reinstall the CLI with install scripts enabled, then retry",
			target, runtime.GOOS, runtime.GOARCH, blockedPostinstallCauses,
		)
	}
	return fmt.Sprintf(
		"%s is not a runnable executable on %s/%s. It is the bin entry of npm package %s, so the usual cause is that its postinstall never ran (%s all block it), leaving the package's placeholder stub in place. Run `%s` to install the native binary, or reinstall the CLI with install scripts enabled, then retry",
		target, runtime.GOOS, runtime.GOARCH, pkg, blockedPostinstallCauses, command,
	)
}

// npmPackagePostinstall reports the npm package owning execPath and the
// postinstall command npm would have run for it, so the message can name the
// exact repair command instead of a generic "reinstall".
//
// It only recognises the one layout that produces this failure — a bin/ entry
// point directly under the package root — and reads the repair command out of
// the package's own manifest. That keeps it provider-agnostic: claude's
// `node install.cjs` and opencode's `node ./postinstall.mjs` are both just
// whatever scripts.postinstall says, and a CLI added later needs no change here.
func npmPackagePostinstall(execPath string) (name, command string, ok bool) {
	if execPath == "" {
		return "", "", false
	}
	dir := filepath.Dir(execPath)
	if !strings.EqualFold(filepath.Base(dir), "bin") {
		return "", "", false
	}
	root := filepath.Dir(dir)
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return "", "", false
	}
	var manifest struct {
		Name    string `json:"name"`
		Scripts struct {
			Postinstall string `json:"postinstall"`
		} `json:"scripts"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", "", false
	}
	script := strings.TrimSpace(manifest.Scripts.Postinstall)
	if script == "" {
		return "", "", false
	}
	name = strings.TrimSpace(manifest.Name)
	if name == "" {
		name = filepath.Base(root)
	}
	return name, fmt.Sprintf("cd %s && %s", shellQuote(root), script), true
}

// shellQuote makes an arbitrary path safe to paste into a POSIX shell. The
// repair command is meant to be copied and run verbatim, so a path holding a
// quote or a space has to survive the round trip intact.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
