package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxPendingRecords = 100
	maxPendingAge     = 7 * 24 * time.Hour
	maxTmpAge         = 1 * time.Hour
)

// pendingTaskRecord stores a completed task result that failed to be reported
// due to transient network errors or server downtime.
type pendingTaskRecord struct {
	TaskID    string     `json:"task_id"`
	Result    TaskResult `json:"result"`
	CreatedAt time.Time  `json:"created_at"`
}

// pendingResultsDir returns the absolute path to the local directory where
// unreported task results are buffered on disk.
func (d *Daemon) pendingResultsDir() string {
	if d.cfg.WorkspacesRoot == "" {
		return filepath.Join(os.TempDir(), "multica_pending_results")
	}
	return filepath.Join(d.cfg.WorkspacesRoot, ".pending_results")
}

// notifyPendingQueue signals the retrier loop to wake up and attempt a flush.
// Non-blocking: if a notification is already pending in the channel buffer,
// duplicate signals are silently dropped.
func (d *Daemon) notifyPendingQueue() {
	select {
	case d.pendingQueueNotify <- struct{}{}:
	default:
	}
}

// enqueuePendingResult serializes a completed or failed task result to disk using an
// atomic write-then-rename pattern, ensuring a concurrent poller or crashing
// daemon never observes a half-written JSON record.
func (d *Daemon) enqueuePendingResult(taskID string, result TaskResult) {
	dir := d.pendingResultsDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		d.logger.Error("failed to create pending results directory", "error", err, "path", dir)
		return
	}
	_ = os.Chmod(dir, 0700)

	record := pendingTaskRecord{
		TaskID:    taskID,
		Result:    result,
		CreatedAt: time.Now().UTC(),
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		d.logger.Error("failed to marshal pending task record", "error", err, "task_id", taskID)
		return
	}

	targetPath := filepath.Join(dir, fmt.Sprintf("%s.json", taskID))
	tmpPath := targetPath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		d.logger.Error("failed to write temporary pending task record", "error", err, "path", tmpPath)
		return
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		d.logger.Error("failed to persist pending task record", "error", err, "target", targetPath)
		os.Remove(tmpPath)
		return
	}

	d.logger.Info("enqueued pending task result to disk", "task_id", taskID, "path", targetPath)
	d.prunePendingQueue()
	d.notifyPendingQueue()
}

type fileInfoWithPath struct {
	path string
	info os.FileInfo
}

// prunePendingQueue enforces retention bounds (max count, max age) and cleans up
// orphaned temporary write files (.json.tmp). Called on startup and before flush.
func (d *Daemon) prunePendingQueue() {
	dir := d.pendingResultsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	now := time.Now()
	var jsonFiles []fileInfoWithPath

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		filePath := filepath.Join(dir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Clean up orphaned .tmp files older than maxTmpAge
		if strings.HasSuffix(name, ".tmp") {
			if now.Sub(info.ModTime()) > maxTmpAge {
				os.Remove(filePath)
			}
			continue
		}

		if !strings.HasSuffix(name, ".json") {
			continue
		}

		// Clean up expired JSON records older than maxPendingAge
		if now.Sub(info.ModTime()) > maxPendingAge {
			d.logger.Warn("pruning expired pending task record", "path", filePath, "age", now.Sub(info.ModTime()))
			os.Remove(filePath)
			continue
		}

		jsonFiles = append(jsonFiles, fileInfoWithPath{path: filePath, info: info})
	}

	// Enforce max count by oldest-first eviction
	if len(jsonFiles) > maxPendingRecords {
		sort.Slice(jsonFiles, func(i, j int) bool {
			return jsonFiles[i].info.ModTime().Before(jsonFiles[j].info.ModTime())
		})
		evictCount := len(jsonFiles) - maxPendingRecords
		for i := 0; i < evictCount; i++ {
			d.logger.Warn("evicting oldest pending task record to enforce max limit", "path", jsonFiles[i].path)
			os.Remove(jsonFiles[i].path)
		}
	}
}

// pendingQueueRetrierLoop runs a background supervisor that periodically retries
// reporting buffered task results to the server. It wakes up on a fixed cadence
// or immediately when notified by a fresh enqueue or WebSocket reconnection.
func (d *Daemon) pendingQueueRetrierLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	// Initial flush attempt after a short delay to let registration settle
	timer := time.NewTimer(5 * time.Second)
	select {
	case <-ctx.Done():
		timer.Stop()
		return
	case <-timer.C:
		d.prunePendingQueue()
		d.flushPendingQueue(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.prunePendingQueue()
			d.flushPendingQueue(ctx)
		case <-d.pendingQueueNotify:
			d.flushPendingQueue(ctx)
		}
	}
}

// flushPendingQueue scans the pending results directory and attempts to re-send
// every valid JSON record to the upstream server.
func (d *Daemon) flushPendingQueue(ctx context.Context) {
	dir := d.pendingResultsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			d.logger.Debug("failed to read pending results directory", "error", err, "path", dir)
		}
		return
	}

	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		d.retryPendingRecord(ctx, filePath)
	}
}

// retryPendingRecord reads a single buffered task record from disk and attempts
// to report it via CompleteTask or FailTask. If reported successfully (or if rejected due to
// permanent 4xx client errors), the disk record is permanently deleted.
func (d *Daemon) retryPendingRecord(ctx context.Context, filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		d.logger.Debug("failed to read pending task record", "error", err, "path", filePath)
		return
	}

	var record pendingTaskRecord
	if err := json.Unmarshal(data, &record); err != nil {
		d.logger.Error("failed to unmarshal pending task record; discarding corrupt file", "error", err, "path", filePath)
		os.Remove(filePath)
		return
	}

	res := record.Result
	var reportErr error

	switch res.Status {
	case "completed":
		reportErr = d.client.CompleteTaskWithSchedule(ctx, record.TaskID, res.Comment, res.BranchName, res.SessionID, res.WorkDir, nil)
	default:
		reportErr = d.client.FailTaskWithSchedule(ctx, record.TaskID, res.Comment, res.SessionID, res.WorkDir, res.FailureReason, nil)
	}

	if reportErr == nil {
		d.logger.Info("successfully flushed pending task result to server", "task_id", record.TaskID)
		if removeErr := os.Remove(filePath); removeErr != nil && !os.IsNotExist(removeErr) {
			d.logger.Warn("failed to remove pending task record after successful report", "error", removeErr, "path", filePath)
		}
		return
	}

	if isTransientError(reportErr) {
		d.logger.Debug("transient error while flushing pending task record; will retry later", "task_id", record.TaskID, "error", reportErr)
		return
	}

	// Permanent error (e.g. 400 Bad Request, 404 Task Not Found); discard record so we don't loop forever
	d.logger.Error("permanent error while flushing pending task record; discarding", "task_id", record.TaskID, "error", reportErr)
	os.Remove(filePath)
}
