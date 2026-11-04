package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Import recreates an export in a workspace (spec 4.9). It is idempotent: every
// entity is matched by name (a knowledge kind by key, a goal by scope and title,
// a budget by scope and period) first, so a second import changes nothing. The
// same plan drives the dry run and the apply, so the report is what the apply
// does. An apply runs in one transaction; the writes go through the service
// functions that own each entity, bound to that transaction.
//
// What the import creates: floors, agents (on the runtime the caller names),
// squads with leader, members, seat, charter and autonomy level, routines
// (paused), home squads, floor chiefs, pipelines, initiatives (quiet: no handoff,
// no wake), knowledge kinds, goals with their key results (a squad goal as a
// draft), budgets and standing approval policies (a revoked one revoked). An
// archived squad is archived last. It never creates secrets, runtimes, documents
// or history, and it does not overwrite what the workspace already has.

// ImportAction is what the import does with one entity.
const (
	ImportCreate  = "create"
	ImportExists  = "exists"
	ImportBlocked = "blocked"
	ImportSkipped = "skipped"
)

// ImportItem is one entity's line in the report.
type ImportItem struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// ImportReport is the answer of a dry run or an apply.
type ImportReport struct {
	DryRun bool         `json:"dry_run"`
	Items  []ImportItem `json:"items"`
	// Created, Existing, Blocked and Skipped count the items by action.
	Created  int `json:"created"`
	Existing int `json:"existing"`
	Blocked  int `json:"blocked"`
	Skipped  int `json:"skipped"`
}

// ImportOptions says how to import.
type ImportOptions struct {
	DryRun bool
	// RuntimeID is the runtime the created agents run on. Agents cannot be created
	// without one; a dry run without it reports them as blocked.
	RuntimeID pgtype.UUID
	// ActorID is the member doing the import: the owner of created agents, the
	// creator of created squads and the owner of imported member goals.
	ActorID pgtype.UUID
	// Kinds says which proposal kinds a standing approval policy may name. Nil
	// refuses every policy.
	Kinds *proposalkinds.Registry
	// Issues backs the initiatives the import creates: each one gets its backing
	// issue, as an initiative does. Nil refuses an export that carries initiatives.
	Issues *IssueService
}

// importPlaceholderID stands in for an entity a dry run would create. It has the
// shape of a UUID so validation that checks shape passes; nothing is written with it.
const importPlaceholderID = "00000000-0000-0000-0000-000000000000"

// importAutonomyReason is the governance entry of an autonomy level an import sets.
const importAutonomyReason = "Set by the company import."

func (r *ImportReport) add(kind, name, action, reason string) {
	r.Items = append(r.Items, ImportItem{Kind: kind, Name: name, Action: action, Reason: reason})
	switch action {
	case ImportCreate:
		r.Created++
	case ImportExists:
		r.Existing++
	case ImportBlocked:
		r.Blocked++
	case ImportSkipped:
		r.Skipped++
	}
}

// ImportCompany imports doc into the workspace. A dry run writes nothing. An
// apply runs in one transaction: a failure leaves the workspace as it was.
func ImportCompany(ctx context.Context, q *db.Queries, txStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}, workspaceID pgtype.UUID, doc CompanyExport, opts ImportOptions) (ImportReport, error) {
	if err := ValidateCompanyExport(doc); err != nil {
		return ImportReport{}, fmt.Errorf("%w", err)
	}
	report := ImportReport{DryRun: opts.DryRun, Items: []ImportItem{}}
	if opts.DryRun {
		if err := importPlan(ctx, q, nil, workspaceID, doc, opts, &report); err != nil {
			return ImportReport{}, err
		}
		return report, nil
	}
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return ImportReport{}, fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := importPlan(ctx, q.WithTx(tx), tx, workspaceID, doc, opts, &report); err != nil {
		return ImportReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ImportReport{}, fmt.Errorf("commit import: %w", err)
	}
	return report, nil
}

