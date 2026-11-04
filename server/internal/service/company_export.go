package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Company as code (spec 4.9). The export describes the organisation as data:
// floors, seats, squads with charters, routines and autonomy levels, agents with
// their roles and template references, pipelines, initiatives, knowledge kinds,
// floor chiefs, goals, budgets and standing approval policies. Archived squads,
// revoked policies and draft or active squad goals are exported with their status.
// It excludes secrets, runtimes, documents, history, progress and spend. Entities
// are matched by name, so an import into another workspace is idempotent.

// CompanyExportVersion is the schema version of the export document. An import
// refuses any other version rather than guess at its shape.
const CompanyExportVersion = 1

// CompanyExportRetentionNote travels with every export. Metric readings are the
// goals step's, and they are downsampled; the structure does not depend on them.
const CompanyExportRetentionNote = "Metric readings older than 90 days are downsampled to daily values in the source workspace. " +
	"This export carries the structure and definitions only: no readings, no progress, no run history, no documents."

// CompanyExportExcluded names what an export never carries.
var CompanyExportExcluded = []string{
	"secrets", "runtimes", "documents", "history", "run_logs",
	"goal_progress", "budget_spend", "closed_initiatives",
}

// CompanyExport is the document.
type CompanyExport struct {
	SchemaVersion    int                `json:"schema_version"`
	ExportedAt       time.Time          `json:"exported_at"`
	RetentionNote    string             `json:"retention_note"`
	Excluded         []string           `json:"excluded"`
	Floors           []ExportFloor      `json:"floors"`
	Squads           []ExportSquad      `json:"squads"`
	Agents           []ExportAgent      `json:"agents"`
	FloorChiefs      []ExportChief      `json:"floor_chiefs"`
	Pipelines        []ExportPipeline   `json:"pipelines"`
	Initiatives      []ExportInitiative `json:"initiatives"`
	KnowledgeKinds   []ExportKind       `json:"knowledge_kinds"`
	Goals            []ExportGoal       `json:"goals"`
	Budgets          []ExportBudget     `json:"budgets"`
	ApprovalPolicies []ExportPolicy     `json:"approval_policies"`
	// Governance holds sections a later step provides that this build does not
	// read. The import reports each one as not imported.
	Governance map[string]json.RawMessage `json:"governance"`
}

// ExportFloor is a floor by name. Position orders the floors on the Office.
type ExportFloor struct {
	Name     string `json:"name"`
	Position int32  `json:"position"`
}

// ExportSquad is a squad: its seat, leader, members, charter, routines and
// autonomy level. The leader and members are agent names. Status is active or
// archived; an archived squad has no seat, and the import archives it after its
// members and routines are written, without activating either.
type ExportSquad struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Floor         string          `json:"floor"`
	OfficeSlot    *int32          `json:"office_slot"`
	Leader        string          `json:"leader"`
	AutonomyLevel int32           `json:"autonomy_level"`
	Status        string          `json:"status"`
	Members       []ExportMember  `json:"members"`
	Charter       *ExportCharter  `json:"charter"`
	Routines      []ExportRoutine `json:"routines"`
}

// Squad statuses in an export.
const (
	ExportSquadActive   = "active"
	ExportSquadArchived = "archived"
)

// ExportInitiativeRef names an initiative by its identity: its pipeline (by name,
// since pipeline ids are per workspace), its stage key and its title. A goal, key
// result or budget scoped to an initiative carries this reference.
type ExportInitiativeRef struct {
	Pipeline string `json:"pipeline"`
	Stage    string `json:"stage"`
	Title    string `json:"title"`
}

// ExportInitiative is an initiative that is not closed. The import creates it from
// this entry, with its owner squad and status, and attaches the goals and budgets
// that name it. Closed initiatives are history and are not exported.
type ExportInitiative struct {
	Pipeline   string `json:"pipeline"`
	Stage      string `json:"stage"`
	Title      string `json:"title"`
	OwnerSquad string `json:"owner_squad"`
	Status     string `json:"status"`
}

// ExportMember is an agent's place in a squad.
type ExportMember struct {
	Agent string `json:"agent"`
	Role  string `json:"role"`
}

