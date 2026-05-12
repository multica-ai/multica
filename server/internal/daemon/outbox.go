package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	defaultOutboxMinBackoff = 1 * time.Second
	defaultOutboxMaxBackoff = 60 * time.Second
	defaultOutboxMaxRetries = 10
	outboxDirName           = "outbox"
	outboxDeadDirName       = "outbox_dead"
	outboxFileExt           = ".json"
	outboxOutputMaxLen      = 4096
	outboxErrorMsgMaxLen    = 512
)

// OutboxEntry represents a single durable delivery entry for task completion
// or failure reporting to the server. The entry is persisted to disk as JSON
// and retried with exponential backoff until delivery succeeds or max retries
// are exhausted.
type OutboxEntry struct {
	ID             string    `json:"id"`
	TaskID         string    `json:"task_id"`
	ResultType     string    `json:"result_type"` // "complete" or "fail"
	IdempotencyKey string    `json:"idempotency_key"`
	Output         string    `json:"output,omitempty"`
	ErrorMsg       string    `json:"error_msg,omitempty"`
	BranchName     string    `json:"branch_name,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	WorkDir        string    `json:"work_dir,omitempty"`
	FailureReason  string    `json:"failure_reason,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	LastAttemptAt  time.Time `json:"last_attempt_at"`
	NextAttemptAt  time.Time `json:"next_attempt_at"`
	Attempts       int       `json:"attempts"`
	MaxAttempts    int       `json:"max_attempts"`
	DeliveryError  string    `json:"delivery_error,omitempty"`
}

// Outbox manages durable delivery of task results to the server.
// Entries are persisted as individual JSON files in a directory under
// the daemon's config directory (~/.multica/outbox/).
//
// The outbox attempts immediate (synchronous) delivery on Enqueue,
// and retries failed deliveries in a background goroutine with
// exponential backoff.
type Outbox struct {
	dir        string
	deadDir    string
	client     *Client
	logger     *slog.Logger
	minBackoff time.Duration
	maxBackoff time.Duration
	maxRetries int
	replayPoll time.Duration

	mu       sync.Mutex
	inflight map[string]struct{}
	wakeup   chan struct{}
}

// NewOutbox creates a new Outbox instance. The outbox directory is resolved
// from the daemon's profile directory.
func NewOutbox(client *Client, logger *slog.Logger, profile string) (*Outbox, error) {
	dir, err := cli.ProfileDir(profile)
	if err != nil {
		return nil, fmt.Errorf("resolve outbox dir: %w", err)
	}
	outboxDir := filepath.Join(dir, outboxDirName)
	if err := os.MkdirAll(outboxDir, 0o700); err != nil {
		return nil, fmt.Errorf("create outbox dir: %w", err)
	}
	deadDir := filepath.Join(dir, outboxDeadDirName)
	if err := os.MkdirAll(deadDir, 0o700); err != nil {
		return nil, fmt.Errorf("create dead-letter dir: %w", err)
	}
	return &Outbox{
		dir:        outboxDir,
		deadDir:    deadDir,
		client:     client,
		logger:     logger,
		minBackoff: defaultOutboxMinBackoff,
		maxBackoff: defaultOutboxMaxBackoff,
		maxRetries: defaultOutboxMaxRetries,
		replayPoll: 5 * time.Second,
		inflight:   make(map[string]struct{}),
		wakeup:     make(chan struct{}, 1),
	}, nil
}

// EnqueueComplete persists a task completion for durable delivery.
// The delivery is attempted immediately (synchronously); on failure the
// entry is persisted and will be retried by the background replay loop.
//
// The daemon must NOT fall back to FailTask when CompleteTask fails —
// the outbox handles retry independently, and the server's orphan sweeper
// is the fallback path.
func (o *Outbox) EnqueueComplete(ctx context.Context, taskID, output, branchName, sessionID, workDir string) error {
	entry := &OutboxEntry{
		ID:             newOutboxEntryID(),
		TaskID:         taskID,
		ResultType:     "complete",
		IdempotencyKey: uuid.New().String(),
		Output:         truncateString(output, outboxOutputMaxLen),
		BranchName:     branchName,
		SessionID:      sessionID,
		WorkDir:        workDir,
		CreatedAt:      time.Now(),
		MaxAttempts:    o.maxRetries + 1, // +1 for the initial attempt
	}
	return o.enqueueAndDeliver(ctx, entry)
}