// importPlan walks the entities in dependency order. With opts.DryRun false it
// also writes through q, inside tx. In a dry run the entities it would create are
// recorded by name only, so the entities that depend on them are planned as well.
func importPlan(ctx context.Context, q *db.Queries, tx pgx.Tx, workspaceID pgtype.UUID, doc CompanyExport, opts ImportOptions, report *ImportReport) error {
	write := !opts.DryRun
	actor := GovernanceActor{Type: GovernanceActorMember, ID: opts.ActorID}

	// Floors.
	floors, err := q.ListOfficeFloors(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("import: list floors: %w", err)
	}
	floorByName := map[string]pgtype.UUID{}
	floorNameByID := map[string]string{}
	for _, f := range floors {
		floorByName[f.Name] = f.ID
		floorNameByID[util.UUIDToString(f.ID)] = f.Name
	}
	if write {
		if err := q.LockOfficeLayout(ctx, workspaceID); err != nil {
			return fmt.Errorf("import: lock office layout: %w", err)
		}
	}
	for _, f := range doc.Floors {
		if _, ok := floorByName[f.Name]; ok {
			report.add("floor", f.Name, ImportExists, "")
			continue
		}
		if !write {
			floorByName[f.Name] = pgtype.UUID{}
			report.add("floor", f.Name, ImportCreate, "")
			continue
		}
		created, err := q.CreateOfficeFloor(ctx, db.CreateOfficeFloorParams{WorkspaceID: workspaceID, Name: f.Name})
		if err != nil {
			return fmt.Errorf("import: create floor %q: %w", f.Name, err)
		}
		floorByName[f.Name] = created.ID
		report.add("floor", f.Name, ImportCreate, "")
	}

	// Agents. Created on the runtime the caller names; matched by name otherwise.
	agents, err := q.ListAgents(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("import: list agents: %w", err)
	}
	agentByName := map[string]db.Agent{}
	for _, a := range agents {
		agentByName[a.Name] = a
	}
	for _, a := range doc.Agents {
		if _, ok := agentByName[a.Name]; ok {
			report.add("agent", a.Name, ImportExists, "")
			continue
		}
		if !opts.RuntimeID.Valid {
			report.add("agent", a.Name, ImportBlocked, "agents need a runtime: pass the runtime the agents should run on")
			continue
		}
		if !write {
			agentByName[a.Name] = db.Agent{Name: a.Name}
			report.add("agent", a.Name, ImportCreate, "")
			continue
		}
		created, err := createImportedAgent(ctx, q, workspaceID, opts, a)
		if err != nil {
			return err
		}
		agentByName[a.Name] = created
		report.add("agent", a.Name, ImportCreate, "")
	}

	// Squads. A squad needs its leader; a squad whose leader is missing is blocked.
	// Archived squads count too: a name the workspace holds, archived or not, exists.
	squads, err := q.ListAllSquads(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("import: list squads: %w", err)
	}
	squadByName := map[string]db.Squad{}
	squadNameByID := map[string]string{}
	for _, sq := range squads {
		squadByName[sq.Name] = sq
		squadNameByID[util.UUIDToString(sq.ID)] = sq.Name
	}
	createdSquad := map[string]bool{}
	for _, sq := range doc.Squads {
		if _, ok := squadByName[sq.Name]; ok {
			report.add("squad", sq.Name, ImportExists, "")
			continue
		}
		leader, ok := agentByName[sq.Leader]
		if !ok {
			report.add("squad", sq.Name, ImportBlocked, "its leader "+quoteOr(sq.Leader, "(none)")+" is not in the workspace yet")
			continue
		}
		if !ValidAutonomyLevel(int(sq.AutonomyLevel)) {
			report.add("squad", sq.Name, ImportBlocked, fmt.Sprintf("autonomy level %d is not one of 0 to 3", sq.AutonomyLevel))
			continue
		}
		reason := ""
		if sq.Status == ExportSquadArchived {
			reason = "archived: its members and routines are written, then the squad is archived; nothing is activated"
		}
		if !write {
			squadByName[sq.Name] = db.Squad{Name: sq.Name}
			report.add("squad", sq.Name, ImportCreate, reason)
			continue
		}
		created, err := importSquad(ctx, q, workspaceID, opts, sq, leader, floorByName, agentByName, actor)
		if err != nil {
			return err
		}
		squadByName[sq.Name] = created
		squadNameByID[util.UUIDToString(created.ID)] = created.Name
		createdSquad[sq.Name] = true
		report.add("squad", sq.Name, ImportCreate, reason)
	}

	// Home squads: an agent drawn in a squad it belongs to. The first membership
	// already sets one; this applies the export's choice when it differs.
	for _, a := range doc.Agents {
		if a.HomeSquad == "" {
			continue
		}
		agent, aok := agentByName[a.Name]
		squad, sok := squadByName[a.HomeSquad]
		if !aok || !sok || !write || agent.HomeSquadID == squad.ID {
			continue
		}
		if _, err := q.SetAgentHomeSquad(ctx, db.SetAgentHomeSquadParams{
			ID: agent.ID, WorkspaceID: workspaceID, HomeSquadID: squad.ID,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("import: home squad of %q: %w", a.Name, err)
		}
	}

	// Floor chiefs.
	for _, c := range doc.FloorChiefs {
		floorID, fok := floorByName[c.Floor]
		agent, aok := agentByName[c.Agent]
		if !fok || !aok {
			report.add("floor_chief", c.Floor, ImportBlocked, "the floor or its chief agent is not in the workspace")
			continue
		}
		if prev, err := q.GetFloorChief(ctx, db.GetFloorChiefParams{WorkspaceID: workspaceID, FloorID: floorID}); err == nil && prev.AgentID == agent.ID {
			report.add("floor_chief", c.Floor, ImportExists, "")
			continue
		}
		if !write {
			report.add("floor_chief", c.Floor, ImportCreate, "")
			continue
		}
		// The chief's floor brief routine is recreated paused, like every routine.
		if _, err := SetFloorChiefIn(ctx, q, FloorChiefInput{
			WorkspaceID: workspaceID, FloorID: floorID, AgentID: agent.ID,
			ActorUserID: opts.ActorID, RoutineStatus: "paused",
		}); err != nil {
			return fmt.Errorf("import: floor chief of %q: %w", c.Floor, err)
		}
		report.add("floor_chief", c.Floor, ImportCreate, "")
	}

	if err := importRoutines(ctx, q, workspaceID, doc, opts, squadByName, report); err != nil {
		return err
	}
	if err := importArchivedSquads(ctx, q, workspaceID, doc, write, squadByName, createdSquad, actor.ID); err != nil {
		return err
	}
	if err := importPipelines(ctx, q, workspaceID, doc, write, squadByName, report); err != nil {
		return err
	}
	inis, err := importInitiatives(ctx, q, tx, workspaceID, doc, opts, write, squadByName, report)
	if err != nil {
		return err
	}
	if err := importKnowledgeKinds(ctx, q, workspaceID, doc, write, report); err != nil {
		return err
	}
	scopes := importScopes{floorByName: floorByName, squadByName: squadByName, iniByKey: inis.byKey}
	if err := importGoals(ctx, q, workspaceID, doc, opts, write, scopes, inis.keyByID, floorNameByID, squadNameByID, agentByName, report); err != nil {
		return err
	}
	if err := importBudgets(ctx, q, workspaceID, doc, write, actor, scopes, report); err != nil {
		return err
	}
	if err := importPolicies(ctx, q, workspaceID, doc, opts, write, actor, floorByName, squadByName, report); err != nil {
		return err
	}

	// Sections this build does not read, with the reason, so the report says what is left.
	for name := range doc.Governance {
		report.add("governance", name, ImportSkipped, "the governance section is not read by this build; not imported")
	}
	return nil
}

// importSquad creates a squad with its leader, members, seat, charter and
// autonomy level, inside q's transaction.
func importSquad(ctx context.Context, q *db.Queries, ws pgtype.UUID, opts ImportOptions, sq ExportSquad, leader db.Agent,
	floorByName map[string]pgtype.UUID, agentByName map[string]db.Agent, actor GovernanceActor) (db.Squad, error) {
	created, err := q.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: ws, Name: sq.Name, Description: sq.Description,
		LeaderID: leader.ID, CreatorID: opts.ActorID,
	})
	if err != nil {
		return db.Squad{}, fmt.Errorf("import: create squad %q: %w", sq.Name, err)
	}
	if _, err := q.AddSquadMember(ctx, db.AddSquadMemberParams{
		SquadID: created.ID, MemberType: "agent", MemberID: leader.ID, Role: "leader",
	}); err != nil {
		return db.Squad{}, fmt.Errorf("import: add leader of %q: %w", sq.Name, err)
	}
	for _, m := range sq.Members {
		if m.Agent == "" || m.Agent == leader.Name {
			continue
		}
		member, ok := agentByName[m.Agent]
		if !ok {
			return db.Squad{}, fmt.Errorf("import: member %q of %q is not in the workspace", m.Agent, sq.Name)
		}
		if _, err := q.AddSquadMember(ctx, db.AddSquadMemberParams{
			SquadID: created.ID, MemberType: "agent", MemberID: member.ID, Role: normalizedRole(m.Role),
		}); err != nil {
			return db.Squad{}, fmt.Errorf("import: add %q to %q: %w", m.Agent, sq.Name, err)
		}
	}
	if sq.Charter != nil {
		if err := insertImportedCharter(ctx, q, ws, created.ID, *sq.Charter, opts.ActorID); err != nil {
			return db.Squad{}, err
		}
	}
	if sq.Floor != "" && sq.OfficeSlot != nil {
		if floorID, ok := floorByName[sq.Floor]; ok {
			if _, err := q.GetLiveSquadAtSeat(ctx, db.GetLiveSquadAtSeatParams{WorkspaceID: ws, FloorID: floorID, OfficeSlot: pgtype.Int2{Int16: int16(*sq.OfficeSlot), Valid: true}}); errors.Is(err, pgx.ErrNoRows) {
				if _, err := q.SetSquadSeat(ctx, db.SetSquadSeatParams{
					ID: created.ID, WorkspaceID: ws,
					FloorID: floorID, OfficeSlot: pgtype.Int2{Int16: int16(*sq.OfficeSlot), Valid: true},
				}); err != nil {
					return db.Squad{}, fmt.Errorf("import: seat %q: %w", sq.Name, err)
				}
			}
		}
	}
	// The autonomy level goes through the autonomy service, which logs the change.
	// A squad created at the default level is not changed, so nothing is logged.
	if _, err := ApplyAutonomyChange(ctx, q, ws, created.ID, int(sq.AutonomyLevel), actor, importAutonomyReason); err != nil {
		return db.Squad{}, fmt.Errorf("import: autonomy of %q: %w", sq.Name, err)
	}
	return created, nil
}

