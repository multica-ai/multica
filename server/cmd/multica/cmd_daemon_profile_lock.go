package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const daemonShutdownLockWait = 40 * time.Second

// A fallback port no longer acts as the profile's single-instance guard.
// Keep this OS lock for the daemon's entire lifetime. A successor spawned by
// self-update may wait briefly for its predecessor to release it.
func acquireDaemonProfileLock(profile string, handoff bool) (*os.File, error) {
	dir := daemonDirForProfile(profile)
	if dir == "" {
		return nil, fmt.Errorf("resolve daemon profile directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "daemon.lock")
	deadline := time.Now().Add(5 * time.Second)
	if handoff {
		deadline = deadline.Add(30 * time.Second)
	}
	for {
		f, acquired, err := tryLockDaemonProfile(path)
		if err != nil {
			return nil, fmt.Errorf("lock daemon profile %q: %w", profile, err)
		}
		if acquired {
			return f, nil
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("daemon profile %q is already running or starting", profile)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Wait in the launcher, before spawning a child with its own startup timeout.
// During shutdown the health listener closes before active tasks and runtime
// cleanup finish, so an immediate successor would exhaust its 5-second lock
// wait and exit while the predecessor is still holding the lock.
func waitForDaemonProfileRelease(profile string, timeout time.Duration) error {
	path := filepath.Join(daemonDirForProfile(profile), "daemon.lock")
	deadline := time.Now().Add(timeout)
	nextHealthCheck := time.Time{}
	warned := false
	for {
		lock, acquired, err := tryLockDaemonProfile(path)
		if err != nil {
			return fmt.Errorf("check daemon profile %q lock: %w", profile, err)
		}
		if acquired {
			return lock.Close()
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("daemon %s did not finish starting or shutting down within %s; retry once it exits", describeProfile(profile), timeout)
		}
		if !time.Now().Before(nextHealthCheck) {
			port := resolveHealthPort(profile)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			health := checkDaemonHealthOnPort(ctx, port)
			cancel()
			if daemonAlive(health) && daemonIdentityMismatch(health, profile, port) == nil {
				return fmt.Errorf("daemon %s is already running or starting", describeProfile(profile))
			}
			if !warned {
				fmt.Fprintf(os.Stderr, "Daemon %s is still shutting down or starting; waiting for its profile lock...\n", describeProfile(profile))
				warned = true
			}
			nextHealthCheck = time.Now().Add(time.Second)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
