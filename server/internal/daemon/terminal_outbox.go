package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const terminalOutboxSchemaVersion = 1

var terminalOutboxReplayInterval = 30 * time.Second

type terminalOutbox struct {
	dir string
}

type terminalOutboxRecord struct {
	Version               int    `json:"version"`
	Kind                  string `json:"kind"`
	TaskID                string `json:"task_id"`
	Output                string `json:"output,omitempty"`
	BranchName            string `json:"branch_name,omitempty"`
	ErrorMessage          string `json:"error_message,omitempty"`
	SessionID             string `json:"session_id,omitempty"`
	WorkDir               string `json:"work_dir,omitempty"`
	DurableWorkDir        string `json:"durable_work_dir,omitempty"`
	FailureReason         string `json:"failure_reason,omitempty"`
	SessionRolloutMissing bool   `json:"session_rollout_missing,omitempty"`
	RetiredSessionID      string `json:"retired_session_id,omitempty"`
}

type terminalOutboxEntry struct {
	path   string
	report terminalTaskReport
	err    error
}

func newTerminalOutbox(rootDir, serverBaseURL string) *terminalOutbox {
	if strings.TrimSpace(rootDir) == "" {
		return nil
	}
	// A profile can be repointed to another self-hosted server. Scope pending
	// callbacks to the origin that issued their task IDs so a later config
	// change cannot replay private output into an unrelated deployment.
	origin := sha256.Sum256([]byte(strings.TrimRight(strings.TrimSpace(serverBaseURL), "/")))
	return &terminalOutbox{dir: filepath.Join(rootDir, hex.EncodeToString(origin[:]))}
}

func terminalReportRecord(report terminalTaskReport) (terminalOutboxRecord, error) {
	kind := ""
	switch report.kind {
	case terminalTaskReportComplete:
		kind = "complete"
	case terminalTaskReportFail:
		kind = "fail"
	default:
		return terminalOutboxRecord{}, fmt.Errorf("unsupported terminal task report kind %d", report.kind)
	}
	if strings.TrimSpace(report.taskID) == "" {
		return terminalOutboxRecord{}, errors.New("terminal task report has empty task id")
	}
	return terminalOutboxRecord{
		Version:               terminalOutboxSchemaVersion,
		Kind:                  kind,
		TaskID:                report.taskID,
		Output:                report.output,
		BranchName:            report.branchName,
		ErrorMessage:          report.errorMessage,
		SessionID:             report.sessionID,
		WorkDir:               report.workDir,
		DurableWorkDir:        report.durableWorkDir,
		FailureReason:         report.failureReason,
		SessionRolloutMissing: report.sessionRolloutMissing,
		RetiredSessionID:      report.retiredSessionID,
	}, nil
}

func (record terminalOutboxRecord) report() (terminalTaskReport, error) {
	if record.Version != terminalOutboxSchemaVersion {
		return terminalTaskReport{}, fmt.Errorf("unsupported terminal outbox version %d", record.Version)
	}
	kind := terminalTaskReportKind(0)
	switch record.Kind {
	case "complete":
		kind = terminalTaskReportComplete
	case "fail":
		kind = terminalTaskReportFail
	default:
		return terminalTaskReport{}, fmt.Errorf("unsupported terminal outbox kind %q", record.Kind)
	}
	if strings.TrimSpace(record.TaskID) == "" {
		return terminalTaskReport{}, errors.New("terminal outbox record has empty task id")
	}
	return terminalTaskReport{
		kind:                  kind,
		taskID:                record.TaskID,
		output:                record.Output,
		branchName:            record.BranchName,
		errorMessage:          record.ErrorMessage,
		sessionID:             record.SessionID,
		workDir:               record.WorkDir,
		durableWorkDir:        record.DurableWorkDir,
		failureReason:         record.FailureReason,
		sessionRolloutMissing: record.SessionRolloutMissing,
		retiredSessionID:      record.RetiredSessionID,
	}, nil
}

