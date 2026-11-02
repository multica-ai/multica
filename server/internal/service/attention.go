package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Adaptive attention (spec 2.6). A squad's per-squad proposal caps are defaults
// that it earns or loses by how often the boss approves what it files. The
// nightly job computes them; an admin may pin other values. The workspace cap
// (ProposalMaxPendingPerWorkspace) never moves.
const (
	// AttentionMinSample is the fewest decided proposals a squad needs in the
	// window before its caps move. Below it the squad keeps the defaults.
	AttentionMinSample = 5
	// AttentionWindow is how far back decided proposals count.
	AttentionWindow = 60 * 24 * time.Hour
	// AttentionLowPercent: an acceptance strictly below it gets the low band.
	AttentionLowPercent = 20
	// AttentionHighPercent: an acceptance strictly above it gets the high band.
	// Exactly at either percentage keeps the defaults.
	AttentionHighPercent = 60
	// AttentionMaxCap bounds every cap a row stores, computed or pinned.
	AttentionMaxCap = 10
)

// Reasons the attention read reports, machine-readable.
const (
	AttentionReasonPinned         = "pinned"
	AttentionReasonSampleTooSmall = "sample_too_small"
	AttentionReasonBelowThreshold = "below_threshold"
	AttentionReasonAboveThreshold = "above_threshold"
	AttentionReasonDefault        = "default"
)

// SquadCaps are the per-squad limits on a squad's proposals.
type SquadCaps struct {
	Pending int
	Weekly  int
}

// The caps of each band. The defaults are the proposal limits themselves.
var (
	attentionDefaultCaps = SquadCaps{Pending: ProposalDefaultPendingPerSquad, Weekly: ProposalDefaultWeeklyPerSquad}
	attentionLowCaps     = SquadCaps{Pending: 1, Weekly: 1}
	attentionHighCaps    = SquadCaps{Pending: 4, Weekly: 5}
)

// DefaultSquadCaps returns the caps every squad has before its attention is
// computed or pinned.
func DefaultSquadCaps() SquadCaps { return attentionDefaultCaps }

// attentionBand returns the computed caps for a squad's decided proposals and
// the reason. Acceptance is approved over approved, rejected and expired: an
// expired proposal is a non-acceptance. The comparisons are on integers, so a
// boundary is exact: 1 of 5 is 20 percent, which is the default band.
func attentionBand(approved, decided int64) (SquadCaps, string) {
	if decided < AttentionMinSample {
		return attentionDefaultCaps, AttentionReasonSampleTooSmall
	}
	switch {
	case approved*100 < AttentionLowPercent*decided:
		return attentionLowCaps, AttentionReasonBelowThreshold
	case approved*100 > AttentionHighPercent*decided:
		return attentionHighCaps, AttentionReasonAboveThreshold
	}
	return attentionDefaultCaps, AttentionReasonDefault
}

// attentionReasonFor explains a stored row. A pin wins; otherwise the sample
// decides between "sample_too_small" and the band its computed caps show.
func attentionReasonFor(sample int32, computed SquadCaps, pinned bool) string {
	switch {
	case pinned:
		return AttentionReasonPinned
	case sample < AttentionMinSample:
		return AttentionReasonSampleTooSmall
	case computed == attentionLowCaps:
		return AttentionReasonBelowThreshold
	case computed == attentionHighCaps:
		return AttentionReasonAboveThreshold
	}
	return AttentionReasonDefault
}

// effectiveCapsOf is what filings are checked against: the pin if pinned, else
// the computed caps.
func effectiveCapsOf(row db.SquadAttention) SquadCaps {
	if row.Pinned && row.PinnedPendingCap.Valid && row.PinnedWeeklyCap.Valid {
		return SquadCaps{Pending: int(row.PinnedPendingCap.Int32), Weekly: int(row.PinnedWeeklyCap.Int32)}
	}
	return SquadCaps{Pending: int(row.PendingCap), Weekly: int(row.WeeklyCap)}
}

