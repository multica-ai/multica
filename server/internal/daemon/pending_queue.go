package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// enqueuePendingResult serializes a completed task result to disk using an
// atomic write-then-rename pattern, ensuring a concurrent poller or crashing
// daemon never observes a half-written JSON record.
func (d *Daemon) enqueuePendingResult(taskID string, result TaskResult) {
	dir := d.pendingResultsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		d.logger.Error("failed to create pending results directory", "error", err, "path", dir)
		return
	}

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

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		d.logger.Error("failed to write temporary pending task record", "error", err, "path", tmpPath)
		return
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		d.logger.Error("failed to persist pending task record", "error", err, "target", targetPath)
		os.Remove(tmpPath)
		return
	}

	d.logger.Info("enqueued pending task result to disk", "task_id", taskID, "path", targetPath)
	d.notifyPendingQueue()
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
		d.flushPendingQueue(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
// to report it via CompleteTask. If reported successfully (or if rejected due to
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
		reportErr = d.client.CompleteTask(ctx, record.TaskID, res.Comment, res.BranchName, res.SessionID, res.WorkDir)
	default:
		// Unknown or unhandled status in pending queue; discard
		d.logger.Warn("discarding pending record with unhandled status", "task_id", record.TaskID, "status", res.Status)
		os.Remove(filePath)
		return
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
