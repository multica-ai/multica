package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// chooseZcodeInvocation selects the actual program (argv[0]) and the full argv
// to spawn a ZCode run.
//
// ZCode ships as a Node bundle (zcode.cjs) inside the desktop app rather than
// as a native binary, so a resolved .cjs/.mjs path runs under `node`. Execute
// has already verified node exists by the time this runs. Shims and native
// builds pass through unchanged.
func chooseZcodeInvocation(execName, lookedUp string, args []string, _ *slog.Logger) (string, []string) {
	if isNodeScriptPath(lookedUp) {
		return "node", append([]string{lookedUp}, args...)
	}
	return execName, args
}

func isNodeScriptPath(path string) bool {
	return strings.HasSuffix(path, ".cjs") || strings.HasSuffix(path, ".mjs")
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
