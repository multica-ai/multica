package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// taskCLIPath returns a PATH that always exposes the running daemon as the
// `multica` command. Official builds already use that filename and only need
// their directory prepended. Local builds are often installed under a
// descriptive name, so they receive a task-private alias inside the temp tree
// instead of requiring a machine-wide symlink.
//
// On alias failure the returned PATH still preserves the historical behavior
// (the executable's directory first) and the error is left to the caller to
// log. Losing the convenience command must not prevent the agent itself from
// starting.
func taskCLIPath(selfBin, taskTempDir, inheritedPath string) (string, error) {
	binDir := filepath.Dir(selfBin)
	pathParts := []string{binDir}

	if !isCanonicalTaskCLIName(filepath.Base(selfBin)) {
		aliasDir := filepath.Join(taskTempDir, "multica-cli")
		if err := os.MkdirAll(aliasDir, 0o700); err != nil {
			return joinTaskPath(pathParts, inheritedPath), fmt.Errorf("create alias directory: %w", err)
		}
		if err := createTaskCLIAlias(aliasDir, selfBin); err != nil {
			return joinTaskPath(pathParts, inheritedPath), fmt.Errorf("create alias: %w", err)
		}
		pathParts = append([]string{aliasDir}, pathParts...)
	}

	return joinTaskPath(pathParts, inheritedPath), nil
}

func joinTaskPath(prefix []string, inheritedPath string) string {
	if inheritedPath != "" {
		prefix = append(prefix, inheritedPath)
	}
	return strings.Join(prefix, string(os.PathListSeparator))
}
