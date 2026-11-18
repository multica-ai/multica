//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package kagentbroker

import "os"

// No unqualified locking substitute on other platforms.
func lockJournal(*os.File) error { return errDenied }

func openOwnershipFile(string, int) (*os.File, error) { return nil, errDenied }
func singleLink(os.FileInfo) bool                     { return false }
