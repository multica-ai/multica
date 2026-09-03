//go:build !windows

package daemon

import (
	"os"
	"path/filepath"
)

func isCanonicalTaskCLIName(name string) bool {
	return name == "multica"
}

func createTaskCLIAlias(aliasDir, selfBin string) error {
	return os.Symlink(selfBin, filepath.Join(aliasDir, "multica"))
}