// EnqueueFail persists a task failure for durable delivery.
func (o *Outbox) EnqueueFail(ctx context.Context, taskID, errMsg, sessionID, workDir, failureReason string) error {
	entry := &OutboxEntry{
		ID:             newOutboxEntryID(),
		TaskID:         taskID,
		ResultType:     "fail",
		IdempotencyKey: uuid.New().String(),
		ErrorMsg:       truncateString(errMsg, outboxErrorMsgMaxLen),
		SessionID:      sessionID,
		WorkDir:        workDir,
		FailureReason:  failureReason,
		CreatedAt:      time.Now(),
		MaxAttempts:    o.maxRetries + 1,
	}
	return o.enqueueAndDeliver(ctx, entry)
}

// enqueueAndDeliver persists the entry and attempts immediate delivery.
// On success the entry is removed; on failure it stays on disk for retry.
func (o *Outbox) enqueueAndDeliver(ctx context.Context, entry *OutboxEntry) error {
	if err := o.save(entry); err != nil {
		o.logger.Error("outbox save failed", "entry_id", entry.ID, "task_id", entry.TaskID, "error", err)
		return err
	}

	if err := o.deliverSafe(ctx, entry); err != nil {
		o.logger.Warn("outbox delivery failed, will retry",
			"entry_id", entry.ID,
			"task_id", entry.TaskID,
			"result_type", entry.ResultType,
			"attempt", entry.Attempts,
			"error", err,
		)
		o.nudge()
		return err
	}

	if err := o.remove(entry); err != nil {
		o.logger.Warn("outbox remove after success failed (non-fatal)", "entry_id", entry.ID, "error", err)
	}
	return nil
}

// deliver attempts to send a single outbox entry to the server.
// Returns an error if delivery failed. 4xx errors (permanent) are NOT
// retried — the entry is logged and removed.
func (o *Outbox) deliver(ctx context.Context, entry *OutboxEntry) error {
	entry.Attempts++
	entry.LastAttemptAt = time.Now()

	var err error
	switch entry.ResultType {
	case "complete":
		err = o.client.CompleteTask(ctx, entry.TaskID, entry.Output, entry.BranchName, entry.SessionID, entry.WorkDir)
	case "fail":
		err = o.client.FailTask(ctx, entry.TaskID, entry.ErrorMsg, entry.SessionID, entry.WorkDir, entry.FailureReason)
	default:
		return fmt.Errorf("unknown outbox result type: %s", entry.ResultType)
	}

	if err != nil {
		entry.DeliveryError = truncateString(err.Error(), 256)
		if isTaskNotFoundError(err) {
			o.logger.Info("outbox: task not found server-side, dropping entry",
				"entry_id", entry.ID,
				"task_id", entry.TaskID,
				"result_type", entry.ResultType,
			)
			if rmErr := o.remove(entry); rmErr != nil {
				o.logger.Warn("outbox remove for deleted task failed", "entry_id", entry.ID, "error", rmErr)
			}
			return err
		}
		entry.NextAttemptAt = time.Now().Add(o.backoff(entry.Attempts))
		if saveErr := o.save(entry); saveErr != nil {
			o.logger.Warn("outbox save after retry failed", "entry_id", entry.ID, "error", saveErr)
		}
		return err
	}
	return nil
}

// deliverSafe wraps deliver with inflight tracking to prevent concurrent
// delivery of the same entry.
func (o *Outbox) deliverSafe(ctx context.Context, entry *OutboxEntry) error {
	o.mu.Lock()
	if _, ok := o.inflight[entry.ID]; ok {
		o.mu.Unlock()
		return nil
	}
	o.inflight[entry.ID] = struct{}{}
	o.mu.Unlock()

	defer func() {
		o.mu.Lock()
		delete(o.inflight, entry.ID)
		o.mu.Unlock()
	}()

	return o.deliver(ctx, entry)
}

// backoff returns the duration to wait before the next retry attempt.
// Uses exponential backoff capped at maxBackoff.
// Attempt 1 (first retry) -> minBackoff, attempt 2 -> 2*minBackoff, etc.
func (o *Outbox) backoff(attempt int) time.Duration {
	if attempt < 1 {
		return 0
	}
	d := o.minBackoff * (1 << (attempt - 1))
	if d > o.maxBackoff {
		d = o.maxBackoff
	}
	return d
}

