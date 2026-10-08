//go:build windows

package agent

import (
	"log/slog"
	"os"
)

// platformCopilotInvocation keeps the multi-line -p prompt built by
// buildCopilotArgs intact on Windows by spawning the native copilot.exe
// bundled in the npm package, so neither cmd.exe's %* expansion nor
// PowerShell's $args re-serialisation can re-tokenise it (see
// resolveCopilotNativeFromShim for why neither launcher can be made safe).
//
// When the native binary can't be located — a partial install, or a Copilot
// build old enough to predate the platform packages — fall back to
// `powershell -File copilot.ps1`, which still avoids the strictly worse
// cmd.exe layer. powerShellLookup and rewriteCmdToPS1 are defined in
// cursor_invocation_windows.go.
func platformCopilotInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	if native := resolveCopilotNativeFromShim(lookedUp, os.Stat); native != "" {
		if logger != nil {
			logger.Info("copilot: spawning the bundled native binary to keep argv intact",
				"shim", lookedUp,
				"native", native,
			)
		}
		return native, args, true
	}
	return rewriteCmdToPS1("copilot", lookedUp, args, logger)
}
