package agent

import "log/slog"

// chooseZcodeInvocation selects the actual program (argv[0]) and the full argv
// to spawn a ZCode CLI run.
//
//   - On macOS/Linux the npm-installed `zcode` binstub is a shebang script that
//     execs node with the JS entrypoint, so argv passes through unchanged.
//   - On Windows npm ships zcode.cmd instead. CreateProcess for a .cmd file
//     goes through cmd.exe, and %* in a .cmd batch file is expanded by
//     re-tokenising the original command line. To keep the stream protocol and
//     custom flags out of that re-tokenisation path, we resolve zcode.ps1 next
//     to the .cmd and invoke PowerShell with `-File <ps1>` directly, letting Go
//     pass each argv as a separate token.
//
// Unlike qwen, the task prompt itself still travels on argv: ZCode has no
// stdin prompt mode (see buildZcodeArgs), so prompts containing embedded
// double quotes remain a known PowerShell limitation on Windows.
//
// The Windows-specific behaviour is implemented in zcode_invocation_windows.go;
// on other platforms we fall through to a passthrough.
func chooseZcodeInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	if argv0, full, ok := platformZcodeInvocation(lookedUp, args, logger); ok {
		return argv0, full
	}
	return execName, args
}
