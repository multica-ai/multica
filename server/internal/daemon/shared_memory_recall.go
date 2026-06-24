package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/multica-ai/multica/server/internal/recall"
)

type recallLogRecord struct {
	Timestamp    string        `json:"timestamp"`
	TaskID       string        `json:"task_id"`
	IssueID      string        `json:"issue_id,omitempty"`
	Status       recall.Status `json:"status"`
	HitCount     int           `json:"hit_count"`
	QueryHash    string        `json:"query_hash"`
	BytesUsed    int           `json:"bytes_used"`
	IndexVersion int           `json:"index_version"`
	SkippedFiles []string      `json:"skipped_files,omitempty"`
	Reason       string        `json:"reason,omitempty"`
}

const sharedMemoryRecallTimeout = 2 * time.Second

func (d *Daemon) runSharedMemoryRecall(ctx context.Context, task Task) recall.Result {
	options := recall.Options{
		VaultRoot:      d.cfg.SharedMemoryVault,
		MaxHits:        d.cfg.SharedMemoryMaxHits,
		MaxBundleBytes: d.cfg.SharedMemoryMaxBytes,
		MaxIndexAge:    d.cfg.SharedMemoryIndexMaxAge,
	}
	query := recall.Query{
		IssueTitle:       task.IssueTitle,
		IssueDescription: task.IssueDescription,
		TriggerComment:   task.TriggerCommentContent,
	}
	result := runRecallWithTimeout(ctx, sharedMemoryRecallTimeout, func() recall.Result {
		return recall.Run(ctx, options, query)
	})
	if result.Reason == "recall_timeout" {
		budget := d.cfg.SharedMemoryMaxBytes
		if budget < 512 {
			budget = DefaultSharedMemoryMaxBytes
		}
		result.Query = query.Text()
		result.IndexVersion = recall.CurrentIndexVersion
		result.ByteBudget = budget
		result.Hits = []recall.Hit{}
	}
	result.Render()
	if d.cfg.SharedMemoryLogPath != "" {
		if err := writeRecallLog(d.cfg.SharedMemoryLogPath, task, result, time.Now()); err != nil {
			d.logger.Warn("shared memory recall log failed", "task_id", task.ID, "error", err)
		}
	}
	return result
}

func runRecallWithTimeout(ctx context.Context, timeout time.Duration, run func() recall.Result) recall.Result {
	results := make(chan recall.Result, 1)
	go func() {
		results <- run()
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		return recall.Result{Status: recall.StatusControlledError, Reason: "context_cancelled"}
	case <-timer.C:
		return recall.Result{Status: recall.StatusControlledError, Reason: "recall_timeout"}
	}
}

func prependRecallBundle(prompt string, result *recall.Result) string {
	return "<shared-memory-recall>\n" + result.Render() + "\n</shared-memory-recall>\n\n" + prompt
}

func writeRecallLog(logPath string, task Task, result recall.Result, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("create recall log directory: %w", err)
	}
	digest := sha256.Sum256([]byte(result.Query))
	record := recallLogRecord{
		Timestamp: now.UTC().Format(time.RFC3339), TaskID: task.ID, IssueID: task.IssueID,
		Status: result.Status, HitCount: result.HitCount, QueryHash: hex.EncodeToString(digest[:]),
		BytesUsed: result.BytesUsed, IndexVersion: result.IndexVersion,
		SkippedFiles: result.SkippedFiles, Reason: result.Reason,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode recall log: %w", err)
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open recall log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append recall log: %w", err)
	}
	return nil
}
