package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The starter company (spec 3.4, plan 10.18 and 10.9). Apply installs the
// squads a boss picked, in dependency order: floors, agents, squads with their
// charters and operating reviews, routines, the default pipeline, and the
// knowledge kinds and ventures base. Each step is idempotent and runs in its own
// transaction, so an apply that stops at any step is completed by running it
// again. A step that cannot finish stops the apply: the steps after it depend
// on it, and the caller reports the failed step.

// ErrCompanyTemplateInvalid means the request names a squad, start option or
// selection the starter company does not have.
var ErrCompanyTemplateInvalid = errors.New("invalid starter company request")

// CompanyTemplateService installs the starter company.
type CompanyTemplateService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	// Knowledge provisions the ventures base. Nil, or a deployment that does not
	// offer knowledge, makes the knowledge step fail and the rest install.
	Knowledge *KnowledgeService

	// failStep, when set, fails the named step before it runs. Tests use it to
	// stop an apply halfway and check that a rerun completes it.
	failStep func(key string) error
}

// NewCompanyTemplateService returns the starter company installer.
func NewCompanyTemplateService(q *db.Queries, tx TxStarter, knowledge *KnowledgeService) *CompanyTemplateService {
	return &CompanyTemplateService{Queries: q, TxStarter: tx, Knowledge: knowledge}
}

// CompanyApplyInput is an apply request the handler has checked: the actor is an
// owner or admin, and Runtime is a runtime the actor may use in the workspace.
type CompanyApplyInput struct {
	WorkspaceID pgtype.UUID
	ActorUserID pgtype.UUID
	Runtime     db.AgentRuntime
	// Model is the model for the worker agents. Empty means the runtime's default.
	Model  string
	Squads []string
	// Start is "all", "per_floor" or "leave_paused".
	Start string
}

