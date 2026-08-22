//go:build !linux && !darwin && !windows

package execenv

import (
	"fmt"
	"io/fs"
	"os"
)

func preserveRuntimeConfigFileMetadata(staged *os.File, sourcePath string, mode fs.FileMode) error {
	if sourcePath != "" {
		return fmt.Errorf("metadata-preserving runtime config replacement is unsupported on this platform")
	}
	return staged.Chmod(mode)
}
