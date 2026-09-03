//go:build windows

package daemon

import (
	"os"
	"path/filepath"
	"strings"
)

func isCanonicalTaskCLIName(name string) bool {
	return strings.EqualFold(name, "multica.exe")
}

func createTaskCLIAlias(aliasDir, selfBin string) error {
	// Creating symlinks requires elevated privileges on many Windows hosts.
	// A tiny cmd wrapper is sufficient for normal PATH command resolution and
	// keeps us from copying the full daemon binary into every task temp tree.
	escaped := strings.ReplaceAll(selfBin, "%", "%%")
	body := "@\"" + escaped + "\" %*\r\n"
	return os.WriteFile(filepath.Join(aliasDir, "multica.cmd"), []byte(body), 0o600)
}
