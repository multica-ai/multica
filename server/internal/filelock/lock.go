// Package filelock provides process-lifetime exclusive file locks.
package filelock

import (
	"context"
	"os"
	"time"
)

// Acquire waits for an exclusive lock until ctx is canceled. Close the returned
// file to release the lock. Leave the file in place so all callers lock the same
// file even when an attempt fails.
func Acquire(ctx context.Context, path string) (*os.File, error) {
	retry := time.NewTicker(100 * time.Millisecond)
	defer retry.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, acquired, err := Try(path)
		if err != nil || acquired {
			return file, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-retry.C:
		}
	}
}