// Run starts the background replay loop. It scans for pending outbox entries
// on startup and retries them, then polls periodically for new entries.
// Run blocks until ctx is cancelled.
func (o *Outbox) Run(ctx context.Context) {
	o.replayPending(ctx)

	ticker := time.NewTicker(o.replayPoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.wakeup:
			o.replayPending(ctx)
		case <-ticker.C:
			o.replayPending(ctx)
		}
	}
}

// replayPending loads all outbox entries from disk and attempts delivery
// for any that are due. Expired entries (max retries exhausted) are moved
// to a dead-letter directory for operator inspection. If any entry is still
// pending (not yet due), schedules a wake-up at the earliest due time so
// retries are prompt.
func (o *Outbox) replayPending(ctx context.Context) {
	entries, err := o.loadAll()
	if err != nil {
		o.logger.Warn("outbox replay: failed to load entries", "error", err)
		return
	}

	now := time.Now()
	var earliestDue time.Time
	for _, entry := range entries {
		if entry.Attempts >= entry.MaxAttempts {
			o.logger.Error("outbox entry exhausted max retries, moving to dead-letter",
				"entry_id", entry.ID,
				"task_id", entry.TaskID,
				"result_type", entry.ResultType,
				"attempts", entry.Attempts,
				"max_attempts", entry.MaxAttempts,
				"last_error", entry.DeliveryError,
			)
			if err := o.moveToDead(entry); err != nil {
				o.logger.Warn("outbox move to dead-letter failed", "entry_id", entry.ID, "error", err)
			}
			continue
		}

		if entry.NextAttemptAt.After(now) {
			if earliestDue.IsZero() || entry.NextAttemptAt.Before(earliestDue) {
				earliestDue = entry.NextAttemptAt
			}
			continue
		}

		if err := o.deliverSafe(ctx, entry); err != nil {
			if entry.NextAttemptAt.After(now) && (earliestDue.IsZero() || entry.NextAttemptAt.Before(earliestDue)) {
				earliestDue = entry.NextAttemptAt
			}
			continue
		}

		o.logger.Info("outbox delivery succeeded on replay",
			"entry_id", entry.ID,
			"task_id", entry.TaskID,
			"result_type", entry.ResultType,
			"attempts", entry.Attempts,
		)
		if err := o.remove(entry); err != nil {
			o.logger.Warn("outbox remove after replay success failed", "entry_id", entry.ID, "error", err)
		}
	}

	if !earliestDue.IsZero() {
		delay := time.Until(earliestDue)
		if delay < 0 {
			delay = 0
		}
		time.AfterFunc(delay, func() {
			o.nudge()
		})
	}
}

// nudge wakes the replay loop without blocking.
func (o *Outbox) nudge() {
	select {
	case o.wakeup <- struct{}{}:
	default:
	}
}

// ---- persistence helpers ----

func (o *Outbox) path(entryID string) string {
	return filepath.Join(o.dir, entryID+outboxFileExt)
}

func (o *Outbox) save(entry *OutboxEntry) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal outbox entry: %w", err)
	}
	path := o.path(entry.ID)
	tmp, err := os.CreateTemp(o.dir, ".outbox-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp outbox file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write outbox entry: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync outbox entry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close outbox entry: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename outbox entry: %w", err)
	}
	return nil
}

func (o *Outbox) remove(entry *OutboxEntry) error {
	return os.Remove(o.path(entry.ID))
}

func (o *Outbox) loadAll() ([]*OutboxEntry, error) {
	entries, err := os.ReadDir(o.dir)
	if err != nil {
		return nil, fmt.Errorf("read outbox dir: %w", err)
	}
	var result []*OutboxEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), outboxFileExt) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(o.dir, e.Name()))
		if err != nil {
			o.logger.Warn("outbox: failed to read entry file", "file", e.Name(), "error", err)
			continue
		}
		var entry OutboxEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			o.logger.Warn("outbox: failed to parse entry file", "file", e.Name(), "error", err)
			continue
		}
		result = append(result, &entry)
	}
	return result, nil
}

func newOutboxEntryID() string {
	return uuid.New().String()
}

func (o *Outbox) moveToDead(entry *OutboxEntry) error {
	deadPath := filepath.Join(o.deadDir, entry.ID+outboxFileExt)
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dead-letter entry: %w", err)
	}
	tmp, err := os.CreateTemp(o.deadDir, ".dead-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp dead-letter file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write dead-letter entry: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync dead-letter entry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close dead-letter entry: %w", err)
	}
	if err := os.Rename(tmpPath, deadPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename dead-letter entry: %w", err)
	}
	return os.Remove(o.path(entry.ID))
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
