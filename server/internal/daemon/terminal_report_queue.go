package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	terminalReportRecordVersion = 1
	terminalReportReplayWorkers = 4

	terminalReportReplayInitialBackoff = 5 * time.Second
	terminalReportReplayMaxBackoff     = 5 * time.Minute

	// Only an unchanged request that receives three explicit permanent
	// rejections over at least ten minutes is quarantined. The count rejects a
	// one-off proxy response; the age prevents reconnect/startup nudges from
	// turning three rapid attempts into a false permanent verdict.
	terminalReportPermanentRejectionLimit = 3
	terminalReportPermanentRejectionAge   = 10 * time.Minute
)

// persistedTerminalTaskReport is the versioned on-disk form of one terminal
// callback. It deliberately contains no auth token: replay always uses the
// daemon's current credential, while the potentially sensitive agent output is
// protected by owner-only modes on Unix. On Windows Go's mode bits are not an
// ACL boundary, so protection comes from the current user's profile/workspace
// ACL; the queue never carries an auth token on either platform.
//
// Version stays at 1 because the rejection fields are additive and older
// daemons ignore unknown JSON fields. A future incompatible version is left
// untouched and reported on every replay pass; downgrading must never delete a
// payload merely because the older binary cannot decode it.
type persistedTerminalTaskReport struct {
	Version               int       `json:"version"`
	CreatedAt             time.Time `json:"created_at"`
	Kind                  string    `json:"kind"`
	TaskID                string    `json:"task_id"`
	Output                string    `json:"output,omitempty"`
	BranchName            string    `json:"branch_name,omitempty"`
	ErrorMessage          string    `json:"error,omitempty"`
	SessionID             string    `json:"session_id,omitempty"`
	WorkDir               string    `json:"work_dir,omitempty"`
	DurableWorkDir        string    `json:"durable_work_dir,omitempty"`
	FailureReason         string    `json:"failure_reason,omitempty"`
	SessionRolloutMissing bool      `json:"session_rollout_missing,omitempty"`
	RetiredSessionID      string    `json:"retired_session_id,omitempty"`

	PermanentRejectionCount   int        `json:"permanent_rejection_count,omitempty"`
	FirstPermanentRejectionAt *time.Time `json:"first_permanent_rejection_at,omitempty"`
	LastPermanentRejectionAt  *time.Time `json:"last_permanent_rejection_at,omitempty"`
	LastPermanentStatus       int        `json:"last_permanent_status,omitempty"`
	QuarantinedAt             *time.Time `json:"quarantined_at,omitempty"`
}

type pendingTerminalTaskReport struct {
	fileName string
	report   terminalTaskReport
}

type terminalReportStoreStats struct {
	PendingCount int
	PendingBytes int64
	FailedCount  int
	FailedBytes  int64
}

type terminalReportNamespaceStats struct {
	name  string
	stats terminalReportStoreStats
}

// terminalReportStore is a file-backed outbox. One file per task keeps each
// acknowledgement independent, and hashing task IDs prevents a malformed or
// tampered task ID from becoming a path traversal primitive.
type terminalReportStore struct {
	root      string
	namespace string
	dir       string
	mu        sync.Mutex
}

func newTerminalReportStore(cfg Config) *terminalReportStore {
	if strings.TrimSpace(cfg.WorkspacesRoot) == "" {
		return nil
	}
	identity := strings.TrimRight(cfg.ServerBaseURL, "/") + "\x00" + cfg.Profile + "\x00" + cfg.DaemonID
	sum := sha256.Sum256([]byte(identity))
	namespace := hex.EncodeToString(sum[:16])
	root := filepath.Join(cfg.WorkspacesRoot, ".pending-terminal-reports", "v1")
	return &terminalReportStore{
		root:      root,
		namespace: namespace,
		dir:       filepath.Join(root, namespace),
	}
}

func (s *terminalReportStore) failedDir() string { return filepath.Join(s.dir, "failed") }

func terminalReportFileName(taskID string) string {
	sum := sha256.Sum256([]byte(taskID))
	return hex.EncodeToString(sum[:]) + ".json"
}

func terminalReportKindName(kind terminalTaskReportKind) (string, error) {
	switch kind {
	case terminalTaskReportComplete:
		return "complete", nil
	case terminalTaskReportFail:
		return "fail", nil
	default:
		return "", fmt.Errorf("unsupported terminal task report kind %d", kind)
	}
}

