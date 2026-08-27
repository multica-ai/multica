package daemon

// Durable terminal-report outbox (GAP-14, fork issue #15).
//
// CompleteTask/FailTask retry six times over ~124s and then give up. The task
// row stays `running` forever: FailStaleTasks explicitly spares tasks on
// runtimes that keep heartbeating (MUL-4107), so only a daemon restart or the
// runtime going offline recovers them — the finished work is invisible until
// then. The comment at the old daemon.go:185 insertion point already planned
// this outbox; reportTerminalTask is the single choke point it waits for.
//
// Shape: append-only JSONL journal under the Multica profile dir (survives env
// GC, scoped per profile like hermes-state). Every failed terminal send is
// appended; every healthy heartbeat drains the journal back through the same
// client calls. The server's complete/fail handlers are idempotent on already-
// terminal tasks, so replay after a partial recovery is harmless.
//
// Deliberate ceiling: entries older than pendingReportMaxAge are dropped with
// a warning during drain. By then the server has reconciled the task through
// another path (retry child, manual close) or the report is permanently
// rejected; replaying forever would churn every heartbeat.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	pendingReportsFile = "pending_reports.jsonl"

	// Reports older than this are dropped at drain time.
	pendingReportMaxAge = 24 * time.Hour
)

// terminalReportPayload mirrors terminalTaskReport for JSON. The struct keeps
// unexported fields, so the outbox owns an explicit wire shape instead of
// exporting the core type's fields.
type terminalReportPayload struct {
	TaskID                string `json:"task_id"`
	Output                string `json:"output,omitempty"`
	BranchName            string `json:"branch_name,omitempty"`
	ErrorMessage          string `json:"error_message,omitempty"`
	SessionID             string `json:"session_id,omitempty"`
	WorkDir               string `json:"work_dir,omitempty"`
	FailureReason         string `json:"failure_reason,omitempty"`
	SessionRolloutMissing bool   `json:"session_rollout_missing,omitempty"`
	RetiredSessionID      string `json:"retired_session_id,omitempty"`
}

type pendingReport struct {
	Kind     terminalTaskReportKind `json:"kind"`
	Report   terminalReportPayload  `json:"report"`
	QueuedAt time.Time              `json:"queued_at"`
}

func newPendingReport(kind terminalTaskReportKind, r terminalTaskReport, now time.Time) pendingReport {
	return pendingReport{
		Kind: kind,
		Report: terminalReportPayload{
			TaskID:                r.taskID,
			Output:                r.output,
			BranchName:            r.branchName,
			ErrorMessage:          r.errorMessage,
			SessionID:             r.sessionID,
			WorkDir:               r.workDir,
			FailureReason:         r.failureReason,
			SessionRolloutMissing: r.sessionRolloutMissing,
			RetiredSessionID:      r.retiredSessionID,
		},
		QueuedAt: now.UTC(),
	}
}

func (p pendingReport) terminalTaskReport() terminalTaskReport {
	return terminalTaskReport{
		kind:                  p.Kind,
		taskID:                p.Report.TaskID,
		output:                p.Report.Output,
		branchName:            p.Report.BranchName,
		errorMessage:          p.Report.ErrorMessage,
		sessionID:             p.Report.SessionID,
		workDir:               p.Report.WorkDir,
		failureReason:         p.Report.FailureReason,
		sessionRolloutMissing: p.Report.SessionRolloutMissing,
		retiredSessionID:      p.Report.RetiredSessionID,
	}
}

// reportOutbox persists terminal reports that could not be delivered and
// replays them once the server is reachable again. A nil path disables the
// outbox (profile dir unresolvable): sends then behave exactly as before.
type reportOutbox struct {
	path     string // empty = disabled
	logger   *slog.Logger
	mu       sync.Mutex
	draining atomic.Bool
}

func newReportOutbox(profile string, logger *slog.Logger) *reportOutbox {
	dir, err := cli.ProfileDir(profile)
	if err != nil {
		logger.Warn("terminal-report outbox disabled: cannot resolve profile dir", "error", err)
		return &reportOutbox{logger: logger}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn("terminal-report outbox disabled: cannot create profile dir", "dir", dir, "error", err)
		return &reportOutbox{logger: logger}
	}
	return &reportOutbox{path: filepath.Join(dir, pendingReportsFile), logger: logger}
}

