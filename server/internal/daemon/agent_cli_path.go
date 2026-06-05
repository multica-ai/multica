package daemon

import (
	"os"
	"path/filepath"
)

// MulticaCLIPathEnv is the environment variable a launcher (typically the
// Electron Desktop app) sets to tell the daemon which `multica` binary the
// agent subprocess should use. The daemon prepends `filepath.Dir(value)` to
// the agent PATH so `multica issue …` calls inside the agent resolve.
//
// This indirection exists because the daemon's own executable (returned by
// os.Executable) can sit on a path the agent subprocess cannot enumerate or
// execute. The concrete case is Windows Desktop (#2672 / MUL-2285): the
// bundled binary lives under
//
//	%LOCALAPPDATA%\Programs\<...>\app.asar.unpacked\resources\bin\multica.exe
//
// and the agent reports `Access is denied` when listing or invoking that
// directory. The Desktop app routes around it by mirroring the binary into a
// stable user-writable location (its Electron `userData` dir) and pointing
// MULTICA_CLI_PATH at the mirrored copy.
//
// When the env var is unset (CLI run from the user's shell, `make daemon`,
// brew install, etc.) the helper falls back to the daemon's own executable
// directory — same behavior the inline block here had before.
const MulticaCLIPathEnv = "MULTICA_CLI_PATH"

// resolveAgentCLIDir returns the directory to prepend to the agent
// subprocess's PATH so the `multica` CLI resolves. Returns "" only when
// neither the env hint nor os.Executable yields a usable directory, in
// which case the agent inherits the daemon's PATH unmodified.
func resolveAgentCLIDir() string {
	if hint := os.Getenv(MulticaCLIPathEnv); hint != "" {
		if info, err := os.Stat(hint); err == nil && !info.IsDir() {
			return filepath.Dir(hint)
		}
	}
	if selfBin, err := os.Executable(); err == nil {
		return filepath.Dir(selfBin)
	}
	return ""
}
