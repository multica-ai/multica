package semantic

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Worker drains cerebro_search_embedding_queue, calls the configured
// provider, and upserts the embedding. It is a small ticker; concurrency
// across pods is handled by FOR UPDATE SKIP LOCKED inside FetchPending.
type Worker struct {
	pool     *pgxpool.Pool
	provider Provider
	cfg      Config
}

// NewWorker constructs a Worker. Both pool and provider must be non-nil; use
// StartWorker for the env-gated factory.
func NewWorker(pool *pgxpool.Pool, provider Provider, cfg Config) *Worker {
	return &Worker{pool: pool, provider: provider, cfg: cfg}
}

// StartWorker is the boot-time entry point called from server/cmd/server.
// It loads config, builds the provider, and starts the goroutine. A
// disabled provider returns silently — no goroutine is started.
func StartWorker(ctx context.Context, pool *pgxpool.Pool, cfg Config) {
	if !cfg.Enabled {
		slog.Info("semantic search worker disabled (no CEREBRO_SEARCH_SEMANTIC_PROVIDER)")
		return
	}
	provider := cfg.BuildProvider()
	if IsDisabled(provider) {
		return
	}
	slog.Info("semantic search worker started",
		"provider", cfg.Provider,
		"model", provider.Model(),
		"interval", cfg.WorkerInterval.String(),
		"batch", cfg.BatchSize,
	)
	w := NewWorker(pool, provider, cfg)
	go w.Run(ctx)
}

// Run loops until ctx is cancelled, draining the queue every WorkerInterval.
// One tick processes up to BatchSize entries before sleeping again.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.WorkerInterval)
	defer ticker.Stop()
	// One run on startup so a cold deploy starts catching up immediately
	// instead of waiting a full interval.
	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// Tick is exported for tests that want to drive the worker deterministically
// (one batch at a time) instead of relying on the timer.
func (w *Worker) Tick(ctx context.Context) (processed int, err error) {
	return w.runOnce(ctx)
}

func (w *Worker) tick(ctx context.Context) {
	processed, err := w.runOnce(ctx)
	if err != nil {
		if errors.Is(err, ErrStoreUnavailable) {
			slog.Debug("semantic worker: pgvector unavailable, skipping tick")
			return
		}
		slog.Warn("semantic worker: tick failed", "error", err)
		return
	}
	if processed > 0 {
		slog.Debug("semantic worker: tick", "processed", processed)
	}
}

// runOnce drains one batch in a single transaction. Returning the count
// keeps tests honest about how much work happened.
func (w *Worker) runOnce(ctx context.Context) (int, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	items, err := FetchPending(ctx, tx, w.cfg.BatchSize)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}

	var processed int
	for _, item := range items {
		if err := w.embedAndStore(ctx, tx, item); err != nil {
			MarkFailed(ctx, tx, item.ID, truncErr(err))
			slog.Warn("semantic worker: item failed",
				"target_type", item.TargetType,
				"target_id", item.TargetID.String(),
				"error", err,
			)
			if errors.Is(err, ErrStoreUnavailable) {
				// pgvector is missing — stop trying this tick. Commit the
				// MarkFailed bumps so attempts accumulate visibly.
				if cerr := tx.Commit(ctx); cerr != nil {
					return processed, cerr
				}
				return processed, ErrStoreUnavailable
			}
			continue
		}
		processed++
	}
	if err := tx.Commit(ctx); err != nil {
		return processed, err
	}
	return processed, nil
}

func (w *Worker) embedAndStore(ctx context.Context, tx pgx.Tx, item QueueItem) error {
	content := strings.TrimSpace(item.Content)
	if content == "" {
		// Source row was deleted between the trigger firing and our claim.
		// Drop the queue row and move on — no embedding to compute.
		if _, err := tx.Exec(ctx, `DELETE FROM cerebro_search_embedding_queue WHERE id = $1`, item.ID); err != nil {
			return err
		}
		return nil
	}
	hash := ContentHash(content)
	// Cheap reuse: if the existing embedding's hash matches we can drop the
	// queue row without spending another model call.
	if reusable, err := alreadyEmbedded(ctx, tx, item, hash); err == nil && reusable {
		_, _ = tx.Exec(ctx, `DELETE FROM cerebro_search_embedding_queue WHERE id = $1`, item.ID)
		return nil
	}
	vec, err := w.provider.Embed(ctx, content)
	if err != nil {
		return err
	}
	if len(vec) != EmbeddingDim {
		return errors.New("provider returned wrong-dimension vector")
	}
	return Upsert(ctx, tx, item, vec, w.provider.Model(), hash)
}

func alreadyEmbedded(ctx context.Context, tx pgx.Tx, item QueueItem, hash string) (bool, error) {
	if !storeAvailable(ctx, tx) {
		return false, nil
	}
	var existing string
	err := tx.QueryRow(ctx,
		`SELECT content_hash FROM cerebro_search_embedding
		 WHERE target_type = $1 AND target_id = $2`,
		item.TargetType, item.TargetID,
	).Scan(&existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return existing == hash, nil
}

func truncErr(err error) string {
	s := err.Error()
	if len(s) > 500 {
		return s[:500] + "…"
	}
	return s
}
