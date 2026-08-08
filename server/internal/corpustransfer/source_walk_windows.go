//go:build windows

package corpustransfer

import (
	"fmt"
	"os"
)

func walkSourceRoot(root string, visit func(string, os.FileInfo, *os.File) error) error {
	return fmt.Errorf("secure source packing is not supported on Windows; send an existing ZIP instead")
}

func openSourceNoFollow(filename string) (*os.File, error) {
	return nil, fmt.Errorf("secure source packing is not supported on Windows; send an existing ZIP instead")
}
