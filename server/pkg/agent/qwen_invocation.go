package agent

import "log/slog"

// chooseQwenInvocation selects the actual program (argv[0]) and the full argv
// to spawn a Qwen Code run.
//
// Background:
//   - On macOS/Linux the npm-installed `qwen` binstub is a shebang script that
//     execs node with the JS entrypoint, so argv passes through unchanged.
//   - On Windows npm ships qwen.cmd instead. CreateProcess for a .cmd file goes
//     through cmd.exe, and %* in a .cmd batch file is expanded by re-tokenising
//     the original command line, which mangles arguments containing newlines,
//     quotes or `&`. buildQwenArgs puts the task prompt on argv as the `-p`
//     value and the stream-json protocol flags behind it, so a real chat prompt
//     does not survive that re-expansion: the daemon saw qwen exit 0 having
//     emitted a single unparseable stdout line and no `result` event, surfaced
//     as "qwen stream ended without terminal result" (#6082). To stay on the
//     official launch path while avoiding the re-tokenisation, we resolve
//     qwen.ps1 next to the .cmd and invoke PowerShell with `-File <ps1>`
//     directly, letting Go pass each argv as a separate token.
//
// The Windows-specific behaviour is implemented in qwen_invocation_windows.go;
// on other platforms we fall through to a passthrough.
func chooseQwenInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	if argv0, full, ok := platformQwenInvocation(lookedUp, args, logger); ok {
		return argv0, full
	}
	return execName, args
}
