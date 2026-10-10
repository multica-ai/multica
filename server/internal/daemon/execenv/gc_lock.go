package execenv

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/multica-ai/multica/server/internal/daemon/filelock"
)

// rootMutationLockPath is stable even while GC removes and recreates envRoot.
// Keep this gate outside envRoot: .task_lock alone cannot cover its own unlink.
func rootMutationLockPath(envRoot string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(envRoot)), ".task_locks", filepath.Base(filepath.Dir(envRoot)), filepath.Base(envRoot)+".lock")
}

// WithEnvRootGCLock excludes task startup and holds the execution's .task_lock
// while fn validates and mutates an idle root. Missing PID files grant no rights.
// The sidecar remains locked even if fn releases .task_lock to remove the root
// on Windows, where an open delete-pending file prevents directory removal.
func WithEnvRootGCLock(envRoot string, fn func(releaseTaskLock func()) error) (bool, error) {
	gate, err := filelock.Try(rootMutationLockPath(envRoot))
	if err != nil || gate == nil {
		return false, err
	}
	defer filelock.Release(gate)
	if _, err := os.Stat(envRoot); err != nil {
		return false, err
	}
	lock, err := openLockFile(filepath.Join(envRoot, envRootLockFile))
	if err != nil {
		return false, err
	}
	locked, err := lockFileExclusiveNonBlocking(lock)
	if err != nil || !locked {
		lock.Close()
		return false, err
	}
	release := func() {
		if lock != nil {
			releaseLockFile(lock)
			lock = nil
		}
	}
	defer release()
	return true, fn(release)
}

func lockRootMutation(envRoot string) (*os.File, error) {
	gate, err := filelock.Try(rootMutationLockPath(envRoot))
	if err != nil {
		return nil, err
	}
	if gate == nil {
		return nil, fmt.Errorf("env root %s is being reclaimed or claimed", envRoot)
	}
	return gate, nil
}