// importRoutines recreates each squad's routines, paused, with their schedules.
// A routine whose title the squad already has is left alone.
func importRoutines(ctx context.Context, q *db.Queries, ws pgtype.UUID, doc CompanyExport, opts ImportOptions, squadByName map[string]db.Squad, report *ImportReport) error {
	write := !opts.DryRun
	for _, sq := range doc.Squads {
		squad, ok := squadByName[sq.Name]
		existing := map[string]bool{}
		if ok && write && squad.ID.Valid {
			rows, err := q.ListSquadRoutines(ctx, db.ListSquadRoutinesParams{WorkspaceID: ws, SquadID: squad.ID})
			if err != nil {
				return fmt.Errorf("import: list routines of %q: %w", sq.Name, err)
			}
			for _, r := range rows {
				existing[r.AutopilotTitle] = true
			}
		}
		for _, r := range sq.Routines {
			if !ok {
				report.add("routine", r.Title, ImportBlocked, "its squad "+quoteOr(sq.Name, "(none)")+" is not in the workspace yet")
				continue
			}
			if existing[r.Title] {
				report.add("routine", r.Title, ImportExists, "")
				continue
			}
			next, err := ComputeNextRun(r.Cron, r.Timezone)
			if err != nil {
				report.add("routine", r.Title, ImportBlocked, "its schedule does not compute: "+err.Error())
				continue
			}
			if !write {
				report.add("routine", r.Title, ImportCreate, "created paused: the boss resumes it")
				continue
			}
			ap, err := CreateSquadScheduledAutopilot(ctx, q, SquadScheduledAutopilot{
				WorkspaceID: ws, SquadID: squad.ID, ActorUserID: opts.ActorID, Title: r.Title,
				Status: "paused", Cron: r.Cron, Timezone: r.Timezone, NextRun: next,
			})
			if err != nil {
				return fmt.Errorf("import: routine %q: %w", r.Title, err)
			}
			if _, err := q.InsertSquadRoutine(ctx, db.InsertSquadRoutineParams{
				AutopilotID: ap.ID, WorkspaceID: ws, SquadID: squad.ID, Purpose: r.Purpose,
				ExpectedOutput: r.ExpectedOutput, Required: r.Required, GraceHours: r.GraceHours,
			}); err != nil {
				return fmt.Errorf("import: link routine %q: %w", r.Title, err)
			}
			report.add("routine", r.Title, ImportCreate, "created paused: the boss resumes it")
		}
	}
	return nil
}

// importPipelines writes each exported pipeline the workspace does not have by
// name. Only one pipeline is the default, so an exported default is written as
// a non-default when the workspace already has one.
func importPipelines(ctx context.Context, q *db.Queries, ws pgtype.UUID, doc CompanyExport, write bool, squadByName map[string]db.Squad, report *ImportReport) error {
	if len(doc.Pipelines) == 0 {
		return nil
	}
	if write {
		if err := q.LockPipelineInstall(ctx, ws); err != nil {
			return fmt.Errorf("import: lock pipeline install: %w", err)
		}
	}
	existing, err := q.ListPipelines(ctx, ws)
	if err != nil {
		return fmt.Errorf("import: list pipelines: %w", err)
	}
	names := map[string]bool{}
	hasDefault := false
	for _, p := range existing {
		names[p.Name] = true
		hasDefault = hasDefault || p.IsDefault
	}
	for _, p := range doc.Pipelines {
		if names[p.Name] {
			report.add("pipeline", p.Name, ImportExists, "")
			continue
		}
		stages, unowned, err := importStages(p.Stages, squadByName, write)
		if err != nil {
			report.add("pipeline", p.Name, ImportBlocked, "its stages do not read: "+err.Error())
			continue
		}
		isDefault := p.IsDefault && !hasDefault
		reason := ""
		if len(unowned) > 0 {
			reason = "no owner squad in this workspace for: " + strings.Join(unowned, ", ") + "; those stages stay unowned"
		}
		if !write {
			report.add("pipeline", p.Name, ImportCreate, reason)
			names[p.Name] = true
			hasDefault = hasDefault || isDefault
			continue
		}
		if _, err := writePipeline(ctx, q, ws, p.Name, isDefault, stages); err != nil {
			return fmt.Errorf("import: pipeline %q: %w", p.Name, err)
		}
		names[p.Name] = true
		hasDefault = hasDefault || isDefault
		report.add("pipeline", p.Name, ImportCreate, reason)
	}
	return nil
}

