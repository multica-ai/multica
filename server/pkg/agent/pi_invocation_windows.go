//go:build windows

package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// platformPiInvocation rewrites the pi invocation on Windows when the
// resolved executable is the npm-installed pi.cmd launcher (or a .bat
// alias) that delegates to pi.ps1.
//
// We replace
//
//	pi.cmd <args...>
//
// with
//
//	powershell.exe -NoProfile -ExecutionPolicy Bypass -File pi.ps1 <args...>
//
// which is what the .cmd does internally, but lets Go pass each arg as
// a discrete token instead of routing through cmd.exe's %* re-expansion
// (which mangles the multi-line positional prompt the daemon builds in
// buildPiArgs — see #3306).
//
// powerShellLookup is shared with the cursor backend: both npm shims
// have the same launcher shape and need the same PowerShell host on the
// same Windows installation.
func platformPiInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	ext := strings.ToLower(filepath.Ext(lookedUp))
	if ext != ".cmd" && ext != ".bat" {
		return "", nil, false
	}
	dir := filepath.Dir(lookedUp)
	ps1 := filepath.Join(dir, "pi.ps1")
	if st, err := os.Stat(ps1); err != nil || st.IsDir() {
		return "", nil, false
	}

	psExe, ok := powerShellLookup()
	if !ok {
		return "", nil, false
	}

	full := make([]string, 0, 5+len(args))
	full = append(full, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ps1)
	full = append(full, args...)

	if logger != nil {
		logger.Info("pi: routing through powershell -File to preserve argv tokens",
			"powershell", psExe,
			"ps1", ps1,
			"original", lookedUp,
		)
	}
	return psExe, full, true
}
