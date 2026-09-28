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
	// terminalReportRecordVersion is the on-disk format for records that name
	// the claim generation they belong to. The version had to move: the
	// generation is a replay-safety input, not metadata. An older daemon that
	// reads a generation-aware record as version 1 would ignore
	// claim_dispatched_at and replay the callback unfenced — the stale-reclaim
	// mutation this whole fence exists to prevent. Older daemons therefore
	// reject version 2 as unsupported and leave it untouched.
	terminalReportRecordVersion = 2
	// legacyTerminalReportRecordVersion is the pre-fence layout: keyed by task
	// id alone, no generation. It keeps the #8533 legacy semantics: replayable
	// through the unfenced legacy terminal endpoint. The generation fence
	// applies only to version 2 records.
	legacyTerminalReportRecordVersion = 1

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
// Generation-aware records are version 2 and always carry claim_dispatched_at;
// version 1 is the pre-fence #8533 layout, keyed by task id alone and replayed
// through the legacy terminal endpoint. A record of any other version is left
// untouched and reported on every replay pass: downgrading must never delete a
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
	// ClaimDispatchedAt is the claim generation this result belongs to: the
	// server-issued dispatched_at of the claim that produced it. Absent means a
	// pre-fence version 1 report, which keeps the legacy unfenced replay path.
	// Version 2 records must carry a generation and are never replayed without it
	// (see replayPendingTerminalReports).
	ClaimDispatchedAt *time.Time `json:"claim_dispatched_at,omitempty"`

	PermanentRejectionCount   int        `json:"permanent_rejection_count,omitempty"`
	FirstPermanentRejectionAt *time.Time `json:"first_permanent_rejection_at,omitempty"`
	LastPermanentRejectionAt  *time.Time `json:"last_permanent_rejection_at,omitempty"`
	LastPermanentStatus       int        `json:"last_permanent_status,omitempty"`
	QuarantinedAt             *time.Time `json:"quarantined_at,omitempty"`
	// SupersededAt marks a report the generation fence refused because a newer
	// claim owns the task. Distinct from QuarantinedAt: a superseded report was
	// not refused as malformed, and it must never be turned into a failure
	// compensation against the reclaim that owns the task now.
	SupersededAt *time.Time `json:"superseded_at,omitempty"`
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

// terminalReportStore is a file-backed outbox. One file per terminal-report
// identity — one task id plus one claim generation — keeps each
// acknowledgement, rejection and supersede independent of every other
// generation of the same task, and hashing that identity prevents a malformed
// or tampered task id from becoming a path traversal primitive.
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

// terminalReportIdentity is one terminal result's durable identity: the task it
// belongs to plus the claim generation that produced it. A task id alone is not
// an identity — the server can reclaim a task, and the later claim produces its
// own terminal result — so every queue operation keys on this value: the
// pending file, the failed file, temp-file recovery, acknowledgement, permanent
// rejection, supersede, and in-flight delivery ownership. Two generations of
// one task are then independent records that cannot collide, while the same
// generation stays one record whose payload may not change.
type terminalReportIdentity struct {
	taskID            string
	claimDispatchedAt time.Time
}

func (id terminalReportIdentity) hasGeneration() bool { return !id.claimDispatchedAt.IsZero() }

// key is the identity's canonical byte form. NUL separates the parts so no task
// id can absorb the generation, and the generation is rendered in UTC so the
// same instant can never hash two ways on a machine in another timezone.
func (id terminalReportIdentity) key() string {
	if !id.hasGeneration() {
		return id.taskID
	}
	return id.taskID + "\x00" + id.claimDispatchedAt.UTC().Format(time.RFC3339Nano)
}

// fileName is the on-disk name for this identity. Hashing the whole key keeps a
// malformed or tampered task id from becoming a path segment.
func (id terminalReportIdentity) fileName() string {
	sum := sha256.Sum256([]byte(id.key()))
	return hex.EncodeToString(sum[:]) + ".json"
}

// legacyTerminalReportFileName is the version-1 name: hash(task id) only. It
// stays separate so a legacy file can never collide with a generation-aware one
// and so existing files stay readable without being renamed in place.
func legacyTerminalReportFileName(taskID string) string {
	sum := sha256.Sum256([]byte(taskID))
	return hex.EncodeToString(sum[:]) + ".json"
}