func persistedTerminalReport(report terminalTaskReport, createdAt time.Time) (persistedTerminalTaskReport, error) {
	if strings.TrimSpace(report.taskID) == "" {
		return persistedTerminalTaskReport{}, errors.New("terminal task report has no task id")
	}
	kind, err := terminalReportKindName(report.kind)
	if err != nil {
		return persistedTerminalTaskReport{}, err
	}
	return persistedTerminalTaskReport{
		Version:               terminalReportRecordVersion,
		CreatedAt:             createdAt.UTC(),
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

func (record persistedTerminalTaskReport) terminalReport() (terminalTaskReport, error) {
	if record.Version != terminalReportRecordVersion {
		return terminalTaskReport{}, fmt.Errorf("unsupported terminal report version %d", record.Version)
	}
	if strings.TrimSpace(record.TaskID) == "" {
		return terminalTaskReport{}, errors.New("terminal report has no task id")
	}
	var kind terminalTaskReportKind
	switch record.Kind {
	case "complete":
		kind = terminalTaskReportComplete
	case "fail":
		kind = terminalTaskReportFail
	default:
		return terminalTaskReport{}, fmt.Errorf("unsupported terminal report kind %q", record.Kind)
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

func (s *terminalReportStore) ensureDir() error {
	if s == nil {
		return errors.New("terminal report store is not configured")
	}
	return ensureTerminalReportDir(s.dir)
}

func ensureTerminalReportDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create terminal report queue: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect terminal report queue: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("terminal report queue is not a real directory: %s", dir)
	}
	// Tighten an existing directory as well as a newly-created one. Terminal
	// payloads may include private prompts, results, paths, and error details.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure terminal report queue: %w", err)
	}
	return nil
}

func (s *terminalReportStore) enqueue(report terminalTaskReport) error {
	record, err := persistedTerminalReport(report, time.Now())
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDir(); err != nil {
		return err
	}
	name := terminalReportFileName(report.taskID)
	path := filepath.Join(s.dir, name)
	if existingBody, readErr := os.ReadFile(path); readErr == nil {
		existing, decodeErr := decodePersistedTerminalReport(existingBody)
		if decodeErr != nil {
			return fmt.Errorf("existing terminal report %s is unreadable: %w", name, decodeErr)
		}
		existingReport, decodeErr := existing.terminalReport()
		if decodeErr != nil {
			return fmt.Errorf("existing terminal report %s is invalid: %w", name, decodeErr)
		}
		if existingReport != report {
			return fmt.Errorf("terminal report for task %s conflicts with the original pending payload", report.taskID)
		}
		return nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read existing terminal report %s: %w", name, readErr)
	}

	return writeTerminalReportRecord(s.dir, name, record)
}

// writeTerminalReportRecord publishes a fully-flushed record with an atomic
// rename. Callers hold the store mutex. Temp files use the task-derived prefix
// so startup recovery can validate where an interrupted write belonged.
func writeTerminalReportRecord(dir, name string, record persistedTerminalTaskReport) error {
	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode terminal report: %w", err)
	}
	body = append(body, '\n')

	tmp, err := os.CreateTemp(dir, "."+strings.TrimSuffix(name, ".json")+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create terminal report temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("secure terminal report temp file: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		cleanup()
		return fmt.Errorf("write terminal report temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync terminal report temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close terminal report temp file: %w", err)
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("publish terminal report: %w", err)
	}
	if err := syncTerminalReportDir(dir); err != nil {
		return fmt.Errorf("sync terminal report queue after enqueue: %w", err)
	}
	return nil
}

func decodePersistedTerminalReport(body []byte) (persistedTerminalTaskReport, error) {
	var record persistedTerminalTaskReport
	if err := json.Unmarshal(body, &record); err != nil {
		return persistedTerminalTaskReport{}, err
	}
	return record, nil
}

func (s *terminalReportStore) list() ([]pendingTerminalTaskReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read terminal report queue: %w", err)
	}
	recoveryErr := s.recoverTempFiles(entries)
	entries, err = os.ReadDir(s.dir)
	if err != nil {
		return nil, errors.Join(recoveryErr, fmt.Errorf("reread terminal report queue: %w", err))
	}
	items := make([]pendingTerminalTaskReport, 0, len(entries))
	var errs []error
	if recoveryErr != nil {
		errs = append(errs, recoveryErr)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if readErr != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", entry.Name(), readErr))
			continue
		}
		record, decodeErr := decodePersistedTerminalReport(body)
		if decodeErr != nil {
			errs = append(errs, fmt.Errorf("decode %s: %w", entry.Name(), decodeErr))
			continue
		}
		report, decodeErr := record.terminalReport()
		if decodeErr != nil {
			errs = append(errs, fmt.Errorf("validate %s: %w", entry.Name(), decodeErr))
			continue
		}
		if want := terminalReportFileName(report.taskID); entry.Name() != want {
			errs = append(errs, fmt.Errorf("terminal report %s does not match task id", entry.Name()))
			continue
		}
		items = append(items, pendingTerminalTaskReport{fileName: entry.Name(), report: report})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].fileName < items[j].fileName })
	return items, errors.Join(errs...)
}

