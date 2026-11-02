package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The bands at their boundaries. approved and decided are counts; decided is
// approved + rejected + expired, so an expired proposal widens the denominator
// and is a non-acceptance.
func TestAttentionBand(t *testing.T) {
	cases := []struct {
		name     string
		approved int64
		decided  int64
		want     SquadCaps
		reason   string
	}{
		{"below the sample minimum keeps the defaults", 4, 4, DefaultSquadCaps(), AttentionReasonSampleTooSmall},
		{"no decisions keeps the defaults", 0, 0, DefaultSquadCaps(), AttentionReasonSampleTooSmall},
		{"zero of five is below", 0, 5, SquadCaps{Pending: 1, Weekly: 1}, AttentionReasonBelowThreshold},
		{"one of six is below (16.7 percent)", 1, 6, SquadCaps{Pending: 1, Weekly: 1}, AttentionReasonBelowThreshold},
		{"exactly 20 percent is the default band", 1, 5, DefaultSquadCaps(), AttentionReasonDefault},
		{"two of ten is exactly 20 percent", 2, 10, DefaultSquadCaps(), AttentionReasonDefault},
		{"forty percent is the default band", 2, 5, DefaultSquadCaps(), AttentionReasonDefault},
		{"exactly 60 percent is the default band", 3, 5, DefaultSquadCaps(), AttentionReasonDefault},
		{"six of ten is exactly 60 percent", 6, 10, DefaultSquadCaps(), AttentionReasonDefault},
		{"seven of ten is above 60 percent", 7, 10, SquadCaps{Pending: 4, Weekly: 5}, AttentionReasonAboveThreshold},
		{"four of five is above", 4, 5, SquadCaps{Pending: 4, Weekly: 5}, AttentionReasonAboveThreshold},
		{"all approved is above", 5, 5, SquadCaps{Pending: 4, Weekly: 5}, AttentionReasonAboveThreshold},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := attentionBand(c.approved, c.decided)
			if got != c.want || reason != c.reason {
				t.Fatalf("attentionBand(%d, %d) = %+v %s, want %+v %s", c.approved, c.decided, got, reason, c.want, c.reason)
			}
		})
	}
}

// An expired proposal is a non-acceptance: one approval and four expiries is
// 20 percent (the default band), not 100 percent.
func TestAttentionBandCountsExpiredAsNonAcceptance(t *testing.T) {
	approved, rejected, expired := int64(1), int64(0), int64(4)
	got, reason := attentionBand(approved, approved+rejected+expired)
	if got != DefaultSquadCaps() || reason != AttentionReasonDefault {
		t.Fatalf("1 approved and 4 expired = %+v %s, want the default band", got, reason)
	}
}

func TestAttentionReasonForAStoredRow(t *testing.T) {
	low := SquadCaps{Pending: 1, Weekly: 1}
	high := SquadCaps{Pending: 4, Weekly: 5}
	cases := []struct {
		name     string
		sample   int32
		computed SquadCaps
		pinned   bool
		want     string
	}{
		{"a pin is reported as a pin, whatever the sample", 40, low, true, AttentionReasonPinned},
		{"a short sample is too small even with changed caps", 4, low, false, AttentionReasonSampleTooSmall},
		{"low caps are below the threshold", 9, low, false, AttentionReasonBelowThreshold},
		{"high caps are above the threshold", 9, high, false, AttentionReasonAboveThreshold},
		{"default caps with a sample are the default band", 9, DefaultSquadCaps(), false, AttentionReasonDefault},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := attentionReasonFor(c.sample, c.computed, c.pinned); got != c.want {
				t.Fatalf("reason = %s, want %s", got, c.want)
			}
		})
	}
}

func TestEffectiveCapsOfPrefersThePin(t *testing.T) {
	// Pinned values are what the row carries when pinned is set; the computed
	// caps are used otherwise.
	pinned := effectiveCapsOf(attentionRow(true, 2, 2, 1, 1))
	if pinned != (SquadCaps{Pending: 2, Weekly: 2}) {
		t.Fatalf("pinned caps = %+v, want 2/2", pinned)
	}
	computed := effectiveCapsOf(attentionRow(false, 2, 2, 1, 1))
	if computed != (SquadCaps{Pending: 1, Weekly: 1}) {
		t.Fatalf("computed caps = %+v, want 1/1 when not pinned", computed)
	}
}

// attentionRow builds a squad_attention row for the pure tests.
func attentionRow(pinned bool, pinnedPending, pinnedWeekly, computedPending, computedWeekly int32) db.SquadAttention {
	return db.SquadAttention{
		PendingCap:       computedPending,
		WeeklyCap:        computedWeekly,
		Pinned:           pinned,
		PinnedPendingCap: pgtype.Int4{Int32: pinnedPending, Valid: true},
		PinnedWeeklyCap:  pgtype.Int4{Int32: pinnedWeekly, Valid: true},
	}
}