// enqueue appends one failed terminal send to the journal. Best-effort: a
// journal write failure must never mask or delay the original report error.
func (o *reportOutbox) enqueue(kind terminalTaskReportKind, r terminalTaskReport) error {
	if o == nil || o.path == "" {
		return nil
	}
	line, err := json.Marshal(newPendingReport(kind, r, time.Now()))
	if err != nil {
		return fmt.Errorf("marshal pending report: %w", err)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	f, err := os.OpenFile(o.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open pending reports journal: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append pending report: %w", err)
	}
	return nil
}

// pending reports whether the journal holds undelivered reports. Cheap stat;
// safe to call from every heartbeat tick.
func (o *reportOutbox) pending() bool {
	if o == nil || o.path == "" {
		return false
	}
	fi, err := os.Stat(o.path)
	return err == nil && fi.Size() > 0
}

// drain replays every queued report through send. Successful and expired
// entries are removed; failures stay queued for the next drain. Single-flight:
// concurrent heartbeats collapse into one running drain.
func (o *reportOutbox) drain(send func(context.Context, terminalTaskReport) error) {
	if o == nil || o.path == "" {
		return
	}
	if !o.draining.CompareAndSwap(false, true) {
		return
	}
	defer o.draining.Store(false)

	o.mu.Lock()
	raw, err := os.ReadFile(o.path)
	o.mu.Unlock()
	if err != nil {
		if !os.IsNotExist(err) {
			o.logger.Warn("read pending reports journal failed", "error", err)
		}
		return
	}

	var kept []pendingReport
	dropped := 0
	for _, line := range bytesLines(raw) {
		var pr pendingReport
		if err := json.Unmarshal(line, &pr); err != nil {
			// Torn line (crash between write syscalls). Drop it: the entry
			// was never acknowledged by our own enqueue, and the task will
			// surface through stale-task sweeps if it was truly lost.
			o.logger.Warn("dropping corrupt pending report line", "error", err)
			dropped++
			continue
		}
		if time.Since(pr.QueuedAt) > pendingReportMaxAge {
			o.logger.Warn("dropping expired pending report",
				"task_id", pr.Report.TaskID, "age", time.Since(pr.QueuedAt).Round(time.Minute))
			dropped++
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), terminalTaskReportTimeout)
		err := send(ctx, pr.terminalTaskReport())
		cancel()
		if err != nil {
			kept = append(kept, pr)
			continue
		}
		o.logger.Info("replayed pending terminal report", "task_id", pr.Report.TaskID, "kind", pr.Kind)
	}
	if len(kept) == len(bytesLines(raw)) && dropped == 0 {
		return // nothing changed; leave the file untouched
	}
	o.rewrite(kept)
}

// rewrite replaces the journal with the still-undelivered entries, atomically.
func (o *reportOutbox) rewrite(kept []pendingReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(kept) == 0 {
		if err := os.Remove(o.path); err != nil && !os.IsNotExist(err) {
			o.logger.Warn("remove pending reports journal failed", "error", err)
		}
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(o.path), ".pending-reports-*")
	if err != nil {
		o.logger.Warn("rewrite pending reports journal failed", "error", err)
		return
	}
	tmpName := tmp.Name()
	for _, pr := range kept {
		line, err := json.Marshal(pr)
		if err != nil {
			continue // already-validated shape; unreachable in practice
		}
		if _, err := tmp.Write(append(line, '\n')); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			o.logger.Warn("rewrite pending reports journal failed", "error", err)
			return
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		o.logger.Warn("rewrite pending reports journal failed", "error", err)
		return
	}
	if err := os.Rename(tmpName, o.path); err != nil {
		os.Remove(tmpName)
		o.logger.Warn("rewrite pending reports journal failed", "error", err)
	}
}

// bytesLines splits raw JSONL content into non-empty lines.
func bytesLines(raw []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == '\n' {
			if i > start {
				lines = append(lines, raw[start:i])
			}
			start = i + 1
		}
	}
	return lines
}
