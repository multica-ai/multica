package daemon

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Machine-wide local_directory exclusion (GH #8280).
//
// LocalPathLocker is process-local, and that was enough while one machine ran one
// daemon. It no longer is: two profile processes may serve different logical
// runtimes and still be handed the same local_directory project resource, so the
// same on-disk checkout can have two writers - or one writer and one worktree
// snapshot - with each process believing it holds the path.
//
// The correctness boundary is therefore an OS claim keyed on the canonical real
// path, held in a machine-global lock root. Deliberately NOT the work-state scope
// root: a local directory is a filesystem resource rather than a runtime one, so
// two daemons talking to different backends must still contend when they are
// pointed at the same checkout.
//
// Lock ordering, used by every call site: the process-local LocalPathLocker first
// (it owns fairness and the holder hint), then the machine-wide claim. Taking them
// in the other order in even one path would deadlock against the paths that do not.

// machineLockDirName is the machine-global lock directory, a sibling of the
// per-backend work-state scopes under the Multica root.
const machineLockDirName = "locks"

// machineScopeLocks returns the machine-global lock set, or a disabled set when
// the Multica root cannot be resolved. A disabled set degrades to the previous
// process-local behaviour rather than failing every local_directory task.
func machineScopeLocks() execenv.ScopeLocks {
	root, err := cli.ProfileDir("")
	if err != nil {
		return execenv.ScopeLocks{}
	}
	return execenv.NewScopeLocks(filepath.Join(root, machineLockDirName))
}

// holdLocalDirectoryPath takes the machine-wide claim for one canonical real path,
// waiting until it is free or ctx ends.
//
// The wait is context-cancellable and unbounded on purpose: a local_directory task
// can legitimately wait for a sibling writer for hours, so the short generic
// scope-lock timeout used for stores and env roots would turn a normal queue into
// a task failure. Cancellation comes from the task (server-side terminal state)
// and from daemon shutdown, both of which already drive the ctx this is handed.
//
// onWait, when non-nil, is invoked at most once, before blocking, so the daemon
// can flip the task into its server-side waiting state.
func (d *Daemon) holdLocalDirectoryPath(ctx context.Context, realPath, taskID string, onWait func(holder string)) (func(), error) {
	if realPath == "" {
		return nil, fmt.Errorf("local_directory: real path required for the machine-wide claim")
	}
	target := execenv.LocalDirectoryTarget(realPath)

	// Non-blocking first: the common case is an uncontended path, and it must not
	// pay for a waiter notification.
	claim, ok, err := d.machineLocks.TryAcquireTargetDelete(target)
	if err != nil {
		return nil, fmt.Errorf("local_directory: machine-wide claim for %s: %w", realPath, err)
	}
	if !ok {
		if onWait != nil {
			// The holder lives in another process, so its task id is not knowable
			// here. An unknown holder is better than a fabricated one - the UI
			// simply omits the "(held by task X)" clause.
			onWait("")
		}
		claim, err = d.machineLocks.AcquireTargetExclusiveContext(ctx, target)
		if err != nil {
			return nil, err
		}
	}
	if claim == nil {
		// Locking is disabled (no Multica root): nothing to hold.
		return func() {}, nil
	}
	_ = taskID
	return claim.Release, nil
}