// recoverTempFiles closes the atomic-write crash window after the temp file is
// fully flushed but before its rename. A partial/corrupt temp file is retained
// for forensic recovery and reported as an error; silently deleting it could
// discard the only copy of a terminal payload.
func (s *terminalReportStore) recoverTempFiles(entries []os.DirEntry) error {
	var errs []error
	changed := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".tmp") {
			continue
		}
		tempPath := filepath.Join(s.dir, name)
		body, err := os.ReadFile(tempPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("read interrupted terminal report %s: %w", name, err))
			continue
		}
		record, err := decodePersistedTerminalReport(body)
		if err != nil {
			errs = append(errs, fmt.Errorf("decode interrupted terminal report %s: %w", name, err))
			continue
		}
		report, err := record.terminalReport()
		if err != nil {
			errs = append(errs, fmt.Errorf("validate interrupted terminal report %s: %w", name, err))
			continue
		}
		targetName := terminalReportFileName(report.taskID)
		wantPrefix := "." + strings.TrimSuffix(targetName, ".json") + "-"
		if !strings.HasPrefix(name, wantPrefix) {
			errs = append(errs, fmt.Errorf("interrupted terminal report %s does not match task id", name))
			continue
		}
		targetPath := filepath.Join(s.dir, targetName)
		if existingBody, readErr := os.ReadFile(targetPath); readErr == nil {
			existing, decodeErr := decodePersistedTerminalReport(existingBody)
			if decodeErr != nil {
				errs = append(errs, fmt.Errorf("decode terminal report while recovering %s: %w", targetName, decodeErr))
				continue
			}
			existingReport, decodeErr := existing.terminalReport()
			if decodeErr != nil || existingReport != report {
				errs = append(errs, fmt.Errorf("interrupted terminal report %s conflicts with existing payload", name))
				continue
			}
			if err := os.Remove(tempPath); err != nil {
				errs = append(errs, fmt.Errorf("remove duplicate interrupted terminal report %s: %w", name, err))
				continue
			}
			changed = true
			continue
		} else if !errors.Is(readErr, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("inspect terminal report while recovering %s: %w", targetName, readErr))
			continue
		}
		if err := os.Chmod(tempPath, 0o600); err != nil {
			errs = append(errs, fmt.Errorf("secure interrupted terminal report %s: %w", name, err))
			continue
		}
		if err := os.Rename(tempPath, targetPath); err != nil {
			errs = append(errs, fmt.Errorf("recover interrupted terminal report %s: %w", name, err))
			continue
		}
		changed = true
	}
	if changed {
		if err := syncTerminalReportDir(s.dir); err != nil {
			errs = append(errs, fmt.Errorf("sync terminal report queue after recovery: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *terminalReportStore) acknowledge(item pendingTerminalTaskReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.fileName != terminalReportFileName(item.report.taskID) {
		return errors.New("terminal report acknowledgement does not match task id")
	}
	path := filepath.Join(s.dir, item.fileName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove acknowledged terminal report: %w", err)
	}
	if err := syncTerminalReportDir(s.dir); err != nil {
		return fmt.Errorf("sync terminal report queue after acknowledgement: %w", err)
	}
	return nil
}

// terminalReportPermanentRejection classifies only response semantics that are
// stable for an unchanged terminal request. Authentication expiry (401), rate
// limiting (429), timeout (408), conflicts, and generic/missing-route 404s stay
// pending because credentials, deployment version, or server state can recover.
func terminalReportPermanentRejection(err error) (int, bool) {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return 0, false
	}
	switch reqErr.StatusCode {
	case http.StatusBadRequest, http.StatusForbidden:
		return reqErr.StatusCode, true
	case http.StatusNotFound:
		return reqErr.StatusCode, isTaskNotFoundError(err)
	default:
		return 0, false
	}
}

// recordPermanentRejection durably counts explicit server rejections and moves
// the unchanged original payload to failed/ once both the count and age gates
// are met. The failed record is intentionally retained indefinitely: automatic
// TTL/size eviction would silently discard the result this outbox protects.
func (s *terminalReportStore) recordPermanentRejection(item pendingTerminalTaskReport, status int, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDir(); err != nil {
		return false, err
	}
	path := filepath.Join(s.dir, item.fileName)
	body, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read rejected terminal report: %w", err)
	}
	record, err := decodePersistedTerminalReport(body)
	if err != nil {
		return false, fmt.Errorf("decode rejected terminal report: %w", err)
	}
	report, err := record.terminalReport()
	if err != nil {
		return false, fmt.Errorf("validate rejected terminal report: %w", err)
	}
	if item.fileName != terminalReportFileName(report.taskID) || report != item.report {
		return false, errors.New("rejected terminal report no longer matches queued payload")
	}

	now = now.UTC()
	if record.FirstPermanentRejectionAt == nil {
		first := now
		record.FirstPermanentRejectionAt = &first
	}
	last := now
	record.LastPermanentRejectionAt = &last
	record.LastPermanentStatus = status
	record.PermanentRejectionCount++
	age := now.Sub(*record.FirstPermanentRejectionAt)
	quarantine := record.PermanentRejectionCount >= terminalReportPermanentRejectionLimit && age >= terminalReportPermanentRejectionAge
	if quarantine {
		quarantinedAt := now
		record.QuarantinedAt = &quarantinedAt
	}
	if err := writeTerminalReportRecord(s.dir, item.fileName, record); err != nil {
		return false, fmt.Errorf("persist terminal report rejection: %w", err)
	}
	if !quarantine {
		return false, nil
	}
	if err := ensureTerminalReportDir(s.failedDir()); err != nil {
		return false, fmt.Errorf("create failed terminal report queue: %w", err)
	}
	failedPath := filepath.Join(s.failedDir(), item.fileName)
	if existingBody, readErr := os.ReadFile(failedPath); readErr == nil {
		existing, decodeErr := decodePersistedTerminalReport(existingBody)
		if decodeErr != nil {
			return false, fmt.Errorf("existing failed terminal report is unreadable: %w", decodeErr)
		}
		existingReport, decodeErr := existing.terminalReport()
		if decodeErr != nil || existingReport != report {
			return false, errors.New("failed terminal report conflicts with queued payload")
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("remove duplicate quarantined terminal report: %w", err)
		}
	} else if errors.Is(readErr, os.ErrNotExist) {
		if err := os.Rename(path, failedPath); err != nil {
			return false, fmt.Errorf("quarantine terminal report: %w", err)
		}
	} else {
		return false, fmt.Errorf("inspect failed terminal report: %w", readErr)
	}
	// The rename/removal has completed at this point. Report sync failures to
	// operators, but also return quarantined=true so the caller performs the
	// one-time server compensation. Returning false would leave no pending file
	// for a later pass to rediscover and could strand the server row in running.
	failedSyncErr := syncTerminalReportDir(s.failedDir())
	pendingSyncErr := syncTerminalReportDir(s.dir)
	return true, errors.Join(
		wrapTerminalReportSyncError("sync failed terminal report queue", failedSyncErr),
		wrapTerminalReportSyncError("sync pending terminal report queue after quarantine", pendingSyncErr),
	)
}

