package filetree

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// OnChangeFunc is called when the file tree changes.
type OnChangeFunc func(snapshot *Snapshot)

// Watcher periodically scans a worktree and reports changes.
type Watcher struct {
	worktreePath string
	interval     time.Duration
	onChange     OnChangeFunc
	logger       *slog.Logger

	mu       sync.Mutex
	lastHash string
	stopCh   chan struct{}
	stopped  bool
}

// NewWatcher creates a watcher that scans worktreePath every interval.
func NewWatcher(worktreePath string, interval time.Duration, onChange OnChangeFunc, logger *slog.Logger) *Watcher {
	return &Watcher{
		worktreePath: worktreePath,
		interval:     interval,
		onChange:      onChange,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}
}

// Start begins the periodic scanning loop. Non-blocking.
func (w *Watcher) Start() {
	go w.loop()
}

// Stop halts the watcher.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stopped {
		w.stopped = true
		close(w.stopCh)
	}
}

func (w *Watcher) loop() {
	// Do an initial scan immediately.
	w.scan()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.scan()
		}
	}
}

func (w *Watcher) scan() {
	snapshot, err := ScanSnapshot(w.worktreePath)
	if err != nil {
		w.logger.Warn("filetree: scan failed", "path", w.worktreePath, "error", err)
		return
	}

	hash := hashSnapshot(snapshot)

	w.mu.Lock()
	changed := hash != w.lastHash
	if changed {
		w.lastHash = hash
	}
	w.mu.Unlock()

	if changed {
		w.logger.Debug("filetree: change detected", "path", w.worktreePath)
		w.onChange(snapshot)
	}
}

func hashSnapshot(s *Snapshot) string {
	data, _ := json.Marshal(s)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}
