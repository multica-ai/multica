package daemon

import (
	"sync"
	"time"
)

// ShutdownCleanupConfirmed is meaningful after Run returns. Daemon exit alone
// does not prove that the providers' independent process groups have exited.
func (d *Daemon) ShutdownCleanupConfirmed() bool {
	return !d.cleanupUnconfirmed.Load()
}

func (d *Daemon) waitForTaskCleanup(tasks *sync.WaitGroup, timeout time.Duration) {
	done := make(chan struct{})
	go func() { tasks.Wait(); close(done) }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		d.cleanupUnconfirmed.Store(true)
		d.logger.Warn("timed out waiting for in-flight tasks")
	}
}
