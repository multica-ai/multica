package agent

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// zcodeWrapperMaxBytes bounds how much of a `zcode` launcher is read when
// looking for the Node entry it runs. Launchers are a few lines long; anything
// larger is a real program and is left alone.
const zcodeWrapperMaxBytes = 16 << 10

// zcodeWrapperWordRe splits a launcher line into shell words: a quoted string
// or a run of characters without whitespace or quotes.
var zcodeWrapperWordRe = regexp.MustCompile(`"([^"]*)"|'([^']*)'|([^\s"']+)`)

// resolveZcodeScript returns the Node entry behind executable, so the files
// ZCode ships beside that entry can be located when `zcode` on PATH is not the
// script itself. It follows a symlink to a .cjs/.mjs file and reads a small
// `#!` launcher for the absolute script path it runs; the official installer
// writes `exec node "<install dir>/current/bin/zcode.mjs" "$@"`. Anything else
// (a native build, a launcher whose script is missing or not spelled out as an
// absolute path) is returned unchanged.
func resolveZcodeScript(executable string) string {
	if isNodeScriptPath(executable) {
		return executable
	}
	if target, err := filepath.EvalSymlinks(executable); err == nil && isNodeScriptPath(target) {
		return target
	}
	file, err := os.Open(executable)
	if err != nil {
		return executable
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, zcodeWrapperMaxBytes+1))
	if err != nil || len(data) > zcodeWrapperMaxBytes || !bytes.HasPrefix(data, []byte("#!")) {
		return executable
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, word := range zcodeWrapperWordRe.FindAllStringSubmatch(line, -1) {
			script := word[1] + word[2] + word[3]
			if !isNodeScriptPath(script) || !filepath.IsAbs(script) {
				continue
			}
			// The CLI locates its companion files from the real entry, so a
			// versioned install behind a `current` symlink resolves the same way.
			target, err := filepath.EvalSymlinks(script)
			if err != nil {
				continue
			}
			if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
				return target
			}
		}
	}
	return executable
}

// zcodeBuiltinProviderConfig returns the provider config ZCode ships beside
// the Node entry script, or "" when script is not a Node entry or none of the
// known layouts has one.
func zcodeBuiltinProviderConfig(script string) string {
	if !isNodeScriptPath(script) {
		return ""
	}
	dir := filepath.Dir(script)
	for _, config := range []string{
		// Desktop app bundle: Resources/glm/zcode.cjs.
		filepath.Join(dir, "..", "config", "provider", "zcode-builtin.json"),
		// CLI bundle entry: zcode.cjs with provider/ beside it.
		filepath.Join(dir, "provider", "zcode-builtin.json"),
		// Standalone install: bin/zcode.mjs launching agent/zcode.cjs.
		filepath.Join(dir, "..", "agent", "provider", "zcode-builtin.json"),
	} {
		if info, err := os.Stat(config); err == nil && info.Mode().IsRegular() {
			return config
		}
	}
	return ""
}

// zcodeRuntimeEnv supplies the companion config shipped with the Node entry
// script (see resolveZcodeScript). The bundled CLI's relative lookup does not
// cover Resources/glm in 0.17. Explicit provider configuration always wins.
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
	if config := zcodeBuiltinProviderConfig(executable); config != "" {
		return append(append([]string(nil), env...), key+"="+config)
	}
	return env
}
