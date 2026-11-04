package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// windowEvening is 22:00 to 07:00 in Paris, the spec default in a real zone.
var windowEvening = QuietHours{Enabled: true, Start: "22:00", End: "07:00", Timezone: "Europe/Paris"}

func parisAt(t *testing.T, h, m int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, time.November, 3, h, m, 0, 0, loc)
}

// TestCompanyVerdictFollowsTheStateAndTheWindowForEveryClass is the whole
// admission table: every stored setting, with the window off and on, for every
// run class, at times inside, outside and on each edge of the window. The
// expectation is written out from spec 4.5, not read back from the rule.
func TestCompanyVerdictFollowsTheStateAndTheWindowForEveryClass(t *testing.T) {
	windowOff := QuietHours{Enabled: false, Start: "22:00", End: "07:00", Timezone: "Europe/Paris"}
	times := []struct {
		name   string
		now    time.Time
		inside bool // inside the 22:00 to 07:00 window
	}{
		{"noon", parisAt(t, 12, 0), false},
		{"window opens at 22:00", parisAt(t, 22, 0), true},
		{"late evening", parisAt(t, 23, 30), true},
		{"after midnight", parisAt(t, 2, 0), true},
		{"last minute inside", parisAt(t, 6, 59), true},
		{"window closes at 07:00", parisAt(t, 7, 0), false},
		{"just before opening", parisAt(t, 21, 59), false},
	}
	settings := []string{CompanyStateRunning, CompanyStatePaused, CompanyStateQuiet}
	classes := []RunClass{RunClassHuman, RunClassRoutine, RunClassDelegated, RunClassExempt}

	// want is spec 4.5 stated per case: paused holds routine and delegated;
	// quiet (the setting, or an open window) defers routine only; a human run
	// and an exempt run are admitted in every state.
	want := func(setting string, windowOn, inside bool, class RunClass) Verdict {
		paused := setting == CompanyStatePaused
		quiet := setting == CompanyStateQuiet || (windowOn && inside)
		switch {
		case paused && (class == RunClassRoutine || class == RunClassDelegated):
			return Verdict{Reason: VerdictReasonCompanyPaused}
		case !paused && quiet && class == RunClassRoutine:
			return Verdict{Reason: VerdictReasonQuietHours}
		}
		return Verdict{Admit: true}
	}

	for _, setting := range settings {
		for _, windowOn := range []bool{false, true} {
			qh := windowOff
			if windowOn {
				qh = windowEvening
			}
			for _, tc := range times {
				for _, class := range classes {
					name := fmt.Sprintf("%s/window=%v/%s/%s", setting, windowOn, tc.name, class)
					t.Run(name, func(t *testing.T) {
						got := companyVerdict(setting, qh, class, tc.now)
						exp := want(setting, windowOn, tc.inside, class)
						if got != exp {
							t.Fatalf("companyVerdict = %+v, want %+v", got, exp)
						}
					})
				}
			}
		}
	}
}

