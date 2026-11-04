package daemon

import (
	"sync"
	"testing"
	"time"
)

func TestShutdownCleanupWait(t *testing.T) {
	for _, finishes := range []bool{true, false} {
		t.Run(map[bool]string{true: "drained", false: "timed_out"}[finishes], func(t *testing.T) {
			d := newTestDaemon(t)
			var tasks sync.WaitGroup
			tasks.Add(1)
			if finishes {
				tasks.Done()
			} else {
				defer tasks.Done()
			}
			d.waitForTaskCleanup(&tasks, 50*time.Millisecond)
			if d.ShutdownCleanupConfirmed() != finishes {
				t.Fatalf("cleanup confirmed=%t after finishes=%t", d.ShutdownCleanupConfirmed(), finishes)
			}
		})
	}
}
