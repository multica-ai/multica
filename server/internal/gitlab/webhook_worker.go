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
	// decrypt is used only to construct a Resolver per-event for reverse
	// user resolution. Reverse resolution itself never calls decrypt (it
	// only reads the unencrypted identity-mapping tables), so a no-op
	// decrypter is fine when webhook handlers don't need write-token
	// resolution. Kept as a field for forward-compatibility.
	decrypt TokenDecrypter
	// taskEnqueuer is optional. When non-nil, ApplyIssueHookEvent hands
	// off to it after the upsert so a human assigning ~agent::<slug> from
	// gitlab.com spawns a task on the same path as the POST/PATCH route
	// in handler/issue.go. Wired by cmd/server/main.go.
	taskEnqueuer TaskEnqueuer
	// issueDeleter is optional. When non-nil, ApplyIssueHookEvent tears down
	// the local cache row on action="delete" events (cancels agent tasks,
	// fails autopilot runs, clears attachments) — matching the handler's
	// write-through delete path. Wired by cmd/server/main.go.
	issueDeleter IssueDeleter
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
	return &WebhookWorker{
		queries:    queries,
		tx:         tx,
		numWorkers: numWorkers,
		idleSleep:  idleSleep,
		decrypt:    noopDecrypter,
	}
}

// noopDecrypter is installed on workers that don't need write-token
// resolution. Reverse user resolution never calls decrypt; if it ever does,
// this will surface as a loud error rather than silently returning bytes
// as a string.
func noopDecrypter(_ context.Context, _ []byte) (string, error) {
	return "", errors.New("webhook worker resolver: decrypt not available")
}

// WithDecrypter installs a TokenDecrypter on the worker. Optional — the
// default no-op decrypter is sufficient for reverse user resolution (which
// only reads unencrypted identity-mapping rows).
func (w *WebhookWorker) WithDecrypter(d TokenDecrypter) *WebhookWorker {
	if d != nil {
		w.decrypt = d
	}
	return w
}

// WithTaskEnqueuer installs a TaskEnqueuer on the worker so the issue-hook
// handler can spawn agent work when a webhook event newly assigns an agent.
// Optional; when omitted, webhook-initiated assignments land in the cache
// but no task is queued.
func (w *WebhookWorker) WithTaskEnqueuer(te TaskEnqueuer) *WebhookWorker {
	if te != nil {
		w.taskEnqueuer = te
	}
	return w
}

// WithIssueDeleter installs an IssueDeleter on the worker so the issue-hook
// handler can tear down the local cache row on GitLab issue deletion.
// Optional; when omitted, delete events are acknowledged but the cache row
// is left in place — preserving pre-wiring behavior.
func (w *WebhookWorker) WithIssueDeleter(d IssueDeleter) *WebhookWorker {
	if d != nil {
		w.issueDeleter = d
	}
	return w
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

	// Build a per-event Resolver bound to the transactional queries so
	// reverse-resolution sees rows written by siblings in the same tx. The
	// resolver never calls decrypt during reverse-lookup (only identity
	// mapping tables are read), so the no-op decrypter is safe here.
	resolver := NewResolver(q, w.decrypt)

	deps := WebhookDeps{
		Queries:      q,
		WorkspaceID:  row.WorkspaceID,
		ProjectID:    conn.GitlabProjectID,
		Resolver:     resolver,
		TaskEnqueuer: w.taskEnqueuer,
		IssueDeleter: w.issueDeleter,
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