// importStages reads an exported pipeline's stages and resolves each owner squad
// name to the squad's id. An owner the workspace lacks leaves its stage unowned,
// and the stage keys that stay unowned are returned.
func importStages(raw json.RawMessage, squadByName map[string]db.Squad, write bool) ([]PipelineStage, []string, error) {
	var stages []PipelineStage
	if err := decodeStages(raw, &stages); err != nil {
		return nil, nil, err
	}
	if len(stages) == 0 {
		return nil, nil, errors.New("the pipeline has no stages")
	}
	var unowned []string
	for i := range stages {
		if stages[i].OwnerSquadID == nil {
			unowned = append(unowned, stages[i].Key)
			continue
		}
		sq, ok := squadByName[*stages[i].OwnerSquadID]
		if !ok {
			stages[i].OwnerSquadID = nil
			unowned = append(unowned, stages[i].Key)
			continue
		}
		id := importPlaceholderID
		if write {
			id = util.UUIDToString(sq.ID)
		}
		stages[i].OwnerSquadID = &id
	}
	return stages, unowned, nil
}

// importKnowledgeKinds creates each exported kind the workspace does not have by
// key. An existing kind is not changed: an admin may have edited it.
func importKnowledgeKinds(ctx context.Context, q *db.Queries, ws pgtype.UUID, doc CompanyExport, write bool, report *ImportReport) error {
	existing, err := q.ListKnowledgeKinds(ctx, ws)
	if err != nil {
		return fmt.Errorf("import: list knowledge kinds: %w", err)
	}
	keys := map[string]bool{}
	for _, k := range existing {
		keys[k.Key] = true
	}
	for _, k := range doc.KnowledgeKinds {
		if keys[k.Key] {
			report.add("knowledge_kind", k.Key, ImportExists, "")
			continue
		}
		if strings.TrimSpace(k.Name) == "" {
			report.add("knowledge_kind", k.Key, ImportBlocked, "the kind has no name")
			continue
		}
		if !write {
			keys[k.Key] = true
			report.add("knowledge_kind", k.Key, ImportCreate, "")
			continue
		}
		required := k.RequiredSections
		if required == nil {
			required = []string{}
		}
		created, err := SeedKnowledgeKindIn(ctx, q, db.SeedKnowledgeKindParams{
			WorkspaceID: ws, Key: k.Key, Name: k.Name, DefaultBaseKey: k.DefaultBaseKey,
			Template: k.Template, ReviewDays: k.ReviewDays, RequiredSections: required,
		})
		if err != nil {
			return fmt.Errorf("import: %w", err)
		}
		keys[k.Key] = true
		if created {
			report.add("knowledge_kind", k.Key, ImportCreate, "")
		} else {
			report.add("knowledge_kind", k.Key, ImportExists, "")
		}
	}
	return nil
}

// importScopes resolves the scopes an exported goal, key result or budget names to
// the ids the import has: floors and squads by name, initiatives by identity.
type importScopes struct {
	floorByName map[string]pgtype.UUID
	squadByName map[string]db.Squad
	iniByKey    map[string]pgtype.UUID
}

// resolve returns the id of a company, floor, squad or initiative scope. The
// company scope has no id. An initiative is named by ini; the other scopes by name.
// In a dry run the ids are placeholders, so only presence is checked.
func (sc importScopes) resolve(scopeType, name string, ini *ExportInitiativeRef) (pgtype.UUID, bool) {
	switch scopeType {
	case ScopeCompany:
		return pgtype.UUID{}, true
	case ScopeFloor:
		id, ok := sc.floorByName[name]
		return id, ok
	case ScopeSquad:
		sq, ok := sc.squadByName[name]
		if !ok {
			return pgtype.UUID{}, false
		}
		return sq.ID, true
	case ScopeInitiative:
		if ini == nil {
			return pgtype.UUID{}, false
		}
		id, ok := sc.iniByKey[initiativeKey(ini.Pipeline, ini.Stage, ini.Title)]
		return id, ok
	}
	return pgtype.UUID{}, false
}

// initiativeKey is an initiative's identity in an export: its pipeline's name, its
// stage key and its title.
func initiativeKey(pipeline, stage, title string) string {
	return pipeline + "|" + stage + "|" + title
}

// scopeLabel names a scope in the report.
func scopeLabel(scopeType, name string, ini *ExportInitiativeRef) string {
	if ini != nil {
		return "initiative " + quoteOr(ini.Title, "(none)") + " (" + ini.Pipeline + ", " + ini.Stage + ")"
	}
	if name == "" {
		return scopeType
	}
	return scopeType + " " + quoteOr(name, "(none)")
}

// initiativeIndex holds the initiatives the workspace has after the import's
// initiative step. byKey finds an initiative's id by identity; keyByID names an
// existing initiative's identity by id, for the existing goals' keys.
type initiativeIndex struct {
	byKey   map[string]pgtype.UUID
	keyByID map[string]string
}

