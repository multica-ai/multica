package gitlab

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// txStarter mirrors the existing handler.txStarter — duplicated here to
// avoid a cross-package import.
type txStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// WebhookWorker drains gitlab_webhook_event into the cache. Construct via
// NewWebhookWorker; start with Run(ctx) which blocks until ctx is cancelled.
type WebhookWorker struct {
	queries    *db.Queries
	tx         txStarter
	numWorkers int
	idleSleep  time.Duration
}

// NewWebhookWorker returns a worker that runs `numWorkers` goroutines and
// sleeps `idleSleep` between empty-queue checks.
func NewWebhookWorker(queries *db.Queries, tx txStarter, numWorkers int, idleSleep time.Duration) *WebhookWorker {
	if numWorkers <= 0 {
		numWorkers = 5
	}
	if idleSleep <= 0 {
		idleSleep = 250 * time.Millisecond
	}
	return &WebhookWorker{queries: queries, tx: tx, numWorkers: numWorkers, idleSleep: idleSleep}
}

// Run starts the worker pool and blocks until ctx is cancelled.
func (w *WebhookWorker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < w.numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w.loop(ctx, id)
		}(i)
	}
	wg.Wait()
}

func (w *WebhookWorker) loop(ctx context.Context, id int) {
	for {
		if ctx.Err() != nil {
			return
		}
		processed, err := w.processOne(ctx)
		if err != nil {
			slog.Error("webhook worker", "id", id, "error", err)
		}
		if !processed {
			// Empty queue — sleep before retrying.
			select {
			case <-time.After(w.idleSleep):
			case <-ctx.Done():
				return
			}
		}
	}
}

// processOne claims one unprocessed event, applies it, marks processed.
// Returns (true, nil) when an event was processed, (false, nil) when the
// queue was empty.
func (w *WebhookWorker) processOne(ctx context.Context) (bool, error) {
	tx, err := w.tx.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	q := w.queries.WithTx(tx)

	row, err := q.ClaimNextWebhookEvent(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	// Look up the workspace's project_id so per-event handlers can build
	// gitlabapi.Issue/etc.
	conn, err := q.GetWorkspaceGitlabConnection(ctx, row.WorkspaceID)
	if err != nil {
		// Connection was disconnected between webhook receipt and worker
		// processing — skip + mark processed so we don't loop.
		slog.Warn("webhook event for unconnected workspace; dropping",
			"workspace_id", row.WorkspaceID, "event_type", row.EventType)
		if err := q.MarkWebhookEventProcessed(ctx, row.ID); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}

	deps := WebhookDeps{
		Queries:     q,
		WorkspaceID: row.WorkspaceID,
		ProjectID:   conn.GitlabProjectID,
	}

	if err := dispatchWebhookEvent(ctx, deps, row.EventType, row.Payload); err != nil {
		// Record the failure in the same transaction so the UPDATE runs while
		// we already hold the FOR UPDATE lock on the row. Committing the tx
		// persists the failure_count increment without marking processed —
		// the row stays claimable once the backoff window elapses.
		if recErr := q.RecordWebhookEventFailure(ctx, db.RecordWebhookEventFailureParams{
			ID:        row.ID,
			LastError: pgtype.Text{String: err.Error(), Valid: true},
		}); recErr != nil {
			slog.Error("webhook event apply failed (and failure-record write failed)",
				"workspace_id", row.WorkspaceID, "event_type", row.EventType,
				"object_id", row.ObjectID, "apply_error", err, "rec_error", recErr)
			return false, nil
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			slog.Error("webhook event apply failed (failure-record commit failed)",
				"workspace_id", row.WorkspaceID, "event_type", row.EventType,
				"object_id", row.ObjectID, "apply_error", err, "commit_error", commitErr)
			return false, nil
		}

		nextRetry := time.Duration(row.FailureCount+1) * 5 * time.Second
		logFn := slog.Error
		if row.FailureCount >= 3 && row.FailureCount < 9 {
			// Reduce log noise for events that keep failing.
			logFn = slog.Warn
		}
		logFn("webhook event apply failed",
			"workspace_id", row.WorkspaceID,
			"event_type", row.EventType,
			"object_id", row.ObjectID,
			"failure_count", row.FailureCount+1,
			"next_retry_in", nextRetry.String(),
			"error", err)
		return false, nil
	}

	if err := q.MarkWebhookEventProcessed(ctx, row.ID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// dispatchWebhookEvent routes by event_type to the right per-event handler.
func dispatchWebhookEvent(ctx context.Context, deps WebhookDeps, eventType string, body []byte) error {
	switch eventType {
	case "issue":
		return ApplyIssueHookEvent(ctx, deps, body)
	case "note":
		return ApplyNoteHookEvent(ctx, deps, body)
	case "emoji":
		return ApplyEmojiHookEvent(ctx, deps, body)
	case "label":
		return ApplyLabelHookEvent(ctx, deps, body)
	default:
		// Shouldn't happen — receiver validates event_type before insert.
		// Mark processed by returning nil to stop the retry loop.
		slog.Warn("unknown event_type in queue; ignoring", "event_type", eventType)
		return nil
	}
}
