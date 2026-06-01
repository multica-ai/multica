package runtime

import (
	"encoding/binary"

	"github.com/jackc/pgx/v5/pgtype"
)

// defaultCostSavingHoldoutPct is the share of "on" runs that are deliberately
// held out (run WITHOUT the saving) so the dashboard can compare the held-out
// control group's real cost against the treatment group's. 20% is large enough
// to compare yet small enough that 80% of runs still get the saving. Overridable
// via MULTICA_SERVER_FIRTAL_GATEWAY_COST_HOLDOUT_PCT (FIR-2325 phase 5).
const defaultCostSavingHoldoutPct = 20

// chooseHoldoutPct picks the effective holdout share for a run: the
// per-workspace UI override (FIR-2640) when present, otherwise the env/default
// fallback. The result is clamped to [0,100] so a bad override can never push
// the sampling out of range. override is nil when the workspace has no row.
func chooseHoldoutPct(override *int, fallback int) int {
	pct := fallback
	if override != nil {
		pct = *override
	}
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// costSavingHeldOut decides, deterministically, whether one run is the control
// arm for one "on" saving. Deterministic on (task, saving) so a retried task
// keeps the same arm (it never flips group between attempts) and each saving is
// held out independently. The hash is uniform over [0,100), so the share held
// out converges to pct.
//
// pct <= 0 never holds out (holdout disabled); pct >= 100 always holds out.
func costSavingHeldOut(taskID pgtype.UUID, savingKey string, pct int) bool {
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	if !taskID.Valid {
		return false
	}
	return holdoutBucket(taskID, savingKey) < pct
}

// holdoutBucket maps (task, saving) to a stable bucket in [0,100). FNV-1a over
// the task bytes plus the saving key gives a uniform spread without pulling in
// crypto; this is a sampling decision, not a security boundary.
func holdoutBucket(taskID pgtype.UUID, savingKey string) int {
	const (
		offset uint64 = 1469598103934665603
		prime  uint64 = 1099511628211
	)
	h := offset
	for _, b := range taskID.Bytes {
		h = (h ^ uint64(b)) * prime
	}
	for i := 0; i < len(savingKey); i++ {
		h = (h ^ uint64(savingKey[i])) * prime
	}
	// Fold to avoid leaning on only the low byte.
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], h)
	folded := binary.LittleEndian.Uint64(buf[:])
	return int(folded % 100)
}