func TestQuietWindowOpensAndClosesInItsTimezone(t *testing.T) {
	utc := func(h, m int) time.Time { return time.Date(2026, time.November, 3, h, m, 0, 0, time.UTC) }
	// Paris is UTC+1 in November, so 22:00 Paris is 21:00 UTC.
	cases := []struct {
		name string
		qh   QuietHours
		now  time.Time
		want bool
	}{
		{"21:30 UTC is 22:30 Paris, inside", windowEvening, utc(21, 30), true},
		{"20:30 UTC is 21:30 Paris, outside", windowEvening, utc(20, 30), false},
		{"06:30 UTC is 07:30 Paris, outside", windowEvening, utc(6, 30), false},
		{"05:30 UTC is 06:30 Paris, inside", windowEvening, utc(5, 30), true},
		{"a disabled window never opens", QuietHours{Enabled: false, Start: "22:00", End: "07:00", Timezone: "Europe/Paris"}, utc(21, 30), false},
		{"an unloadable zone reads as UTC", QuietHours{Enabled: true, Start: "09:00", End: "17:00", Timezone: "Nowhere/Land"}, utc(10, 0), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := quietWindowOpen(tc.qh, tc.now); got != tc.want {
				t.Fatalf("quietWindowOpen = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestQuietWindowSameDayAndDegenerateCases(t *testing.T) {
	company := QuietHours{Enabled: true, Start: "09:00", End: "17:00", Timezone: "UTC"}
	at := func(h, m int) time.Time { return time.Date(2026, time.November, 3, h, m, 0, 0, time.UTC) }
	if quietWindowOpen(company, at(8, 59)) || !quietWindowOpen(company, at(9, 0)) || !quietWindowOpen(company, at(16, 59)) || quietWindowOpen(company, at(17, 0)) {
		t.Fatal("a window within one day is [start, end)")
	}
	same := QuietHours{Enabled: true, Start: "12:00", End: "12:00", Timezone: "UTC"}
	if quietWindowOpen(same, at(12, 0)) {
		t.Fatal("a window whose start equals its end is no window")
	}
}

func TestClockAndQuietHoursValidation(t *testing.T) {
	for _, s := range []string{"00:00", "07:05", "23:59"} {
		if _, err := parseClockMinutes(s); err != nil {
			t.Errorf("%q should parse: %v", s, err)
		}
	}
	for _, s := range []string{"", "7:00", "24:00", "07:60", "07-00", "0700", "ab:cd"} {
		if _, err := parseClockMinutes(s); err == nil {
			t.Errorf("%q should not parse", s)
		}
	}
	if err := validateQuietHours(windowEvening); err != nil {
		t.Fatalf("the default window is valid: %v", err)
	}
	if err := validateQuietHours(QuietHours{Enabled: true, Start: "08:00", End: "08:00", Timezone: "UTC"}); err == nil {
		t.Error("an enabled window with equal start and end is refused")
	}
	if err := validateQuietHours(QuietHours{Enabled: false, Start: "08:00", End: "08:00", Timezone: "UTC"}); err != nil {
		t.Errorf("a disabled window may hold any times: %v", err)
	}
	if err := validateQuietHours(QuietHours{Enabled: true, Start: "22:00", End: "07:00", Timezone: "Mars/Olympus"}); err == nil {
		t.Error("an unknown timezone is refused")
	}
	if err := validateQuietHours(QuietHours{Enabled: true, Start: "22:00", End: "07:00", Timezone: ""}); err == nil {
		t.Error("an empty timezone is refused")
	}
}

func TestEffectiveStateReportsTheStateInForceNow(t *testing.T) {
	at := parisAt(t, 23, 0)
	if effectiveState(CompanyStateRunning, windowEvening, at) != CompanyStateQuiet {
		t.Error("an open window makes a running company quiet")
	}
	if effectiveState(CompanyStatePaused, windowEvening, at) != CompanyStatePaused {
		t.Error("paused wins over an open window")
	}
	if effectiveState(CompanyStateQuiet, windowEvening, parisAt(t, 12, 0)) != CompanyStateQuiet {
		t.Error("a company set to quiet stays quiet outside the window")
	}
	if effectiveState(CompanyStateRunning, windowEvening, parisAt(t, 12, 0)) != CompanyStateRunning {
		t.Error("a running company outside the window runs")
	}
}

// companyFixture is a workspace with an owner and a plain member, and the
// company controls service over it.
type companyFixture struct {
	*testutil.Fixture
	ws    pgtype.UUID
	owner pgtype.UUID
	svc   *CompanyStateService
}

func newCompanyFixture(t *testing.T) companyFixture {
	t.Helper()
	f, owner := newPrincipalFixture(t)
	return companyFixture{
		Fixture: f.Fixture,
		ws:      util.MustParseUUID(f.WorkspaceID),
		owner:   util.MustParseUUID(owner),
		svc:     &CompanyStateService{Queries: f.q, TxStarter: f.Pool, Tasks: &TaskService{Queries: f.q, TxStarter: f.Pool, Bus: events.New()}},
	}
}

func (f companyFixture) actor() GovernanceActor {
	return GovernanceActor{Type: GovernanceActorMember, ID: f.owner}
}

func (f companyFixture) eventCount(t *testing.T, kind string) int {
	t.Helper()
	return f.Count(t, `SELECT count(*) FROM governance_event WHERE workspace_id = $1 AND kind = $2`, f.WorkspaceID, kind)
}

func TestCompanyStateSetRecordsPauseAndResumeAndRefusesUnknownStates(t *testing.T) {
	f := newCompanyFixture(t)
	ctx := context.Background()

	if _, err := f.svc.Set(ctx, f.ws, f.actor(), "asleep"); err != ErrCompanyStateInvalid {
		t.Fatalf("unknown state: err = %v, want ErrCompanyStateInvalid", err)
	}
	if v, err := f.svc.Get(ctx, f.ws); err != nil || v.State != CompanyStateRunning || v.Setting != CompanyStateRunning {
		t.Fatalf("a refused change moves nothing: %+v, %v", v, err)
	}

	paused, err := f.svc.Set(ctx, f.ws, f.actor(), CompanyStatePaused)
	if err != nil || paused.State != CompanyStatePaused {
		t.Fatalf("pause: %+v, %v", paused, err)
	}
	if n := f.eventCount(t, GovernanceKindCompanyPaused); n != 1 {
		t.Fatalf("pause recorded %d company_paused events, want 1", n)
	}
	if _, err := f.svc.Set(ctx, f.ws, f.actor(), CompanyStatePaused); err != nil {
		t.Fatal(err)
	}
	if n := f.eventCount(t, GovernanceKindCompanyPaused); n != 1 {
		t.Fatalf("pausing a paused company recorded a second event (%d)", n)
	}

	// Quiet is not running, but leaving paused is a resume.
	if _, err := f.svc.Set(ctx, f.ws, f.actor(), CompanyStateQuiet); err != nil {
		t.Fatal(err)
	}
	if n := f.eventCount(t, GovernanceKindCompanyResumed); n != 1 {
		t.Fatalf("leaving paused recorded %d company_resumed events, want 1", n)
	}

	var actorType string
	var actorID pgtype.UUID
	f.QueryRow(t, `SELECT actor_type, actor_id FROM governance_event WHERE workspace_id = $1 AND kind = $2`,
		f.WorkspaceID, GovernanceKindCompanyPaused).Scan(&actorType, &actorID)
	if actorType != GovernanceActorMember || util.UUIDToString(actorID) != util.UUIDToString(f.owner) {
		t.Fatalf("pause recorded actor %s %s, want the member", actorType, util.UUIDToString(actorID))
	}
}

func TestQuietHoursSaveAndDecideRoutineRuns(t *testing.T) {
	f := newCompanyFixture(t)
	ctx := context.Background()

	now := time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC)
	saved, err := f.svc.SetQuietHours(ctx, f.ws, QuietHours{Enabled: true, Start: "11:00", End: "13:00", Timezone: "UTC"})
	if err != nil {
		t.Fatalf("save quiet hours: %v", err)
	}
	if !saved.QuietHours.Enabled || saved.QuietHours.Start != "11:00" || saved.QuietHours.End != "13:00" || saved.QuietHours.Timezone != "UTC" {
		t.Fatalf("saved window reads back as %+v", saved.QuietHours)
	}
	read, err := f.svc.Get(ctx, f.ws)
	if err != nil || read.QuietHours != saved.QuietHours {
		t.Fatalf("Get after save = %+v, %v", read.QuietHours, err)
	}

	// The window [11:00, 13:00) holds 12:00 and admits 13:00.
	v, err := f.svc.Verdict(ctx, f.ws, RunClassRoutine, now)
	if err != nil || v.Admit || v.Reason != VerdictReasonQuietHours {
		t.Fatalf("routine at 12:00 inside the window = %+v, %v; want quiet_hours", v, err)
	}
	if v, _ := f.svc.Verdict(ctx, f.ws, RunClassDelegated, now); !v.Admit {
		t.Fatal("a delegated run is not held by the quiet window")
	}
	if v, _ := f.svc.Verdict(ctx, f.ws, RunClassHuman, now); !v.Admit {
		t.Fatal("a human run is never held by the quiet window")
	}
	if v, _ := f.svc.Verdict(ctx, f.ws, RunClassRoutine, now.Add(time.Hour)); !v.Admit {
		t.Fatal("the window closes at 13:00 and routine runs resume")
	}

	if _, err := f.svc.SetQuietHours(ctx, f.ws, QuietHours{Enabled: true, Start: "25:00", End: "07:00", Timezone: "UTC"}); err != ErrQuietHoursInvalid {
		t.Fatalf("a bad start: err = %v, want ErrQuietHoursInvalid", err)
	}
}

func TestCompanyStatePauseAndStopCancelsOnlyRoutineAndDelegatedRuns(t *testing.T) {
	f := newCompanyFixture(t)
	ctx := context.Background()
	runtime := f.Runtime(t, "stop-runtime", testutil.Cols{"owner_id": f.owner})
	agent := f.Agent(t, "stop-agent", runtime, testutil.Cols{"owner_id": f.owner})

	routine := f.Task(t, agent, testutil.Cols{"runtime_id": runtime, "status": "running", "originator_source": "trigger_owner"})
	delegated := f.Task(t, agent, testutil.Cols{"runtime_id": runtime, "status": "running", "originator_source": "delegation"})
	human := f.Task(t, agent, testutil.Cols{"runtime_id": runtime, "status": "running", "originator_source": "direct_human"})
	queued := f.Task(t, agent, testutil.Cols{"runtime_id": runtime, "status": "queued", "originator_source": "trigger_owner"})

	view, cancelled, err := f.svc.PauseAndStop(ctx, f.ws, f.actor())
	if err != nil {
		t.Fatalf("PauseAndStop: %v", err)
	}
	if view.State != CompanyStatePaused || view.Setting != CompanyStatePaused {
		t.Fatalf("PauseAndStop left the company %+v, want paused", view)
	}
	if cancelled != 2 {
		t.Fatalf("cancelled %d runs, want the routine and the delegated run (2)", cancelled)
	}
	status := func(id string) string {
		var s string
		f.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, id).Scan(&s)
		return s
	}
	if got := status(routine); got != "cancelled" {
		t.Errorf("routine run is %q, want cancelled", got)
	}
	if got := status(delegated); got != "cancelled" {
		t.Errorf("delegated run is %q, want cancelled", got)
	}
	if got := status(human); got != "running" {
		t.Errorf("a person's run is %q, want it left running", got)
	}
	if got := status(queued); got != "queued" {
		t.Errorf("a queued run is %q, want it left queued for the claim gate", got)
	}
	if n := f.eventCount(t, GovernanceKindCompanyPaused); n != 1 {
		t.Fatalf("PauseAndStop recorded %d pause events, want 1", n)
	}
	var stop string
	f.QueryRow(t, `SELECT details->>'stop_running' FROM governance_event WHERE workspace_id = $1 AND kind = $2`,
		f.WorkspaceID, GovernanceKindCompanyPaused).Scan(&stop)
	if stop != "true" {
		t.Errorf("pause event details stop_running = %q, want true", stop)
	}
}

// The autopilot dispatch gate (spec 4.5). A scheduled run is skipped with the
// reason; a run a person starts is admitted in every state.
func TestAutopilotDispatchIsHeldByTheCompanyControls(t *testing.T) {
	f := newCompanyFixture(t)
	ctx := context.Background()
	apFx := principalFixture{Fixture: f.Fixture, q: f.svc.Queries, svc: &AutopilotService{Queries: f.svc.Queries, TxStarter: f.svc.TxStarter, Bus: events.New(), TaskSvc: f.svc.Tasks}}
	agent := apFx.privateAgentOwnedBy(t, util.UUIDToString(f.owner), "gate")

	// fresh starts a new autopilot so each dispatch is the only active run.
	fresh := func(t *testing.T) (db.Autopilot, pgtype.UUID) {
		t.Helper()
		apID, trigID := apFx.autopilotWithTrigger(t, agent, util.UUIDToString(f.owner), util.UUIDToString(f.owner))
		ap, err := f.svc.Queries.GetAutopilot(ctx, util.MustParseUUID(apID))
		if err != nil {
			t.Fatal(err)
		}
		return ap, util.MustParseUUID(trigID)
	}
	schedule := func(t *testing.T) *db.AutopilotRun {
		t.Helper()
		ap, trig := fresh(t)
		run, err := apFx.svc.DispatchAutopilot(ctx, ap, trig, "schedule", nil)
		if err != nil || run == nil {
			t.Fatalf("schedule dispatch: run %v, err %v", run, err)
		}
		return run
	}
	byHand := func(t *testing.T) *db.AutopilotRun {
		t.Helper()
		ap, trig := fresh(t)
		run, _, err := apFx.svc.DispatchAutopilotManual(ctx, ap, trig, nil, f.owner)
		if err != nil || run == nil {
			t.Fatalf("manual dispatch: run %v, err %v", run, err)
		}
		return run
	}

	// Running: a schedule is admitted.
	if run := schedule(t); run.Status == "skipped" {
		t.Fatalf("a running company refused a schedule: %q", run.FailureReason.String)
	}

	// Paused: the schedule is skipped with the reason; a run by hand is admitted.
	if _, err := f.svc.Set(ctx, f.ws, f.actor(), CompanyStatePaused); err != nil {
		t.Fatal(err)
	}
	run := schedule(t)
	if run.Status != "skipped" || run.ReasonCode.String != "company_paused" {
		t.Fatalf("paused schedule: status %q, reason %q; want skipped company_paused", run.Status, run.ReasonCode.String)
	}
	if byHand(t).Status == "skipped" {
		t.Fatal("a run a person starts by hand is refused while paused")
	}

	// Resumed, then the quiet window open now: the schedule is deferred with its reason.
	if _, err := f.svc.Set(ctx, f.ws, f.actor(), CompanyStateRunning); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := f.svc.SetQuietHours(ctx, f.ws, QuietHours{Enabled: true, Start: now.Add(-time.Hour).Format("15:04"), End: now.Add(time.Hour).Format("15:04"), Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	// Quiet hours defer the schedule (spec 4.5): it is deferred with its reason,
	// and nothing is recorded as skipped.
	run = schedule(t)
	if run.Status != "deferred" || run.ReasonCode.String != "quiet_hours" {
		t.Fatalf("quiet schedule: status %q, reason %q; want deferred quiet_hours", run.Status, run.ReasonCode.String)
	}
	if byHand(t).Status == "skipped" {
		t.Fatal("a run a person starts by hand is refused in quiet hours")
	}

	// Still inside the window the sweep leaves the deferred run where it is.
	if n, err := apFx.svc.ReleaseQuietDeferredRuns(ctx, 100); err != nil || n != 0 {
		t.Fatalf("sweep inside the window dispatched %d (err %v); want none", n, err)
	}
	// The window closes: the sweep dispatches the deferred run.
	if _, err := f.svc.SetQuietHours(ctx, f.ws, QuietHours{Enabled: false, Start: "22:00", End: "07:00", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	if n, err := apFx.svc.ReleaseQuietDeferredRuns(ctx, 100); err != nil || n != 1 {
		t.Fatalf("sweep after the window dispatched %d (err %v); want 1", n, err)
	}
	released, err := f.svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil || released.Status == "deferred" || released.Status == "skipped" {
		t.Fatalf("deferred run after the window: status %q (err %v); want it dispatched", released.Status, err)
	}
}

// The wakeup gate: a routine firing waits while paused and fires after resume,
// with its input still pending.
func TestWakeupFiringWaitsWhilePausedAndFiresAfterResume(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	css := &CompanyStateService{Queries: f.q, TxStarter: f.Pool, Tasks: f.svc.TaskSvc}
	actor := GovernanceActor{Type: GovernanceActorMember, ID: parseTestUUID(t, f.UserID)}
	ws := parseTestUUID(t, f.WorkspaceID)

	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Review latest changes"})
	if _, err := css.Set(ctx, ws, actor, CompanyStatePaused); err != nil {
		t.Fatal(err)
	}
	f.Comment(t, util.UUIDToString(issue), "while paused")
	wakeDispatch(t, s, w)

	held, err := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if held.LastTaskID.Valid {
		t.Fatal("a routine wakeup fired while the company was paused")
	}
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); n == 0 {
		t.Fatal("the held input was consumed; it must wait for the resume")
	}

	if _, err := css.Set(ctx, ws, actor, CompanyStateRunning); err != nil {
		t.Fatal(err)
	}
	wakeDispatch(t, s, held)
	after, err := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if !after.LastTaskID.Valid {
		t.Fatal("the wakeup did not fire after the company resumed")
	}
}
