//go:build windows

package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// platformPiInvocation rewrites pi.cmd → PowerShell -File pi.ps1 on
// Windows to avoid cmd.exe %* re-tokenisation (see #3306).
// powerShellLookup and rewriteCmdToPS1 are defined in cursor_invocation_windows.go.
func platformPiInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	if isPiRPCInvocation(args) {
		return rewriteCmdToPS1Command("pi", lookedUp, args, logger)
	}
	return rewriteCmdToPS1("pi", lookedUp, args, logger)
}

func isPiRPCInvocation(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--mode" && i+1 < len(args) && args[i+1] == "rpc" {
			return true
		}
		if arg == "--mode=rpc" {
			return true
		}
	}
	return false
}

func rewriteCmdToPS1Command(toolName, lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
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

	quotedPS1 := strings.ReplaceAll(ps1, "'", "''")
	full := make([]string, 0, 6+len(args))
	full = append(full, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "& '"+quotedPS1+"' @args")
	full = append(full, args...)

	if logger != nil {
		logger.Info(toolName+": routing RPC through powershell -Command to preserve stdin",
			"powershell", psExe,
			"ps1", ps1,
			"original", lookedUp,
		)
	}
	return psExe, full, true
}
