package execenv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	codexSessionWriterLockRoot = "multica-session-writer-locks"
	codexSessionWriterLockPoll = 25 * time.Millisecond
)

// CodexSessionWriterLock serializes writers to one persistent Codex session
// store. Each local_directory task gets a fresh CODEX_HOME, so Codex's own
// writer lock (which lives under CODEX_HOME/thread-writer-locks) cannot see a
// sibling task that mounts the same persistent sessions directory. Without a
// store-scoped lock, a replacement task can append to the same rollout while
// the cancelled task is still unwinding and corrupt its ordinal sequence.
type CodexSessionWriterLock struct {
	file *os.File
	once sync.Once
}

// AcquireCodexSessionWriterLock waits until no other Multica task is writing
// store. The lock is cross-process and crash-safe: the OS releases it when the
// daemon exits even if Release is not called.
//
// The lock file deliberately lives outside the session store. Store GC may
// remove and recreate the store; putting the lock inside it would let a waiter
// lock a new inode while the old writer still held the unlinked one. Lock files
// are stable coordination objects and must not be deleted as stale markers.
// onWait is called at most once, when another writer is first observed.
func AcquireCodexSessionWriterLock(ctx context.Context, store string, onWait func()) (*CodexSessionWriterLock, error) {
	if store == "" {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path := codexSessionWriterLockPath(store)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create Codex session writer lock directory: %w", err)
	}
	f, err := openLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("open Codex session writer lock %s: %w", path, err)
	}

	announced := false
	ticker := time.NewTicker(codexSessionWriterLockPoll)
	defer ticker.Stop()
	for {
		locked, lockErr := lockFileExclusiveNonBlocking(f)
		if lockErr != nil {
			_ = f.Close()
			return nil, fmt.Errorf("lock Codex session store %s: %w", store, lockErr)
		}
		if locked {
			return &CodexSessionWriterLock{file: f}, nil
		}
		if !announced {
			announced = true
			if onWait != nil {
				onWait()
			}
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("wait for Codex session writer lock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// Release drops the store writer lock. It is safe to call more than once.
func (l *CodexSessionWriterLock) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		releaseLockFile(l.file)
		l.file = nil
	})
}

func codexSessionWriterLockPath(store string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(store)))
	return filepath.Join(resolveSharedCodexHome(), codexSessionWriterLockRoot, hex.EncodeToString(sum[:])+".lock")
}
