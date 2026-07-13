//go:build windows

package daemon

import "fmt"

func freeDiskBytes(path string) (freeBytes, totalBytes int64, err error) {
	return 0, 0, fmt.Errorf("disk pressure guard is not implemented on windows")
}
