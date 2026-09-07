package agent

import "log/slog"

// chooseCommandCodeInvocation selects the actual program (argv[0]) and the full
// argv to spawn a CommandCode run.
//
// CommandCode ships as a single native binary (`cmd`) with no `.cmd`/`.ps1`
// shim, so unlike Qwen Code on Windows there is no shell re-tokenisation hazard
// to route around: Go's os/exec passes argv through unchanged on every
// platform. The task prompt itself is delivered through stdin and never appears
// in argv (#6082). This is therefore a pure passthrough.
func chooseCommandCodeInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	return execName, args
}