// ExportCharter is a squad's charter. Responsibilities and outputs are the
// stored arrays, passed through.
type ExportCharter struct {
	Mission                string          `json:"mission"`
	Responsibilities       json.RawMessage `json:"responsibilities"`
	Outputs                json.RawMessage `json:"outputs"`
	Boundaries             string          `json:"boundaries"`
	Escalation             string          `json:"escalation"`
	Intake                 string          `json:"intake"`
	ResponseHours          int32           `json:"response_hours"`
	OperatingReviewEnabled bool            `json:"operating_review_enabled"`
}

// ExportRoutine is a routine: its definition and the schedule of its autopilot.
// The import recreates it paused, so a new company runs nothing until the boss
// resumes it. Status records what the source had.
type ExportRoutine struct {
	Title          string `json:"title"`
	Purpose        string `json:"purpose"`
	ExpectedOutput string `json:"expected_output"`
	Required       bool   `json:"required"`
	GraceHours     int32  `json:"grace_hours"`
	Cron           string `json:"cron"`
	Timezone       string `json:"timezone"`
	Status         string `json:"status"`
}

// ExportAgent is an agent's definition. Runtime and credentials are not part of
// it. Template names the instruction template whose text the agent carries,
// when it carries that text unchanged; the reference is derived from the text
// because no template id is stored on the agent.
type ExportAgent struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	HomeSquad    string `json:"home_squad"`
	Template     string `json:"template"`
}

// ExportChief is a floor's chief, by floor and agent name.
type ExportChief struct {
	Floor string `json:"floor"`
	Agent string `json:"agent"`
}

// ExportPipeline is a pipeline's stages. Owner squads are named.
type ExportPipeline struct {
	Name      string          `json:"name"`
	IsDefault bool            `json:"is_default"`
	Stages    json.RawMessage `json:"stages"`
}

// ExportKind is a knowledge kind's editable definition. The key is its identity.
type ExportKind struct {
	Key              string   `json:"key"`
	Name             string   `json:"name"`
	DefaultBaseKey   string   `json:"default_base_key"`
	Template         string   `json:"template"`
	ReviewDays       int32    `json:"review_days"`
	RequiredSections []string `json:"required_sections"`
}

// ExportGoal is a goal's structure: its scope, owner, parent, period, status and
// key results. Progress and confidence are not exported. Scope and Owner name a
// floor, squad or agent; an initiative scope is named by Initiative instead. A
// member owner is empty, and the importer owns the goal. A squad goal is exported
// only while it is a draft or active; the import recreates it as a draft.
type ExportGoal struct {
	Title       string               `json:"title"`
	Why         string               `json:"why"`
	ScopeType   string               `json:"scope_type"`
	Scope       string               `json:"scope"`
	Initiative  *ExportInitiativeRef `json:"initiative,omitempty"`
	Parent      string               `json:"parent"`
	PeriodStart string               `json:"period_start"`
	PeriodEnd   string               `json:"period_end"`
	OwnerType   string               `json:"owner_type"`
	Owner       string               `json:"owner"`
	Status      string               `json:"status"`
	KeyResults  []ExportKeyResult    `json:"key_results"`
}

// ExportKeyResult is a key result's definition, with the start and target values.
type ExportKeyResult struct {
	Title       string               `json:"title"`
	MetricKey   string               `json:"metric_key"`
	ScopeType   string               `json:"scope_type"`
	Scope       string               `json:"scope"`
	Initiative  *ExportInitiativeRef `json:"initiative,omitempty"`
	StartValue  float64              `json:"start_value"`
	TargetValue float64              `json:"target_value"`
	Weight      int                  `json:"weight"`
}

// ExportBudget is a budget's definition. Spend is history and is not exported.
type ExportBudget struct {
	ScopeType    string               `json:"scope_type"`
	Scope        string               `json:"scope"`
	Initiative   *ExportInitiativeRef `json:"initiative,omitempty"`
	Period       string               `json:"period"`
	AmountMicros int64                `json:"amount_micros"`
	SoftPercent  int32                `json:"soft_percent"`
	Mode         string               `json:"mode"`
	Classes      []string             `json:"classes"`
	Status       string               `json:"status"`
}

// ExportPolicy is a standing approval policy. Its squads and floors are named.
// Status is active or revoked: a revoked policy is imported revoked, so the
// revocation is not lost, and it never approves anything.
type ExportPolicy struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Status     string            `json:"status"`
	Match      ExportPolicyMatch `json:"match"`
	MaxPerWeek int32             `json:"max_per_week"`
	ValidUntil string            `json:"valid_until"`
}