// CompanyStep is one step of an apply. Key names the step, such as
// "squad:market_research" or "agent:Market Scout".
type CompanyStep struct {
	Key     string `json:"key"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// CompanyApplyResult is what an apply did. Steps stop at the first failed step.
// PipelineID is invalid when the apply did not reach the pipeline. The created
// rows let the caller publish them.
type CompanyApplyResult struct {
	Steps         []CompanyStep
	PipelineID    pgtype.UUID
	UnownedStages []string

	Floors     []db.OfficeFloor
	Agents     []db.Agent
	Squads     []db.Squad
	Autopilots []db.Autopilot
}

// companyStepError is a refusal the boss can act on. Its text is shown to the
// caller; any other error is logged and reported generically.
type companyStepError struct{ msg string }

func (e companyStepError) Error() string { return e.msg }

func companyErr(format string, args ...any) error {
	return companyStepError{msg: fmt.Sprintf(format, args...)}
}

// Apply installs the chosen squads of the starter company. An invalid request
// returns ErrCompanyTemplateInvalid. A failed step is not an error: it is the
// last entry of the result's steps.
func (s *CompanyTemplateService) Apply(ctx context.Context, in CompanyApplyInput) (CompanyApplyResult, error) {
	data, err := loadCompanyData()
	if err != nil {
		return CompanyApplyResult{}, err
	}
	keys, err := companySquadKeys(data, in.Squads)
	if err != nil {
		return CompanyApplyResult{}, fmt.Errorf("%w: %v", ErrCompanyTemplateInvalid, err)
	}
	if !validCompanyStart(data, in.Start) {
		return CompanyApplyResult{}, fmt.Errorf("%w: start must be all, per_floor or leave_paused", ErrCompanyTemplateInvalid)
	}
	run := newCompanyRun(s, data, in, selectedSquads(data, keys))
	run.apply(ctx)
	return run.res, nil
}

// BackfillLibrarian adds the Librarian to each knowledge-ready workspace that
// has an Executive Office. The Librarian is part of the starter company's
// Executive Office, so a workspace set up before phase 3 gets it here. Nothing
// else is installed: the pipeline, the charter and the brief stay as they are.
// It returns how many workspaces it changed.
func (s *CompanyTemplateService) BackfillLibrarian(ctx context.Context) (int, error) {
	ids, err := s.Queries.ListReadyKnowledgeWorkspaceIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list knowledge workspaces: %w", err)
	}
	return s.BackfillLibrarianIn(ctx, ids)
}

// BackfillLibrarianIn is BackfillLibrarian for the given workspaces. A workspace
// that fails is reported and the others still run.
func (s *CompanyTemplateService) BackfillLibrarianIn(ctx context.Context, workspaceIDs []pgtype.UUID) (int, error) {
	data, err := loadCompanyData()
	if err != nil {
		return 0, err
	}
	changed := 0
	var failures []error
	for _, ws := range workspaceIDs {
		owners, err := s.Queries.ListWorkspaceManagerUserIDs(ctx, ws)
		if err != nil {
			failures = append(failures, fmt.Errorf("workspace %s: list owners: %w", util.UUIDToString(ws), err))
			continue
		}
		if len(owners) == 0 {
			continue
		}
		// The backfill has no runtime choice of its own, so the Librarian runs on
		// the Secretary's runtime, the one the boss chose for the office.
		runtime, ok, err := s.secretaryRuntime(ctx, ws)
		if err != nil {
			failures = append(failures, fmt.Errorf("workspace %s: %w", util.UUIDToString(ws), err))
			continue
		}
		if !ok {
			continue
		}
		run := newCompanyRun(s, data, CompanyApplyInput{
			WorkspaceID: ws, ActorUserID: owners[0], Runtime: runtime, Start: CompanyStartLeavePaused,
		}, nil)
		if err := run.librarian(ctx); err != nil {
			failures = append(failures, fmt.Errorf("workspace %s: %w", util.UUIDToString(ws), err))
			continue
		}
		if run.changed {
			changed++
		}
	}
	return changed, errors.Join(failures...)
}

// secretaryRuntime is the runtime the workspace's Secretary runs on. ok is false
// when the workspace has no live Secretary.
func (s *CompanyTemplateService) secretaryRuntime(ctx context.Context, ws pgtype.UUID) (db.AgentRuntime, bool, error) {
	w, err := s.Queries.GetWorkspace(ctx, ws)
	if err != nil {
		return db.AgentRuntime{}, false, err
	}
	if !w.SecretaryAgentID.Valid {
		return db.AgentRuntime{}, false, nil
	}
	agent, err := s.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: w.SecretaryAgentID, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.AgentRuntime{}, false, nil
	}
	if err != nil {
		return db.AgentRuntime{}, false, err
	}
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		return db.AgentRuntime{}, false, nil
	}
	rt, err := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return db.AgentRuntime{}, false, err
	}
	return rt, true, nil
}

// companyRun holds one apply's state: the request, the cards it installs, and
// the result so far.
type companyRun struct {
	svc   *CompanyTemplateService
	data  companyData
	in    CompanyApplyInput
	start string
	cards []companySquad
	res   CompanyApplyResult
	// changed is true when a step created something. The backfill reports it.
	changed bool
}

func newCompanyRun(s *CompanyTemplateService, data companyData, in CompanyApplyInput, cards []companySquad) *companyRun {
	return &companyRun{svc: s, data: data, in: in, start: in.Start, cards: cards, res: CompanyApplyResult{UnownedStages: []string{}}}
}

// run executes one step and records it. It returns false when the step failed,
// and the caller stops.
func (r *companyRun) run(key string, fn func() (status, msg string, err error)) bool {
	var status, msg string
	var err error
	if r.svc.failStep != nil {
		err = r.svc.failStep(key)
	}
	if err == nil {
		status, msg, err = fn()
	}
	if err != nil {
		status, msg = CompanyStepFailed, r.failureMessage(key, err)
	}
	if status == CompanyStepCreated {
		r.changed = true
	}
	r.res.Steps = append(r.res.Steps, CompanyStep{Key: key, Status: status, Message: msg})
	return err == nil
}

func (r *companyRun) failureMessage(key string, err error) string {
	var refusal companyStepError
	if errors.As(err, &refusal) {
		return refusal.msg
	}
	slog.Warn("starter company: step failed", "step", key, "workspace_id", util.UUIDToString(r.in.WorkspaceID), "error", err)
	return "unexpected error; run the apply again"
}

// inTx runs fn in one transaction. Created rows are recorded by the caller
// after it returns nil, so a rolled-back step records nothing.
func (r *companyRun) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := r.svc.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(r.svc.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// apply runs every step in order, stopping at the first failure.
func (r *companyRun) apply(ctx context.Context) {
	floors := r.floorKeys()
	for _, key := range floors {
		if !r.run("floor:"+key, func() (string, string, error) { return r.ensureFloor(ctx, key) }) {
			return
		}
	}
	seenRoles := map[string]bool{}
	for _, card := range r.cards {
		for _, role := range card.Roles {
			if seenRoles[role.Name] {
				continue
			}
			seenRoles[role.Name] = true
			if !r.run("agent:"+role.Name, func() (string, string, error) { return r.ensureAgent(ctx, role) }) {
				return
			}
		}
	}
	for _, card := range r.cards {
		if !r.run("squad:"+card.Key, func() (string, string, error) { return r.ensureSquad(ctx, card) }) {
			return
		}
	}
	for _, card := range r.cards {
		if !r.run("charter:"+card.Key, func() (string, string, error) { return r.ensureCharter(ctx, card) }) {
			return
		}
	}
	for _, card := range r.cards {
		for _, routine := range companyRoutinesOf(card) {
			key := "routine:" + card.Key + ":" + routine.Title
			if !r.run(key, func() (string, string, error) { return r.ensureRoutine(ctx, card, routine) }) {
				return
			}
		}
	}
	if !r.run("pipeline", func() (string, string, error) { return r.ensurePipeline(ctx) }) {
		return
	}
	kn := r.knowledge()
	if !r.run("knowledge:kinds", func() (string, string, error) { return r.ensureKinds(ctx, kn) }) {
		return
	}
	r.run("knowledge:ventures", func() (string, string, error) { return r.ensureVentures(ctx) })
	r.run("knowledge:policies", func() (string, string, error) { return r.ensurePublishPolicies(ctx) })
}

// librarian runs the Librarian's part of the starter company for the Executive
// Office: its agent, its membership and its routines. It changes nothing when the
// workspace has no Executive Office squad yet.
func (r *companyRun) librarian(ctx context.Context) error {
	executive, ok := r.data.squad("executive_office")
	if !ok {
		return fmt.Errorf("the starter company has no executive_office card")
	}
	exists, err := r.squadExists(ctx, executive.Name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	var librarian companyRole
	for _, role := range executive.Roles {
		if role.Key == "librarian" {
			librarian = role
		}
	}
	if librarian.Name == "" {
		return fmt.Errorf("the executive_office card has no librarian role")
	}
	r.cards = []companySquad{executive}
	if !r.run("agent:"+librarian.Name, func() (string, string, error) { return r.ensureAgent(ctx, librarian) }) {
		return r.failure()
	}
	if !r.run("squad:"+executive.Key, func() (string, string, error) { return r.ensureSquad(ctx, executive) }) {
		return r.failure()
	}
	for _, routine := range executive.Routines {
		if !strings.HasPrefix(routine.Key, "librarian_") {
			continue
		}
		key := "routine:" + executive.Key + ":" + routine.Title
		if !r.run(key, func() (string, string, error) { return r.ensureRoutine(ctx, executive, routine) }) {
			return r.failure()
		}
	}
	return nil
}

// squadExists reports whether a live squad with the name exists.
func (r *companyRun) squadExists(ctx context.Context, name string) (bool, error) {
	_, found, err := r.squadNamed(ctx, r.svc.Queries, name)
	return found, err
}

// failure is the error of the last failed step, for callers that stop on one.
func (r *companyRun) failure() error {
	last := r.res.Steps[len(r.res.Steps)-1]
	return fmt.Errorf("step %s: %s", last.Key, last.Message)
}

// floorKeys lists the floors the chosen squads sit on, in floor order.
func (r *companyRun) floorKeys() []string {
	used := map[string]bool{}
	for _, card := range r.cards {
		used[card.Floor] = true
	}
	var out []string
	for _, key := range r.data.FloorKeys {
		if used[key] {
			out = append(out, key)
		}
	}
	return out
}

// knowledge returns the knowledge service the kinds step seeds with. A deployment
// without knowledge still has the kinds table, so the kinds are seeded either way.
func (r *companyRun) knowledge() *KnowledgeService {
	if r.svc.Knowledge != nil {
		return r.svc.Knowledge
	}
	return &KnowledgeService{Queries: r.svc.Queries}
}

func (r *companyRun) ensureFloor(ctx context.Context, key string) (string, string, error) {
	name := r.data.FloorNames[key]
	var created *db.OfficeFloor
	status, msg := CompanyStepReused, "the floor exists"
	err := r.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockOfficeLayout(ctx, r.in.WorkspaceID); err != nil {
			return err
		}
		floors, err := q.ListOfficeFloors(ctx, r.in.WorkspaceID)
		if err != nil {
			return err
		}
		for _, f := range floors {
			if f.Name == name {
				return nil
			}
		}
		if len(floors) >= OfficeMaxFloors {
			return companyErr("a workspace can have at most %d floors", OfficeMaxFloors)
		}
		floor, err := q.CreateOfficeFloor(ctx, db.CreateOfficeFloorParams{WorkspaceID: r.in.WorkspaceID, Name: name})
		if err != nil {
			return err
		}
		created = &floor
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if created != nil {
		r.res.Floors = append(r.res.Floors, *created)
		status, msg = CompanyStepCreated, "created the floor "+name
	}
	return status, msg, nil
}

// liveAgentFor returns the agent that fills a role. The secretary runtime is the
// workspace's Secretary; any other role is the agent with the role's name. found
// is false when none exists; live is false when the one found is archived.
func (r *companyRun) liveAgentFor(ctx context.Context, q *db.Queries, role companyRole) (agent db.Agent, found bool, err error) {
	if role.Runtime == "secretary" {
		ws, err := q.GetWorkspace(ctx, r.in.WorkspaceID)
		if err != nil {
			return db.Agent{}, false, err
		}
		if !ws.SecretaryAgentID.Valid {
			return db.Agent{}, false, nil
		}
		a, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: ws.SecretaryAgentID, WorkspaceID: r.in.WorkspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Agent{}, false, nil
		}
		if err != nil {
			return db.Agent{}, false, err
		}
		return a, !a.ArchivedAt.Valid, nil
	}
	agents, err := q.ListAllAgents(ctx, r.in.WorkspaceID)
	if err != nil {
		return db.Agent{}, false, err
	}
	for _, a := range agents {
		if a.Name == role.Name {
			return a, !a.ArchivedAt.Valid, nil
		}
	}
	return db.Agent{}, false, nil
}

// ensureAgent creates the agent of a role, private to the boss and given the
// knowledge MCP server, unless a live agent with the role's name exists.
func (r *companyRun) ensureAgent(ctx context.Context, role companyRole) (string, string, error) {
	existing, found, err := r.liveAgentFor(ctx, r.svc.Queries, role)
	if err != nil {
		return "", "", err
	}
	if role.Runtime == "secretary" {
		if !found || existing.ArchivedAt.Valid {
			return "", "", companyErr("the Secretary is not set up; run the office setup first")
		}
		return CompanyStepReused, "the Secretary of the office", nil
	}
	if found {
		if existing.ArchivedAt.Valid {
			return "", "", companyErr("an archived agent named %q exists; restore it or rename it", role.Name)
		}
		return CompanyStepReused, "already in the workspace", nil
	}
	instructions, ok := r.data.instructions(role.AgentTemplate)
	if !ok {
		return "", "", companyErr("no instruction template %q", role.AgentTemplate)
	}
	var agent db.Agent
	err = r.inTx(ctx, func(q *db.Queries) error {
		a, err := q.CreateAgent(ctx, db.CreateAgentParams{
			WorkspaceID:          r.in.WorkspaceID,
			Name:                 role.Name,
			Description:          role.Description,
			Instructions:         instructions,
			RuntimeMode:          r.in.Runtime.RuntimeMode,
			RuntimeConfig:        []byte("{}"),
			RuntimeID:            r.in.Runtime.ID,
			Visibility:           "private",
			PermissionMode:       "private",
			MaxConcurrentTasks:   1,
			OwnerID:              r.in.ActorUserID,
			CustomEnv:            []byte("{}"),
			CustomArgs:           []byte("[]"),
			ConversationStarters: []byte("[]"),
			Model:                pgtype.Text{String: r.in.Model, Valid: r.in.Model != ""},
		})
		if err != nil {
			return err
		}
		if err := q.AssignKnowledgeMcpToAgent(ctx, db.AssignKnowledgeMcpToAgentParams{AgentID: a.ID, WorkspaceID: r.in.WorkspaceID}); err != nil {
			return err
		}
		agent = a
		return nil
	})
	if err != nil {
		return "", "", err
	}
	r.res.Agents = append(r.res.Agents, agent)
	return CompanyStepCreated, "created " + role.Name, nil
}

func (r *companyRun) squadNamed(ctx context.Context, q *db.Queries, name string) (db.Squad, bool, error) {
	squads, err := q.ListSquads(ctx, r.in.WorkspaceID)
	if err != nil {
		return db.Squad{}, false, err
	}
	for _, s := range squads {
		if s.Name == name && !s.ArchivedAt.Valid {
			return s, true, nil
		}
	}
	return db.Squad{}, false, nil
}

// ensureSquad creates the squad, with its leader and members, and seats it on
// its floor. A squad with the card's name is reused: a member it lacks is added,
// and a squad with no seat takes one.
func (r *companyRun) ensureSquad(ctx context.Context, card companySquad) (string, string, error) {
	var squad db.Squad
	created := false
	err := r.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockOfficeLayout(ctx, r.in.WorkspaceID); err != nil {
			return err
		}
		leaderRole, ok := companyRoleNamed(card, card.Leader)
		if !ok {
			return fmt.Errorf("card %s has no leader role", card.Key)
		}
		leader, found, err := r.liveAgentFor(ctx, q, leaderRole)
		if err != nil {
			return err
		}
		if !found || leader.ArchivedAt.Valid {
			return companyErr("the leader of %s is missing; its agent step did not finish", card.Name)
		}
		squad, found, err = r.squadNamed(ctx, q, card.Name)
		if err != nil {
			return err
		}
		if !found {
			squad, err = q.CreateSquad(ctx, db.CreateSquadParams{
				WorkspaceID: r.in.WorkspaceID,
				Name:        card.Name,
				Description: card.Description,
				LeaderID:    leader.ID,
				CreatorID:   r.in.ActorUserID,
				AvatarUrl:   pgtype.Text{},
			})
			if err != nil {
				return err
			}
			created = true
		}

		members, err := q.ListSquadMembers(ctx, squad.ID)
		if err != nil {
			return err
		}
		inSquad := map[string]bool{}
		for _, m := range members {
			if m.MemberType == "agent" {
				inSquad[util.UUIDToString(m.MemberID)] = true
			}
		}
		for _, role := range card.Roles {
			agent, found, err := r.liveAgentFor(ctx, q, role)
			if err != nil {
				return err
			}
			if !found || agent.ArchivedAt.Valid {
				return companyErr("the %s agent of %s is missing; its agent step did not finish", role.Name, card.Name)
			}
			if inSquad[util.UUIDToString(agent.ID)] {
				continue
			}
			memberRole := "member"
			if role.Key == card.Leader {
				memberRole = "leader"
			}
			if _, err := q.AddSquadMember(ctx, db.AddSquadMemberParams{
				SquadID: squad.ID, MemberType: "agent", MemberID: agent.ID, Role: memberRole,
			}); err != nil {
				return err
			}
			// The agent's first squad becomes its home. Any other membership keeps it.
			if _, err := q.SetAgentHomeSquadOnFirstMembership(ctx, db.SetAgentHomeSquadOnFirstMembershipParams{
				ID: agent.ID, WorkspaceID: r.in.WorkspaceID, SquadID: squad.ID,
			}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}

		if !squad.FloorID.Valid {
			floor, err := r.floorNamed(ctx, q, card.Floor)
			if err != nil {
				return err
			}
			slot, err := freeOfficeSeat(ctx, q, r.in.WorkspaceID, floor.ID)
			if errors.Is(err, ErrExecutiveFloorFull) {
				return companyErr("the %s floor has no free seat", floor.Name)
			}
			if err != nil {
				return err
			}
			squad, err = q.SetSquadSeat(ctx, db.SetSquadSeatParams{
				ID:          squad.ID,
				WorkspaceID: r.in.WorkspaceID,
				FloorID:     floor.ID,
				OfficeSlot:  pgtype.Int2{Int16: slot, Valid: true},
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if created {
		r.res.Squads = append(r.res.Squads, squad)
		return CompanyStepCreated, "created " + card.Name, nil
	}
	return CompanyStepReused, "the squad exists", nil
}

func (r *companyRun) floorNamed(ctx context.Context, q *db.Queries, key string) (db.OfficeFloor, error) {
	name := r.data.FloorNames[key]
	floors, err := q.ListOfficeFloors(ctx, r.in.WorkspaceID)
	if err != nil {
		return db.OfficeFloor{}, err
	}
	for _, f := range floors {
		if f.Name == name {
			return f, nil
		}
	}
	return db.OfficeFloor{}, companyErr("the %s floor is missing", name)
}

func companyRoleNamed(card companySquad, key string) (companyRole, bool) {
	for _, role := range card.Roles {
		if role.Key == key {
			return role, true
		}
	}
	return companyRole{}, false
}

// companyRoutinesOf lists the routines of a card, with the squad's operating
// review last. A card that lists the review sets its flags; the rest get a
// paused review, which the charter path makes for every squad.
func companyRoutinesOf(card companySquad) []companyRoutine {
	out := append([]companyRoutine(nil), card.Routines...)
	for _, r := range out {
		if r.Title == OperatingReviewTitle {
			return out
		}
	}
	return append(out, companyRoutine{Key: "operating_review", Title: OperatingReviewTitle})
}

// charterJSON is the charter's list columns, as the charter handler stores them.
type charterResponsibilityJSON struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

type charterOutputJSON struct {
	Key         string `json:"key"`
	Kind        string `json:"kind"`
	KBKey       string `json:"kb_key"`
	CadenceHint string `json:"cadence_hint"`
}

// ensureCharter saves the card's charter the first time, and its first save
// creates the operating review, as the charter handler does. A squad that has a
// charter keeps it.
func (r *companyRun) ensureCharter(ctx context.Context, card companySquad) (string, string, error) {
	var review db.Autopilot
	created := false
	err := r.inTx(ctx, func(q *db.Queries) error {
		squad, found, err := r.squadNamed(ctx, q, card.Name)
		if err != nil {
			return err
		}
		if !found {
			return companyErr("the squad %s is missing; its squad step did not finish", card.Name)
		}
		_, err = q.GetSquadCharter(ctx, db.GetSquadCharterParams{SquadID: squad.ID, WorkspaceID: r.in.WorkspaceID})
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		responsibilities := make([]charterResponsibilityJSON, 0, len(card.Charter.Responsibilities))
		for _, item := range card.Charter.Responsibilities {
			responsibilities = append(responsibilities, charterResponsibilityJSON{Key: item.Key, Text: item.Text})
		}
		outputs := make([]charterOutputJSON, 0, len(card.Charter.Outputs))
		for _, item := range card.Charter.Outputs {
			outputs = append(outputs, charterOutputJSON{Key: item.Key, Kind: item.Kind, CadenceHint: item.CadenceHint})
		}
		responsibilitiesJSON, err := json.Marshal(responsibilities)
		if err != nil {
			return err
		}
		outputsJSON, err := json.Marshal(outputs)
		if err != nil {
			return err
		}
		reviewRoutine, _ := companyRoutineTitled(card, OperatingReviewTitle)
		charter, err := q.InsertSquadCharter(ctx, db.InsertSquadCharterParams{
			SquadID:                squad.ID,
			WorkspaceID:            r.in.WorkspaceID,
			Mission:                card.Charter.Mission,
			Responsibilities:       responsibilitiesJSON,
			Outputs:                outputsJSON,
			Boundaries:             card.Charter.Boundaries,
			Escalation:             card.Charter.Escalation,
			Intake:                 card.Charter.Intake,
			ResponseHours:          CharterDefaultResponseHours,
			OperatingReviewEnabled: routineWanted(r.start, reviewRoutine),
			UpdatedByType:          "member",
			UpdatedByID:            r.in.ActorUserID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// A concurrent first save wrote it. Its review is its own.
			return nil
		}
		if err != nil {
			return err
		}
		tz := companyTimezone(ctx, q, r.in.ActorUserID)
		review, err = CreateOperatingReview(ctx, q, squad, charter, r.in.ActorUserID, tz)
		if err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if !created {
		return CompanyStepReused, "the charter exists", nil
	}
	r.res.Autopilots = append(r.res.Autopilots, review)
	return CompanyStepCreated, "saved the charter and its operating review", nil
}

// CharterDefaultResponseHours is the response window a charter starts with (2.1).
const CharterDefaultResponseHours = 48

func companyRoutineTitled(card companySquad, title string) (companyRoutine, bool) {
	for _, r := range companyRoutinesOf(card) {
		if r.Title == title {
			return r, true
		}
	}
	return companyRoutine{}, false
}

// companyTimezone is the actor's saved timezone, or UTC, as the routine handler
// resolves it.
func companyTimezone(ctx context.Context, q *db.Queries, userID pgtype.UUID) string {
	user, err := q.GetUser(ctx, userID)
	if err == nil && user.Timezone.Valid && ValidateTimezone(user.Timezone.String) == nil {
		return user.Timezone.String
	}
	return "UTC"
}

// ensureRoutine makes sure a routine exists and runs when its start option wants
// it running. A routine that exists is reused, and a paused one is resumed when
// the start option wants it running. A start option never pauses a routine.
func (r *companyRun) ensureRoutine(ctx context.Context, card companySquad, routine companyRoutine) (string, string, error) {
	wanted := routineWanted(r.start, routine)
	if card.Key == "executive_office" && (routine.Title == DailyBriefTitle || routine.Title == WeeklyReviewTitle) {
		return r.ensureBrief(ctx, routine.Title, wanted)
	}
	var created *db.Autopilot
	resumed := false
	err := r.inTx(ctx, func(q *db.Queries) error {
		squad, found, err := r.squadNamed(ctx, q, card.Name)
		if err != nil {
			return err
		}
		if !found {
			return companyErr("the squad %s is missing; its squad step did not finish", card.Name)
		}
		if routine.Title == OperatingReviewTitle {
			return r.ensureOperatingReview(ctx, q, squad, wanted, &resumed, &created)
		}
		ap, found, err := findAutopilotTitled(ctx, q, r.in.WorkspaceID, "squad", squad.ID, routine.Title)
		if err != nil {
			return err
		}
		if found {
			if err := linkRoutine(ctx, q, r.in.WorkspaceID, squad, ap, routine.Title); err != nil {
				return err
			}
			_, changed, err := r.activateIfWanted(ctx, q, ap, wanted)
			if err != nil {
				return err
			}
			resumed = changed
			return nil
		}
		status := "paused"
		if wanted {
			status = "active"
		}
		tz := companyTimezone(ctx, q, r.in.ActorUserID)
		next, err := ComputeNextRun(routine.Cron, tz)
		if err != nil {
			return companyErr("the schedule of %q is invalid: %v", routine.Title, err)
		}
		ap, err = CreateSquadScheduledAutopilot(ctx, q, SquadScheduledAutopilot{
			WorkspaceID: r.in.WorkspaceID,
			SquadID:     squad.ID,
			ActorUserID: r.in.ActorUserID,
			Title:       routine.Title,
			Description: pgtype.Text{String: routine.Description, Valid: true},
			Status:      status,
			Cron:        routine.Cron,
			Timezone:    tz,
			NextRun:     next,
		})
		if err != nil {
			return err
		}
		if err := insertRoutineRow(ctx, q, r.in.WorkspaceID, squad.ID, ap.ID, routine.Title); err != nil {
			return err
		}
		created = &ap
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return r.routineOutcome(created, resumed, routine.Title, wanted)
}

// ensureOperatingReview reuses the squad's review and starts it when wanted. A
// squad with a charter but no review gets one here.
func (r *companyRun) ensureOperatingReview(ctx context.Context, q *db.Queries, squad db.Squad, wanted bool, resumed *bool, created **db.Autopilot) error {
	routine, err := q.GetOperatingReviewRoutine(ctx, db.GetOperatingReviewRoutineParams{
		SquadID: squad.ID, WorkspaceID: r.in.WorkspaceID, Purpose: OperatingReviewPurpose,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		charter, err := q.GetSquadCharter(ctx, db.GetSquadCharterParams{SquadID: squad.ID, WorkspaceID: r.in.WorkspaceID})
		if err != nil {
			return companyErr("the charter of %s is missing; its charter step did not finish", squad.Name)
		}
		charter.OperatingReviewEnabled = wanted
		ap, err := CreateOperatingReview(ctx, q, squad, charter, r.in.ActorUserID, companyTimezone(ctx, q, r.in.ActorUserID))
		if err != nil {
			return err
		}
		*created = &ap
		return nil
	}
	if err != nil {
		return err
	}
	ap, err := q.GetAutopilotInWorkspace(ctx, db.GetAutopilotInWorkspaceParams{ID: routine.AutopilotID, WorkspaceID: r.in.WorkspaceID})
	if err != nil {
		return err
	}
	_, changed, err := r.activateIfWanted(ctx, q, ap, wanted)
	if err != nil {
		return err
	}
	*resumed = changed
	return nil
}

// ensureBrief makes sure one of the Secretary's brief autopilots exists, with the
// schedule and description the office setup gives it, and runs when wanted. The
// setup makes the daily brief active and the weekly review paused.
func (r *companyRun) ensureBrief(ctx context.Context, title string, wanted bool) (string, string, error) {
	var created *db.Autopilot
	resumed := false
	err := r.inTx(ctx, func(q *db.Queries) error {
		ws, err := q.GetWorkspace(ctx, r.in.WorkspaceID)
		if err != nil {
			return err
		}
		secretary, found, err := r.liveAgentFor(ctx, q, companyRole{Runtime: "secretary"})
		if err != nil {
			return err
		}
		if !found || !ws.SecretaryAgentID.Valid || secretary.ArchivedAt.Valid {
			return companyErr("the Secretary is not set up; run the office setup first")
		}
		ap, found, err := findAutopilotTitled(ctx, q, r.in.WorkspaceID, "agent", secretary.ID, title)
		if err != nil {
			return err
		}
		if found {
			_, changed, err := r.activateIfWanted(ctx, q, ap, wanted)
			if err != nil {
				return err
			}
			resumed = changed
			return nil
		}
		in := OfficeSetupInput{WorkspaceID: r.in.WorkspaceID, Actor: db.Member{UserID: r.in.ActorUserID}}
		tz := companyTimezone(ctx, q, r.in.ActorUserID)
		switch title {
		case DailyBriefTitle:
			created, err = ensureBriefAutopilot(ctx, q, in, secretary.ID, DailyBriefTitle, DailyBriefDescription, "active", DailyBriefCron, tz)
		default:
			created, err = ensureBriefAutopilot(ctx, q, in, secretary.ID, WeeklyReviewTitle, WeeklyReviewDescription, "paused", WeeklyReviewCron, tz)
		}
		if err != nil {
			return err
		}
		if created == nil {
			return companyErr("the %s autopilot was not created", title)
		}
		// The weekly review is created paused; a start option that wants it running resumes it.
		if updated, changed, err := r.activateIfWanted(ctx, q, *created, wanted); err != nil {
			return err
		} else if changed {
			created = updated
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return r.routineOutcome(created, resumed, title, wanted)
}

// routineOutcome names what a routine step did.
func (r *companyRun) routineOutcome(created *db.Autopilot, resumed bool, title string, wanted bool) (string, string, error) {
	if created != nil {
		r.res.Autopilots = append(r.res.Autopilots, *created)
		state := "paused"
		if created.Status == "active" {
			state = "active"
		}
		return CompanyStepCreated, fmt.Sprintf("created %s, %s", title, state), nil
	}
	if resumed {
		return CompanyStepReused, fmt.Sprintf("%s was paused; resumed it", title), nil
	}
	if wanted {
		return CompanyStepReused, fmt.Sprintf("%s exists and runs", title), nil
	}
	return CompanyStepReused, fmt.Sprintf("%s exists", title), nil
}

// activateIfWanted resumes a paused autopilot when the start option wants it
// running. It reports whether it changed the autopilot.
func (r *companyRun) activateIfWanted(ctx context.Context, q *db.Queries, ap db.Autopilot, wanted bool) (*db.Autopilot, bool, error) {
	if !wanted || ap.Status != "paused" {
		return nil, false, nil
	}
	updated, err := q.UpdateAutopilot(ctx, db.UpdateAutopilotParams{
		ID:                 ap.ID,
		Status:             pgtype.Text{String: "active", Valid: true},
		IssueTitleTemplate: ap.IssueTitleTemplate,
		ProjectID:          ap.ProjectID,
	})
	if err != nil {
		return nil, false, err
	}
	if err := RecordAutopilotRuleVersion(ctx, q, updated, "member", r.in.ActorUserID); err != nil {
		return nil, false, err
	}
	return &updated, true, nil
}

// findAutopilotTitled returns the live autopilot of an assignee with a title.
func findAutopilotTitled(ctx context.Context, q *db.Queries, ws pgtype.UUID, assigneeType string, assigneeID pgtype.UUID, title string) (db.Autopilot, bool, error) {
	rows, err := q.ListAutopilots(ctx, db.ListAutopilotsParams{WorkspaceID: ws})
	if err != nil {
		return db.Autopilot{}, false, err
	}
	for _, row := range rows {
		ap := row.Autopilot
		if ap.AssigneeType == assigneeType && ap.AssigneeID == assigneeID && ap.Title == title && ap.Status != "archived" {
			return ap, true, nil
		}
	}
	return db.Autopilot{}, false, nil
}

// linkRoutine makes an autopilot a routine of the squad if it is not one yet.
func linkRoutine(ctx context.Context, q *db.Queries, ws pgtype.UUID, squad db.Squad, ap db.Autopilot, purpose string) error {
	_, err := q.GetSquadRoutine(ctx, db.GetSquadRoutineParams{AutopilotID: ap.ID, SquadID: squad.ID, WorkspaceID: ws})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return insertRoutineRow(ctx, q, ws, squad.ID, ap.ID, purpose)
}

// insertRoutineRow records a squad routine with the defaults the routine handler
// uses for a routine created from a cadence.
func insertRoutineRow(ctx context.Context, q *db.Queries, ws, squadID, autopilotID pgtype.UUID, purpose string) error {
	_, err := q.InsertSquadRoutine(ctx, db.InsertSquadRoutineParams{
		AutopilotID:    autopilotID,
		WorkspaceID:    ws,
		SquadID:        squadID,
		Purpose:        purpose,
		ExpectedOutput: "none",
		Required:       true,
		GraceHours:     routineDefaultGraceHours,
	})
	return err
}

// ensurePipeline installs the default pipeline with the chosen squads as stage
// owners. A stage whose squad was not chosen stays unowned and is reported.
func (r *companyRun) ensurePipeline(ctx context.Context) (string, string, error) {
	chosen := map[string]pgtype.UUID{}
	for _, card := range r.cards {
		squad, found, err := r.squadNamed(ctx, r.svc.Queries, card.Name)
		if err != nil {
			return "", "", err
		}
		if found {
			chosen[card.Key] = squad.ID
		}
	}
	resolve := func(_ context.Context, key string) (pgtype.UUID, bool, error) {
		id, ok := chosen[key]
		return id, ok, nil
	}
	res, err := NewPipelineService(r.svc.Queries, r.svc.TxStarter).InstallDefault(ctx, InstallPipelineParams{
		WorkspaceID:  r.in.WorkspaceID,
		ResolveSquad: resolve,
	})
	if err != nil {
		return "", "", err
	}
	r.res.PipelineID = res.Pipeline.ID
	r.res.UnownedStages = append([]string{}, res.UnownedStages...)
	if res.Created {
		return CompanyStepCreated, "created the default pipeline", nil
	}
	return CompanyStepReused, "the default pipeline exists", nil
}

// ensureKinds seeds the default knowledge kinds the workspace lacks. Existing
// kinds, with any admin edits, are left as they are.
func (r *companyRun) ensureKinds(ctx context.Context, kn *KnowledgeService) (string, string, error) {
	before, err := kn.Queries.ListKnowledgeKinds(ctx, r.in.WorkspaceID)
	if err != nil {
		return "", "", err
	}
	if err := kn.SeedKnowledgeKinds(ctx, r.in.WorkspaceID); err != nil {
		return "", "", err
	}
	after, err := kn.Queries.ListKnowledgeKinds(ctx, r.in.WorkspaceID)
	if err != nil {
		return "", "", err
	}
	if len(after) > len(before) {
		return CompanyStepCreated, fmt.Sprintf("added %d knowledge kinds", len(after)-len(before)), nil
	}
	return CompanyStepReused, "the knowledge kinds exist", nil
}

// ensureVentures provisions knowledge for the workspace, which creates the
// ventures base with the others. A workspace whose knowledge is ready is left as it is.
func (r *companyRun) ensureVentures(ctx context.Context) (string, string, error) {
	kn := r.svc.Knowledge
	if kn == nil {
		return "", "", companyErr("knowledge is not offered on this deployment")
	}
	if ok, reason := kn.offeredReason(); !ok {
		return "", "", companyErr("knowledge is not offered on this deployment (%s)", reason)
	}
	if kn.Ready(ctx, r.in.WorkspaceID) {
		return CompanyStepReused, "the knowledge base is ready", nil
	}
	status, err := kn.Enable(ctx, r.in.WorkspaceID, r.in.ActorUserID)
	if errors.Is(err, ErrKnowledgeBusy) {
		return "", "", companyErr("knowledge provisioning is already running; run the apply again")
	}
	if err != nil {
		return "", "", err
	}
	if status.State != KnowledgeStateReady {
		return "", "", companyErr("knowledge is %s; run the apply again", status.State)
	}
	return CompanyStepCreated, "provisioned the knowledge base and its ventures base", nil
}

// systemWriterKinds are the kinds Multica writes itself: a decision record when
// the boss decides, a brief archive when the Secretary publishes a brief. Their
// publish policy is auto by design (spec 3.4). Every other kind keeps the
// default manual policy, so the step does not write those rows.
var systemWriterKinds = []string{"decision_record", "brief_archive"}

// ensurePublishPolicies saves the auto policy of the system writer kinds. It runs
// after the kinds step, which creates the kinds the policies name. A kind already
// saved as auto is left as it is, so a rerun changes nothing.
func (r *companyRun) ensurePublishPolicies(ctx context.Context) (string, string, error) {
	kn := r.knowledge()
	existing, err := kn.ListPublishPolicies(ctx, r.in.WorkspaceID)
	if err != nil {
		return "", "", err
	}
	autoSaved := map[string]bool{}
	for _, p := range existing {
		if p.Scope == PublishScopeKind && p.Mode == PublishModeAuto {
			autoSaved[p.Key] = true
		}
	}
	saved := 0
	for _, kind := range systemWriterKinds {
		if autoSaved[kind] {
			continue
		}
		if _, err := kn.SetPublishPolicy(ctx, r.in.WorkspaceID, r.in.ActorUserID, PublishScopeKind, kind, PublishModeAuto); err != nil {
			return "", "", err
		}
		saved++
	}
	if saved > 0 {
		return CompanyStepCreated, fmt.Sprintf("set %d system kinds to publish automatically", saved), nil
	}
	return CompanyStepReused, "the publish policies exist", nil
}
