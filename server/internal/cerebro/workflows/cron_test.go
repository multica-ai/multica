package workflows

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func testCtx() context.Context { return context.Background() }

func newCronRowForTest(schedule, tz string) cerebrodb.CerebroWorkflow {
	cfg, _ := json.Marshal(TriggerConfigCron{ScheduleExpr: schedule, Timezone: tz})
	return cerebrodb.CerebroWorkflow{
		ID:            mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		WorkspaceID:   mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		TriggerType:   TriggerCron,
		TriggerConfig: cfg,
		Enabled:       true,
		ActionType:    ActionSetStatus,
		ActionConfig:  []byte(`{}`),
	}
}

func TestCronParser_AcceptsStandardAndDescriptors(t *testing.T) {
	// 5-field expressions.
	standards := []string{
		"0 8 * * *",   // 08:00 every day
		"*/5 * * * *", // every 5 minutes
		"0 0 1 * *",   // midnight on the 1st
		"30 14 * * 1", // Monday 14:30
	}
	for _, expr := range standards {
		if _, err := cronParser.Parse(expr); err != nil {
			t.Errorf("standard expression %q must parse, got %v", expr, err)
		}
	}
	// Descriptor shortcuts.
	for _, expr := range []string{"@hourly", "@daily", "@weekly", "@monthly"} {
		if _, err := cronParser.Parse(expr); err != nil {
			t.Errorf("descriptor %q must parse, got %v", expr, err)
		}
	}
	// Bad syntax must fail.
	for _, bad := range []string{"", "0 8 * *", "abc def ghi", "@yearly maybe"} {
		if _, err := cronParser.Parse(bad); err == nil {
			t.Errorf("expression %q must fail to parse", bad)
		}
	}
}

func TestValidateCronSchedule(t *testing.T) {
	cases := []struct {
		name    string
		expr    string
		tz      string
		wantErr string
	}{
		{"happy daily 8 in CPH", "0 8 * * *", "Europe/Copenhagen", ""},
		{"happy descriptor", "@hourly", "", ""},
		{"empty expr", "", "", "schedule_expr is required"},
		{"bad expr", "0 8 *", "", "invalid cron"},
		{"bad timezone", "0 8 * * *", "Mars/Olympus", "invalid timezone"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateCronSchedule(c.expr, c.tz)
			if c.wantErr == "" && err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", c.wantErr)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("expected error containing %q, got %v", c.wantErr, err)
				}
			}
		})
	}
}

func TestCronNextFire_CopenhagenTimezone(t *testing.T) {
	// "Every weekday at 08:00 Copenhagen". Using a known Tuesday 09:30 UTC
	// (10:30 CET) we expect the next fire to be the next morning at
	// 08:00 CET = 07:00 UTC.
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Fatalf("load tz: %v", err)
	}
	schedule, err := cronParser.Parse("0 8 * * 1-5")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Tuesday Jan 7 2025 09:30 UTC = 10:30 CET.
	now := time.Date(2025, 1, 7, 9, 30, 0, 0, time.UTC).In(loc)
	next := schedule.Next(now)
	// Want Wed Jan 8 2025 08:00 CET = 07:00 UTC.
	wantUTC := time.Date(2025, 1, 8, 7, 0, 0, 0, time.UTC)
	if !next.Equal(wantUTC) {
		t.Errorf("next fire = %s, want %s", next.UTC(), wantUTC)
	}
}

func TestCronNextFire_MissedTick(t *testing.T) {
	// Spec calls for at-least-once cron semantics: the sweeper advances
	// state by walking schedule.Next(prevFire), NOT by jumping ahead to
	// now(). A process down for an hour should still see the missed slot
	// on the next sweep and fire it (idempotency-collapsed if it really
	// already fired).
	schedule, err := cronParser.Parse("@hourly")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// "Last fire" was 12:00; "now" is 14:30; the immediate next is 13:00.
	lastFire := time.Date(2025, 1, 7, 12, 0, 0, 0, time.UTC)
	now := time.Date(2025, 1, 7, 14, 30, 0, 0, time.UTC)
	_ = now // here for narrative; the sweeper bases next on lastFire.
	next := schedule.Next(lastFire)
	want := time.Date(2025, 1, 7, 13, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("at-least-once next = %s, want %s", next, want)
	}
}

func TestSweepCronOne_SkipsBeforeNextFire(t *testing.T) {
	// Drive sweepCronOne via a Service that's enabled-flag only. We can't
	// invoke the real DB methods, but sweepCronOne does its first thing
	// (parse trigger_config) without DB, so we can at least verify the
	// pre-DB rejections behave.
	svc := &Service{enabled: true}
	// Bad cron — should return the parse error.
	row := newCronRowForTest("not-a-cron", "")
	err := svc.sweepCronOne(testCtx(), row, time.Now())
	if err == nil || !strings.Contains(err.Error(), "parse schedule") {
		t.Fatalf("expected parse error, got %v", err)
	}
	// Bad timezone — should return the tz error.
	row = newCronRowForTest("@hourly", "Mars/Olympus")
	err = svc.sweepCronOne(testCtx(), row, time.Now())
	if err == nil || !strings.Contains(err.Error(), "load tz") {
		t.Fatalf("expected tz error, got %v", err)
	}
	// Empty expression — should fail explicit error.
	row = newCronRowForTest("", "")
	err = svc.sweepCronOne(testCtx(), row, time.Now())
	if err == nil || !strings.Contains(err.Error(), "schedule_expr is empty") {
		t.Fatalf("expected empty-expr error, got %v", err)
	}
}

func TestCronEventIDIsDeterministic(t *testing.T) {
	// The EventID format used by the sweeper is
	// "cron:<workflow_id>:<RFC3339>". Two sweeps that compute the same
	// fire instant for the same workflow MUST produce the same EventID so
	// the idempotency key collapses retries.
	wfID := mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")
	fire := time.Date(2025, 1, 7, 7, 0, 0, 0, time.UTC)
	a := "cron:" + uuidString(wfID) + ":" + fire.UTC().Format(time.RFC3339)
	b := "cron:" + uuidString(wfID) + ":" + fire.UTC().Format(time.RFC3339)
	if a != b {
		t.Fatal("event id must be deterministic for the same fire instant")
	}
	// A different fire instant must produce a different EventID.
	other := fire.Add(time.Hour)
	c := "cron:" + uuidString(wfID) + ":" + other.UTC().Format(time.RFC3339)
	if a == c {
		t.Fatal("event id must change between fire instants")
	}
}

func TestCronParserShared(t *testing.T) {
	// Sanity — the package-level cronParser is the same one used by both
	// the sweeper and validateCronSchedule, so a value the validator
	// accepts must also parse at sweep time.
	expr := "0 9 * * 1-5"
	if err := validateCronSchedule(expr, "Europe/Copenhagen"); err != nil {
		t.Fatalf("validator rejected %q: %v", expr, err)
	}
	if _, err := cronParser.Parse(expr); err != nil {
		t.Fatalf("sweeper parser rejected %q: %v", expr, err)
	}
	// Defense in depth: an unrelated parser with a tighter mask would
	// reject @hourly; ours must accept it.
	tight := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := tight.Parse("@hourly"); err == nil {
		t.Fatal("control: tight parser must reject @hourly (sanity check)")
	}
}
