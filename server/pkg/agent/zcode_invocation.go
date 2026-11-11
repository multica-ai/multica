package agent

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// chooseZcodeInvocation selects the actual program (argv[0]) and the full argv
// to spawn a ZCode run.
//
// ZCode ships as a Node bundle (zcode.cjs) inside the desktop app rather than
// as a native binary, so a resolved .cjs/.mjs path runs under `node`. Callers
// (Execute, model discovery, --version probes) must ensure `node` is on PATH
// before spawning — see requireNodeForZcodeScript. Shims and native builds
// pass through unchanged.
func chooseZcodeInvocation(execName, lookedUp string, args []string, _ *slog.Logger) (string, []string) {
	if isNodeScriptPath(lookedUp) {
		return "node", append([]string{lookedUp}, args...)
	}
	return execName, args
}

func isNodeScriptPath(path string) bool {
	return strings.HasSuffix(path, ".cjs") || strings.HasSuffix(path, ".mjs")
}

// requireNodeForZcodeScript reports a clear error when a Desktop .cjs/.mjs
// entry needs Node.js on PATH and none is resolvable. Native/shim paths pass.
func requireNodeForZcodeScript(lookedUp string) error {
	if !isNodeScriptPath(lookedUp) {
		return nil
	}
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("ZCode Desktop requires Node.js on PATH to run %s, but node was not found; install Node.js or ensure the daemon inherits a PATH that includes it", filepath.Base(lookedUp))
	}
	return nil
}

// zcodeRuntimeEnv supplies the companion config shipped in the Desktop app.
// The bundled CLI's relative lookup does not cover Resources/glm in 0.17.
// Explicit environment configuration always wins.
func zcodeRuntimeEnv(executable string, env []string) []string {
	const key = "ZCODE_BUILTIN_PROVIDER_CONFIG_FILE"
	for _, entry := range env {
		if strings.HasPrefix(entry, key+"=") {
			return env
		}
	}
	if !isNodeScriptPath(executable) {
		return env
	}
	config := filepath.Join(filepath.Dir(executable), "..", "config", "provider", "zcode-builtin.json")
	if info, err := os.Stat(config); err == nil && info.Mode().IsRegular() {
		return append(append([]string(nil), env...), key+"="+config)
	}
	return env
}