// importInitiatives recreates the exported initiatives the workspace does not
// have, matched by identity (pipeline, stage, title). Each is created quiet: with
// its owner squad and status, but with no handoff and no wake, since an import
// starts no work. It returns the ids the goals and budgets that name an initiative
// resolve against. An initiative whose pipeline, stage or owner squad the workspace
// lacks is blocked with the reason.
func importInitiatives(ctx context.Context, q *db.Queries, tx pgx.Tx, ws pgtype.UUID, doc CompanyExport, opts ImportOptions, write bool,
	squadByName map[string]db.Squad, report *ImportReport) (initiativeIndex, error) {
	idx := initiativeIndex{byKey: map[string]pgtype.UUID{}, keyByID: map[string]string{}}
	pipelines, err := q.ListPipelines(ctx, ws)
	if err != nil {
		return idx, fmt.Errorf("import: list pipelines: %w", err)
	}
	pipeByName := map[string]db.Pipeline{}
	pipeNameByID := map[string]string{}
	for _, p := range pipelines {
		pipeByName[p.Name] = p
		pipeNameByID[util.UUIDToString(p.ID)] = p.Name
	}
	if !write {
		// A dry run writes no pipeline, so the pipelines it would create are planned
		// from the export, with their stages, for the initiatives that name them.
		for _, p := range doc.Pipelines {
			if _, ok := pipeByName[p.Name]; !ok {
				pipeByName[p.Name] = db.Pipeline{Name: p.Name, Stages: []byte(p.Stages)}
			}
		}
	}
	existing, err := q.ListInitiatives(ctx, db.ListInitiativesParams{WorkspaceID: ws})
	if err != nil {
		return idx, fmt.Errorf("import: list initiatives: %w", err)
	}
	for _, ini := range existing {
		if ini.Status == InitiativeStatusClosed {
			continue
		}
		key := initiativeKey(pipeNameByID[util.UUIDToString(ini.PipelineID)], ini.StageKey, ini.Title)
		idx.byKey[key] = ini.ID
		idx.keyByID[util.UUIDToString(ini.ID)] = key
	}
	if len(doc.Initiatives) == 0 {
		return idx, nil
	}
	if write && opts.Issues == nil {
		return idx, errors.New("import: initiatives need the issue service")
	}
	var svc *InitiativeService
	if write {
		svc, _ = NewInitiativeSet(opts.Issues, nil)
	}
	for _, in := range doc.Initiatives {
		name := in.Title + " (" + in.Pipeline + ", " + in.Stage + ")"
		key := initiativeKey(in.Pipeline, in.Stage, in.Title)
		if _, ok := idx.byKey[key]; ok {
			report.add("initiative", name, ImportExists, "")
			continue
		}
		pipe, ok := pipeByName[in.Pipeline]
		if !ok {
			report.add("initiative", name, ImportBlocked, "its pipeline "+quoteOr(in.Pipeline, "(none)")+" is not in the workspace")
			continue
		}
		var stages []PipelineStage
		if err := decodeStages(pipe.Stages, &stages); err != nil {
			report.add("initiative", name, ImportBlocked, "its pipeline's stages do not read: "+err.Error())
			continue
		}
		if stageIndex(stages, in.Stage) < 0 {
			report.add("initiative", name, ImportBlocked, "its stage "+quoteOr(in.Stage, "(none)")+" is not a stage of pipeline "+quoteOr(in.Pipeline, "(none)"))
			continue
		}
		if !importableInitiativeStatus(in.Status) {
			report.add("initiative", name, ImportBlocked, "its status "+quoteOr(in.Status, "(none)")+" is not one an initiative has")
			continue
		}
		ownerID := pgtype.UUID{}
		if in.OwnerSquad != "" {
			sq, ok := squadByName[in.OwnerSquad]
			if !ok {
				report.add("initiative", name, ImportBlocked, "its owner squad "+quoteOr(in.OwnerSquad, "(none)")+" is not in the workspace yet")
				continue
			}
			ownerID = sq.ID
		}
		if !write {
			idx.byKey[key] = pgtype.UUID{}
			report.add("initiative", name, ImportCreate, "created quiet: no handoff and no wake")
			continue
		}
		created, err := svc.openInTx(ctx, tx, q, CreateInitiativeParams{
			WorkspaceID: ws, PipelineID: pipe.ID, StageKey: in.Stage, Title: in.Title,
			Actor: InitiativeActor{Type: "member", ID: opts.ActorID}, Quiet: true, OwnerSquadID: ownerID,
		})
		if err != nil {
			return idx, fmt.Errorf("import: initiative %q: %w", in.Title, err)
		}
		if in.Status != InitiativeStatusActive {
			created, err = setImportedInitiativeStatus(ctx, q, ws, created, in.Status)
			if err != nil {
				return idx, fmt.Errorf("import: status of initiative %q: %w", in.Title, err)
			}
		}
		idx.byKey[key] = created.ID
		idx.keyByID[util.UUIDToString(created.ID)] = key
		report.add("initiative", name, ImportCreate, "created quiet: no handoff and no wake")
	}
	return idx, nil
}

// setImportedInitiativeStatus sets the status an exported initiative had: shelved
// or operating. The timestamp the status carries is set to now, as the transition
// that reached it would set it.
func setImportedInitiativeStatus(ctx context.Context, q *db.Queries, ws pgtype.UUID, ini db.Initiative, status string) (db.Initiative, error) {
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	params := db.UpdateInitiativeStateParams{
		StageKey: ini.StageKey, OwnerSquadID: ini.OwnerSquadID, Status: status,
		StageEnteredAt: ini.StageEnteredAt, BuildProjectID: ini.BuildProjectID,
		ID: ini.ID, WorkspaceID: ws,
	}
	switch status {
	case InitiativeStatusShelved:
		params.ShelvedAt = now
	case InitiativeStatusOperating:
		params.OperatingSince = now
	}
	return q.UpdateInitiativeState(ctx, params)
}

// importArchivedSquads archives the squads this import created that the export
// marks archived. It runs after their members and routines are written. Archiving
// does what archiving a squad does: it frees the seat, moves the squad's routines to
// its leader, and releases its agents' home squads. Nothing is activated, so the
// routines stay paused.
func importArchivedSquads(ctx context.Context, q *db.Queries, ws pgtype.UUID, doc CompanyExport, write bool,
	squadByName map[string]db.Squad, created map[string]bool, actor pgtype.UUID) error {
	if !write {
		return nil
	}
	for _, sq := range doc.Squads {
		if sq.Status != ExportSquadArchived || !created[sq.Name] {
			continue
		}
		squad := squadByName[sq.Name]
		if err := q.TransferSquadAutopilotsToLeader(ctx, db.TransferSquadAutopilotsToLeaderParams{
			AssigneeID: squad.ID, AssigneeID_2: squad.LeaderID,
		}); err != nil {
			return fmt.Errorf("import: move routines of archived squad %q: %w", sq.Name, err)
		}
		if _, err := q.ArchiveSquad(ctx, db.ArchiveSquadParams{ID: squad.ID, ArchivedBy: actor}); err != nil {
			return fmt.Errorf("import: archive squad %q: %w", sq.Name, err)
		}
		if _, err := q.ReassignAgentHomeSquad(ctx, db.ReassignAgentHomeSquadParams{
			SquadID: squad.ID, WorkspaceID: ws,
		}); err != nil {
			return fmt.Errorf("import: release homes of archived squad %q: %w", sq.Name, err)
		}
	}
	return nil
}

