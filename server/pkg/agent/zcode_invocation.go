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
// as a native binary. A .cjs/.mjs path runs under `node`; the Linux desktop
// entry runs its companion script under Electron's bundled Node runtime.
// Callers must validate the entry with requireNodeForZcodeScript and apply
// zcodeRuntimeEnv for Electron. Shims and native builds pass through.
func chooseZcodeInvocation(execName, lookedUp string, args []string, _ *slog.Logger) (string, []string) {
	if script, desktop := zcodeDesktopScript(lookedUp); desktop {
		return lookedUp, append([]string{script}, args...)
	}
	if isNodeScriptPath(lookedUp) {
		return "node", append([]string{lookedUp}, args...)
	}
	return execName, args
}

// zcodeDesktopScript recognizes the Linux desktop layout, including PATH
// symlinks to its Electron entry point. Keep recognizing an incomplete bundle
// so a missing CLI produces an error instead of launching the GUI.
func zcodeDesktopScript(executable string) (string, bool) {
	if real, err := filepath.EvalSymlinks(executable); err == nil {
		executable = real
	}
	if filepath.Base(executable) != "zcode" {
		return "", false
	}
	resources := filepath.Join(filepath.Dir(executable), "resources")
	if info, err := os.Stat(resources); err != nil || !info.IsDir() {
		return "", false
	}
	return filepath.Join(resources, "glm", "zcode.cjs"), true
}

func isNodeScriptPath(path string) bool {
	return strings.HasSuffix(path, ".cjs") || strings.HasSuffix(path, ".mjs")
}

// requireNodeForZcodeScript reports a clear error when a Desktop .cjs/.mjs
// entry needs Node.js on PATH and none is resolvable, or a Linux Desktop entry
// has no companion CLI. Linux Desktop uses Electron's own Node runtime.
func requireNodeForZcodeScript(lookedUp string) error {
	if script, desktop := zcodeDesktopScript(lookedUp); desktop {
		if info, err := os.Stat(script); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("ZCode Desktop CLI is missing at %s; set MULTICA_ZCODE_PATH to a headless ZCode CLI or wrapper", script)
		}
		return nil
	}
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
// Explicit provider configuration always wins.
func zcodeRuntimeEnv(executable string, env []string) []string {
	if script, desktop := zcodeDesktopScript(executable); desktop {
		// This must override an inherited 0: Electron otherwise starts its GUI.
		env = append(append([]string(nil), env...), "ELECTRON_RUN_AS_NODE=1")
		executable = script
	}
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
