package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestBriefCatchupSettingsReadDefaultsAndValues(t *testing.T) {
	cases := []struct {
		name     string
		settings string
		enabled  bool
		hour     int
	}{
		{"no settings", "", true, 10},
		{"empty object", "{}", true, 10},
		{"null", "null", true, 10},
		{"off", `{"brief_catchup_enabled": false}`, false, 10},
		{"hour", `{"brief_catchup_hour": 7}`, true, 7},
		{"midnight", `{"brief_catchup_hour": 0}`, true, 0},
		{"hour past the day", `{"brief_catchup_hour": 24}`, true, 10},
		{"fractional hour", `{"brief_catchup_hour": 8.5}`, true, 10},
		{"enabled not a boolean", `{"brief_catchup_enabled": "no"}`, true, 10},
		{"unrelated keys only", `{"always_redact_env": true}`, true, 10},
		{"unreadable", `{not json`, true, 10},
	}
	for _, c := range cases {
		enabled, hour := BriefCatchupSettings([]byte(c.settings))
		if enabled != c.enabled || hour != c.hour {
			t.Errorf("%s: BriefCatchupSettings(%q) = %v, %d; want %v, %d", c.name, c.settings, enabled, hour, c.enabled, c.hour)
		}
	}
}

// The local day is the schedule's timezone. 03:30 UTC on 10 January is 10:30 in
// Ho Chi Minh City, where the day began at 17:00 UTC on 9 January.
func TestBriefCatchupLocalDayFollowsTheSchedulesTimezone(t *testing.T) {
	now := time.Date(2026, 1, 10, 3, 30, 0, 0, time.UTC)

	day := briefCatchupLocalDay(now, briefCatchupTZ)
	if day.hour != 10 {
		t.Fatalf("local hour = %d, want 10", day.hour)
	}
	if want := time.Date(2026, 1, 9, 17, 0, 0, 0, time.UTC); !day.start.Equal(want) {
		t.Fatalf("day start = %s, want %s", day.start, want)
	}
	if want := time.Date(2026, 1, 10, 17, 0, 0, 0, time.UTC); !day.end.Equal(want) {
		t.Fatalf("day end = %s, want %s", day.end, want)
	}

	// No timezone, or one that does not load, is UTC.
	for _, tz := range []string{"", "Mars/Olympus"} {
		utc := briefCatchupLocalDay(now, tz)
		if utc.hour != 3 || !utc.start.Equal(time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("timezone %q: hour %d, start %s; want UTC", tz, utc.hour, utc.start)
		}
	}
}

func TestBriefCatchupSourceIsAnAutopilotRunSource(t *testing.T) {
	if BriefCatchupSource != "catchup" {
		t.Fatalf("source = %q, want catchup", BriefCatchupSource)
	}
	if !validAutopilotExecutionSource(BriefCatchupSource) {
		t.Fatalf("the autopilot service refuses its own catch-up source")
	}
}

// --- database-backed passes ---

const briefCatchupTZ = "Asia/Ho_Chi_Minh"

var briefCatchupSeq atomic.Int64

// fakeCatchupDispatcher stands in for the autopilot service. It records each
// dispatch, and for this workspace writes the run row the way the real dispatch
// does, so the next pass sees the run.
type fakeCatchupDispatcher struct {
	t       *testing.T
	fx      *testutil.Fixture
	ws      string
	now     func() time.Time
	calls   []string
	sources []string
}

func (d *fakeCatchupDispatcher) DispatchAutopilot(_ context.Context, ap db.Autopilot, triggerID pgtype.UUID, source string, _ []byte) (*db.AutopilotRun, error) {
	d.calls = append(d.calls, util.UUIDToString(ap.ID))
	d.sources = append(d.sources, source)
	if util.UUIDToString(ap.WorkspaceID) == d.ws {
		d.fx.Insert(d.t, "autopilot_run", testutil.Cols{
			"autopilot_id": util.UUIDToString(ap.ID),
			"trigger_id":   util.UUIDToString(triggerID),
			"source":       source,
			"status":       "running",
			// The test clock, so the run falls in the local day the pass is judging.
			"triggered_at": d.now(),
		})
	}
	return &db.AutopilotRun{Source: source, Status: "running"}, nil
}