// put writes one immutable, content-addressed record. Persisting before the
// HTTP callback closes the crash window where the agent has exited but the
// server never learns its terminal state. Content-addressing makes identical
// re-enqueues idempotent without relying on rename-over-existing semantics on
// Windows.
func (o *terminalOutbox) put(report terminalTaskReport) (string, error) {
	if o == nil || o.dir == "" {
		return "", nil
	}
	record, err := terminalReportRecord(report)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	path := filepath.Join(o.dir, hex.EncodeToString(sum[:])+".json")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		return "", fmt.Errorf("create terminal outbox: %w", err)
	}
	if err := os.Chmod(o.dir, 0o700); err != nil {
		return "", fmt.Errorf("secure terminal outbox: %w", err)
	}
	tmp, err := os.CreateTemp(o.dir, ".terminal-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create terminal outbox temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("secure terminal outbox temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write terminal outbox temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("sync terminal outbox temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close terminal outbox temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// A concurrent identical enqueue may have won the race. That is success:
		// both writers produced the same content-addressed record.
		if _, statErr := os.Stat(path); statErr == nil {
			return path, nil
		}
		return "", fmt.Errorf("publish terminal outbox record: %w", err)
	}
	removeTemp = false
	return path, nil
}

func (o *terminalOutbox) list() ([]terminalOutboxEntry, error) {
	if o == nil || o.dir == "" {
		return nil, nil
	}
	files, err := os.ReadDir(o.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries := make([]terminalOutboxEntry, 0, len(files))
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}
		path := filepath.Join(o.dir, file.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			entries = append(entries, terminalOutboxEntry{path: path, err: readErr})
			continue
		}
		var record terminalOutboxRecord
		if decodeErr := json.Unmarshal(data, &record); decodeErr != nil {
			entries = append(entries, terminalOutboxEntry{path: path, err: decodeErr})
			continue
		}
		report, reportErr := record.report()
		entries = append(entries, terminalOutboxEntry{path: path, report: report, err: reportErr})
	}
	return entries, nil
}

func (o *terminalOutbox) remove(path string) error {
	if o == nil || path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (d *Daemon) signalTerminalOutboxReplay() {
	if d.terminalOutboxWake == nil {
		return
	}
	select {
	case d.terminalOutboxWake <- struct{}{}:
	default:
	}
}

func (d *Daemon) terminalOutboxLoop(ctx context.Context) {
	d.replayTerminalOutbox(ctx)
	ticker := time.NewTicker(terminalOutboxReplayInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.replayTerminalOutbox(ctx)
		case <-d.terminalOutboxWake:
			d.replayTerminalOutbox(ctx)
		}
	}
}

func (d *Daemon) replayTerminalOutbox(ctx context.Context) {
	entries, err := d.terminalOutbox.list()
	if err != nil {
		d.logger.Error("terminal outbox scan failed", "error", err)
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if entry.err != nil {
			d.logger.Error("terminal outbox record unreadable", "path", entry.path, "error", entry.err)
			continue
		}
		replayCtx, cancel := context.WithTimeout(ctx, terminalTaskReportTimeout)
		err := d.sendTerminalTaskReport(replayCtx, entry.report)
		cancel()
		if err == nil {
			if removeErr := d.terminalOutbox.remove(entry.path); removeErr != nil {
				d.logger.Warn("terminal outbox acknowledgement cleanup failed", "task_id", entry.report.taskID, "error", removeErr)
				continue
			}
			d.logger.Info("terminal outbox report delivered", "task_id", entry.report.taskID, "kind", entry.report.kind)
			continue
		}
		if !isTransientError(err) && !isUnauthorizedError(err) {
			if removeErr := d.terminalOutbox.remove(entry.path); removeErr != nil {
				d.logger.Warn("terminal outbox rejected record cleanup failed", "task_id", entry.report.taskID, "error", removeErr)
			}
			d.logger.Error("terminal outbox report permanently rejected", "task_id", entry.report.taskID, "kind", entry.report.kind, "error", err)
			continue
		}
		d.logger.Warn("terminal outbox report still pending", "task_id", entry.report.taskID, "kind", entry.report.kind, "error", err)
		if isUnauthorizedError(err) {
			return
		}
	}
}

func terminalReportKindName(kind terminalTaskReportKind) string {
	switch kind {
	case terminalTaskReportComplete:
		return "complete"
	case terminalTaskReportFail:
		return "fail"
	default:
		return fmt.Sprintf("unknown(%d)", kind)
	}
}

func logTerminalOutboxPersistFailure(logger *slog.Logger, report terminalTaskReport, err error) {
	if logger == nil || err == nil {
		return
	}
	logger.Error("terminal report could not be persisted before delivery",
		"task_id", report.taskID,
		"kind", terminalReportKindName(report.kind),
		"error", err,
	)
}