// importGoals creates the exported goals, parents before children, each with its
// key results and its exported status. A squad's goal is written as a draft
// whatever its exported status was, and a governance entry says so. A goal that
// names an initiative the import does not have is skipped with the reason. A goal
// matches by scope and title.
func importGoals(ctx context.Context, q *db.Queries, ws pgtype.UUID, doc CompanyExport, opts ImportOptions, write bool,
	scopes importScopes, iniKeyByID map[string]string,
	floorNameByID map[string]string, squadNameByID map[string]string, agentByName map[string]db.Agent, report *ImportReport) error {
	if len(doc.Goals) == 0 {
		return nil
	}
	existing, err := q.ListGoalsInWorkspace(ctx, db.ListGoalsInWorkspaceParams{WorkspaceID: ws})
	if err != nil {
		return fmt.Errorf("import: list goals: %w", err)
	}
	keyOf := func(scopeType, scope, title string) string { return scopeType + "|" + scope + "|" + title }
	have := map[string]bool{}
	goalIDByTitle := map[string]pgtype.UUID{}
	for _, g := range existing {
		scope := ""
		switch g.ScopeType {
		case ScopeFloor:
			scope = floorNameByID[util.UUIDToString(g.ScopeID)]
		case ScopeSquad:
			scope = squadNameByID[util.UUIDToString(g.ScopeID)]
		case ScopeInitiative:
			scope = iniKeyByID[util.UUIDToString(g.ScopeID)]
		}
		have[keyOf(g.ScopeType, scope, g.Title)] = true
		if _, ok := goalIDByTitle[g.Title]; !ok {
			goalIDByTitle[g.Title] = g.ID
		}
	}

	goals := &GoalService{Queries: q, Metrics: NewMetricService(q, nil)}
	actor := GovernanceActor{Type: GovernanceActorMember, ID: opts.ActorID}
	pending := append([]ExportGoal(nil), doc.Goals...)
	for progress := true; progress && len(pending) > 0; {
		progress = false
		var waiting []ExportGoal
		for _, g := range pending {
			if g.Parent != "" {
				if _, ok := goalIDByTitle[g.Parent]; !ok {
					waiting = append(waiting, g)
					continue
				}
			}
			progress = true
			scope := g.Scope
			if g.Initiative != nil {
				scope = initiativeKey(g.Initiative.Pipeline, g.Initiative.Stage, g.Initiative.Title)
			}
			if have[keyOf(g.ScopeType, scope, g.Title)] {
				report.add("goal", g.Title, ImportExists, "")
				continue
			}
			params, reason, skipped := goalWriteParams(ws, g, opts, write, scopes, agentByName)
			if reason != "" {
				if skipped {
					report.add("goal", g.Title, ImportSkipped, reason)
				} else {
					report.add("goal", g.Title, ImportBlocked, reason)
				}
				continue
			}
			if g.Parent != "" {
				params.ParentGoalID = goalIDByTitle[g.Parent]
			}
			status := g.Status
			reportReason := ""
			if g.ScopeType == ScopeSquad {
				status = GoalStatusDraft
				reportReason = "imported as a draft: a squad's goal comes from a goal proposal, so the import approves nothing; activate it from Goals"
				if g.Status != GoalStatusDraft {
					reportReason = "imported as a draft, not as " + g.Status + ": a squad's goal comes from a goal proposal, so the import approves nothing; activate it from Goals"
				}
			}
			if !write {
				if err := validateGoalImport(params); err != nil {
					report.add("goal", g.Title, ImportBlocked, err.Error())
					continue
				}
				goalIDByTitle[g.Title] = pgtype.UUID{}
				have[keyOf(g.ScopeType, scope, g.Title)] = true
				report.add("goal", g.Title, ImportCreate, reportReason)
				continue
			}
			created, err := goals.WriteImportedGoal(ctx, q, params, status)
			if errors.Is(err, ErrGoalInvalid) || errors.Is(err, ErrGoalOwnerInvalid) || errors.Is(err, ErrGoalSquadScope) {
				report.add("goal", g.Title, ImportBlocked, err.Error())
				continue
			}
			if err != nil {
				return fmt.Errorf("import: goal %q: %w", g.Title, err)
			}
			if g.ScopeType == ScopeSquad {
				if err := NewGovernanceLog().Record(ctx, q, GovernanceEvent{
					WorkspaceID: ws,
					Kind:        GovernanceKindGoalImported,
					ActorType:   actor.Type,
					ActorID:     actor.ID,
					SubjectType: "goal",
					SubjectID:   created.ID,
					Summary:     fmt.Sprintf("Imported the squad goal %q as a draft. Its approval was not skipped: activate it from Goals.", g.Title),
					Details:     map[string]any{"source": "company import", "status": GoalStatusDraft, "exported_status": g.Status},
				}); err != nil {
					return fmt.Errorf("import: goal %q: %w", g.Title, err)
				}
			}
			goalIDByTitle[g.Title] = created.ID
			have[keyOf(g.ScopeType, scope, g.Title)] = true
			report.add("goal", g.Title, ImportCreate, reportReason)
		}
		pending = waiting
	}
	for _, g := range pending {
		report.add("goal", g.Title, ImportBlocked, "its parent goal "+quoteOr(g.Parent, "(none)")+" is not in the workspace")
	}
	return nil
}