// briefCatchupFixture is a workspace whose Secretary runs a daily brief that
// fires at 08:00 Ho Chi Minh City time, and whose runtime is online.
type briefCatchupFixture struct {
	t         *testing.T
	fx        *testutil.Fixture
	ws        string
	runtime   string
	secretary string
	autopilot string
	trigger   string
	disp      *fakeCatchupDispatcher
	svc       *BriefCatchupService
	clock     time.Time
}

func newBriefCatchupFixture(t *testing.T) *briefCatchupFixture {
	t.Helper()
	pool := sharedTestPool(t)
	n := briefCatchupSeq.Add(1)
	fx := testutil.New(pool, "", "")
	owner := fx.User(t, "boss", fmt.Sprintf("brief-catchup-boss-%d@multica.test", n))
	ws := fx.Workspace(t, "brief catch-up ws", fmt.Sprintf("brief-catchup-ws-%d", n))
	fx.Member(t, ws, owner, "owner")
	fx.WorkspaceID = ws
	fx.UserID = owner

	runtime := fx.Runtime(t, "secretary runtime", testutil.Cols{"workspace_id": ws})
	secretary := fx.Agent(t, fmt.Sprintf("Secretary %d", n), runtime, testutil.Cols{"workspace_id": ws})
	fx.Exec(t, `UPDATE workspace SET secretary_agent_id = $1 WHERE id = $2`, secretary, ws)
	autopilot := fx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    ws,
		"title":           DailyBriefTitle,
		"assignee_type":   "agent",
		"assignee_id":     secretary,
		"status":          "active",
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   owner,
	})
	trigger := fx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id":    autopilot,
		"kind":            "schedule",
		"enabled":         true,
		"cron_expression": "0 8 * * *",
		"timezone":        briefCatchupTZ,
		"next_run_at":     time.Now().Add(time.Hour),
	})

	f := &briefCatchupFixture{
		t: t, fx: fx, ws: ws, runtime: runtime, secretary: secretary,
		autopilot: autopilot, trigger: trigger,
		clock: briefCatchupAt(t, 9, 0),
	}
	f.disp = &fakeCatchupDispatcher{t: t, fx: fx, ws: ws, now: func() time.Time { return f.clock }}
	f.svc = NewBriefCatchupService(db.New(pool), f.disp)
	f.svc.now = func() time.Time { return f.clock }
	return f
}

// briefCatchupAt is a local clock time on 10 January 2026 in briefCatchupTZ.
func briefCatchupAt(t *testing.T, hour, minute int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(briefCatchupTZ)
	if err != nil {
		t.Fatalf("load %s: %v", briefCatchupTZ, err)
	}
	return time.Date(2026, 1, 10, hour, minute, 0, 0, loc)
}

func (f *briefCatchupFixture) at(hour, minute int) {
	f.clock = briefCatchupAt(f.t, hour, minute)
}

// pass runs one hourly pass. Other workspaces in the database may be candidates
// too, so the tests judge this workspace by firedHere, not the total.
func (f *briefCatchupFixture) pass() {
	f.t.Helper()
	if _, err := f.svc.CatchUpBriefs(context.Background()); err != nil {
		f.t.Fatalf("CatchUpBriefs: %v", err)
	}
}

func (f *briefCatchupFixture) firedHere() int {
	n := 0
	for _, id := range f.disp.calls {
		if id == f.autopilot {
			n++
		}
	}
	return n
}

func (f *briefCatchupFixture) setSettings(settings string) {
	f.fx.Exec(f.t, `UPDATE workspace SET settings = $2::jsonb WHERE id = $1`, f.ws, settings)
}

