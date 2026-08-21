package daemon

import (
	"fmt"
	"os"
	"time"
)

var taskTempCleanupRetryDelays = [...]time.Duration{
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2 * time.Second,
}

func cleanupTaskTempDir(path string) (int, error) {
	return cleanupTaskTempDirWith(path, os.RemoveAll, time.Sleep, taskTempCleanupRetryDelays[:])
}

func cleanupTaskTempDirWith(
	path string,
	removeAll func(string) error,
	sleep func(time.Duration),
	retryDelays []time.Duration,
) (int, error) {
	attempts := 1
	err := removeAll(path)
	for _, delay := range retryDelays {
		if err == nil {
			return attempts, nil
		}
		sleep(delay)
		attempts++
		err = removeAll(path)
	}
	if err != nil {
		return attempts, fmt.Errorf("remove task temp dir after %d attempts: %w", attempts, err)
	}
	return attempts, nil
}
