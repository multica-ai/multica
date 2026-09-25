package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// chooseCopilotInvocation selects the actual program (argv[0]) and the full
// argv to spawn a Copilot CLI run.
//
// On macOS/Linux the npm binstub is a symlink to npm-loader.js with a
// `#!/usr/bin/env node` shebang, so execve hands argv to node unchanged and
// node's spawnSync forwards it to the bundled native binary unchanged.
//
// On Windows there is no shebang, so npm ships copilot.cmd/copilot.ps1
// launchers and every layer in between re-serialises argv onto a new command
// line. Both launchers are lossy for the multi-line, quote-bearing -p prompt
// built by buildCopilotArgs; the symptom is Copilot's own
// "It looks like your prompt was not quoted, so the extra words were treated
// as separate arguments" error. The Windows rewrite in
// copilot_invocation_windows.go therefore skips the launchers and spawns the
// bundled copilot.exe directly.
func chooseCopilotInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	if argv0, full, ok := platformCopilotInvocation(lookedUp, args, logger); ok {
		return argv0, full
	}
	return execName, args
}

// resolveCopilotNativeFromShim returns the path to the native Copilot
// executable bundled inside the npm package, given the path to the npm
// `copilot.cmd` shim that PATH lookup found on Windows. Returns "" if the
// shim doesn't end in `.cmd` or no candidate platform package ships a binary
// at either known location, in which case the caller falls back to the
// PowerShell launcher.
//
// Why bypass the launchers instead of quoting harder: nothing on the Go side
// can make them lossless. Both hops re-parse a command line with rules that
// differ from the CRT rules Go's os/exec escapes for.
//
//   - copilot.cmd forwards with `%*`, which cmd.exe expands by re-tokenising
//     the raw command line — newlines and quotes in the -p prompt do not
//     survive.
//   - copilot.ps1 ends in `& node.exe npm-loader.js $args`, and PowerShell
//     re-serialises $args onto node's command line. Under Windows PowerShell
//     5.1 (and pwsh <= 7.2, which default to Legacy native argument passing)
//     embedded double quotes are not re-escaped, so a prompt containing them
//     is re-tokenised — the same defect already documented for cursor-agent
//     in buildCursorArgs (#5649).
//
// Copilot has no stdin prompt channel (the escape hatch cursor-agent offers),
// so the prompt must stay on the command line and the launchers have to go.
// Spawning copilot.exe directly leaves exactly one hop, Go -> native binary,
// and Go's syscall.EscapeArg is the exact inverse of the CRT parsing the
// binary uses.
//
// Layout when installed via `npm install -g @github/copilot` (verified
// against @github/copilot 1.0.77):
//
//	<prefix>\copilot.cmd                                                                     (shim)
//	<prefix>\node_modules\@github\copilot\npm-loader.js                                      (JS entry)
//	<prefix>\node_modules\@github\copilot\node_modules\@github\copilot-win32-x64\copilot.exe (native)
//
// npm keeps the platform package nested under the parent package; older npm
// versions and other package managers hoist it to the top-level
// node_modules instead, so both locations are probed.
//
// statFn is injected so this is testable on non-Windows hosts.
func resolveCopilotNativeFromShim(shimPath string, statFn func(string) (os.FileInfo, error)) string {
	if !strings.EqualFold(filepath.Ext(shimPath), ".cmd") {
		return ""
	}
	prefix := filepath.Dir(shimPath)
	for _, pkg := range copilotWindowsPackageCandidates(runtime.GOARCH) {
		candidates := []string{
			// npm's current layout: optional platform dep nested under the parent.
			filepath.Join(prefix, "node_modules", "@github", "copilot", "node_modules", "@github", pkg, "copilot.exe"),
			// Hoisted layout used by older npm versions and other installers.
			filepath.Join(prefix, "node_modules", "@github", pkg, "copilot.exe"),
		}
		for _, candidate := range candidates {
			if _, err := statFn(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

// copilotWindowsPackageCandidates returns the npm platform package names that
// may host the bundled copilot.exe, ordered so the most likely match for the
// given GOARCH comes first. ARM64 hosts try the arm64 build first; everything
// else tries x64 first, because an x64 Node running under emulation on an
// ARM64 host installs the x64 platform package. Cost is one extra statFn call
// per miss when the GOARCH-preferred package isn't installed.
func copilotWindowsPackageCandidates(goarch string) []string {
	switch goarch {
	case "arm64":
		return []string{"copilot-win32-arm64", "copilot-win32-x64"}
	default:
		return []string{"copilot-win32-x64", "copilot-win32-arm64"}
	}
}
