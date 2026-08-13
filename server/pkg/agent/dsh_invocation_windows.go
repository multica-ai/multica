//go:build windows

package agent

import "log/slog"

// platformDshInvocation rewrites the npm-installed dsh.cmd launcher to
// PowerShell -File dsh.ps1 on Windows. npm ships both launchers; routing
// through cmd.exe would re-tokenise the raw command line via %*, which mangles
// the multi-line, quote-bearing task prompt. PowerShell -File receives each
// argv element as a discrete token (see rewriteCmdToPS1).
func platformDshInvocation(execPath string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1("dsh", execPath, args, logger)
}
