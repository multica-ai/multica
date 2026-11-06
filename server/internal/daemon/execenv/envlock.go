package execenv

import (
	"github.com/multica-ai/multica/server/internal/daemon/filelock"
	"os"
)

func openLockFile(path string) (*os.File, error)            { return filelock.Open(path) }
func lockFileExclusiveNonBlocking(f *os.File) (bool, error) { return filelock.TryFile(f) }
func unlockFile(f *os.File) error                           { return filelock.Unlock(f) }