func wrapTerminalReportSyncError(message string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}

func terminalReportDirectoryStats(dir string) (count int, bytes int64, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			errs = append(errs, fmt.Errorf("stat %s: %w", entry.Name(), infoErr))
			continue
		}
		count++
		bytes += info.Size()
	}
	return count, bytes, errors.Join(errs...)
}

func (s *terminalReportStore) stats() (terminalReportStoreStats, error) {
	if s == nil {
		return terminalReportStoreStats{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var stats terminalReportStoreStats
	var errs []error
	if count, bytes, err := terminalReportDirectoryStats(s.dir); err != nil {
		errs = append(errs, fmt.Errorf("scan pending terminal reports: %w", err))
	} else {
		stats.PendingCount, stats.PendingBytes = count, bytes
	}
	if count, bytes, err := terminalReportDirectoryStats(s.failedDir()); err != nil {
		errs = append(errs, fmt.Errorf("scan failed terminal reports: %w", err))
	} else {
		stats.FailedCount, stats.FailedBytes = count, bytes
	}
	return stats, errors.Join(errs...)
}

// otherNamespaceStats surfaces records owned by a different server/profile/
// daemon identity. Replaying them with the current credential could cross an
// account boundary, so startup warns instead of adopting or deleting them.
func (s *terminalReportStore) otherNamespaceStats() ([]terminalReportNamespaceStats, error) {
	if s == nil {
		return nil, nil
	}
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan terminal report namespaces: %w", err)
	}
	var out []terminalReportNamespaceStats
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == s.namespace {
			continue
		}
		dir := filepath.Join(s.root, entry.Name())
		pendingCount, pendingBytes, pendingErr := terminalReportDirectoryStats(dir)
		failedCount, failedBytes, failedErr := terminalReportDirectoryStats(filepath.Join(dir, "failed"))
		if pendingErr != nil || failedErr != nil {
			errs = append(errs, fmt.Errorf("scan terminal report namespace %s: %w", entry.Name(), errors.Join(pendingErr, failedErr)))
		}
		stats := terminalReportStoreStats{
			PendingCount: pendingCount,
			PendingBytes: pendingBytes,
			FailedCount:  failedCount,
			FailedBytes:  failedBytes,
		}
		if stats.PendingCount+stats.FailedCount > 0 {
			out = append(out, terminalReportNamespaceStats{name: entry.Name(), stats: stats})
		}
	}
	return out, errors.Join(errs...)
}