// goalWriteParams resolves an exported goal's names to the write's fields. The
// returned reason is non-empty when the goal cannot be written from the export;
// skipped is set when the cause is an initiative the import does not have.
func goalWriteParams(ws pgtype.UUID, g ExportGoal, opts ImportOptions, write bool,
	scopes importScopes, agentByName map[string]db.Agent) (GoalWriteParams, string, bool) {
	scopeID, ok := scopes.resolve(g.ScopeType, g.Scope, g.Initiative)
	if !ok {
		if g.ScopeType == ScopeInitiative {
			return GoalWriteParams{}, "the " + scopeLabel(g.ScopeType, g.Scope, g.Initiative) + " is not in the workspace, so the goal is skipped", true
		}
		return GoalWriteParams{}, "the " + scopeLabel(g.ScopeType, g.Scope, nil) + " is not in the workspace yet", false
	}
	// A dry run's planned owner has no id yet; the placeholder is valid in shape only.
	ownerID := pgtype.UUID{Valid: true}
	switch g.OwnerType {
	case "member":
		ownerID = opts.ActorID
	case "squad":
		sq, ok := scopes.squadByName[g.Owner]
		if !ok {
			return GoalWriteParams{}, "its owner squad " + quoteOr(g.Owner, "(none)") + " is not in the workspace yet", false
		}
		if write {
			ownerID = sq.ID
		}
	case "agent":
		a, ok := agentByName[g.Owner]
		if !ok {
			return GoalWriteParams{}, "its owner agent " + quoteOr(g.Owner, "(none)") + " is not in the workspace yet", false
		}
		if write {
			ownerID = a.ID
		}
	default:
		return GoalWriteParams{}, "owner_type " + quoteOr(g.OwnerType, "(none)") + " is not known", false
	}
	start, err := time.Parse("2006-01-02", g.PeriodStart)
	if err != nil {
		return GoalWriteParams{}, "period_start must be a date as YYYY-MM-DD", false
	}
	end, err := time.Parse("2006-01-02", g.PeriodEnd)
	if err != nil {
		return GoalWriteParams{}, "period_end must be a date as YYYY-MM-DD", false
	}
	krs := make([]GoalKeyResultInput, 0, len(g.KeyResults))
	for _, kr := range g.KeyResults {
		krScope, ok := scopes.resolve(kr.ScopeType, kr.Scope, kr.Initiative)
		if !ok {
			return GoalWriteParams{}, "a key result's " + scopeLabel(kr.ScopeType, kr.Scope, kr.Initiative) + " is not in the workspace", false
		}
		krs = append(krs, GoalKeyResultInput{
			Title: kr.Title, MetricKey: kr.MetricKey, ScopeType: kr.ScopeType, ScopeID: krScope,
			StartValue: kr.StartValue, TargetValue: kr.TargetValue, Weight: kr.Weight,
		})
	}
	return GoalWriteParams{
		WorkspaceID: ws, ScopeType: g.ScopeType, ScopeID: scopeID, Title: g.Title, Why: g.Why,
		PeriodStart: start, PeriodEnd: end, OwnerType: g.OwnerType, OwnerID: ownerID, KeyResults: krs,
		Actor: GoalActor{Type: GovernanceActorMember, ID: opts.ActorID},
	}, "", false
}

// validateGoalImport runs the checks that need no database, for a dry run.
func validateGoalImport(p GoalWriteParams) error {
	if err := validateGoalFields(p.Title, p.Why, p.PeriodStart, p.PeriodEnd, p.OwnerType, p.OwnerID); err != nil {
		return err
	}
	return validateKeyResultInputs(p.KeyResults)
}