// EffectiveSquadCaps returns the caps a squad's filings are checked against:
// pinned caps if pinned, else computed caps, else the defaults. A squad with no
// row has the defaults.
func EffectiveSquadCaps(ctx context.Context, q *db.Queries, workspaceID, squadID pgtype.UUID) (SquadCaps, error) {
	row, err := q.GetSquadAttention(ctx, db.GetSquadAttentionParams{SquadID: squadID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return attentionDefaultCaps, nil
	}
	if err != nil {
		return SquadCaps{}, fmt.Errorf("load squad attention: %w", err)
	}
	return effectiveCapsOf(row), nil
}

// SquadAttentionView is a squad's attention as the read endpoint reports it.
type SquadAttentionView struct {
	Effective  SquadCaps
	Computed   SquadCaps
	Pinned     bool
	PinnedCaps *SquadCaps
	// Rate is approved over decided, 0 to 1; nil when nothing was decided.
	Rate       *float64
	SampleSize int32
	Reason     string
	ComputedAt *time.Time
}

// SquadAttentionFor reads a squad's attention, or the defaults when it has none.
func SquadAttentionFor(ctx context.Context, q *db.Queries, workspaceID, squadID pgtype.UUID) (SquadAttentionView, error) {
	row, err := q.GetSquadAttention(ctx, db.GetSquadAttentionParams{SquadID: squadID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SquadAttentionView{Effective: attentionDefaultCaps, Computed: attentionDefaultCaps, Reason: AttentionReasonSampleTooSmall}, nil
	}
	if err != nil {
		return SquadAttentionView{}, fmt.Errorf("load squad attention: %w", err)
	}
	computed := SquadCaps{Pending: int(row.PendingCap), Weekly: int(row.WeeklyCap)}
	view := SquadAttentionView{
		Effective:  effectiveCapsOf(row),
		Computed:   computed,
		Pinned:     row.Pinned,
		SampleSize: row.SampleSize,
		Reason:     attentionReasonFor(row.SampleSize, computed, row.Pinned),
		Rate:       numericRate(row.AcceptanceRate),
	}
	if row.Pinned && row.PinnedPendingCap.Valid && row.PinnedWeeklyCap.Valid {
		pin := SquadCaps{Pending: int(row.PinnedPendingCap.Int32), Weekly: int(row.PinnedWeeklyCap.Int32)}
		view.PinnedCaps = &pin
	}
	if row.ComputedAt.Valid {
		t := row.ComputedAt.Time
		view.ComputedAt = &t
	}
	return view, nil
}

func numericRate(n pgtype.Numeric) *float64 {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	v := f.Float64
	return &v
}

// RecomputeSquadAttention writes the computed caps of every squad with decided
// proposals in the window, and returns squads with no sample to the defaults.
// Pins are not touched. It runs in one transaction and returns how many squads
// had decisions in the window.
func RecomputeSquadAttention(ctx context.Context, txs TxStarter, q *db.Queries, now time.Time) (int, error) {
	tx, err := txs.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)

	counts, err := qtx.ListSquadDecisionCountsSince(ctx, pgtype.Timestamptz{Time: now.Add(-AttentionWindow), Valid: true})
	if err != nil {
		return 0, fmt.Errorf("count decided proposals: %w", err)
	}
	keep := make([]pgtype.UUID, 0, len(counts))
	for _, c := range counts {
		decided := c.Approved + c.Rejected + c.Expired
		caps, _ := attentionBand(c.Approved, decided)
		var rate pgtype.Numeric
		if decided > 0 {
			if err := rate.Scan(fmt.Sprintf("%.4f", float64(c.Approved)/float64(decided))); err != nil {
				return 0, fmt.Errorf("record acceptance rate: %w", err)
			}
		}
		if err := qtx.UpsertComputedSquadAttention(ctx, db.UpsertComputedSquadAttentionParams{
			SquadID:        c.SquadID,
			WorkspaceID:    c.WorkspaceID,
			PendingCap:     int32(caps.Pending),
			WeeklyCap:      int32(caps.Weekly),
			AcceptanceRate: rate,
			SampleSize:     int32(decided),
		}); err != nil {
			return 0, fmt.Errorf("write squad attention: %w", err)
		}
		keep = append(keep, c.SquadID)
	}
	if _, err := qtx.ResetComputedSquadAttentionExcept(ctx, db.ResetComputedSquadAttentionExceptParams{
		PendingCap: int32(attentionDefaultCaps.Pending),
		WeeklyCap:  int32(attentionDefaultCaps.Weekly),
		SquadIds:   keep,
	}); err != nil {
		return 0, fmt.Errorf("reset squad attention: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return len(counts), nil
}