func (d *Daemon) signalTerminalReportReplay() {
	if d.terminalReportWakeup == nil {
		return
	}
	select {
	case d.terminalReportWakeup <- struct{}{}:
	default:
	}
}

// handleTerminalReportDeliveryError records an explicit permanent rejection.
// It returns true only after the original payload has been moved out of the
// replay set into failed/. Transient failures and early permanent rejections
// remain pending; a post-rename sync error is logged while compensation still
// runs because there is no pending path left for a later pass to discover.
func (d *Daemon) handleTerminalReportDeliveryError(ctx context.Context, item pendingTerminalTaskReport, deliveryErr error) bool {
	status, permanent := terminalReportPermanentRejection(deliveryErr)
	if !permanent || d.terminalReports == nil {
		return false
	}
	quarantined, err := d.terminalReports.recordPermanentRejection(item, status, d.terminalReportClock())
	if err != nil {
		d.logger.Error("record permanent terminal report rejection",
			"task", item.report.taskID,
			"kind", item.report.kind,
			"status", status,
			"error", err,
		)
	}
	if !quarantined {
		return false
	}
	d.logger.Error("terminal report permanently rejected and quarantined",
		"task", item.report.taskID,
		"kind", item.report.kind,
		"status", status,
		"failed_queue", true,
	)

	// A rejected success can otherwise leave a server row in running forever.
	// Preserve the complete payload in failed/ first, then make the legacy fail
	// compensation as a separate request. It can settle the row but can never
	// overwrite or delete the user's original successful result. A semantic
	// task-not-found response needs no compensation because no row remains.
	if item.report.kind == terminalTaskReportComplete && !isTaskNotFoundError(deliveryErr) {
		fallback := terminalTaskReport{
			kind:                  terminalTaskReportFail,
			taskID:                item.report.taskID,
			errorMessage:          fmt.Sprintf("successful terminal result was rejected by the server with HTTP %d; the original completion is preserved in the daemon failed terminal-report queue", status),
			branchName:            item.report.branchName,
			sessionID:             item.report.sessionID,
			workDir:               item.report.workDir,
			durableWorkDir:        item.report.durableWorkDir,
			failureReason:         "agent_error.unknown",
			sessionRolloutMissing: item.report.sessionRolloutMissing,
			retiredSessionID:      item.report.retiredSessionID,
		}
		if err := d.sendTerminalTaskReport(ctx, fallback, defaultTerminalRetrySchedule); err != nil {
			d.logger.Error("terminal report quarantine failure compensation was not accepted",
				"task", item.report.taskID,
				"error", err,
			)
		} else {
			d.logger.Warn("terminal report quarantine settled server task as failed; original completion retained",
				"task", item.report.taskID,
			)
		}
	}
	return true
}

