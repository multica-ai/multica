package notetypes

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// Sweeper runs the daily note-types materialisation loop. One per process,
// owned by main.go via NewSweeper(...) and started with Run(ctx, interval).
type Sweeper struct {
	Pool    *pgxpool.Pool
	Cerebro *cerebrodb.Queries

	// nowFunc is overridden in tests so the sweeper can run at a known
	// instant without mocking time globally.
	nowFunc func() time.Time
}

// NewSweeper builds a Sweeper.
func NewSweeper(pool *pgxpool.Pool, cerebro *cerebrodb.Queries) *Sweeper {
	return &Sweeper{Pool: pool, Cerebro: cerebro, nowFunc: time.Now}
}

// Run blocks on ctx and ticks at the requested interval. Returns when ctx is
// cancelled.
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("cerebro note-types sweeper: initial tick failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("cerebro note-types sweeper: tick failed", "error", err)
			}
		}
	}
}

// Tick runs one pass: for every enabled, scheduled (non-manual) note type
// whose current period has not yet been materialised, create the period's
// note (new_note) or prepend it to the rolling document (running_doc).
// Workspaces with the cerebro_note_types flag OFF are skipped.
func (s *Sweeper) Tick(ctx context.Context) error {
	types, err := s.Cerebro.ListEnabledCerebroNoteTypesForSweep(ctx)
	if err != nil {
		return err
	}
	now := s.nowFunc()
	flagCache := map[[16]byte]bool{}
	for _, nt := range types {
		// Already materialised for this period — nothing to do.
		if PeriodKey(now, nt.CadenceUnit) == nt.LastPeriodKey {
			continue
		}
		enabled, err := s.isFlagEnabled(ctx, nt.WorkspaceID, flagCache)
		if err != nil {
			slog.Warn("cerebro note-types sweeper: flag lookup failed", "error", err)
			continue
		}
		if !enabled {
			continue
		}
		if err := s.runInTx(ctx, func(tx pgx.Tx) error {
			_, _, e := Apply(ctx, s.Cerebro.WithTx(tx), nt, now)
			return e
		}); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("cerebro note-types sweeper: apply failed", "note_type", nt.Name, "error", err)
		}
	}
	return nil
}

// isFlagEnabled reports whether the workspace cerebro_note_types flag is ON,
// caching per Tick so a workspace with several types pays one round-trip.
func (s *Sweeper) isFlagEnabled(ctx context.Context, workspaceID pgtype.UUID, cache map[[16]byte]bool) (bool, error) {
	if !workspaceID.Valid {
		return false, nil
	}
	if v, ok := cache[workspaceID.Bytes]; ok {
		return v, nil
	}
	enabled, err := s.Cerebro.GetCerebroNoteTypesFlagForWorkspace(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			cache[workspaceID.Bytes] = false
			return false, nil
		}
		return false, err
	}
	cache[workspaceID.Bytes] = enabled
	return enabled, nil
}

func (s *Sweeper) runInTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
