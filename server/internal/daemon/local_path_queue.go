package daemon

import (
	"context"
	"errors"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// LocalPathLocker queues actual shared-directory writers. Ready independent
// run-owned rooms never enter this queue. Ordering is explicit instead of
// depending on the Go mutex scheduler. The server retains each parked task's
// original created_at and priority for redelivery after a daemon restart.
type LocalPathLocker struct {
	mu    sync.Mutex
	locks map[string]*pathLockEntry
}
type pathLockEntry struct {
	holder *pathWaiter
	queue  []*pathWaiter
}
type pathWaiter struct {
	id       string
	priority int32
	queuedAt time.Time
	ready    chan struct{}
}

func NewLocalPathLocker() *LocalPathLocker {
	return &LocalPathLocker{locks: make(map[string]*pathLockEntry)}
}
func localLockKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
func (l *LocalPathLocker) Holder(path string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.locks[localLockKey(path)]; e != nil && e.holder != nil {
		return e.holder.id
	}
	return ""
}
func (l *LocalPathLocker) Acquire(ctx context.Context, path, id string, onWait func(string)) (func(), error) {
	return l.AcquireOrdered(ctx, path, id, 0, time.Now(), onWait)
}
func effectivePathPriority(w *pathWaiter, now time.Time) int32 {
	if w.priority >= 4 {
		return w.priority
	} // emergency precedence is never aged away
	p := w.priority
	if age := now.Sub(w.queuedAt); age > 0 {
		p += int32(age / (30 * time.Minute))
	}
	if p > 3 {
		p = 3
	}
	return p
}
func (l *LocalPathLocker) grantNext(e *pathLockEntry) {
	if len(e.queue) == 0 {
		e.holder = nil
		return
	}
	now := time.Now()
	sort.SliceStable(e.queue, func(i, j int) bool {
		a, b := e.queue[i], e.queue[j]
		pa, pb := effectivePathPriority(a, now), effectivePathPriority(b, now)
		if pa != pb {
			return pa > pb
		}
		if !a.queuedAt.Equal(b.queuedAt) {
			return a.queuedAt.Before(b.queuedAt)
		}
		return a.id < b.id
	})
	e.holder = e.queue[0]
	e.queue = e.queue[1:]
	close(e.holder.ready)
}
func (l *LocalPathLocker) AcquireOrdered(ctx context.Context, path, id string, priority int32, queuedAt time.Time, onWait func(string)) (func(), error) {
	if path == "" || id == "" {
		return nil, errors.New("local_directory: realpath and task ID required for lock")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if queuedAt.IsZero() {
		queuedAt = time.Now()
	}
	w := &pathWaiter{id: id, priority: priority, queuedAt: queuedAt, ready: make(chan struct{})}
	key := localLockKey(path)
	l.mu.Lock()
	e := l.locks[key]
	if e == nil {
		e = &pathLockEntry{}
		l.locks[key] = e
	}
	if e.holder == nil {
		e.holder = w
		close(w.ready)
		l.mu.Unlock()
	} else {
		holder := e.holder.id
		e.queue = append(e.queue, w)
		l.mu.Unlock()
		if onWait != nil {
			onWait(holder)
		}
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if e.holder == w {
				l.grantNext(e)
			}
		})
	}
	select {
	case <-w.ready:
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		return release, nil
	case <-ctx.Done():
		l.mu.Lock()
		if e.holder == w {
			l.grantNext(e)
		} else {
			for i, q := range e.queue {
				if q == w {
					e.queue = append(e.queue[:i], e.queue[i+1:]...)
					break
				}
			}
		}
		l.mu.Unlock()
		return nil, ctx.Err()
	}
}

func taskQueueTime(task Task) time.Time {
	if !task.OriginalQueuedAt.IsZero() {
		return task.OriginalQueuedAt
	}
	return task.CreatedAt
}
