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

// rewriteCmdToPS1 handles the .cmd → PowerShell rewrite for npm-installed
// agent CLIs on Windows. Each CLI ships a <name>.cmd launcher whose body is
// effectively
//
//	powershell -NoProfile -ExecutionPolicy Bypass -File <name>.ps1 %*
//
// Going through cmd.exe causes %* re-expansion to re-tokenise the raw
// command-line string, which mangles multi-line arguments (most critically
// the -p prompt). We instead invoke PowerShell with -File <name>.ps1
// directly so Go passes each argv element as a discrete token.
//
// toolName is used both for the log message and to derive the canonical ps1
// filename (toolName + ".ps1" in the same directory as lookedUp). Using the
// canonical name rather than the launcher basename ensures we find the right
// script even when the user has a custom .bat wrapper with a different name.
func rewriteCmdToPS1(toolName, lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	ext := strings.ToLower(filepath.Ext(lookedUp))
	if ext != ".cmd" && ext != ".bat" {
		return "", nil, false
	}
	ps1 := filepath.Join(filepath.Dir(lookedUp), toolName+".ps1")
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
		logger.Info(toolName+": routing through powershell -File to preserve argv tokens",
			"powershell", psExe,
			"ps1", ps1,
			"original", lookedUp,
		)
	}
	return psExe, full, true
}

// platformCursorInvocation rewrites cursor-agent.cmd → PowerShell -File
// cursor-agent.ps1 on Windows to avoid cmd.exe %* re-tokenisation.
func platformCursorInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1("cursor-agent", lookedUp, args, logger)
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
