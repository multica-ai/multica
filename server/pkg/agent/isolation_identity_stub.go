//go:build !linux

package agent

import "os"

func openPathIdentityLinux(path string, directory bool) (*os.File, os.FileInfo, error) {
	return openPathIdentityPortable(path, directory)
}
