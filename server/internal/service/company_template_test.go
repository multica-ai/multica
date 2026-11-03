package service

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// companyEnv is a workspace with the Secretary set up, the knowledge base ready
// (against the fake WeKnora), and a runtime to apply the company on.
type companyEnv struct {
	t      *testing.T
	ctx    context.Context
	ke     *knowledgeEnv
	svc    *CompanyTemplateService
	rt     db.AgentRuntime
	data   companyData
	ws     pgtype.UUID
	user   pgtype.UUID
	wsID   string
	userID string
}

// newCompanyEnv returns a fresh workspace with the Executive Office set up, the
// Secretary included. Its knowledge base is not enabled; a test enables it when
// it needs to.
func newCompanyEnv(t *testing.T) *companyEnv {
	t.Helper()
	ke := newKnowledgeEnv(t)
	ctx := context.Background()
	fx := testutil.New(ke.pool, ke.wsID, ke.userID)
	runtimeID := fx.Runtime(t, "Company runtime", testutil.Cols{"provider": "hermes"})
	rt, err := ke.q.GetAgentRuntime(ctx, mustUUID(t, runtimeID))
	if err != nil {
		t.Fatal(err)
	}
	data, err := loadCompanyData()
	if err != nil {
		t.Fatal(err)
	}
	e := &companyEnv{
		t: t, ctx: ctx, ke: ke, rt: rt, data: data,
		ws: ke.ws, user: ke.user, wsID: ke.wsID, userID: ke.userID,
	}
	e.svc = NewCompanyTemplateService(ke.q, ke.pool, ke.svc)
	// Registered after newKnowledgeEnv's cleanup, so it runs first: these tables
	// have no foreign keys, and the workspace teardown would not remove them.
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DELETE FROM squad_routine WHERE workspace_id = $1`,
			`DELETE FROM autopilot_trigger WHERE autopilot_id IN (SELECT id FROM autopilot WHERE workspace_id = $1)`,
			`DELETE FROM autopilot WHERE workspace_id = $1`,
			`DELETE FROM squad_charter WHERE workspace_id = $1`,
			`DELETE FROM pipeline WHERE workspace_id = $1`,
		} {
			ke.pool.Exec(context.Background(), stmt, ke.wsID)
		}
	})
	if _, err := NewOfficeSetupService(ke.q, ke.pool).Setup(ctx, OfficeSetupInput{
		WorkspaceID:           ke.ws,
		Actor:                 db.Member{UserID: ke.user},
		Runtime:               rt,
		SecretaryInstructions: "You are the Secretary.",
		Timezone:              "UTC",
	}); err != nil {
		t.Fatalf("office setup: %v", err)
	}
	return e
}

func (e *companyEnv) enableKnowledge() {
	e.t.Helper()
	if _, err := e.ke.enable(); err != nil {
		e.t.Fatalf("enable knowledge: %v", err)
	}
}

func (e *companyEnv) apply(squads []string, start string) CompanyApplyResult {
	e.t.Helper()
	res, err := e.svc.Apply(e.ctx, CompanyApplyInput{
		WorkspaceID: e.ws, ActorUserID: e.user, Runtime: e.rt, Model: "test-model", Squads: squads, Start: start,
	})
	if err != nil {
		e.t.Fatalf("apply: %v", err)
	}
	return res
}

func (e *companyEnv) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.ke.pool.QueryRow(e.ctx, sql, append([]any{e.ws}, args...)...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// autopilotStatus returns the status of the one live autopilot with the title
// that an assignee owns.
func (e *companyEnv) autopilotStatus(assigneeType string, assigneeID pgtype.UUID, title string) string {
	e.t.Helper()
	var status string
	rows, err := e.ke.pool.Query(e.ctx,
		`SELECT status FROM autopilot WHERE workspace_id = $1 AND assignee_type = $2 AND assignee_id = $3 AND title = $4 AND status <> 'archived'`,
		e.ws, assigneeType, assigneeID, title)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		if err := rows.Scan(&status); err != nil {
			e.t.Fatal(err)
		}
		got = append(got, status)
	}
	if len(got) != 1 {
		e.t.Fatalf("%s %s: %d live autopilots titled %q, want 1", assigneeType, uuidText(assigneeID), len(got), title)
	}
	return got[0]
}

func (e *companyEnv) squadByName(name string) db.Squad {
	e.t.Helper()
	squads, err := e.ke.q.ListSquads(e.ctx, e.ws)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, s := range squads {
		if s.Name == name && !s.ArchivedAt.Valid {
			return s
		}
	}
	e.t.Fatalf("no live squad %q", name)
	return db.Squad{}
}

// expectRoutines checks every routine of the chosen cards against the start
// option: the squad's routines and reviews, and the Secretary's briefs.
func (e *companyEnv) expectRoutines(start string, keys []string) {
	e.t.Helper()
	secretary := e.secretaryID()
	for _, card := range selectedSquads(e.data, keys) {
		squad := e.squadByName(card.Name)
		for _, routine := range companyRoutinesOf(card) {
			want := "paused"
			if routineWanted(start, routine) {
				want = "active"
			}
			if card.Key == "executive_office" && (routine.Title == DailyBriefTitle || routine.Title == WeeklyReviewTitle) {
				if got := e.autopilotStatus("agent", secretary, routine.Title); got != want {
					e.t.Errorf("%s (%s): status %s, want %s", routine.Title, start, got, want)
				}
				continue
			}
			if got := e.autopilotStatus("squad", squad.ID, routine.Title); got != want {
				e.t.Errorf("%s / %s (%s): status %s, want %s", card.Name, routine.Title, start, got, want)
			}
		}
	}
}

func (e *companyEnv) secretaryID() pgtype.UUID {
	e.t.Helper()
	ws, err := e.ke.q.GetWorkspace(e.ctx, e.ws)
	if err != nil || !ws.SecretaryAgentID.Valid {
		e.t.Fatalf("no Secretary: %v", err)
	}
	return ws.SecretaryAgentID
}

func stepStatuses(res CompanyApplyResult) map[string]string {
	out := map[string]string{}
	for _, s := range res.Steps {
		out[s.Key] = s.Status
	}
	return out
}

func failedSteps(res CompanyApplyResult) []CompanyStep {
	var out []CompanyStep
	for _, s := range res.Steps {
		if s.Status == CompanyStepFailed {
			out = append(out, s)
		}
	}
	return out
}

func allKeys(data companyData) []string {
	keys := make([]string, 0, len(data.Squads))
	for _, s := range data.Squads {
		keys = append(keys, s.Key)
	}
	return keys
}

func uuidText(id pgtype.UUID) string {
	if !id.Valid {
		return "<none>"
	}
	return strings.ToLower(string(id.Bytes[:]))
}

func TestCompanyApplyInstallsTheWholeCompanyOnAnEmptyWorkspace(t *testing.T) {
	e := newCompanyEnv(t)
	e.enableKnowledge()
	keys := allKeys(e.data)

	res := e.apply(keys, CompanyStartLeavePaused)
	if failed := failedSteps(res); len(failed) > 0 {
		t.Fatalf("failed steps: %+v", failed)
	}
	if !res.PipelineID.Valid {
		t.Fatal("the apply installed no pipeline")
	}
	if len(res.UnownedStages) != 0 {
		t.Fatalf("unowned stages %v, want none with every squad chosen", res.UnownedStages)
	}

	if got := e.count(`SELECT count(*) FROM squad WHERE workspace_id = $1 AND archived_at IS NULL`); got != len(e.data.Squads) {
		t.Fatalf("squads = %d, want %d", got, len(e.data.Squads))
	}
	if got := e.count(`SELECT count(*) FROM office_floor WHERE workspace_id = $1`); got != len(e.data.FloorKeys) {
		t.Fatalf("floors = %d, want %d", got, len(e.data.FloorKeys))
	}
	if got := e.count(`SELECT count(*) FROM squad_charter WHERE workspace_id = $1`); got != len(e.data.Squads) {
		t.Fatalf("charters = %d, want one per squad", got)
	}
	if got := e.count(`SELECT count(*) FROM autopilot WHERE workspace_id = $1 AND assignee_type = 'squad' AND title = $2`, OperatingReviewTitle); got != len(e.data.Squads) {
		t.Fatalf("operating reviews = %d, want one per squad", got)
	}
	if got := e.count(`SELECT count(*) FROM pipeline WHERE workspace_id = $1 AND is_default`); got != 1 {
		t.Fatalf("default pipelines = %d, want 1", got)
	}
	if got := e.count(`SELECT count(*) FROM knowledge_kind WHERE workspace_id = $1`); got != len(defaultKnowledgeKinds) {
		t.Fatalf("knowledge kinds = %d, want %d", got, len(defaultKnowledgeKinds))
	}
	if got := e.count(`SELECT count(*) FROM knowledge_base WHERE workspace_id = $1 AND key = $2`, KnowledgeVentureBaseKey); got != 1 {
		t.Fatalf("ventures base rows = %d, want 1", got)
	}
	// The system writer kinds publish automatically; no other kind has a policy row,
	// so every other kind keeps the default manual mode.
	for _, kind := range systemWriterKinds {
		if got := e.count(`SELECT count(*) FROM knowledge_publish_policy WHERE workspace_id = $1 AND scope = 'kind' AND scope_key = $2 AND mode = 'auto'`, kind); got != 1 {
			t.Fatalf("publish policy of %s: %d auto rows, want 1", kind, got)
		}
	}
	if got := e.count(`SELECT count(*) FROM knowledge_publish_policy WHERE workspace_id = $1`); got != len(systemWriterKinds) {
		t.Fatalf("publish policy rows = %d, want only the %d system writer kinds", got, len(systemWriterKinds))
	}

	// Every role has a live agent: private to the boss, on the chosen runtime,
	// with its template's instructions and the requested model.
	for _, card := range e.data.Squads {
		for _, role := range card.Roles {
			if role.Runtime == "secretary" {
				continue
			}
			var visibility, instructions, model string
			var runtimeID pgtype.UUID
			var owner pgtype.UUID
			err := e.ke.pool.QueryRow(e.ctx,
				`SELECT visibility, instructions, COALESCE(model, ''), runtime_id, owner_id FROM agent WHERE workspace_id = $1 AND name = $2 AND archived_at IS NULL`,
				e.ws, role.Name).Scan(&visibility, &instructions, &model, &runtimeID, &owner)
			if err != nil {
				t.Fatalf("agent %q: %v", role.Name, err)
			}
			want, _ := e.data.instructions(role.AgentTemplate)
			if visibility != "private" || instructions != want || model != "test-model" || runtimeID != e.rt.ID || owner != e.user {
				t.Errorf("agent %q: visibility %q model %q runtime %v owner %v", role.Name, visibility, model, runtimeID, owner)
			}
		}
	}

	// The Librarian sits in the Executive Office, with its routines paused.
	exec := e.squadByName("Executive Office")
	librarian := e.agentNamed("Librarian")
	if !e.isMember(exec.ID, librarian.ID) {
		t.Fatal("the Librarian is not a member of the Executive Office")
	}

	e.expectRoutines(CompanyStartLeavePaused, keys)
	// Only the market scan and the Secretary's daily brief run.
	if got := e.count(`SELECT count(*) FROM autopilot WHERE workspace_id = $1 AND status = 'active'`); got != 2 {
		t.Fatalf("active autopilots = %d, want the daily brief and the market scan", got)
	}
}

func (e *companyEnv) agentNamed(name string) db.Agent {
	e.t.Helper()
	agents, err := e.ke.q.ListAllAgents(e.ctx, e.ws)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, a := range agents {
		if a.Name == name && !a.ArchivedAt.Valid {
			return a
		}
	}
	e.t.Fatalf("no agent %q", name)
	return db.Agent{}
}

func (e *companyEnv) isMember(squadID, agentID pgtype.UUID) bool {
	e.t.Helper()
	members, err := e.ke.q.ListSquadMembers(e.ctx, squadID)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, m := range members {
		if m.MemberType == "agent" && m.MemberID == agentID {
			return true
		}
	}
	return false
}

func TestCompanyApplyTwiceChangesNothing(t *testing.T) {
	e := newCompanyEnv(t)
	e.enableKnowledge()
	keys := allKeys(e.data)
	first := e.apply(keys, CompanyStartPerFloor)
	if failed := failedSteps(first); len(failed) > 0 {
		t.Fatalf("first apply failed: %+v", failed)
	}
	before := e.snapshot()

	second := e.apply(keys, CompanyStartPerFloor)
	if failed := failedSteps(second); len(failed) > 0 {
		t.Fatalf("second apply failed: %+v", failed)
	}
	for _, s := range second.Steps {
		if s.Status != CompanyStepReused {
			t.Errorf("second apply: step %s is %s (%s), want reused", s.Key, s.Status, s.Message)
		}
	}
	if len(second.Steps) != len(first.Steps) {
		t.Fatalf("second apply ran %d steps, first ran %d", len(second.Steps), len(first.Steps))
	}
	if second.PipelineID != first.PipelineID {
		t.Fatalf("pipeline changed: %v -> %v", first.PipelineID, second.PipelineID)
	}
	if after := e.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("rows changed on the second apply:\nbefore %v\nafter  %v", before, after)
	}
}

// snapshot counts the rows an apply writes, so a rerun can be checked for changes.
func (e *companyEnv) snapshot() map[string]int {
	return map[string]int{
		"agents":     e.count(`SELECT count(*) FROM agent WHERE workspace_id = $1`),
		"squads":     e.count(`SELECT count(*) FROM squad WHERE workspace_id = $1`),
		"floors":     e.count(`SELECT count(*) FROM office_floor WHERE workspace_id = $1`),
		"members":    e.count(`SELECT count(*) FROM squad_member sm JOIN squad s ON s.id = sm.squad_id WHERE s.workspace_id = $1`),
		"charters":   e.count(`SELECT count(*) FROM squad_charter WHERE workspace_id = $1`),
		"autopilots": e.count(`SELECT count(*) FROM autopilot WHERE workspace_id = $1`),
		"routines":   e.count(`SELECT count(*) FROM squad_routine WHERE workspace_id = $1`),
		"pipelines":  e.count(`SELECT count(*) FROM pipeline WHERE workspace_id = $1`),
		"kinds":      e.count(`SELECT count(*) FROM knowledge_kind WHERE workspace_id = $1`),
		"bases":      e.count(`SELECT count(*) FROM knowledge_base WHERE workspace_id = $1`),
		"policies":   e.count(`SELECT count(*) FROM knowledge_publish_policy WHERE workspace_id = $1`),
	}
}

func TestCompanyApplyCompletesAfterAnInterruptedRun(t *testing.T) {
	e := newCompanyEnv(t)
	e.enableKnowledge()
	keys := allKeys(e.data)

	// Stop the apply at a squad, then at the pipeline; each rerun resumes where
	// the last one stopped, and the last run completes the company.
	interrupt := func(key string) {
		e.svc.failStep = func(step string) error {
			if step == key {
				return companyErr("interrupted before %s", key)
			}
			return nil
		}
		res := e.apply(keys, CompanyStartLeavePaused)
		e.svc.failStep = nil
		failed := failedSteps(res)
		if len(failed) != 1 || failed[0].Key != key {
			t.Fatalf("interrupt at %s: failed steps %+v", key, failed)
		}
		if failed[0].Message != "interrupted before "+key {
			t.Fatalf("interrupt at %s: message %q", key, failed[0].Message)
		}
		if res.Steps[len(res.Steps)-1].Key != key {
			t.Fatalf("interrupt at %s: the apply ran past the failed step", key)
		}
	}
	interrupt("squad:planning")
	interrupt("pipeline")

	final := e.apply(keys, CompanyStartLeavePaused)
	if failed := failedSteps(final); len(failed) > 0 {
		t.Fatalf("the completing run failed: %+v", failed)
	}
	if !final.PipelineID.Valid {
		t.Fatal("the completing run installed no pipeline")
	}
	if got := e.count(`SELECT count(*) FROM squad WHERE workspace_id = $1 AND archived_at IS NULL`); got != len(e.data.Squads) {
		t.Fatalf("squads = %d after completion, want %d", got, len(e.data.Squads))
	}
	if got := e.count(`SELECT count(*) FROM squad_charter WHERE workspace_id = $1`); got != len(e.data.Squads) {
		t.Fatalf("charters = %d after completion, want one per squad", got)
	}
	if got := e.count(`SELECT count(*) FROM pipeline WHERE workspace_id = $1`); got != 1 {
		t.Fatalf("pipelines = %d after completion, want 1", got)
	}
	e.expectRoutines(CompanyStartLeavePaused, keys)
	// The steps the interrupted runs had finished are reused, not made again.
	statuses := stepStatuses(final)
	if statuses["squad:planning"] != CompanyStepCreated && statuses["squad:planning"] != CompanyStepReused {
		t.Fatalf("squad:planning is %q", statuses["squad:planning"])
	}
	if statuses["squad:executive_office"] != CompanyStepReused {
		t.Fatalf("squad:executive_office is %q, want reused: the office setup made it", statuses["squad:executive_office"])
	}
}

func TestCompanyStartOptionsSetTheRoutinesRunning(t *testing.T) {
	for _, start := range []string{CompanyStartAll, CompanyStartPerFloor, CompanyStartLeavePaused} {
		t.Run(start, func(t *testing.T) {
			e := newCompanyEnv(t)
			e.enableKnowledge()
			keys := allKeys(e.data)
			if res := e.apply(keys, start); len(failedSteps(res)) > 0 {
				t.Fatalf("apply failed: %+v", failedSteps(res))
			}
			e.expectRoutines(start, keys)
		})
	}
}

func TestCompanyStartAllRunsEveryRoutine(t *testing.T) {
	e := newCompanyEnv(t)
	e.enableKnowledge()
	keys := allKeys(e.data)
	if res := e.apply(keys, CompanyStartAll); len(failedSteps(res)) > 0 {
		t.Fatalf("apply failed: %+v", failedSteps(res))
	}
	if got := e.count(`SELECT count(*) FROM autopilot WHERE workspace_id = $1 AND status = 'paused' AND assignee_type = 'squad'`); got != 0 {
		t.Fatalf("%d squad routines still paused with start all", got)
	}
}

func TestCompanyRerunWithStartAllResumesPausedRoutines(t *testing.T) {
	e := newCompanyEnv(t)
	e.enableKnowledge()
	keys := allKeys(e.data)
	if res := e.apply(keys, CompanyStartLeavePaused); len(failedSteps(res)) > 0 {
		t.Fatalf("first apply failed: %+v", failedSteps(res))
	}
	res := e.apply(keys, CompanyStartAll)
	if failed := failedSteps(res); len(failed) > 0 {
		t.Fatalf("second apply failed: %+v", failed)
	}
	e.expectRoutines(CompanyStartAll, keys)
}

func TestCompanyApplyLeavesUnchosenStagesUnowned(t *testing.T) {
	e := newCompanyEnv(t)
	e.enableKnowledge()

	first := e.apply([]string{"market_research"}, CompanyStartLeavePaused)
	if failed := failedSteps(first); len(failed) > 0 {
		t.Fatalf("apply failed: %+v", failed)
	}
	if want := []string{"validate", "define", "plan", "build", "launch", "operate", "learn"}; !reflect.DeepEqual(first.UnownedStages, want) {
		t.Fatalf("unowned = %v, want every stage", first.UnownedStages)
	}

	// Choosing a stage's owner later fills it, and the report names what is left.
	second := e.apply([]string{"product_discovery"}, CompanyStartLeavePaused)
	if failed := failedSteps(second); len(failed) > 0 {
		t.Fatalf("second apply failed: %+v", failed)
	}
	if want := []string{"define", "plan", "build", "launch", "operate", "learn"}; !reflect.DeepEqual(second.UnownedStages, want) {
		t.Fatalf("unowned after choosing Product Discovery = %v, want %v", second.UnownedStages, want)
	}
	if second.PipelineID != first.PipelineID {
		t.Fatal("a second apply made a second pipeline")
	}
}

func TestCompanyApplyNeedsTheSecretaryForTheExecutiveOffice(t *testing.T) {
	e := newCompanyEnv(t)
	// A workspace whose Secretary is gone: the Executive Office cannot be built.
	if _, err := e.ke.pool.Exec(e.ctx, `UPDATE workspace SET secretary_agent_id = NULL WHERE id = $1`, e.ws); err != nil {
		t.Fatal(err)
	}
	res := e.apply([]string{"executive_office"}, CompanyStartLeavePaused)
	failed := failedSteps(res)
	if len(failed) != 1 || failed[0].Key != "agent:Secretary" {
		t.Fatalf("failed steps = %+v, want the Secretary step", failed)
	}
	if !strings.Contains(failed[0].Message, "office setup") {
		t.Fatalf("message %q does not say what to do", failed[0].Message)
	}
	if res.PipelineID.Valid {
		t.Fatal("the apply reached the pipeline after a failed step")
	}
}

func TestCompanyApplyRefusesAnUnknownSquadOrStart(t *testing.T) {
	e := newCompanyEnv(t)
	for _, tc := range []struct {
		name   string
		squads []string
		start  string
	}{
		{"unknown squad", []string{"no_such_squad"}, CompanyStartAll},
		{"no squad", nil, CompanyStartAll},
		{"duplicate squad", []string{"planning", "planning"}, CompanyStartAll},
		{"unknown start", []string{"planning"}, "sometime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.svc.Apply(e.ctx, CompanyApplyInput{WorkspaceID: e.ws, ActorUserID: e.user, Runtime: e.rt, Squads: tc.squads, Start: tc.start})
			if !errors.Is(err, ErrCompanyTemplateInvalid) {
				t.Fatalf("err = %v, want ErrCompanyTemplateInvalid", err)
			}
		})
	}
}

func TestBackfillAddsTheLibrarianToAWorkspaceSetUpBeforePhase3(t *testing.T) {
	e := newCompanyEnv(t)
	e.enableKnowledge()
	exec := e.squadByName("Executive Office")
	if e.count(`SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = 'Librarian'`) != 0 {
		t.Fatal("setup made a Librarian; the backfill has nothing to add")
	}

	changed, err := e.svc.BackfillLibrarianIn(e.ctx, []pgtype.UUID{e.ws})
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	librarian := e.agentNamed("Librarian")
	if !e.isMember(exec.ID, librarian.ID) {
		t.Fatal("the Librarian did not join the Executive Office")
	}
	if got := e.autopilotStatus("squad", exec.ID, "Librarian daily triage"); got != "paused" {
		t.Fatalf("triage status %q, want paused under the backfill's leave-paused", got)
	}
	if got := e.count(`SELECT count(*) FROM squad_routine WHERE workspace_id = $1 AND squad_id = $2`, exec.ID); got != 4 {
		t.Fatalf("executive routines = %d, want the four Librarian routines", got)
	}
	// Only the Librarian's part: no pipeline, no charter, no other squad.
	if got := e.count(`SELECT count(*) FROM pipeline WHERE workspace_id = $1`); got != 0 {
		t.Fatalf("the backfill installed %d pipelines", got)
	}
	if got := e.count(`SELECT count(*) FROM squad_charter WHERE workspace_id = $1`); got != 0 {
		t.Fatalf("the backfill saved %d charters", got)
	}

	again, err := e.svc.BackfillLibrarianIn(e.ctx, []pgtype.UUID{e.ws})
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if again != 0 {
		t.Fatalf("second backfill changed %d workspaces, want 0", again)
	}
	if n := e.count(`SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = 'Librarian'`); n != 1 {
		t.Fatalf("Librarian agents = %d after two backfills", n)
	}
}

func TestBackfillSkipsAWorkspaceWithoutAnExecutiveOffice(t *testing.T) {
	e := newCompanyEnv(t)
	e.enableKnowledge()
	if _, err := e.ke.pool.Exec(e.ctx, `UPDATE squad SET archived_at = now() WHERE workspace_id = $1 AND name = 'Executive Office'`, e.ws); err != nil {
		t.Fatal(err)
	}
	changed, err := e.svc.BackfillLibrarianIn(e.ctx, []pgtype.UUID{e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("changed = %d for a workspace with no Executive Office", changed)
	}
	if n := e.count(`SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = 'Librarian'`); n != 0 {
		t.Fatalf("made a Librarian without an Executive Office")
	}
}

func TestCompanyApplyReportsKnowledgeNotOfferedAndInstallsTheRest(t *testing.T) {
	e := newCompanyEnv(t)
	svc := NewCompanyTemplateService(e.ke.q, e.ke.pool, nil)
	res, err := svc.Apply(e.ctx, CompanyApplyInput{
		WorkspaceID: e.ws, ActorUserID: e.user, Runtime: e.rt, Squads: []string{"planning"}, Start: CompanyStartLeavePaused,
	})
	if err != nil {
		t.Fatal(err)
	}
	statuses := stepStatuses(res)
	if statuses["knowledge:kinds"] != CompanyStepCreated && statuses["knowledge:kinds"] != CompanyStepReused {
		t.Fatalf("kinds step: %q", statuses["knowledge:kinds"])
	}
	failed := failedSteps(res)
	if len(failed) != 1 || failed[0].Key != "knowledge:ventures" || !strings.Contains(failed[0].Message, "not offered") {
		t.Fatalf("failed = %+v, want the ventures step refused as not offered", failed)
	}
	if !res.PipelineID.Valid {
		t.Fatal("the pipeline was not installed before the knowledge step")
	}
}

func TestCompanyStartOptionPicksTheRoutinesItRuns(t *testing.T) {
	data, err := loadCompanyData()
	if err != nil {
		t.Fatal(err)
	}
	daily := companyRoutine{Title: "Daily", ActiveAtCreate: true}
	floor := companyRoutine{Title: "Lead", FloorStart: true}
	plain := companyRoutine{Title: "Plain"}
	cases := []struct {
		start string
		r     companyRoutine
		want  bool
	}{
		{CompanyStartAll, plain, true},
		{CompanyStartAll, daily, true},
		{CompanyStartPerFloor, floor, true},
		{CompanyStartPerFloor, plain, false},
		{CompanyStartPerFloor, daily, true},
		{CompanyStartLeavePaused, floor, false},
		{CompanyStartLeavePaused, daily, true},
		{CompanyStartLeavePaused, plain, false},
	}
	for _, c := range cases {
		if got := routineWanted(c.start, c.r); got != c.want {
			t.Errorf("routineWanted(%s, %s) = %v, want %v", c.start, c.r.Title, got, c.want)
		}
	}
	if !validCompanyStart(data, CompanyStartPerFloor) || validCompanyStart(data, "sometime") {
		t.Fatal("start options do not match the card data")
	}
}

func TestCompanyDataAgreesWithTheDefaultPipeline(t *testing.T) {
	data, err := loadCompanyData()
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for _, stage := range DefaultPipelineTemplate() {
		owners[stage.Key] = stage.OwnerKey
	}
	for _, stage := range data.StageOwners {
		if owners[stage.Key] != stage.Owner {
			t.Errorf("stage %s: card data says %s, the default pipeline says %s", stage.Key, stage.Owner, owners[stage.Key])
		}
	}
	if len(data.StageOwners) != len(owners) {
		t.Fatalf("%d stage owners in the cards, %d stages in the default pipeline", len(data.StageOwners), len(owners))
	}
}

func TestCompanyRoleNamesAreUniqueAcrossTheCards(t *testing.T) {
	data, err := loadCompanyData()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, card := range data.Squads {
		for _, role := range card.Roles {
			if role.Runtime == "secretary" {
				continue
			}
			if other, dup := seen[role.Name]; dup {
				t.Errorf("agent name %q is used by %s and %s", role.Name, other, card.Key)
			}
			seen[role.Name] = card.Key
		}
	}
	if !slices.Contains(allKeys(data), "people_performance") {
		t.Fatal("People & Performance is not in the starter company")
	}
}
