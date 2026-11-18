package kagentbroker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const journalHeader = "multica-kagent-broker-attempts-v1\n"

// AttemptJournal is a durable replay-denial fence, not a task ownership journal.
// The operator must retain this file across restarts and keep its directory
// private. One broker process holds its exclusive lock for the entire lifetime.
type AttemptJournal struct {
	file   *os.File
	gate   chan struct{}
	mu     sync.Mutex
	ids    map[string]bool
	failed bool
}

func OpenAttemptJournal(path string) (*AttemptJournal, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, errDenied
	}
	fail := func() (*AttemptJournal, error) { _ = f.Close(); return nil, errDenied }
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || lockJournal(f) != nil {
		return fail()
	}
	raw, err := boundedRead(f, int64(len(journalHeader)+bindingLimit*37))
	if err != nil {
		return fail()
	}
	if len(raw) == 0 {
		if _, err = f.WriteString(journalHeader); err != nil || f.Sync() != nil {
			return fail()
		}
		// Persist the directory entry before any reservation can authorize work.
		dir, err := os.Open(filepath.Dir(path))
		if err != nil {
			return fail()
		}
		err = dir.Sync()
		_ = dir.Close()
		if err != nil {
			return fail()
		}
		raw = []byte(journalHeader)
	}
	if !strings.HasPrefix(string(raw), journalHeader) || raw[len(raw)-1] != '\n' {
		return fail()
	}
	j := &AttemptJournal{file: f, gate: make(chan struct{}, 1), ids: map[string]bool{}}
	for _, id := range strings.Split(strings.TrimSuffix(string(raw[len(journalHeader):]), "\n"), "\n") {
		if id == "" && len(raw) == len(journalHeader) {
			break
		}
		if !uuidPattern.MatchString(id) || j.ids[id] || len(j.ids) >= bindingLimit {
			return fail()
		}
		j.ids[id] = true
	}
	return j, nil
}

func (j *AttemptJournal) allowed(id string) int {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failed {
		return 503
	}
	if j.ids[id] {
		return 409
	}
	if len(j.ids) >= bindingLimit {
		return 403
	}
	return 0
}

// reserve serializes disk writes without holding a broker lock. Waiting sends
// honor cancellation; Get/Cancel never touch this gate or the filesystem.
func (j *AttemptJournal) reserve(ctx context.Context, id string) int {
	select {
	case j.gate <- struct{}{}:
	case <-ctx.Done():
		return 504
	}
	defer func() { <-j.gate }()
	if ctx.Err() != nil {
		return 504
	}
	if status := j.allowed(id); status != 0 {
		return status
	}
	j.mu.Lock()
	j.ids[id] = true
	j.mu.Unlock()
	_, err := j.file.WriteString(id + "\n")
	if err == nil {
		err = j.file.Sync()
	}
	if err != nil {
		j.mu.Lock()
		j.failed = true
		j.mu.Unlock()
		return 503
	}
	return 0
}

// Close must be called only after all requests have stopped.
func (j *AttemptJournal) Close() error { return j.file.Close() }