// importBudgets creates the exported budgets the workspace does not have for the
// same scope and period. A paused budget is created and then paused. A budget on an
// initiative the import does not have is skipped with the reason.
func importBudgets(ctx context.Context, q *db.Queries, ws pgtype.UUID, doc CompanyExport, write bool, actor GovernanceActor,
	scopes importScopes, report *ImportReport) error {
	if len(doc.Budgets) == 0 {
		return nil
	}
	states, err := q.ListWorkspaceBudgetStates(ctx, db.ListWorkspaceBudgetStatesParams{
		WorkspaceID: ws, Now: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("import: list budgets: %w", err)
	}
	have := map[string]bool{}
	for _, b := range states {
		if b.Status == BudgetStatusActive || b.Status == BudgetStatusPaused {
			have[b.ScopeType+"|"+util.UUIDToString(b.ScopeID)+"|"+b.Period] = true
		}
	}
	for _, b := range doc.Budgets {
		label := scopeLabel(b.ScopeType, b.Scope, b.Initiative)
		scopeID, ok := scopes.resolve(b.ScopeType, b.Scope, b.Initiative)
		if !ok {
			if b.ScopeType == ScopeInitiative {
				report.add("budget", label, ImportSkipped, "the "+label+" is not in the workspace, so the budget is skipped")
				continue
			}
			report.add("budget", label, ImportBlocked, "the scope is not in the workspace yet")
			continue
		}
		if have[b.ScopeType+"|"+util.UUIDToString(scopeID)+"|"+b.Period] {
			report.add("budget", label, ImportExists, "")
			continue
		}
		in := BudgetInput{
			ScopeType: b.ScopeType, ScopeID: scopeID, Period: b.Period, AmountMicros: b.AmountMicros,
			SoftPercent: b.SoftPercent, Mode: b.Mode, Classes: b.Classes,
		}
		if !write {
			if !ValidBudgetScope(in.ScopeType) || !ValidBudgetPeriod(in.Period) || !ValidBudgetMode(in.Mode) {
				report.add("budget", label, ImportBlocked, ErrBudgetInvalid.Error())
				continue
			}
			report.add("budget", label, ImportCreate, "")
			continue
		}
		created, err := createBudgetIn(ctx, q, ws, actor, in)
		if errors.Is(err, ErrBudgetExists) {
			report.add("budget", label, ImportExists, "")
			continue
		}
		if errors.Is(err, ErrBudgetInvalid) {
			report.add("budget", label, ImportBlocked, err.Error())
			continue
		}
		if err != nil {
			return fmt.Errorf("import: budget of %s: %w", label, err)
		}
		if b.Status == BudgetStatusPaused {
			paused := BudgetStatusPaused
			if _, err := updateBudgetIn(ctx, q, ws, actor, created.ID, BudgetPatch{Status: &paused}); err != nil {
				return fmt.Errorf("import: pause budget of %s: %w", label, err)
			}
		}
		report.add("budget", label, ImportCreate, "")
	}
	return nil
}

// importPolicies creates the exported standing approval policies the workspace
// does not have by name. Each goes through the same validation the API applies. A
// revoked policy is created and revoked at once, so it never approves anything and
// its revocation is in the governance log.
func importPolicies(ctx context.Context, q *db.Queries, ws pgtype.UUID, doc CompanyExport, opts ImportOptions, write bool, actor GovernanceActor,
	floorByName map[string]pgtype.UUID, squadByName map[string]db.Squad, report *ImportReport) error {
	if len(doc.ApprovalPolicies) == 0 {
		return nil
	}
	if write {
		if err := requireGovernanceBoss(ctx, q, ws, actor); err != nil {
			return err
		}
	}
	existing, err := q.ListApprovalPolicies(ctx, ws)
	if err != nil {
		return fmt.Errorf("import: list approval policies: %w", err)
	}
	names := map[string]bool{}
	for _, p := range existing {
		names[p.Name] = true
	}
	known := func(key string) bool { return opts.Kinds != nil && opts.Kinds.Has(key) }
	for _, p := range doc.ApprovalPolicies {
		if names[p.Name] {
			report.add("approval_policy", p.Name, ImportExists, "")
			continue
		}
		spec := proposalkinds.PolicySpec{
			Name: p.Name, Kind: p.Kind, ValidUntil: p.ValidUntil,
			Match: proposalkinds.PolicyMatch{
				MaxCostMicros: p.Match.MaxCostMicros, PayloadEquals: p.Match.PayloadEquals, Tags: p.Match.Tags,
			},
		}
		if p.MaxPerWeek > 0 {
			mpw := p.MaxPerWeek
			spec.MaxPerWeek = &mpw
		}
		blocked := ""
		for _, name := range p.Match.Squads {
			sq, ok := squadByName[name]
			if !ok {
				blocked = "its squad " + quoteOr(name, "(none)") + " is not in the workspace yet"
				break
			}
			spec.Match.SquadIDs = append(spec.Match.SquadIDs, policyIDText(sq.ID, write))
		}
		for _, name := range p.Match.Floors {
			id, ok := floorByName[name]
			if !ok && blocked == "" {
				blocked = "its floor " + quoteOr(name, "(none)") + " is not in the workspace yet"
			}
			spec.Match.FloorIDs = append(spec.Match.FloorIDs, policyIDText(id, write))
		}
		if blocked != "" {
			report.add("approval_policy", p.Name, ImportBlocked, blocked)
			continue
		}
		// A revoked policy is checked as of its last day in force: its valid_until
		// may be past by now, and that is no reason to refuse its record.
		asOf := time.Now()
		if p.Status == ExportPolicyRevoked && p.ValidUntil != "" {
			if day, err := time.Parse("2006-01-02", p.ValidUntil); err == nil && day.Before(asOf) {
				asOf = day
			}
		}
		valid, err := proposalkinds.ValidatePolicySpec(spec, known, asOf)
		if err != nil {
			report.add("approval_policy", p.Name, ImportBlocked, err.Error())
			continue
		}
		reason := ""
		if p.Status == ExportPolicyRevoked {
			reason = "imported revoked"
		}
		if !write {
			report.add("approval_policy", p.Name, ImportCreate, reason)
			continue
		}
		policy, err := createPolicyRow(ctx, q, ws, valid, actor.ID)
		if err != nil {
			return fmt.Errorf("import: approval policy %q: %w", p.Name, err)
		}
		if err := recordPolicyCreated(ctx, q, policy, actor); err != nil {
			return fmt.Errorf("import: approval policy %q: %w", p.Name, err)
		}
		if p.Status == ExportPolicyRevoked {
			if _, err := revokePolicyIn(ctx, q, ws, policy, actor); err != nil {
				return fmt.Errorf("import: revoke approval policy %q: %w", p.Name, err)
			}
		}
		report.add("approval_policy", p.Name, ImportCreate, reason)
	}
	return nil
}

// importableInitiativeStatus reports whether an exported initiative status is one
// an initiative can be created or left in by an import: active, shelved or operating.
func importableInitiativeStatus(status string) bool {
	switch status {
	case InitiativeStatusActive, InitiativeStatusShelved, InitiativeStatusOperating:
		return true
	}
	return false
}

// policyIDText is an id as a policy's match names it. A dry run's planned entity
// has no id yet and gets the placeholder, which has the shape the check wants.
func policyIDText(id pgtype.UUID, write bool) string {
	if !id.Valid || !write {
		return importPlaceholderID
	}
	return util.UUIDToString(id)
}

func createImportedAgent(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, opts ImportOptions, a ExportAgent) (db.Agent, error) {
	instructions := a.Instructions
	if instructions == "" && a.Template != "" {
		text, ok := companyInstructionText(a.Template)
		if !ok {
			return db.Agent{}, fmt.Errorf("import: agent %q names template %q, which is not an instruction template", a.Name, a.Template)
		}
		instructions = text
	}
	rt, err := q.GetAgentRuntime(ctx, opts.RuntimeID)
	if err != nil {
		return db.Agent{}, fmt.Errorf("import: load runtime: %w", err)
	}
	created, err := q.CreateAgent(ctx, db.CreateAgentParams{
		WorkspaceID: workspaceID, Name: a.Name, Description: a.Description,
		RuntimeMode: rt.RuntimeMode, RuntimeConfig: []byte("{}"), RuntimeID: opts.RuntimeID,
		Visibility: "workspace", MaxConcurrentTasks: 1, OwnerID: opts.ActorID,
		Instructions: instructions, CustomEnv: []byte("{}"), CustomArgs: []byte("[]"),
		McpConfig: []byte("{}"), ConversationStarters: []byte("[]"),
	})
	if err != nil {
		return db.Agent{}, fmt.Errorf("import: create agent %q: %w", a.Name, err)
	}
	// The instructions are the agent's first revision, as every write records one (spec 4.6).
	if instructions != "" {
		if _, err := WriteAgentInstructionRevision(ctx, q, workspaceID, created.ID, instructions, InstructionChange{
			Source: InstructionSourceHuman, Actor: GovernanceActor{Type: GovernanceActorMember, ID: opts.ActorID},
		}); err != nil {
			return db.Agent{}, fmt.Errorf("import: instruction revision of %q: %w", a.Name, err)
		}
	}
	return created, nil
}

func insertImportedCharter(ctx context.Context, q *db.Queries, workspaceID, squadID pgtype.UUID, c ExportCharter, actor pgtype.UUID) error {
	responsibilities, outputs := []byte(c.Responsibilities), []byte(c.Outputs)
	if len(responsibilities) == 0 {
		responsibilities = []byte("[]")
	}
	if len(outputs) == 0 {
		outputs = []byte("[]")
	}
	hours := c.ResponseHours
	if hours < 1 {
		hours = 48
	}
	if _, err := q.InsertSquadCharter(ctx, db.InsertSquadCharterParams{
		SquadID: squadID, WorkspaceID: workspaceID, Mission: c.Mission,
		Responsibilities: responsibilities, Outputs: outputs,
		Boundaries: c.Boundaries, Escalation: c.Escalation, Intake: c.Intake,
		ResponseHours: hours, OperatingReviewEnabled: c.OperatingReviewEnabled,
		UpdatedByType: "member", UpdatedByID: actor,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("import: charter: %w", err)
	}
	return nil
}

// normalizedRole keeps a member role the squad knows; anything else is a member.
func normalizedRole(role string) string {
	if strings.TrimSpace(role) == "" {
		return "member"
	}
	return role
}

func quoteOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return "\"" + s + "\""
}