func (f *briefCatchupFixture) setRuntimeStatus(status string) {
	f.fx.Exec(f.t, `UPDATE agent_runtime SET status = $2 WHERE id = $1`, f.runtime, status)
}

// publishBrief writes a daily brief at the given instant.
func (f *briefCatchupFixture) publishBrief(at time.Time) {
	f.t.Helper()
	f.fx.Insert(f.t, "brief", testutil.Cols{
		"workspace_id":         f.ws,
		"kind":                 "daily",
		"period_start":         at.Add(-24 * time.Hour),
		"period_end":           at,
		"created_at":           at,
		"title":                DailyBriefTitle,
		"body":                 "## Headline\nNothing needs you today.",
		"created_by_member_id": f.fx.UserID,
	})
}

func TestBriefCatchup_FiresOnceAtTheHourAndNotAgainThatDay(t *testing.T) {
	f := newBriefCatchupFixture(t)

	f.at(9, 59)
	f.pass()
	if f.firedHere() != 0 {
		t.Fatalf("fired at 09:59, before the 10:00 catch-up hour")
	}

	f.at(10, 0)
	f.pass()
	if f.firedHere() != 1 {
		t.Fatalf("fired %d times at 10:00, want once", f.firedHere())
	}
	if f.disp.sources[0] != "catchup" {
		t.Fatalf("source = %q, want catchup", f.disp.sources[0])
	}

	// The next hourly pass: the catch-up run is still in flight.
	f.at(11, 0)
	f.pass()
	if f.firedHere() != 1 {
		t.Fatalf("a second pass while the run is in flight fired again: %d", f.firedHere())
	}

	// It finishes without publishing. Today's catch-up is already counted, so the
	// day does not fire a second time.
	f.fx.Exec(t, `UPDATE autopilot_run SET status = 'completed' WHERE autopilot_id = $1`, f.autopilot)
	f.at(12, 0)
	f.pass()
	if f.firedHere() != 1 {
		t.Fatalf("a later pass the same day fired again: %d", f.firedHere())
	}
}

func TestBriefCatchup_HonoursTheConfiguredHour(t *testing.T) {
	f := newBriefCatchupFixture(t)
	f.setSettings(`{"brief_catchup_hour": 12}`)

	f.at(11, 59)
	f.pass()
	if f.firedHere() != 0 {
		t.Fatalf("fired at 11:59 with the hour set to 12")
	}
	f.at(12, 0)
	f.pass()
	if f.firedHere() != 1 {
		t.Fatalf("fired %d times at 12:00, want once", f.firedHere())
	}
}

func TestBriefCatchup_SettingOffSkipsAndOnFires(t *testing.T) {
	f := newBriefCatchupFixture(t)
	f.setSettings(`{"brief_catchup_enabled": false}`)

	f.at(10, 0)
	f.pass()
	if f.firedHere() != 0 {
		t.Fatalf("fired with brief_catchup_enabled off")
	}

	f.setSettings(`{"brief_catchup_enabled": true}`)
	f.pass()
	if f.firedHere() != 1 {
		t.Fatalf("fired %d times once the setting is back on, want 1", f.firedHere())
	}
}

func TestBriefCatchup_SkipsWhenTodaysBriefExists(t *testing.T) {
	f := newBriefCatchupFixture(t)
	f.publishBrief(briefCatchupAt(t, 8, 5))

	f.at(10, 0)
	f.pass()
	if f.firedHere() != 0 {
		t.Fatalf("fired although today's brief was published at 08:05")
	}
}

