//go:build windows

package agent

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// powerShellLookup resolves the PowerShell host to use. It is overridable in
// tests; production callers should leave it at its default.
var powerShellLookup = defaultPowerShellLookup

// platformCursorInvocation rewrites the cursor-agent invocation on Windows
// when the resolved executable is the official cursor-agent.cmd launcher
// (or a .bat alias) that delegates to cursor-agent.ps1.
//
// We replace
//
//	cursor-agent.cmd <args...>
//
// with
//
//	powershell.exe -NoProfile -ExecutionPolicy Bypass -File cursor-agent.ps1 <args...>
//
// which is exactly what the .cmd does internally, but lets Go pass each arg
// as a discrete token instead of routing through cmd.exe's %* re-expansion
// (which mangles multi-line / whitespace-heavy prompts such as a long -p).
func platformCursorInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	ext := strings.ToLower(filepath.Ext(lookedUp))
	if ext != ".cmd" && ext != ".bat" {
		return "", nil, false
	}
	dir := filepath.Dir(lookedUp)
	ps1 := filepath.Join(dir, "cursor-agent.ps1")
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
		logger.Info("cursor-agent: routing through powershell -File to preserve argv tokens",
			"powershell", psExe,
			"ps1", ps1,
			"original", lookedUp,
		)
	}
	return psExe, full, true
}

// defaultPowerShellLookup prefers PowerShell on PATH (PowerShell 7's pwsh.exe
// or any user-overridden powershell.exe) and falls back to the system path
// shipped with Windows.
func defaultPowerShellLookup() (string, bool) {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, true
		}
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	candidate := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate, true
	}
	return "", false
}