// Policy statuses in an export.
const (
	ExportPolicyActive  = "active"
	ExportPolicyRevoked = "revoked"
)

// ExportPolicyMatch is a policy's match with squads and floors named.
type ExportPolicyMatch struct {
	Squads        []string       `json:"squads,omitempty"`
	Floors        []string       `json:"floors,omitempty"`
	MaxCostMicros *int64         `json:"max_cost_micros,omitempty"`
	PayloadEquals map[string]any `json:"payload_equals,omitempty"`
	Tags          []string       `json:"tags,omitempty"`
}

// GovernanceExportProvider supplies a governance section this build does not
// export itself. No provider is wired at present.
type GovernanceExportProvider interface {
	ExportSection(ctx context.Context, workspaceID pgtype.UUID) (name string, body any, err error)
}

// ExportCompany builds the export of a workspace. providers may be empty.
func ExportCompany(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, now time.Time, providers ...GovernanceExportProvider) (CompanyExport, error) {
	out := CompanyExport{
		SchemaVersion:    CompanyExportVersion,
		ExportedAt:       now.UTC(),
		RetentionNote:    CompanyExportRetentionNote,
		Excluded:         CompanyExportExcluded,
		Floors:           []ExportFloor{},
		Squads:           []ExportSquad{},
		Agents:           []ExportAgent{},
		FloorChiefs:      []ExportChief{},
		Pipelines:        []ExportPipeline{},
		Initiatives:      []ExportInitiative{},
		KnowledgeKinds:   []ExportKind{},
		Goals:            []ExportGoal{},
		Budgets:          []ExportBudget{},
		ApprovalPolicies: []ExportPolicy{},
		Governance:       map[string]json.RawMessage{},
	}

	floors, err := q.ListOfficeFloors(ctx, workspaceID)
	if err != nil {
		return CompanyExport{}, fmt.Errorf("export floors: %w", err)
	}
	names := scopeNames{floors: map[string]string{}, squads: map[string]string{}, initiatives: map[string]ExportInitiativeRef{}}
	for _, f := range floors {
		names.floors[util.UUIDToString(f.ID)] = f.Name
		out.Floors = append(out.Floors, ExportFloor{Name: f.Name, Position: f.Position})
	}

	agents, err := q.ListAgents(ctx, workspaceID)
	if err != nil {
		return CompanyExport{}, fmt.Errorf("export agents: %w", err)
	}
	agentName := map[string]string{}
	for _, a := range agents {
		agentName[util.UUIDToString(a.ID)] = a.Name
	}
	// Archived squads keep their names here, so a goal, budget or policy that names
	// one is exported with that name, and the import archives the squad it names.
	allSquads, err := q.ListAllSquads(ctx, workspaceID)
	if err != nil {
		return CompanyExport{}, fmt.Errorf("export squads: %w", err)
	}
	squadName := map[string]string{}
	for _, sq := range allSquads {
		squadName[util.UUIDToString(sq.ID)] = sq.Name
		names.squads[util.UUIDToString(sq.ID)] = sq.Name
	}
	for _, a := range agents {
		home := ""
		if a.HomeSquadID.Valid {
			home = squadName[util.UUIDToString(a.HomeSquadID)]
		}
		out.Agents = append(out.Agents, ExportAgent{
			Name: a.Name, Description: a.Description, Instructions: a.Instructions, HomeSquad: home,
			Template: instructionTemplateOf(a.Instructions),
		})
	}

	for _, sq := range allSquads {
		es, err := exportSquad(ctx, q, workspaceID, sq, names.floors, agentName)
		if err != nil {
			return CompanyExport{}, err
		}
		out.Squads = append(out.Squads, es)
	}

	chiefs, err := q.ListFloorChiefs(ctx, workspaceID)
	if err != nil {
		return CompanyExport{}, fmt.Errorf("export floor chiefs: %w", err)
	}
	for _, c := range chiefs {
		out.FloorChiefs = append(out.FloorChiefs, ExportChief{
			Floor: names.floors[util.UUIDToString(c.FloorID)], Agent: agentName[util.UUIDToString(c.AgentID)],
		})
	}

	pipelines, err := q.ListPipelines(ctx, workspaceID)
	if err != nil {
		return CompanyExport{}, fmt.Errorf("export pipelines: %w", err)
	}
	pipelineName := map[string]string{}
	for _, p := range pipelines {
		pipelineName[util.UUIDToString(p.ID)] = p.Name
		stages, err := exportStages(p.Stages, squadName)
		if err != nil {
			return CompanyExport{}, err
		}
		out.Pipelines = append(out.Pipelines, ExportPipeline{Name: p.Name, IsDefault: p.IsDefault, Stages: stages})
	}

	initiatives, err := q.ListInitiatives(ctx, db.ListInitiativesParams{WorkspaceID: workspaceID})
	if err != nil {
		return CompanyExport{}, fmt.Errorf("export initiatives: %w", err)
	}
	for _, ini := range initiatives {
		if ini.Status == InitiativeStatusClosed {
			continue
		}
		ref := ExportInitiativeRef{Pipeline: pipelineName[util.UUIDToString(ini.PipelineID)], Stage: ini.StageKey, Title: ini.Title}
		names.initiatives[util.UUIDToString(ini.ID)] = ref
		out.Initiatives = append(out.Initiatives, ExportInitiative{
			Pipeline: ref.Pipeline, Stage: ref.Stage, Title: ref.Title,
			OwnerSquad: squadName[util.UUIDToString(ini.OwnerSquadID)], Status: ini.Status,
		})
	}

	kinds, err := q.ListKnowledgeKinds(ctx, workspaceID)
	if err != nil {
		return CompanyExport{}, fmt.Errorf("export knowledge kinds: %w", err)
	}
	for _, k := range kinds {
		out.KnowledgeKinds = append(out.KnowledgeKinds, ExportKind{
			Key: k.Key, Name: k.Name, DefaultBaseKey: k.DefaultBaseKey, Template: k.Template,
			ReviewDays: k.ReviewDays, RequiredSections: k.RequiredSections,
		})
	}

	if out.Goals, err = exportGoals(ctx, q, workspaceID, names, agentName); err != nil {
		return CompanyExport{}, err
	}
	if out.Budgets, err = exportBudgets(ctx, q, workspaceID, now, names); err != nil {
		return CompanyExport{}, err
	}
	if out.ApprovalPolicies, err = exportPolicies(ctx, q, workspaceID, names); err != nil {
		return CompanyExport{}, err
	}

	sortCompanyExport(&out)

	for _, p := range providers {
		if p == nil {
			continue
		}
		name, body, err := p.ExportSection(ctx, workspaceID)
		if err != nil {
			return CompanyExport{}, fmt.Errorf("export %s: %w", name, err)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return CompanyExport{}, fmt.Errorf("encode %s: %w", name, err)
		}
		out.Governance[name] = raw
	}
	return out, nil
}