// replayPendingTerminalReports makes one delivery attempt per queued report.
// A small fixed worker pool avoids one dead endpoint blocking every later task
// for the HTTP client's full timeout while still bounding reconnect pressure.
// The caller owns the outer backoff; each pending item gets exactly one HTTP
// attempt. The one-time fail compensation after quarantine uses the normal
// bounded terminal schedule because there will be no later replay for it.
func (d *Daemon) replayPendingTerminalReports(ctx context.Context) (pending, delivered int) {
	if d.terminalReports == nil {
		return 0, 0
	}
	items, err := d.terminalReports.list()
	listFailed := err != nil
	if err != nil {
		d.logger.Error("load pending terminal reports", "error", err)
	}
	if len(items) == 0 {
		if listFailed {
			// Keep the replay timer alive so corrupt/unsupported/unreadable
			// records continue to alert instead of warning once at startup and
			// silently disappearing from operational view.
			return 1, 0
		}
		return 0, 0
	}

	workers := terminalReportReplayWorkers
	if len(items) < workers {
		workers = len(items)
	}
	jobs := make(chan pendingTerminalTaskReport)
	var wg sync.WaitGroup
	var resultMu sync.Mutex
	remaining := len(items)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if ctx.Err() != nil {
					continue
				}
				release, ok := d.beginTerminalReportDelivery(item.report.taskID)
				if !ok {
					continue
				}
				err := d.sendTerminalTaskReport(ctx, item.report, nil)
				quarantined := false
				if err == nil {
					err = d.terminalReports.acknowledge(item)
				} else {
					quarantined = d.handleTerminalReportDeliveryError(ctx, item, err)
				}
				release()
				resultMu.Lock()
				if err == nil {
					remaining--
					delivered++
				} else if quarantined {
					remaining--
				} else {
					d.logger.Warn("pending terminal report remains queued",
						"task", item.report.taskID,
						"kind", item.report.kind,
						"error", err,
					)
				}
				resultMu.Unlock()
			}
		}()
	}
	for _, item := range items {
		select {
		case jobs <- item:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return remaining, delivered
		}
	}
	close(jobs)
	wg.Wait()
	if listFailed && remaining == 0 {
		remaining = 1
	}
	return remaining, delivered
}

func (d *Daemon) terminalReportReplayLoop(ctx context.Context) {
	if namespaces, err := d.terminalReports.otherNamespaceStats(); err != nil {
		d.logger.Warn("scan terminal report namespaces", "error", err)
	} else {
		for _, namespace := range namespaces {
			d.logger.Warn("terminal reports exist for a different daemon identity; not replaying",
				"namespace", namespace.name,
				"pending_count", namespace.stats.PendingCount,
				"pending_bytes", namespace.stats.PendingBytes,
				"failed_count", namespace.stats.FailedCount,
				"failed_bytes", namespace.stats.FailedBytes,
			)
		}
	}
	backoff := terminalReportReplayInitialBackoff
	var timer *time.Timer
	resetTimer := func(delay time.Duration) <-chan time.Time {
		if timer == nil {
			timer = time.NewTimer(delay)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(delay)
		}
		return timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	// Startup is itself a replay trigger. The queue is loaded only after auth
	// preflight, so recovered reports use the daemon's current credential.
	timerCh := resetTimer(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.terminalReportWakeup:
			backoff = terminalReportReplayInitialBackoff
			timerCh = resetTimer(0)
		case <-timerCh:
			pending, delivered := d.replayPendingTerminalReports(ctx)
			if delivered > 0 {
				d.logger.Info("replayed pending terminal reports", "delivered", delivered, "remaining", pending)
			}
			if pending == 0 {
				timerCh = nil
				backoff = terminalReportReplayInitialBackoff
				continue
			}
			timerCh = resetTimer(backoff)
			backoff *= 2
			if backoff > terminalReportReplayMaxBackoff {
				backoff = terminalReportReplayMaxBackoff
			}
		}
	}
}
