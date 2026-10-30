//go:build !windows

package daemon

import "os"

func syncTerminalReportDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