// A brief published at 03:00 local on 10 January is today's, although its UTC
// date is 9 January. A brief from 23:30 local on 9 January is yesterday's.
func TestBriefCatchup_TodayIsTheLocalDayNotTheUTCDay(t *testing.T) {
	today := newBriefCatchupFixture(t)
	today.publishBrief(time.Date(2026, 1, 9, 20, 0, 0, 0, time.UTC))
	today.at(10, 30)
	today.pass()
	if today.firedHere() != 0 {
		t.Fatalf("fired although a brief was published at 03:00 local today")
	}

	yesterday := newBriefCatchupFixture(t)
	yesterday.publishBrief(time.Date(2026, 1, 9, 16, 30, 0, 0, time.UTC))
	yesterday.at(10, 30)
	yesterday.pass()
	if yesterday.firedHere() != 1 {
		t.Fatalf("a brief from 23:30 yesterday is not today's; fired %d times, want 1", yesterday.firedHere())
	}
}

func TestBriefCatchup_SkipsWhenTheRuntimeIsOffline(t *testing.T) {
	f := newBriefCatchupFixture(t)
	f.setRuntimeStatus("offline")

	f.at(10, 0)
	f.pass()
	if f.firedHere() != 0 {
		t.Fatalf("fired with the Secretary's runtime offline")
	}

	f.setRuntimeStatus("online")
	f.pass()
	if f.firedHere() != 1 {
		t.Fatalf("fired %d times once the runtime is back, want 1", f.firedHere())
	}
}

func TestBriefCatchup_SkipsWhileTheSecretaryHasWorkQueuedOrRunning(t *testing.T) {
	for _, status := range []string{"queued", "running"} {
		t.Run(status, func(t *testing.T) {
			f := newBriefCatchupFixture(t)
			task := f.fx.Task(t, f.secretary, testutil.Cols{"status": status, "runtime_id": f.runtime})

			f.at(10, 0)
			f.pass()
			if f.firedHere() != 0 {
				t.Fatalf("fired while the Secretary has a %s task", status)
			}

			f.fx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, task)
			f.pass()
			if f.firedHere() != 1 {
				t.Fatalf("fired %d times once the task completed, want 1", f.firedHere())
			}
		})
	}
}

func TestBriefCatchup_SkipsWhileAScheduledRunIsInFlight(t *testing.T) {
	inFlight := newBriefCatchupFixture(t)
	inFlight.fx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": inFlight.autopilot, "trigger_id": inFlight.trigger, "source": "schedule", "status": "running",
	})
	inFlight.at(10, 0)
	inFlight.pass()
	if inFlight.firedHere() != 0 {
		t.Fatalf("fired while the 08:00 run is still running")
	}

	// A run the admission gate skipped (the machine was asleep at 08:00) is
	// terminal, so it does not hold the brief back.
	skipped := newBriefCatchupFixture(t)
	skipped.fx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": skipped.autopilot, "trigger_id": skipped.trigger, "source": "schedule", "status": "skipped",
	})
	skipped.at(10, 0)
	skipped.pass()
	if skipped.firedHere() != 1 {
		t.Fatalf("a skipped 08:00 run blocked the catch-up: fired %d times, want 1", skipped.firedHere())
	}
}

func TestBriefCatchup_OnlyActiveDailyBriefsAreCandidates(t *testing.T) {
	f := newBriefCatchupFixture(t)
	f.fx.Exec(t, `UPDATE autopilot SET status = 'paused' WHERE id = $1`, f.autopilot)

	f.at(10, 0)
	f.pass()
	if f.firedHere() != 0 {
		t.Fatalf("fired for a paused daily brief")
	}
}

// Each pass judges every workspace on its own state: the one with no brief is
// fired, and the one whose brief is published is not, in the same pass.
func TestBriefCatchup_IsolatedPerWorkspace(t *testing.T) {
	missed := newBriefCatchupFixture(t)
	published := newBriefCatchupFixture(t)
	published.publishBrief(briefCatchupAt(t, 8, 0))

	missed.at(10, 0)
	missed.pass()
	if missed.firedHere() != 1 {
		t.Fatalf("the workspace with no brief fired %d times, want 1", missed.firedHere())
	}
	for _, id := range missed.disp.calls {
		if id == published.autopilot {
			t.Fatalf("the pass fired the autopilot of a workspace whose brief was published")
		}
	}
}