// scopeNames maps the ids a goal, key result, budget or policy names to the names
// the export carries. An initiative is named by its reference; one the export does
// not carry (closed) is not in the map, so its goals and budgets are left out.
type scopeNames struct {
	floors      map[string]string
	squads      map[string]string
	initiatives map[string]ExportInitiativeRef
}

// ref returns the export form of a scope: the company's is empty, a floor's or a
// squad's its name, and an initiative's its reference. ok is false for a scope the
// export does not carry.
func (n scopeNames) ref(scopeType string, id pgtype.UUID) (name string, ini *ExportInitiativeRef, ok bool) {
	switch scopeType {
	case ScopeCompany:
		return "", nil, true
	case ScopeFloor:
		name, ok := n.floors[util.UUIDToString(id)]
		return name, nil, ok
	case ScopeSquad:
		name, ok := n.squads[util.UUIDToString(id)]
		return name, nil, ok
	case ScopeInitiative:
		r, ok := n.initiatives[util.UUIDToString(id)]
		if !ok {
			return "", nil, false
		}
		return "", &r, true
	}
	return "", nil, false
}

func exportSquad(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, sq db.Squad, floorName, agentName map[string]string) (ExportSquad, error) {
	es := ExportSquad{
		Name: sq.Name, Description: sq.Description, Leader: agentName[util.UUIDToString(sq.LeaderID)],
		AutonomyLevel: sq.AutonomyLevel, Status: ExportSquadActive,
		Members: []ExportMember{}, Routines: []ExportRoutine{},
	}
	if sq.ArchivedAt.Valid {
		es.Status = ExportSquadArchived
	}
	if sq.FloorID.Valid {
		es.Floor = floorName[util.UUIDToString(sq.FloorID)]
	}
	if sq.OfficeSlot.Valid {
		slot := int32(sq.OfficeSlot.Int16)
		es.OfficeSlot = &slot
	}
	members, err := q.ListSquadMembers(ctx, sq.ID)
	if err != nil {
		return ExportSquad{}, fmt.Errorf("export squad members: %w", err)
	}
	for _, m := range members {
		if m.MemberType != "agent" {
			continue
		}
		es.Members = append(es.Members, ExportMember{Agent: agentName[util.UUIDToString(m.MemberID)], Role: m.Role})
	}
	charter, err := q.GetSquadCharter(ctx, db.GetSquadCharterParams{SquadID: sq.ID, WorkspaceID: workspaceID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return ExportSquad{}, fmt.Errorf("export charter: %w", err)
	default:
		es.Charter = &ExportCharter{
			Mission: charter.Mission, Responsibilities: charter.Responsibilities, Outputs: charter.Outputs,
			Boundaries: charter.Boundaries, Escalation: charter.Escalation, Intake: charter.Intake,
			ResponseHours: charter.ResponseHours, OperatingReviewEnabled: charter.OperatingReviewEnabled,
		}
	}
	routines, err := q.ListSquadRoutines(ctx, db.ListSquadRoutinesParams{WorkspaceID: workspaceID, SquadID: sq.ID})
	if err != nil {
		return ExportSquad{}, fmt.Errorf("export routines: %w", err)
	}
	for _, r := range routines {
		es.Routines = append(es.Routines, ExportRoutine{
			Title: r.AutopilotTitle, Purpose: r.Purpose, ExpectedOutput: r.ExpectedOutput,
			Required: r.Required, GraceHours: r.GraceHours,
			Cron: r.ScheduleCronExpression.String, Timezone: r.ScheduleTimezone.String, Status: r.AutopilotStatus,
		})
	}
	return es, nil
}

// exportGoals lists the workspace's goals, parents before children, with their
// key results. A goal whose owner or scope the export does not carry is left out,
// and so is a squad goal that is neither a draft nor active.
func exportGoals(ctx context.Context, q *db.Queries, ws pgtype.UUID, names scopeNames, agentName map[string]string) ([]ExportGoal, error) {
	goals, err := q.ListGoalsInWorkspace(ctx, db.ListGoalsInWorkspaceParams{WorkspaceID: ws})
	if err != nil {
		return nil, fmt.Errorf("export goals: %w", err)
	}
	byID := map[string]db.Goal{}
	for _, g := range goals {
		byID[util.UUIDToString(g.ID)] = g
	}
	depth := func(g db.Goal) int {
		d := 0
		for cur := g; cur.ParentGoalID.Valid && d < len(goals); d++ {
			parent, ok := byID[util.UUIDToString(cur.ParentGoalID)]
			if !ok {
				break
			}
			cur = parent
		}
		return d
	}
	sort.SliceStable(goals, func(i, j int) bool {
		di, dj := depth(goals[i]), depth(goals[j])
		if di != dj {
			return di < dj
		}
		return goals[i].Title < goals[j].Title
	})

	out := []ExportGoal{}
	for _, g := range goals {
		if g.ScopeType == ScopeSquad && g.Status != GoalStatusActive && g.Status != GoalStatusDraft {
			continue
		}
		scope, ini, ok := names.ref(g.ScopeType, g.ScopeID)
		if !ok {
			continue
		}
		owner, ok := exportOwnerName(g.OwnerType, g.OwnerID, names, agentName)
		if !ok {
			continue
		}
		parent := ""
		if g.ParentGoalID.Valid {
			p, ok := byID[util.UUIDToString(g.ParentGoalID)]
			if !ok {
				continue
			}
			parent = p.Title
		}
		krs, err := q.ListKeyResultsForGoal(ctx, db.ListKeyResultsForGoalParams{WorkspaceID: ws, GoalID: g.ID})
		if err != nil {
			return nil, fmt.Errorf("export key results: %w", err)
		}
		eg := ExportGoal{
			Title: g.Title, Why: g.Why, ScopeType: g.ScopeType, Scope: scope, Initiative: ini, Parent: parent,
			PeriodStart: dateText(g.PeriodStart), PeriodEnd: dateText(g.PeriodEnd),
			OwnerType: g.OwnerType, Owner: owner, Status: g.Status, KeyResults: []ExportKeyResult{},
		}
		for _, kr := range krs {
			krScope, krIni, ok := names.ref(kr.ScopeType, kr.ScopeID)
			if !ok {
				eg.KeyResults = nil
				break
			}
			start, _ := floatFromNumeric(kr.StartValue)
			target, _ := floatFromNumeric(kr.TargetValue)
			eg.KeyResults = append(eg.KeyResults, ExportKeyResult{
				Title: kr.Title, MetricKey: kr.MetricKey, ScopeType: kr.ScopeType, Scope: krScope, Initiative: krIni,
				StartValue: start, TargetValue: target, Weight: int(kr.Weight),
			})
		}
		if eg.KeyResults == nil {
			continue
		}
		out = append(out, eg)
	}
	return out, nil
}

// exportOwnerName names a goal or policy owner. A member owner has no name the
// export can carry, so it is empty and ok; a squad or agent owner that the
// workspace does not have is not ok.
func exportOwnerName(ownerType string, id pgtype.UUID, names scopeNames, agentName map[string]string) (string, bool) {
	switch ownerType {
	case "member":
		return "", true
	case "squad":
		name, ok := names.squads[util.UUIDToString(id)]
		return name, ok
	case "agent":
		name, ok := agentName[util.UUIDToString(id)]
		return name, ok
	}
	return "", false
}

// exportBudgets lists the workspace's active and paused budgets with their
// definitions. Spend is not exported.
func exportBudgets(ctx context.Context, q *db.Queries, ws pgtype.UUID, now time.Time, names scopeNames) ([]ExportBudget, error) {
	rows, err := q.ListWorkspaceBudgetStates(ctx, db.ListWorkspaceBudgetStatesParams{
		WorkspaceID: ws, Now: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("export budgets: %w", err)
	}
	out := []ExportBudget{}
	for _, b := range rows {
		if b.Status != BudgetStatusActive && b.Status != BudgetStatusPaused {
			continue
		}
		scope, ini, ok := names.ref(b.ScopeType, b.ScopeID)
		if !ok {
			continue
		}
		out = append(out, ExportBudget{
			ScopeType: b.ScopeType, Scope: scope, Initiative: ini, Period: b.Period, AmountMicros: b.AmountMicros,
			SoftPercent: b.SoftPercent, Mode: b.Mode, Classes: b.Classes, Status: b.Status,
		})
	}
	return out, nil
}

// exportPolicies lists the standing approval policies, active and revoked, with
// their squads and floors named.
func exportPolicies(ctx context.Context, q *db.Queries, ws pgtype.UUID, names scopeNames) ([]ExportPolicy, error) {
	rows, err := q.ListApprovalPolicies(ctx, ws)
	if err != nil {
		return nil, fmt.Errorf("export approval policies: %w", err)
	}
	out := []ExportPolicy{}
	for _, p := range rows {
		var match proposalkinds.PolicyMatch
		if err := json.Unmarshal(p.Match, &match); err != nil {
			return nil, fmt.Errorf("read policy %q match: %w", p.Name, err)
		}
		em := ExportPolicyMatch{MaxCostMicros: match.MaxCostMicros, PayloadEquals: match.PayloadEquals, Tags: match.Tags}
		for _, raw := range match.SquadIDs {
			name, ok := names.squads[raw]
			if !ok {
				return nil, fmt.Errorf("policy %q names a squad the workspace does not have", p.Name)
			}
			em.Squads = append(em.Squads, name)
		}
		for _, raw := range match.FloorIDs {
			name, ok := names.floors[raw]
			if !ok {
				return nil, fmt.Errorf("policy %q names a floor the workspace does not have", p.Name)
			}
			em.Floors = append(em.Floors, name)
		}
		status := ExportPolicyRevoked
		if p.Active {
			status = ExportPolicyActive
		}
		out = append(out, ExportPolicy{
			Name: p.Name, Kind: p.Kind, Status: status, Match: em, MaxPerWeek: p.MaxPerWeek, ValidUntil: dateText(p.ValidUntil),
		})
	}
	return out, nil
}

// sortCompanyExport puts every list in a fixed order, so two exports of the same
// company are equal and the import's order does not show in the document.
func sortCompanyExport(out *CompanyExport) {
	sort.SliceStable(out.Agents, func(i, j int) bool { return out.Agents[i].Name < out.Agents[j].Name })
	sort.SliceStable(out.Squads, func(i, j int) bool { return out.Squads[i].Name < out.Squads[j].Name })
	for i := range out.Squads {
		sq := &out.Squads[i]
		sort.SliceStable(sq.Members, func(a, b int) bool {
			if sq.Members[a].Agent != sq.Members[b].Agent {
				return sq.Members[a].Agent < sq.Members[b].Agent
			}
			return sq.Members[a].Role < sq.Members[b].Role
		})
		sort.SliceStable(sq.Routines, func(a, b int) bool { return sq.Routines[a].Title < sq.Routines[b].Title })
	}
	sort.SliceStable(out.FloorChiefs, func(i, j int) bool {
		if out.FloorChiefs[i].Floor != out.FloorChiefs[j].Floor {
			return out.FloorChiefs[i].Floor < out.FloorChiefs[j].Floor
		}
		return out.FloorChiefs[i].Agent < out.FloorChiefs[j].Agent
	})
	sort.SliceStable(out.Pipelines, func(i, j int) bool { return out.Pipelines[i].Name < out.Pipelines[j].Name })
	sort.SliceStable(out.Initiatives, func(i, j int) bool {
		a, b := out.Initiatives[i], out.Initiatives[j]
		if a.Pipeline != b.Pipeline {
			return a.Pipeline < b.Pipeline
		}
		if a.Stage != b.Stage {
			return a.Stage < b.Stage
		}
		return a.Title < b.Title
	})
	sort.SliceStable(out.Budgets, func(i, j int) bool {
		a, b := out.Budgets[i], out.Budgets[j]
		if a.ScopeType != b.ScopeType {
			return a.ScopeType < b.ScopeType
		}
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		return a.Period < b.Period
	})
	sort.SliceStable(out.ApprovalPolicies, func(i, j int) bool { return out.ApprovalPolicies[i].Name < out.ApprovalPolicies[j].Name })
}

// dateText renders a date column as YYYY-MM-DD, or empty when it is null.
func dateText(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

// instructionTemplateOf returns the id of the starter instruction template whose
// text these instructions are, or empty when they are not that text.
func instructionTemplateOf(instructions string) string {
	if strings.TrimSpace(instructions) == "" {
		return ""
	}
	data, err := loadCompanyData()
	if err != nil {
		return ""
	}
	for _, t := range data.InstructionTemplates {
		if t.Instructions == instructions {
			return t.ID
		}
	}
	return ""
}

// exportStages rewrites a pipeline's owner squad ids as squad names.
func exportStages(raw []byte, squadName map[string]string) (json.RawMessage, error) {
	var stages []PipelineStage
	if err := decodeStages(raw, &stages); err != nil {
		return nil, err
	}
	for i := range stages {
		if stages[i].OwnerSquadID != nil {
			name := squadName[*stages[i].OwnerSquadID]
			stages[i].OwnerSquadID = &name
		}
	}
	out, err := json.Marshal(stages)
	if err != nil {
		return nil, fmt.Errorf("encode stages: %w", err)
	}
	return out, nil
}

// ValidateCompanyExport refuses a document this build cannot read.
func ValidateCompanyExport(doc CompanyExport) error {
	if doc.SchemaVersion != CompanyExportVersion {
		return fmt.Errorf("export schema version %d is not supported; this server reads version %d", doc.SchemaVersion, CompanyExportVersion)
	}
	seen := map[string]bool{}
	for _, a := range doc.Agents {
		if strings.TrimSpace(a.Name) == "" {
			return errors.New("an agent has no name")
		}
		if seen["agent:"+a.Name] {
			return fmt.Errorf("agent %q appears twice", a.Name)
		}
		seen["agent:"+a.Name] = true
	}
	for _, sq := range doc.Squads {
		if strings.TrimSpace(sq.Name) == "" {
			return errors.New("a squad has no name")
		}
		if seen["squad:"+sq.Name] {
			return fmt.Errorf("squad %q appears twice", sq.Name)
		}
		seen["squad:"+sq.Name] = true
		switch sq.Status {
		case "", ExportSquadActive, ExportSquadArchived:
		default:
			return fmt.Errorf("squad %q has status %q; it is active or archived", sq.Name, sq.Status)
		}
	}
	for _, p := range doc.ApprovalPolicies {
		switch p.Status {
		case "", ExportPolicyActive, ExportPolicyRevoked:
		default:
			return fmt.Errorf("approval policy %q has status %q; it is active or revoked", p.Name, p.Status)
		}
	}
	return nil
}
