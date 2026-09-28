package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestQuotaHoldTransitionPreservesKnownResetAndDeduplicates(t *testing.T) {
	now := time.Date(2026, 9, 27, 20, 0, 0, 0, time.UTC)
	reset := now.Add(7 * 24 * time.Hour)
	firstTask, laterTask := testUUID(1), testUUID(2)
	pool := db.ProviderQuotaPool{State: "open", Revision: 3}
	arg, event, changed := quotaHoldTransition(pool, firstTask, taskfailure.QuotaResetHint{
		Kind: taskfailure.QuotaResetExact, At: reset,
	}, now)
	if !changed || event != "held" || arg.State != "held_exact" || !arg.ResetAt.Time.Equal(reset) {
		t.Fatalf("initial hold = %+v %q %v", arg, event, changed)
	}
	pool.State = arg.State
	pool.ResetAt = arg.ResetAt
	pool.SourceTaskID = firstTask
	pool.Revision++
	if _, _, changed := quotaHoldTransition(pool, firstTask, taskfailure.QuotaResetHint{Kind: taskfailure.QuotaResetExact, At: reset}, now); changed {
		t.Fatal("duplicate task created a second hold")
	}
	if _, _, changed := quotaHoldTransition(pool, laterTask, taskfailure.QuotaResetHint{Kind: taskfailure.QuotaResetUnknown}, now); changed {
		t.Fatal("unknown reset replaced a known reset")
	}
	if _, _, changed := quotaHoldTransition(pool, laterTask, taskfailure.QuotaResetHint{Kind: taskfailure.QuotaResetExact, At: reset.Add(-time.Hour)}, now); changed {
		t.Fatal("earlier reset shortened an existing hold")
	}
	if _, _, changed := quotaHoldTransition(pool, laterTask, taskfailure.QuotaResetHint{
		Kind: taskfailure.QuotaResetDateOnly, Date: "2026-09-30", Timezone: "UTC",
	}, now); changed {
		t.Fatal("date-only reset shortened an exact hold")
	}
	laterReset := reset.Add(time.Hour)
	arg, event, changed = quotaHoldTransition(pool, laterTask, taskfailure.QuotaResetHint{Kind: taskfailure.QuotaResetExact, At: laterReset}, now)
	if !changed || event != "extended" || !arg.ResetAt.Time.Equal(laterReset) || arg.ExpectedRevision != pool.Revision {
		t.Fatalf("extended hold = %+v %q %v", arg, event, changed)
	}
	// A new failure after the old reset is evidence that the old deadline no
	// longer applies. Do not keep an expired deadline when the provider gives
	// no new one.
	arg, event, changed = quotaHoldTransition(pool, laterTask, taskfailure.QuotaResetHint{Kind: taskfailure.QuotaResetUnknown}, reset.Add(time.Minute))
	if !changed || event != "extended" || arg.State != "reset_unknown" || arg.ResetAt.Valid {
		t.Fatalf("post-reset unknown hold = %+v %q %v", arg, event, changed)
	}
}

func TestQuotaHoldTransitionKeepsDateOnlyDistinct(t *testing.T) {
	now := time.Date(2026, 9, 27, 20, 0, 0, 0, time.UTC)
	pool := db.ProviderQuotaPool{State: "open", Revision: 1}
	arg, event, changed := quotaHoldTransition(pool, pgtype.UUID{Valid: true}, taskfailure.QuotaResetHint{
		Kind: taskfailure.QuotaResetDateOnly, Date: "2026-10-04", Timezone: "America/Los_Angeles",
	}, now)
	if !changed || event != "held" || arg.State != "held_date" || arg.ResetAt.Valid || !arg.ResetDate.Valid || arg.ResetDate.Time.Format("2006-01-02") != "2026-10-04" {
		t.Fatalf("date-only hold = %+v %q %v", arg, event, changed)
	}
}

func TestQuotaPoolResetDueUsesProviderCalendarDate(t *testing.T) {
	date, err := time.Parse("2006-01-02", "2026-10-04")
	if err != nil {
		t.Fatal(err)
	}
	pool := db.ProviderQuotaPool{
		State: "held_date", Timezone: "Asia/Tokyo",
		ResetDate: pgtype.Date{Time: date, Valid: true},
	}
	if quotaPoolResetDue(pool, time.Date(2026, 10, 3, 14, 59, 0, 0, time.UTC)) {
		t.Fatal("date-only reset became due before local midnight")
	}
	if !quotaPoolResetDue(pool, time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC)) {
		t.Fatal("date-only reset did not become probe due at local midnight")
	}
}

func TestQuotaProbeFailureBacksOffThenStops(t *testing.T) {
	now := time.Date(2026, 9, 27, 20, 0, 0, 0, time.UTC)
	pool := db.ProviderQuotaPool{State: "probing", ProbeAttempts: 1}
	first := quotaProbeFailureState(pool, now)
	if first.State != "probe_backoff" || !first.ResetAt.Time.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("first probe failure = %+v", first)
	}
	pool.ProbeAttempts = 3
	last := quotaProbeFailureState(pool, now)
	if last.State != "reset_unknown" || last.ResetAt.Valid {
		t.Fatalf("third probe failure = %+v", last)
	}
}

func TestQuotaProbeSameDayDateFailureBacksOff(t *testing.T) {
	now := time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)
	pool := db.ProviderQuotaPool{State: "probing", Timezone: "America/Los_Angeles", ProbeAttempts: 1, Revision: 2}
	hint := taskfailure.QuotaResetHint{Kind: taskfailure.QuotaResetDateOnly, Date: "2026-09-28", Timezone: pool.Timezone}
	arg, event, changed := quotaHoldTransition(pool, testUUID(3), hint, now)
	if !changed || event != "probe_failed" || arg.State != "probe_backoff" ||
		arg.ResetDate.Valid || arg.ProbeAttempts != 1 || !arg.ResetAt.Time.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("same-day quota probe = %+v %q %v", arg, event, changed)
	}
	pool.ProbeAttempts = 3
	arg, _, changed = quotaHoldTransition(pool, testUUID(4), hint, now)
	if !changed || arg.State != "reset_unknown" || arg.ResetAt.Valid || arg.ResetDate.Valid {
		t.Fatalf("third same-day quota probe = %+v", arg)
	}
}
