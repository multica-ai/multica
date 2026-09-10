package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// A terminal callback is the only thing that moves a task out of 'running'
// server-side. When one is lost, the row stays 'running' forever: the daemon
// keeps heartbeating, so the stale-task sweeper deliberately skips it, and the
// retry budget is unreachable because retries are gated on 'failed'. The agent
// shows 'working' and one of its max_concurrent_tasks slots is consumed for
// good. That is GH #8221 (a partition exhausted the /complete retries, and the
// daemon deliberately left the task running rather than reporting a successful
// run as a failure) and the second half of GH #8272.
//
// The retry schedule inside CompleteTask/FailTask only covers outages shorter
// than its budget (~124s). Anything longer used to end with the report being
// dropped in memory. This outbox is the durable half: a report that survives
// the in-process schedule is written to disk and replayed once the server is
// reachable again, including across a daemon restart.
//
// Safety rests on the server side already being idempotent — CompleteTask
// treats an already-finalized callback as success, and the terminal writes CAS
// on the non-terminal status — so replaying a report that actually landed is a
// no-op rather than a double-finalize.

const (
	terminalOutboxDirName = "pending-terminal-reports"

	// A report is retried for this long before it is abandoned. The window is
	// generous because the whole point is surviving a long outage, and the cost
	// of holding one small file is far below the cost of losing the record of a
	// finished run. Past it, the server-side sweeper is the remaining backstop:
	// a daemon down that long stops heartbeating and its rows become sweepable.
	terminalOutboxMaxAge = 7 * 24 * time.Hour

	terminalOutboxFileMode = 0o600
	terminalOutboxDirMode  = 0o700
)

// pendingTerminalReport is the on-disk form of a terminalTaskReport. It is a
// separate type on purpose: this is a persisted format that must stay readable
// by a later daemon build, so it carries explicit JSON tags rather than
// inheriting whatever the in-memory struct's fields happen to be called.
type pendingTerminalReport struct {
	Kind                  terminalTaskReportKind `json:"kind"`
	TaskID                string                 `json:"task_id"`
	Output                string                 `json:"output,omitempty"`
	BranchName            string                 `json:"branch_name,omitempty"`
	ErrorMessage          string                 `json:"error_message,omitempty"`
	SessionID             string                 `json:"session_id,omitempty"`
	WorkDir               string                 `json:"work_dir,omitempty"`
	DurableWorkDir        string                 `json:"durable_work_dir,omitempty"`
	FailureReason         string                 `json:"failure_reason,omitempty"`
	SessionRolloutMissing bool                   `json:"session_rollout_missing,omitempty"`
	RetiredSessionID      string                 `json:"retired_session_id,omitempty"`
	QueuedAt              time.Time              `json:"queued_at"`
}

func (p pendingTerminalReport) report() terminalTaskReport {
	return terminalTaskReport{
		kind:                  p.Kind,
		taskID:                p.TaskID,
		output:                p.Output,
		branchName:            p.BranchName,
		errorMessage:          p.ErrorMessage,
		sessionID:             p.SessionID,
		workDir:               p.WorkDir,
		durableWorkDir:        p.DurableWorkDir,
		failureReason:         p.FailureReason,
		sessionRolloutMissing: p.SessionRolloutMissing,
		retiredSessionID:      p.RetiredSessionID,
	}
}

func newPendingTerminalReport(r terminalTaskReport) pendingTerminalReport {
	return pendingTerminalReport{
		Kind:                  r.kind,
		TaskID:                r.taskID,
		Output:                r.output,
		BranchName:            r.branchName,
		ErrorMessage:          r.errorMessage,
		SessionID:             r.sessionID,
		WorkDir:               r.workDir,
		DurableWorkDir:        r.durableWorkDir,
		FailureReason:         r.failureReason,
		SessionRolloutMissing: r.sessionRolloutMissing,
		RetiredSessionID:      r.retiredSessionID,
		QueuedAt:              time.Now().UTC(),
	}
}

// terminalOutbox is a small file-per-task queue. One task has exactly one
// terminal outcome, so a later report for the same task legitimately replaces
// an earlier one (the complete → fail downgrade when the server permanently
// rejects a completion) rather than queueing behind it.
type terminalOutbox struct {
	dir string
	mu  sync.Mutex
}

func newTerminalOutbox(stateDir string) *terminalOutbox {
	return &terminalOutbox{dir: filepath.Join(stateDir, terminalOutboxDirName)}
}

// path keeps the task ID out of the filesystem's hands: task IDs are UUIDs, but
// this is a value that arrives from the server, and a "../" in it would let a
// malformed ID write outside the outbox.
func (o *terminalOutbox) path(taskID string) (string, error) {
	clean := strings.TrimSpace(taskID)
	if clean == "" || strings.ContainsAny(clean, `/\`) || clean == "." || clean == ".." {
		return "", fmt.Errorf("refusing to use %q as an outbox filename", taskID)
	}
	return filepath.Join(o.dir, clean+".json"), nil
}

// persist writes the report durably enough to survive a crash: a temp file in
// the same directory, fsynced, then renamed over the target. A half-written
// terminal report that failed to parse on restart would be the same lost
// callback this exists to prevent.
func (o *terminalOutbox) persist(r terminalTaskReport) error {
	target, err := o.path(r.taskID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(newPendingTerminalReport(r))
	if err != nil {
		return fmt.Errorf("marshal pending terminal report: %w", err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if err := os.MkdirAll(o.dir, terminalOutboxDirMode); err != nil {
		return fmt.Errorf("create terminal outbox dir: %w", err)
	}
	tmp, err := os.CreateTemp(o.dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create terminal outbox temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return fmt.Errorf("write terminal outbox temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync terminal outbox temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close terminal outbox temp file: %w", err)
	}
	if err := os.Chmod(tmpName, terminalOutboxFileMode); err != nil {
		return fmt.Errorf("chmod terminal outbox temp file: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("rename terminal outbox file: %w", err)
	}
	return nil
}

// list returns the queued reports oldest-first. Unreadable entries are dropped
// rather than failing the whole drain — one corrupt file must not block every
// other pending report.
func (o *terminalOutbox) list() ([]pendingTerminalReport, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	entries, err := os.ReadDir(o.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read terminal outbox dir: %w", err)
	}

	var out []pendingTerminalReport
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		full := filepath.Join(o.dir, e.Name())
		data, readErr := os.ReadFile(full)
		if readErr != nil {
			continue
		}
		var p pendingTerminalReport
		if json.Unmarshal(data, &p) != nil || p.TaskID == "" || p.Kind == 0 {
			os.Remove(full)
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QueuedAt.Before(out[j].QueuedAt) })
	return out, nil
}

func (o *terminalOutbox) remove(taskID string) error {
	target, err := o.path(taskID)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove terminal outbox file: %w", err)
	}
	return nil
}