// reportFileName is the name a report's identity owns. A report with no
// generation can only have come from a legacy record, so it keeps the legacy
// name.
func reportFileName(report terminalTaskReport) string {
	if report.claimGeneration.dispatchedAt.IsZero() {
		return legacyTerminalReportFileName(report.taskID)
	}
	return report.identity().fileName()
}

// identity is this report's durable identity. The generation is normalized to
// UTC here so a report built from a claim in another timezone hashes — and
// therefore compares — the same as the record it round-trips through.
func (report terminalTaskReport) identity() terminalReportIdentity {
	return terminalReportIdentity{
		taskID:            report.taskID,
		claimDispatchedAt: report.claimGeneration.dispatchedAt.UTC(),
	}
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
	if report.claimGeneration.unreadable {
		return persistedTerminalTaskReport{}, errTerminalReportGenerationUnreadable
	}
	if report.claimGeneration.dispatchedAt.IsZero() {
		// No generation means the claim came from a server that never advertised
		// the fence. It keeps the #8533 legacy contract: a version-1 record,
		// replayable through the unfenced legacy terminal endpoint.
		return persistedTerminalTaskReport{
			Version:               legacyTerminalReportRecordVersion,
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
	record := persistedTerminalTaskReport{
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
		// UTC on the way in as well as on the way out: struct equality is how
		// enqueue detects a conflicting payload for the same identity, and
		// time.Time compares location pointers, not just the instant.
		ClaimDispatchedAt: generationPtr(report.claimGeneration.dispatchedAt),
	}
	return record, nil
}

func generationPtr(generation time.Time) *time.Time {
	utc := generation.UTC()
	return &utc
}

// errTerminalReportGenerationUnreadable marks a claim whose dispatched_at was
// present but unparseable. It is a protocol error, and it must never be treated
// like an unfenced legacy claim.
var errTerminalReportGenerationUnreadable = errors.New("terminal report claim generation is unreadable")

// terminalReport decodes one record. The version switch is explicit because the
// two layouts carry different ownership semantics: version 2 names the claim
// generation and replays through the versioned endpoint, version 1 carries no
// generation and replays through the legacy endpoint per #8533.
// A version-2 record without a generation is corrupt, not legacy, so it is
// reported as an error rather than downgraded into an unfenced report.
func (record persistedTerminalTaskReport) terminalReport() (terminalTaskReport, error) {
	switch record.Version {
	case legacyTerminalReportRecordVersion, terminalReportRecordVersion:
	default:
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
	report := terminalTaskReport{
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
	}
	if record.Version == legacyTerminalReportRecordVersion {
		// Legacy #8533 record: no generation to invent from current server
		// state, and none needed — it replays through the legacy endpoint with
		// the pre-fence semantics. The fence applies only to version 2.
		return report, nil
	}
	if record.ClaimDispatchedAt == nil || record.ClaimDispatchedAt.IsZero() {
		return terminalTaskReport{}, fmt.Errorf("terminal report version %d has no claim generation", record.Version)
	}
	report.claimGeneration = claimGeneration{dispatchedAt: record.ClaimDispatchedAt.UTC()}
	return report, nil
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
	name := reportFileName(report)
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
		if want := reportFileName(report); entry.Name() != want {
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
		targetName := reportFileName(report)
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
	if item.fileName != reportFileName(item.report) {
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
	if item.fileName != reportFileName(report) || report != item.report {
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
	return true, s.movePendingToFailedQueue(path, item, report)
}

// movePendingToFailedQueue publishes the pending record in failed/ and drops
// the pending file. Callers hold the store mutex and have already validated
// that the pending file still matches item. The rename/removal has completed by
// the time it returns: sync failures are reported to operators, but the caller
// must still treat the report as retired, because there is no pending file left
// for a later pass to rediscover.
func (s *terminalReportStore) movePendingToFailedQueue(path string, item pendingTerminalTaskReport, report terminalTaskReport) error {
	if err := ensureTerminalReportDir(s.failedDir()); err != nil {
		return fmt.Errorf("create failed terminal report queue: %w", err)
	}
	failedPath := filepath.Join(s.failedDir(), item.fileName)
	if existingBody, readErr := os.ReadFile(failedPath); readErr == nil {
		existing, decodeErr := decodePersistedTerminalReport(existingBody)
		if decodeErr != nil {
			return fmt.Errorf("existing failed terminal report is unreadable: %w", decodeErr)
		}
		existingReport, decodeErr := existing.terminalReport()
		if decodeErr != nil || existingReport != report {
			return errors.New("failed terminal report conflicts with queued payload")
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove duplicate quarantined terminal report: %w", err)
		}
	} else if errors.Is(readErr, os.ErrNotExist) {
		if err := os.Rename(path, failedPath); err != nil {
			return fmt.Errorf("quarantine terminal report: %w", err)
		}
	} else {
		return fmt.Errorf("inspect failed terminal report: %w", readErr)
	}
	failedSyncErr := syncTerminalReportDir(s.failedDir())
	pendingSyncErr := syncTerminalReportDir(s.dir)
	return errors.Join(
		wrapTerminalReportSyncError("sync failed terminal report queue", failedSyncErr),
		wrapTerminalReportSyncError("sync pending terminal report queue after quarantine", pendingSyncErr),
	)
}

// recordSupersededReport retires a report the server's generation fence proved
// stale: a newer claim owns the task row, so replaying it can only add load,
// never settle anything. Unlike recordPermanentRejection there is no count/age
// gate — the fence is authoritative on the first response — and the payload is
// retained in failed/ so the result the daemon produced is still inspectable.
func (s *terminalReportStore) recordSupersededReport(item pendingTerminalTaskReport, status int, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDir(); err != nil {
		return err
	}
	path := filepath.Join(s.dir, item.fileName)
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read superseded terminal report: %w", err)
	}
	record, err := decodePersistedTerminalReport(body)
	if err != nil {
		return fmt.Errorf("decode superseded terminal report: %w", err)
	}
	report, err := record.terminalReport()
	if err != nil {
		return fmt.Errorf("validate superseded terminal report: %w", err)
	}
	if item.fileName != reportFileName(report) || report != item.report {
		return errors.New("superseded terminal report no longer matches queued payload")
	}
	supersededAt := now.UTC()
	record.SupersededAt = &supersededAt
	record.LastPermanentStatus = status
	if err := writeTerminalReportRecord(s.dir, item.fileName, record); err != nil {
		return fmt.Errorf("persist superseded terminal report: %w", err)
	}
	return s.movePendingToFailedQueue(path, item, report)
}

// quarantineSupersededTerminalReport is the daemon-side arm of
// recordSupersededReport. It never runs the complete→fail compensation: the
// reclaim that owns the task row now must keep its own outcome.
func (d *Daemon) quarantineSupersededTerminalReport(item pendingTerminalTaskReport, deliveryErr error) bool {
	if d.terminalReports == nil {
		return false
	}
	if err := d.terminalReports.recordSupersededReport(item, http.StatusConflict, d.terminalReportClock()); err != nil {
		d.logger.Error("retire superseded terminal report",
			"task", item.report.taskID,
			"kind", item.report.kind,
			"error", err,
		)
		return false
	}
	d.logger.Warn("terminal report rejected as a stale claim generation; retained in the failed terminal-report queue",
		"task", item.report.taskID,
		"kind", item.report.kind,
		"error", deliveryErr,
	)
	return true
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
	// A replica that has no versioned terminal route could not settle this
	// report, but it did not reject it either: keep it pending so a later attempt
	// can reach a fence-capable replica. It must not reach the permanent
	// rejection path below, which would strand a valid result in failed/ because
	// one old replica happened to answer a rolling deployment's request.
	if isFencedTerminalEndpointUnsupported(deliveryErr) {
		d.logger.Warn("terminal report held: this server replica has no fenced terminal endpoint",
			"task", item.report.taskID,
			"kind", item.report.kind,
		)
		return false
	}
	// The server's generation fence is authoritative: it proved inside the
	// terminal UPDATE that a later claim owns this task row, so this result can
	// never settle it. Retire the report instead of replaying it forever, and
	// skip the complete→fail compensation below — failing the row would mutate
	// the reclaim that legitimately owns it, which is the exact outcome the
	// fence exists to prevent.
	if isStaleClaimGenerationError(deliveryErr) {
		return d.quarantineSupersededTerminalReport(item, deliveryErr)
	}
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
		// The compensation must not invent a generation: it reports on behalf of
		// the same claim, so it carries the same one — and stays unfenced exactly
		// when its parent report had none.
		fallback := terminalTaskReport{
			kind:                  terminalTaskReportFail,
			taskID:                item.report.taskID,
			claimGeneration:       item.report.claimGeneration,
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
				release, ok := d.beginTerminalReportDelivery(item.report.identity())
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
