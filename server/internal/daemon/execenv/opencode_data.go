package execenv

import (
	"fmt"
	"os"
	"path/filepath"
)

// openCodeDataDirName is the per-task directory exported as XDG_DATA_HOME for
// opencode tasks (GAP-1). opencode resolves its data root as
// $XDG_DATA_HOME/opencode, so session/auth state lands inside the task env
// root instead of the daemon user's shared ~/.local/share/opencode. The GC
// reclaims it with the env root.
const openCodeDataDirName = "opencode-data"

// prepareOpenCodeDataDir creates the per-task XDG_DATA_HOME root under
// envRoot. Failure is non-fatal at the call site: an empty return leaves the
// task on the shared default rather than blocking dispatch.
func prepareOpenCodeDataDir(envRoot string) (string, error) {
	dir := filepath.Join(envRoot, openCodeDataDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create opencode data dir: %w", err)
	}
	return dir, nil
}
