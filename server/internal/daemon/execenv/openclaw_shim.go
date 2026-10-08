package execenv

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// openclawShimExtensions are the batch-wrapper extensions npm uses when it
// installs the `openclaw` command on Windows. The shim is not the real
// program: it re-execs OpenClaw's JavaScript entrypoint through an
// interpreter, so a shim path that resolves and is executable says nothing
// about whether the interpreter it depends on is reachable.
var openclawShimExtensions = map[string]struct{}{
	".cmd": {},
	".bat": {},
}

// openclawShimInterpreter is the interpreter an npm-installed OpenClaw shim
// re-execs. OpenClaw's package `bin` entry points at `openclaw.mjs`, whose
// shebang is `#!/usr/bin/env node`, so an unreachable `node` breaks the shim
// from the inside while the shim itself still looks valid to the daemon.
const openclawShimInterpreter = "node"

// isOpenclawShimPath reports whether bin is a batch shim rather than a directly
// executable binary.
//
// Keyed on the file extension alone, deliberately not on runtime.GOOS: a
// `.cmd`/`.bat` shim only ever appears on Windows in production, and testing
// the extension instead of the host OS lets the whole diagnostic be exercised
// from the normal Linux/macOS test job rather than only on a Windows runner.
//
// A batch extension is NOT proof the file is an npm shim — an operator can
// point MULTICA_OPENCLAW_PATH at any batch file. The diagnostic below is
// therefore phrased conditionally and never asserts npm shim semantics as fact.
func isOpenclawShimPath(bin string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(bin)))
	_, ok := openclawShimExtensions[ext]
	return ok
}

// openclawInterpreterOrigin describes where a shim's interpreter was found,
// without disclosing an absolute path. See openclawShimDiagnostic for why the
// path itself is withheld.
type openclawInterpreterOrigin struct {
	found bool
	// where is a human phrase ("alongside the shim", "on the daemon PATH"),
	// empty when the interpreter was not found at all.
	where string
}

// findOpenclawShimInterpreter resolves the interpreter the way npm's generated
// shim does, in npm's own order.
//
// npm's cmd-shim template emits:
//
//	IF EXIST "%dp0%\node.exe" ( SET "_prog=%dp0%\node.exe" ) ELSE ( SET "_prog=node" )
//
// so a Node binary sitting next to the shim wins over PATH entirely. Checking
// only PATH (as the first version of this diagnostic did) would report "node is
// not resolvable" for an install that actually runs fine off its co-located
// interpreter — a confidently wrong root cause, which is worse than no hint.
func findOpenclawShimInterpreter(shimPath string) openclawInterpreterOrigin {
	dir := filepath.Dir(shimPath)
	// `.exe` first, matching npm's IF EXIST check; the bare name keeps the
	// helper meaningful on the non-Windows hosts the tests run on.
	for _, name := range []string{openclawShimInterpreter + ".exe", openclawShimInterpreter} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return openclawInterpreterOrigin{found: true, where: "alongside the shim"}
		}
	}
	if _, err := exec.LookPath(openclawShimInterpreter); err == nil {
		return openclawInterpreterOrigin{found: true, where: "on the daemon PATH"}
	}
	return openclawInterpreterOrigin{}
}

// openclawShimDiagnostic explains a batch-shim invocation that failed without
// writing anything to stderr, and returns "" when it has nothing to add.
//
// Why this exists (MUL-5422 / #6061): a Windows user reported every OpenClaw
// task failing in execenv prep with a bare `exit status 1` and no stderr. The
// daemon pins `openclaw` to an absolute path, so the failing command looked
// correct; what the error could not show is that a shim's interpreter lookup is
// a second, invisible resolution step that can fail on its own.
//
// Scope note: CI on windows-latest showed that when `node` is genuinely missing,
// cmd.exe's "'node' is not recognized" DOES reach Go's stderr pipe, so that case
// takes the caller's stderr branch and never arrives here. This diagnostic is
// the fallback for a shim that fails while saying nothing at all — which is what
// #6061's daemon log actually showed, and remains unexplained.
//
// This only enriches error text — it never changes control flow, and never
// suppresses a real stderr message (callers try stderr first).
//
// # Redaction
//
// The returned string is NOT local-log-only: on prep failure it travels through
// reportTerminalTask → Client.FailTask to the server and is persisted as the
// task's error. A Windows shim path embeds the account name and install layout
// (`C:\Users\<name>\AppData\Roaming\npm\...`), so this reports only the shim's
// base name, whether the interpreter resolved, and a PATH entry count — never an
// absolute path and never the PATH contents.
func openclawShimDiagnostic(bin string, runErr error) string {
	// Only an actual non-zero exit is in scope. A missing binary or permission
	// error already describes itself.
	//
	// Note this gate is necessary but NOT sufficient: a context timeout kills
	// the child and also surfaces as *exec.ExitError ("signal: killed"), which
	// would be misdiagnosed here as an interpreter problem. Callers must
	// attribute context cancellation before consulting this function.
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		return ""
	}
	if !isOpenclawShimPath(bin) {
		return ""
	}

	name := filepath.Base(strings.TrimSpace(bin))
	pathSummary := openclawPathEntrySummary()
	origin := findOpenclawShimInterpreter(bin)
	if !origin.found {
		return fmt.Sprintf(
			"no stderr output; if %s is an npm-generated shim it re-execs %q, which resolves neither "+
				"alongside the shim nor on the daemon PATH (%s) — install Node.js, or restart the daemon "+
				"from an environment where %q is on PATH",
			name, openclawShimInterpreter, pathSummary, openclawShimInterpreter,
		)
	}
	// The interpreter being reachable is the more valuable report: it clears
	// PATH of blame and redirects to the remaining hypotheses (PATH drift
	// between the runtime `--version` gate and task prep, or a broken install).
	return fmt.Sprintf(
		"no stderr output; %q resolves %s, so the interpreter is reachable — if %s is an "+
			"npm-generated shim, check the OpenClaw install itself rather than the daemon PATH (%s)",
		openclawShimInterpreter, origin.where, name, pathSummary,
	)
}

// openclawPathEntrySummary describes the daemon PATH by size alone. A count is
// enough to tell "the daemon inherited a stripped environment" apart from "PATH
// looks normal but the interpreter still is not on it", without copying the
// user's PATH into a task error that is persisted server-side.
func openclawPathEntrySummary() string {
	if n := len(filepath.SplitList(os.Getenv("PATH"))); n != 1 {
		return fmt.Sprintf("%d entries", n)
	}
	return "1 entry"
}
