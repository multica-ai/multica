package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Organisation changes (spec 4.6). A proposal of kind org_change carries one of
// eight changes in payload.type. Approval applies it inside the decision
// transaction and records an application row, which holds what the change
// replaced so it can be reverted. Every applied change opens a probation window;
// the probation check (probation.go) judges the windows whose subject has
// scorecard figures before and after it.

// The ten change types: the eight of spec 4.6 and the two rollbacks unretire and
// unmerge, which probation files when a retire or a merge is judged worse.
const (
	OrgChangeInstruction = "instruction_change"
	OrgChangeSkill       = "skill_change"
	OrgChangeCharter     = "charter_change"
	OrgChangeAutonomy    = "autonomy_change"
	OrgChangeHire        = "hire"
	OrgChangeRetire      = "retire"
	OrgChangeMove        = "move"
	OrgChangeMerge       = "merge_squads"
	OrgChangeUnretire    = "unretire"
	OrgChangeUnmerge     = "unmerge"
)

// OrgChangeProbationWindow is how long an applied change is on probation.
const OrgChangeProbationWindow = 14 * 24 * time.Hour

// OrgChangeInstructionsMax bounds an instruction change in characters.
const OrgChangeInstructionsMax = 50000

// Subject types of an application.
const (
	OrgSubjectAgent = "agent"
	OrgSubjectSquad = "squad"
)

// Errors the org change service returns.
var (
	// ErrOrgChangeAutonomyUnavailable: autonomy changes need the autonomy service,
	// which the server did not wire.
	ErrOrgChangeAutonomyUnavailable = errors.New("autonomy changes are not available on this server")
	// ErrOrgChangeCharterMoved: the charter changed after the proposal was filed.
	ErrOrgChangeCharterMoved = errors.New("the squad's charter changed after this proposal was filed; file it again against the current charter")
	// ErrOrgChangeAlreadyReverted: the change was reverted already.
	ErrOrgChangeAlreadyReverted = errors.New("this organisation change was already reverted")
	// ErrOrgChangeMergeUndone: the merge was reverted or unmerged already.
	ErrOrgChangeMergeUndone = errors.New("this merge was already reverted or unmerged")
	// ErrOrgChangeNotFound: no application of that id in the workspace.
	ErrOrgChangeNotFound = errors.New("organisation change not found")
)

// OrgChangeTypes lists the ten change types.
var OrgChangeTypes = []string{OrgChangeInstruction, OrgChangeSkill, OrgChangeCharter, OrgChangeAutonomy, OrgChangeHire, OrgChangeRetire, OrgChangeMove, OrgChangeMerge, OrgChangeUnretire, OrgChangeUnmerge}

// OrgChangeService validates, applies and reverts organisation changes.
type OrgChangeService struct {
	// ApplyAutonomy sets a squad's autonomy level through the autonomy service
	// and returns the level it replaced. The boss's approval is the actor.
	ApplyAutonomy func(ctx context.Context, q *db.Queries, ws, squadID pgtype.UUID, to int, actor GovernanceActor, reason string) (from int, err error)
	// Scorecards reads the before and after figures of a probation window.
	Scorecards *ScorecardService
	Now        func() time.Time
}

// NewOrgChangeService returns the service, wired to the autonomy service.
func NewOrgChangeService() *OrgChangeService {
	return &OrgChangeService{
		ApplyAutonomy: func(ctx context.Context, q *db.Queries, ws, squadID pgtype.UUID, to int, actor GovernanceActor, reason string) (int, error) {
			change, err := ApplyAutonomyChange(ctx, q, ws, squadID, to, actor, reason)
			return change.From, err
		},
		Scorecards: &ScorecardService{},
		Now:        time.Now,
	}
}

func (s *OrgChangeService) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// CharterFields is a squad charter as a change carries it. It mirrors the
// charter API's fields.
type CharterFields struct {
	Mission          string                  `json:"mission"`
	Responsibilities []CharterResponsibility `json:"responsibilities"`
	Outputs          []CharterOutput         `json:"outputs"`
	Boundaries       string                  `json:"boundaries"`
	Escalation       string                  `json:"escalation"`
	Intake           string                  `json:"intake"`
	ResponseHours    int32                   `json:"response_hours"`
}

// CharterResponsibility is one responsibility of a charter.
type CharterResponsibility struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// CharterOutput is one output of a charter.
type CharterOutput struct {
	Key         string `json:"key"`
	Kind        string `json:"kind"`
	KBKey       string `json:"kb_key,omitempty"`
	CadenceHint string `json:"cadence_hint"`
}

// The payloads, one per type. Each names its type and carries the rationale the
// boss reads. Unknown fields are refused at filing.
type instructionChangePayload struct {
	Type         string `json:"type"`
	AgentID      string `json:"agent_id"`
	Instructions string `json:"instructions"`
	DiffSummary  string `json:"diff_summary,omitempty"`
	Rationale    string `json:"rationale"`
}

type skillChangePayload struct {
	Type           string   `json:"type"`
	AgentID        string   `json:"agent_id"`
	AddSkillIDs    []string `json:"add_skill_ids"`
	RemoveSkillIDs []string `json:"remove_skill_ids"`
	Rationale      string   `json:"rationale"`
}

type charterChangePayload struct {
	Type         string        `json:"type"`
	SquadID      string        `json:"squad_id"`
	BaseRevision int64         `json:"base_revision"`
	Charter      CharterFields `json:"charter"`
	Rationale    string        `json:"rationale"`
}

type autonomyChangePayload struct {
	Type      string `json:"type"`
	SquadID   string `json:"squad_id"`
	From      int    `json:"from"`
	To        int    `json:"to"`
	Rationale string `json:"rationale"`
}

type hirePayload struct {
	Type        string `json:"type"`
	SquadID     string `json:"squad_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Template    string `json:"template"`
	RuntimeID   string `json:"runtime_id"`
	Rationale   string `json:"rationale"`
}

type retirePayload struct {
	Type      string `json:"type"`
	AgentID   string `json:"agent_id,omitempty"`
	SquadID   string `json:"squad_id,omitempty"`
	Rationale string `json:"rationale"`
}

// unretirePayload names the archived agent or squad to restore. Exactly one id is set.
type unretirePayload struct {
	Type      string `json:"type"`
	AgentID   string `json:"agent_id,omitempty"`
	SquadID   string `json:"squad_id,omitempty"`
	Rationale string `json:"rationale"`
}

// unmergePayload names the applied merge_squads change to undo. The merge keeps
// its member and routine snapshot in its application row, so the id is enough.
type unmergePayload struct {
	Type          string `json:"type"`
	ApplicationID string `json:"application_id"`
	Rationale     string `json:"rationale"`
}

type movePayload struct {
	Type      string `json:"type"`
	AgentID   string `json:"agent_id"`
	ToSquadID string `json:"to_squad_id"`
	Rationale string `json:"rationale"`
}

type mergePayload struct {
	Type         string         `json:"type"`
	FromSquadID  string         `json:"from_squad_id"`
	IntoSquadID  string         `json:"into_squad_id"`
	BaseRevision int64          `json:"base_revision"`
	Charter      *CharterFields `json:"charter,omitempty"`
	Rationale    string         `json:"rationale"`
}

// orgBefore is what an application replaced, so a revert can put it back. Only
// the fields of the application's type are set.
type orgBefore struct {
	Instructions    string         `json:"instructions,omitempty"`
	AddedSkillIDs   []string       `json:"added_skill_ids,omitempty"`
	RemovedSkillIDs []string       `json:"removed_skill_ids,omitempty"`
	Charter         *CharterFields `json:"charter,omitempty"`
	Level           *int           `json:"level,omitempty"`
	HomeSquadID     string         `json:"home_squad_id,omitempty"`
	// ProbationSquadID is the squad whose scorecard judges a hire or a retire
	// (probation.go): the squad the hire joins, or the squad the retired agent
	// belonged to or the retired squad itself.
	ProbationSquadID string `json:"probation_squad_id,omitempty"`
	// Merge: the source's members and routines, the members added to the target,
	// the members whose home moved to the target, and the target's charter.
	FromMembers    []string       `json:"from_members,omitempty"`
	FromRoutines   []string       `json:"from_routines,omitempty"`
	AddedToInto    []string       `json:"added_to_into,omitempty"`
	RehomedToInto  []string       `json:"rehomed_to_into,omitempty"`
	IntoCharter    *CharterFields `json:"into_charter,omitempty"`
	ArchivedBefore bool           `json:"archived_before,omitempty"`
}

// probationApplies reports whether an applied change of this type opens a
// probation window (spec 4.6: applied changes enter probation). Every type does.
// A window whose subject has no before or after figures stays open and is never
// judged: a hire has no prior scorecard, and a retired agent or squad has no
// later one, so probation defers for it (plan 11.15). A merge is judged on the
// target squad.
func probationApplies(changeType string) bool {
	switch changeType {
	case OrgChangeInstruction, OrgChangeSkill, OrgChangeCharter, OrgChangeAutonomy,
		OrgChangeHire, OrgChangeRetire, OrgChangeMove, OrgChangeMerge, OrgChangeUnretire, OrgChangeUnmerge:
		return true
	}
	return false
}

// decodeOrgChangeStrict decodes a payload object, refusing unknown fields.
func decodeOrgChangeStrict(payload []byte, into any) error {
	if len(payload) == 0 || string(payload) == "null" {
		payload = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	return nil
}

// OrgChangeTypeOf reads payload.type.
func OrgChangeTypeOf(payload []byte) string {
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(payload, &head)
	return head.Type
}

// ValidateOrgChange checks a filed payload against the workspace and returns its
// canonical form and a one-line summary. It reads through q, outside any
// transaction.
func (s *OrgChangeService) ValidateOrgChange(ctx context.Context, q *db.Queries, ws pgtype.UUID, payload []byte) ([]byte, string, error) {
	t := OrgChangeTypeOf(payload)
	var out any
	var err error
	switch t {
	case OrgChangeInstruction:
		var p instructionChangePayload
		if err = decodeOrgChangeStrict(payload, &p); err == nil {
			p.Rationale = strings.TrimSpace(p.Rationale)
			err = validateInstructionChange(ctx, q, ws, p)
			out = p
		}
	case OrgChangeSkill:
		var p skillChangePayload
		if err = decodeOrgChangeStrict(payload, &p); err == nil {
			err = validateSkillChange(ctx, q, ws, p)
			out = p
		}
	case OrgChangeCharter:
		var p charterChangePayload
		if err = decodeOrgChangeStrict(payload, &p); err == nil {
			err = validateCharterChange(ctx, q, ws, p.SquadID, p.BaseRevision, p.Charter, p.Rationale)
			out = p
		}
	case OrgChangeAutonomy:
		var p autonomyChangePayload
		if err = decodeOrgChangeStrict(payload, &p); err == nil {
			err = validateAutonomyChange(ctx, q, ws, p)
			out = p
		}
	case OrgChangeHire:
		var p hirePayload
		if err = decodeOrgChangeStrict(payload, &p); err == nil {
			err = validateHire(ctx, q, ws, p)
			out = p
		}
	case OrgChangeRetire:
		var p retirePayload
		if err = decodeOrgChangeStrict(payload, &p); err == nil {
			err = validateRetire(ctx, q, ws, p)
			out = p
		}
	case OrgChangeMove:
		var p movePayload
		if err = decodeOrgChangeStrict(payload, &p); err == nil {
			err = validateMove(ctx, q, ws, p)
			out = p
		}
	case OrgChangeMerge:
		var p mergePayload
		if err = decodeOrgChangeStrict(payload, &p); err == nil {
			err = validateMerge(ctx, q, ws, p)
			out = p
		}
	case OrgChangeUnretire:
		var p unretirePayload
		if err = decodeOrgChangeStrict(payload, &p); err == nil {
			err = validateUnretire(ctx, q, ws, p)
			out = p
		}
	case OrgChangeUnmerge:
		var p unmergePayload
		if err = decodeOrgChangeStrict(payload, &p); err == nil {
			err = validateUnmerge(ctx, q, ws, p)
			out = p
		}
	default:
		return nil, "", fmt.Errorf("type must be one of %s", strings.Join(OrgChangeTypes, ", "))
	}
	if err != nil {
		return nil, "", err
	}
	canonical, err := json.Marshal(out)
	if err != nil {
		return nil, "", fmt.Errorf("encode organisation change: %w", err)
	}
	return canonical, proposalkinds.OrgChangeTypeLabel(t), nil
}

func requireRationale(r string) error {
	if strings.TrimSpace(r) == "" {
		return errors.New("rationale is required: say what the change is for and what evidence it rests on")
	}
	return nil
}

// orgAgent loads a live agent of the workspace.
func orgAgent(ctx context.Context, q *db.Queries, ws pgtype.UUID, raw string) (db.Agent, error) {
	id, err := util.ParseUUID(raw)
	if err != nil {
		return db.Agent{}, fmt.Errorf("agent_id must be an agent id")
	}
	a, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Agent{}, errors.New("agent not found in this workspace")
	}
	if err != nil {
		return db.Agent{}, err
	}
	if a.ArchivedAt.Valid {
		return db.Agent{}, errors.New("agent is archived")
	}
	return a, nil
}

// orgSquad loads a live squad of the workspace.
func orgSquad(ctx context.Context, q *db.Queries, ws pgtype.UUID, raw, field string) (db.Squad, error) {
	id, err := util.ParseUUID(raw)
	if err != nil {
		return db.Squad{}, fmt.Errorf("%s must be a squad id", field)
	}
	sq, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: id, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Squad{}, fmt.Errorf("%s: squad not found in this workspace", field)
	}
	if err != nil {
		return db.Squad{}, err
	}
	if sq.ArchivedAt.Valid {
		return db.Squad{}, fmt.Errorf("%s: squad is archived", field)
	}
	return sq, nil
}

func validateInstructionChange(ctx context.Context, q *db.Queries, ws pgtype.UUID, p instructionChangePayload) error {
	if _, err := orgAgent(ctx, q, ws, p.AgentID); err != nil {
		return err
	}
	if strings.TrimSpace(p.Instructions) == "" {
		return errors.New("instructions are required")
	}
	if n := len([]rune(p.Instructions)); n > OrgChangeInstructionsMax {
		return fmt.Errorf("instructions must be %d characters or fewer", OrgChangeInstructionsMax)
	}
	return requireRationale(p.Rationale)
}

func validateSkillChange(ctx context.Context, q *db.Queries, ws pgtype.UUID, p skillChangePayload) error {
	if _, err := orgAgent(ctx, q, ws, p.AgentID); err != nil {
		return err
	}
	if len(p.AddSkillIDs)+len(p.RemoveSkillIDs) == 0 {
		return errors.New("name at least one skill to add or remove")
	}
	for _, raw := range append(slices.Clone(p.AddSkillIDs), p.RemoveSkillIDs...) {
		id, err := util.ParseUUID(raw)
		if err != nil {
			return fmt.Errorf("skill id %q is not a skill id", raw)
		}
		if _, err := q.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{ID: id, WorkspaceID: ws}); err != nil {
			return fmt.Errorf("skill %s is not in this workspace", raw)
		}
	}
	for _, id := range p.AddSkillIDs {
		if slices.Contains(p.RemoveSkillIDs, id) {
			return fmt.Errorf("skill %s is both added and removed", id)
		}
	}
	return requireRationale(p.Rationale)
}

// validateCharterChange checks the squad and that base_revision is the charter's
// current revision (0 when the squad has none yet).
func validateCharterChange(ctx context.Context, q *db.Queries, ws pgtype.UUID, squadRaw string, base int64, c CharterFields, rationale string) error {
	sq, err := orgSquad(ctx, q, ws, squadRaw, "squad_id")
	if err != nil {
		return err
	}
	current, err := charterRevision(ctx, q, ws, sq.ID)
	if err != nil {
		return err
	}
	if base != current {
		return fmt.Errorf("base_revision is %d but the charter is at revision %d; file the change against the current charter", base, current)
	}
	if strings.TrimSpace(c.Mission) == "" {
		return errors.New("charter mission is required")
	}
	if c.ResponseHours < 1 || c.ResponseHours > 336 {
		return errors.New("charter response_hours must be from 1 to 336")
	}
	return requireRationale(rationale)
}

func validateAutonomyChange(ctx context.Context, q *db.Queries, ws pgtype.UUID, p autonomyChangePayload) error {
	if _, err := orgSquad(ctx, q, ws, p.SquadID, "squad_id"); err != nil {
		return err
	}
	if p.From < 0 || p.From > 3 || p.To < 0 || p.To > 3 {
		return errors.New("autonomy levels run from 0 to 3")
	}
	if p.From == p.To {
		return errors.New("from and to are the same level")
	}
	return requireRationale(p.Rationale)
}

func validateHire(ctx context.Context, q *db.Queries, ws pgtype.UUID, p hirePayload) error {
	if _, err := orgSquad(ctx, q, ws, p.SquadID, "squad_id"); err != nil {
		return err
	}
	name := strings.TrimSpace(p.Name)
	if name == "" || len([]rune(name)) > 100 {
		return errors.New("name is required and at most 100 characters")
	}
	if _, ok := companyInstructionText(p.Template); !ok {
		return fmt.Errorf("template %q is not an instruction template", p.Template)
	}
	rid, err := util.ParseUUID(p.RuntimeID)
	if err != nil {
		return errors.New("runtime_id must be a runtime id")
	}
	if _, err := q.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{ID: rid, WorkspaceID: ws}); err != nil {
		return errors.New("runtime not found in this workspace")
	}
	return requireRationale(p.Rationale)
}

func validateRetire(ctx context.Context, q *db.Queries, ws pgtype.UUID, p retirePayload) error {
	switch {
	case p.AgentID != "" && p.SquadID == "":
		if _, err := orgAgent(ctx, q, ws, p.AgentID); err != nil {
			return err
		}
	case p.SquadID != "" && p.AgentID == "":
		if _, err := orgSquad(ctx, q, ws, p.SquadID, "squad_id"); err != nil {
			return err
		}
	default:
		return errors.New("name exactly one of agent_id and squad_id")
	}
	return requireRationale(p.Rationale)
}

// archivedSubject loads the archived agent or squad an unretire names. The
// two ids are exclusive, as in a retire.
func archivedSubject(ctx context.Context, q *db.Queries, ws pgtype.UUID, agentRaw, squadRaw string) (orgSubject, error) {
	switch {
	case agentRaw != "" && squadRaw == "":
		id, err := util.ParseUUID(agentRaw)
		if err != nil {
			return orgSubject{}, errors.New("agent_id must be an agent id")
		}
		a, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: ws})
		if errors.Is(err, pgx.ErrNoRows) {
			return orgSubject{}, errors.New("agent not found in this workspace")
		}
		if err != nil {
			return orgSubject{}, err
		}
		if !a.ArchivedAt.Valid {
			return orgSubject{}, errors.New("agent is not archived")
		}
		return orgSubject{OrgSubjectAgent, a.ID}, nil
	case squadRaw != "" && agentRaw == "":
		id, err := util.ParseUUID(squadRaw)
		if err != nil {
			return orgSubject{}, errors.New("squad_id must be a squad id")
		}
		sq, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: id, WorkspaceID: ws})
		if errors.Is(err, pgx.ErrNoRows) {
			return orgSubject{}, errors.New("squad_id: squad not found in this workspace")
		}
		if err != nil {
			return orgSubject{}, err
		}
		if !sq.ArchivedAt.Valid {
			return orgSubject{}, errors.New("squad_id: squad is not archived")
		}
		return orgSubject{OrgSubjectSquad, sq.ID}, nil
	}
	return orgSubject{}, errors.New("name exactly one of agent_id and squad_id")
}

func validateUnretire(ctx context.Context, q *db.Queries, ws pgtype.UUID, p unretirePayload) error {
	if _, err := archivedSubject(ctx, q, ws, p.AgentID, p.SquadID); err != nil {
		return err
	}
	return requireRationale(p.Rationale)
}

// mergeApplication loads the applied merge an unmerge names. It must be a merge
// that has not been reverted or unmerged yet.
func mergeApplication(ctx context.Context, q *db.Queries, ws pgtype.UUID, raw string) (db.OrgChangeApplication, error) {
	id, err := util.ParseUUID(raw)
	if err != nil {
		return db.OrgChangeApplication{}, errors.New("application_id must be an organisation change id")
	}
	app, err := q.GetOrgChangeApplicationInWorkspace(ctx, db.GetOrgChangeApplicationInWorkspaceParams{ID: id, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.OrgChangeApplication{}, ErrOrgChangeNotFound
	}
	if err != nil {
		return db.OrgChangeApplication{}, err
	}
	if app.ChangeType != OrgChangeMerge {
		return db.OrgChangeApplication{}, errors.New("application_id must name an applied merge_squads change")
	}
	if app.RevertedAt.Valid {
		return db.OrgChangeApplication{}, ErrOrgChangeMergeUndone
	}
	return app, nil
}

func validateUnmerge(ctx context.Context, q *db.Queries, ws pgtype.UUID, p unmergePayload) error {
	if _, err := mergeApplication(ctx, q, ws, p.ApplicationID); err != nil {
		return err
	}
	return requireRationale(p.Rationale)
}

func validateMove(ctx context.Context, q *db.Queries, ws pgtype.UUID, p movePayload) error {
	if _, err := orgAgent(ctx, q, ws, p.AgentID); err != nil {
		return err
	}
	if _, err := orgSquad(ctx, q, ws, p.ToSquadID, "to_squad_id"); err != nil {
		return err
	}
	return requireRationale(p.Rationale)
}

func validateMerge(ctx context.Context, q *db.Queries, ws pgtype.UUID, p mergePayload) error {
	if p.FromSquadID == p.IntoSquadID {
		return errors.New("a squad cannot merge into itself")
	}
	if _, err := orgSquad(ctx, q, ws, p.FromSquadID, "from_squad_id"); err != nil {
		return err
	}
	into, err := orgSquad(ctx, q, ws, p.IntoSquadID, "into_squad_id")
	if err != nil {
		return err
	}
	if p.Charter != nil {
		current, err := charterRevision(ctx, q, ws, into.ID)
		if err != nil {
			return err
		}
		if p.BaseRevision != current {
			return fmt.Errorf("base_revision is %d but the charter is at revision %d", p.BaseRevision, current)
		}
		if strings.TrimSpace(p.Charter.Mission) == "" {
			return errors.New("merged charter mission is required")
		}
	}
	return requireRationale(p.Rationale)
}

// companyInstructionText is the text of an instruction template of the starter
// company, the templates a hire starts from.
func companyInstructionText(id string) (string, bool) {
	data, err := loadCompanyData()
	if err != nil {
		return "", false
	}
	return data.instructions(id)
}

// charterRevision is the squad charter's revision, 0 when it has none.
func charterRevision(ctx context.Context, q *db.Queries, ws, squadID pgtype.UUID) (int64, error) {
	c, err := q.GetSquadCharter(ctx, db.GetSquadCharterParams{SquadID: squadID, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return c.Revision, nil
}

// OrgChangeFilerMayFile reports whether an agent may file an org change (spec
// 4.6): the reviewer, the Secretary, or the leader of a squad the change concerns.
// Members (people) may always file; the caller checks that before calling here.
func OrgChangeFilerMayFile(ctx context.Context, q *db.Queries, ws, filer pgtype.UUID, payload []byte) (bool, error) {
	wsRow, err := q.GetWorkspace(ctx, ws)
	if err != nil {
		return false, err
	}
	if wsRow.SecretaryAgentID.Valid && wsRow.SecretaryAgentID == filer {
		return true, nil
	}
	if reviewer, err := q.GetReviewerAgent(ctx, ws); err == nil && reviewer.ID == filer {
		return true, nil
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	concerned, err := orgChangeSquads(ctx, q, ws, payload)
	if err != nil {
		return false, err
	}
	led, err := q.ListSquadsLedByAgent(ctx, db.ListSquadsLedByAgentParams{WorkspaceID: ws, AgentID: filer})
	if err != nil {
		return false, err
	}
	for _, sq := range concerned {
		if slices.Contains(led, sq) {
			return true, nil
		}
	}
	return false, nil
}

// orgChangeSquads lists the squads a change concerns: the squad a change names,
// or the home squad of the agent it names.
func orgChangeSquads(ctx context.Context, q *db.Queries, ws pgtype.UUID, payload []byte) ([]pgtype.UUID, error) {
	var agentIDs, squadIDs []string
	switch OrgChangeTypeOf(payload) {
	case OrgChangeInstruction:
		var p instructionChangePayload
		_ = json.Unmarshal(payload, &p)
		agentIDs = append(agentIDs, p.AgentID)
	case OrgChangeSkill:
		var p skillChangePayload
		_ = json.Unmarshal(payload, &p)
		agentIDs = append(agentIDs, p.AgentID)
	case OrgChangeCharter:
		var p charterChangePayload
		_ = json.Unmarshal(payload, &p)
		squadIDs = append(squadIDs, p.SquadID)
	case OrgChangeAutonomy:
		var p autonomyChangePayload
		_ = json.Unmarshal(payload, &p)
		squadIDs = append(squadIDs, p.SquadID)
	case OrgChangeHire:
		var p hirePayload
		_ = json.Unmarshal(payload, &p)
		squadIDs = append(squadIDs, p.SquadID)
	case OrgChangeRetire:
		var p retirePayload
		_ = json.Unmarshal(payload, &p)
		if p.AgentID != "" {
			agentIDs = append(agentIDs, p.AgentID)
		} else {
			squadIDs = append(squadIDs, p.SquadID)
		}
	case OrgChangeUnretire:
		var p unretirePayload
		_ = json.Unmarshal(payload, &p)
		if p.AgentID != "" {
			agentIDs = append(agentIDs, p.AgentID)
		} else {
			squadIDs = append(squadIDs, p.SquadID)
		}
	case OrgChangeUnmerge:
		var p unmergePayload
		_ = json.Unmarshal(payload, &p)
		if id, err := util.ParseUUID(p.ApplicationID); err == nil {
			if app, err := q.GetOrgChangeApplicationInWorkspace(ctx, db.GetOrgChangeApplicationInWorkspaceParams{ID: id, WorkspaceID: ws}); err == nil {
				var merged mergePayload
				_ = json.Unmarshal(app.AfterState, &merged)
				squadIDs = append(squadIDs, merged.FromSquadID, merged.IntoSquadID)
			}
		}
	case OrgChangeMove:
		var p movePayload
		_ = json.Unmarshal(payload, &p)
		agentIDs = append(agentIDs, p.AgentID)
		squadIDs = append(squadIDs, p.ToSquadID)
	case OrgChangeMerge:
		var p mergePayload
		_ = json.Unmarshal(payload, &p)
		squadIDs = append(squadIDs, p.FromSquadID, p.IntoSquadID)
	}
	var out []pgtype.UUID
	for _, raw := range squadIDs {
		if id, err := util.ParseUUID(raw); err == nil {
			out = append(out, id)
		}
	}
	for _, raw := range agentIDs {
		id, err := util.ParseUUID(raw)
		if err != nil {
			continue
		}
		a, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: ws})
		if err != nil {
			continue
		}
		if a.HomeSquadID.Valid {
			out = append(out, a.HomeSquadID)
		}
	}
	return out, nil
}

// ApplyOrgChange applies an approved change inside the decision transaction q
// is bound to, and records the application and its governance event.
func (s *OrgChangeService) ApplyOrgChange(ctx context.Context, q *db.Queries, d proposalkinds.Decision) error {
	ws := d.WorkspaceID
	actor := GovernanceActor{Type: GovernanceActorMember, ID: d.DeciderID}
	changeType := OrgChangeTypeOf(d.Proposal.Payload)
	subject, before, err := s.apply(ctx, q, ws, actor, d.Proposal.ID, changeType, d.Proposal.Payload)
	if err != nil {
		return err
	}
	beforeRaw, err := json.Marshal(before)
	if err != nil {
		return fmt.Errorf("encode before state: %w", err)
	}
	afterRaw := d.Proposal.Payload
	now := s.now()
	ins := db.InsertOrgChangeApplicationParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   ws,
		ProposalID:    d.Proposal.ID,
		ChangeType:    changeType,
		SubjectType:   subject.kind,
		SubjectID:     subject.id,
		BeforeState:   beforeRaw,
		AfterState:    afterRaw,
		AppliedByType: GovernanceActorMember,
		AppliedByID:   d.DeciderID,
	}
	if probationApplies(changeType) {
		ins.ProbationEndsAt = pgtype.Timestamptz{Time: now.Add(OrgChangeProbationWindow), Valid: true}
		// The before figure is the subject's own scorecard, except for a hire and
		// a retire, which are judged on their squad (see probationJudgement).
		beforeKind, beforeID := subject.kind, subject.id
		if changeType == OrgChangeHire || changeType == OrgChangeRetire {
			beforeKind = ScorecardSubjectSquad
			beforeID = probationSquadUUID(before.ProbationSquadID)
		}
		if beforeID.Valid {
			if sc, err := s.Scorecards.LatestOKAsOf(ctx, q, ws, beforeKind, beforeID, ScorecardPeriodWeek, now); err == nil {
				ins.BeforeScorecardID = sc.ID
			} else if !errors.Is(err, ErrScorecardNotFound) {
				return err
			}
		}
	}
	if _, err := q.InsertOrgChangeApplication(ctx, ins); err != nil {
		return fmt.Errorf("record organisation change: %w", err)
	}
	return NewGovernanceLog().Record(ctx, q, GovernanceEvent{
		WorkspaceID: ws,
		Kind:        GovernanceKindOrgChangeApplied,
		ActorType:   GovernanceActorMember,
		ActorID:     d.DeciderID,
		SubjectType: subject.kind,
		SubjectID:   subject.id,
		Summary:     fmt.Sprintf("Applied %s.", strings.ToLower(proposalkinds.OrgChangeTypeLabel(changeType))),
		Details:     applyDetails(changeType, d.Proposal.ID, before),
	})
}

// applyDetails is the governance entry's detail. A merge carries the source
// squad's member and routine snapshot, the list an unmerge restores.
func applyDetails(changeType string, proposalID pgtype.UUID, before orgBefore) map[string]any {
	details := map[string]any{"type": changeType, "proposal_id": util.UUIDToString(proposalID)}
	if changeType == OrgChangeMerge {
		details["from_members"] = before.FromMembers
		details["from_routines"] = before.FromRoutines
	}
	return details
}

// orgSubject is the subject an application names.
type orgSubject struct {
	kind string
	id   pgtype.UUID
}

// apply performs one change and returns its subject and what it replaced.
func (s *OrgChangeService) apply(ctx context.Context, q *db.Queries, ws pgtype.UUID, actor GovernanceActor, proposalID pgtype.UUID, changeType string, payload []byte) (orgSubject, orgBefore, error) {
	switch changeType {
	case OrgChangeInstruction:
		var p instructionChangePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		agent, err := orgAgent(ctx, q, ws, p.AgentID)
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		before := orgBefore{Instructions: agent.Instructions}
		_, _, err = SetAgentInstructions(ctx, q, agent.ID, p.Instructions, InstructionChange{
			Source: InstructionSourceProposal, ProposalID: proposalID, Actor: actor,
		})
		return orgSubject{OrgSubjectAgent, agent.ID}, before, err

	case OrgChangeSkill:
		var p skillChangePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		agent, err := orgAgent(ctx, q, ws, p.AgentID)
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		current, err := q.ListAgentSkills(ctx, agent.ID)
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		has := map[string]bool{}
		for _, sk := range current {
			has[util.UUIDToString(sk.ID)] = true
		}
		var before orgBefore
		for _, id := range p.AddSkillIDs {
			if !has[id] {
				before.AddedSkillIDs = append(before.AddedSkillIDs, id)
			}
		}
		for _, id := range p.RemoveSkillIDs {
			if has[id] {
				before.RemovedSkillIDs = append(before.RemovedSkillIDs, id)
			}
		}
		for _, id := range before.AddedSkillIDs {
			if err := q.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: agent.ID, SkillID: parseUUIDMust(id)}); err != nil {
				return orgSubject{}, orgBefore{}, err
			}
		}
		for _, id := range before.RemovedSkillIDs {
			if err := q.RemoveAgentSkill(ctx, db.RemoveAgentSkillParams{AgentID: agent.ID, SkillID: parseUUIDMust(id)}); err != nil {
				return orgSubject{}, orgBefore{}, err
			}
		}
		return orgSubject{OrgSubjectAgent, agent.ID}, before, nil

	case OrgChangeCharter:
		var p charterChangePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		sq, err := orgSquad(ctx, q, ws, p.SquadID, "squad_id")
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		before, err := writeCharter(ctx, q, ws, sq.ID, p.BaseRevision, p.Charter, actor)
		return orgSubject{OrgSubjectSquad, sq.ID}, orgBefore{Charter: before}, err

	case OrgChangeAutonomy:
		var p autonomyChangePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		sq, err := orgSquad(ctx, q, ws, p.SquadID, "squad_id")
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		if s.ApplyAutonomy == nil {
			return orgSubject{}, orgBefore{}, ErrOrgChangeAutonomyUnavailable
		}
		from, err := s.ApplyAutonomy(ctx, q, ws, sq.ID, p.To, actor, p.Rationale)
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		return orgSubject{OrgSubjectSquad, sq.ID}, orgBefore{Level: &from}, nil

	case OrgChangeHire:
		var p hirePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		agent, err := hireAgent(ctx, q, ws, actor, proposalID, p)
		return orgSubject{OrgSubjectAgent, agent.ID}, orgBefore{ProbationSquadID: p.SquadID}, err

	case OrgChangeRetire:
		var p retirePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		return retireSubject(ctx, q, ws, actor, p)

	case OrgChangeMove:
		var p movePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		agent, err := orgAgent(ctx, q, ws, p.AgentID)
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		to, err := orgSquad(ctx, q, ws, p.ToSquadID, "to_squad_id")
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		before := orgBefore{}
		if agent.HomeSquadID.Valid {
			before.HomeSquadID = util.UUIDToString(agent.HomeSquadID)
			if agent.HomeSquadID != to.ID {
				if _, err := q.RemoveSquadMember(ctx, db.RemoveSquadMemberParams{SquadID: agent.HomeSquadID, MemberType: "agent", MemberID: agent.ID}); err != nil {
					return orgSubject{}, orgBefore{}, err
				}
			}
		}
		if err := ensureSquadMember(ctx, q, to.ID, agent.ID); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		if _, err := q.SetAgentHomeSquad(ctx, db.SetAgentHomeSquadParams{HomeSquadID: to.ID, ID: agent.ID, WorkspaceID: ws}); err != nil {
			return orgSubject{}, orgBefore{}, fmt.Errorf("set home squad: %w", err)
		}
		return orgSubject{OrgSubjectAgent, agent.ID}, before, nil

	case OrgChangeMerge:
		var p mergePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		return s.merge(ctx, q, ws, actor, p)

	case OrgChangeUnretire:
		var p unretirePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		subject, err := archivedSubject(ctx, q, ws, p.AgentID, p.SquadID)
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		// Restores the subject only: nothing is re-queued, and the knowledge it
		// kept was never removed.
		if subject.kind == OrgSubjectAgent {
			_, err = q.RestoreAgent(ctx, subject.id)
		} else {
			_, err = q.UnarchiveSquad(ctx, db.UnarchiveSquadParams{ID: subject.id, WorkspaceID: ws})
		}
		return subject, orgBefore{}, err

	case OrgChangeUnmerge:
		var p unmergePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		return s.unmerge(ctx, q, ws, actor, p)
	}
	return orgSubject{}, orgBefore{}, fmt.Errorf("unknown organisation change %q", changeType)
}

// ensureSquadMember adds an agent to a squad unless it is already a member.
func ensureSquadMember(ctx context.Context, q *db.Queries, squadID, agentID pgtype.UUID) error {
	member, err := q.IsSquadMember(ctx, db.IsSquadMemberParams{SquadID: squadID, MemberType: "agent", MemberID: agentID})
	if err != nil || member {
		return err
	}
	_, err = q.AddSquadMember(ctx, db.AddSquadMemberParams{SquadID: squadID, MemberType: "agent", MemberID: agentID, Role: "member"})
	return err
}

// writeCharter replaces a squad's charter when its revision is still base, and
// returns the charter it replaced, in the form a charter change carries.
func writeCharter(ctx context.Context, q *db.Queries, ws, squadID pgtype.UUID, base int64, next CharterFields, actor GovernanceActor) (*CharterFields, error) {
	current, err := q.GetSquadCharter(ctx, db.GetSquadCharterParams{SquadID: squadID, WorkspaceID: ws})
	hasRow := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var prior *CharterFields
	if hasRow {
		prior, err = charterFieldsOf(current)
		if err != nil {
			return nil, err
		}
	}
	if (hasRow && current.Revision != base) || (!hasRow && base != 0) {
		return nil, ErrOrgChangeCharterMoved
	}
	resp, err := json.Marshal(nonNilCharter(next.Responsibilities))
	if err != nil {
		return nil, err
	}
	outs, err := json.Marshal(nonNilOutputs(next.Outputs))
	if err != nil {
		return nil, err
	}
	actorID := actor.ID
	if !hasRow {
		_, err = q.InsertSquadCharter(ctx, db.InsertSquadCharterParams{
			SquadID: squadID, WorkspaceID: ws, Mission: next.Mission, Responsibilities: resp, Outputs: outs,
			Boundaries: next.Boundaries, Escalation: next.Escalation, Intake: next.Intake,
			ResponseHours: next.ResponseHours, OperatingReviewEnabled: false,
			UpdatedByType: actor.Type, UpdatedByID: actorID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrgChangeCharterMoved
		}
		return prior, err
	}
	_, err = q.UpdateSquadCharter(ctx, db.UpdateSquadCharterParams{
		SquadID: squadID, WorkspaceID: ws, Mission: next.Mission, Responsibilities: resp, Outputs: outs,
		Boundaries: next.Boundaries, Escalation: next.Escalation, Intake: next.Intake,
		ResponseHours: next.ResponseHours, UpdatedByType: actor.Type, UpdatedByID: actorID,
		Revision: base,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOrgChangeCharterMoved
	}
	return prior, err
}

func nonNilCharter(in []CharterResponsibility) []CharterResponsibility {
	if in == nil {
		return []CharterResponsibility{}
	}
	return in
}

func nonNilOutputs(in []CharterOutput) []CharterOutput {
	if in == nil {
		return []CharterOutput{}
	}
	return in
}

// charterFieldsOf reads a stored charter into the form a change carries.
func charterFieldsOf(c db.SquadCharter) (*CharterFields, error) {
	out := &CharterFields{Mission: c.Mission, Boundaries: c.Boundaries, Escalation: c.Escalation, Intake: c.Intake, ResponseHours: c.ResponseHours}
	if err := json.Unmarshal(c.Responsibilities, &out.Responsibilities); err != nil {
		return nil, fmt.Errorf("read charter responsibilities: %w", err)
	}
	if err := json.Unmarshal(c.Outputs, &out.Outputs); err != nil {
		return nil, fmt.Errorf("read charter outputs: %w", err)
	}
	return out, nil
}

// hireAgent creates an agent from an instruction template, makes the squad its
// home, and records its instructions as the first revision.
func hireAgent(ctx context.Context, q *db.Queries, ws pgtype.UUID, actor GovernanceActor, proposalID pgtype.UUID, p hirePayload) (db.Agent, error) {
	sq, err := orgSquad(ctx, q, ws, p.SquadID, "squad_id")
	if err != nil {
		return db.Agent{}, err
	}
	instructions, ok := companyInstructionText(p.Template)
	if !ok {
		return db.Agent{}, fmt.Errorf("template %q is not an instruction template", p.Template)
	}
	rid, err := util.ParseUUID(p.RuntimeID)
	if err != nil {
		return db.Agent{}, errors.New("runtime_id must be a runtime id")
	}
	rt, err := q.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{ID: rid, WorkspaceID: ws})
	if err != nil {
		return db.Agent{}, errors.New("runtime not found in this workspace")
	}
	owner, err := q.GetOldestWorkspaceOwnerMember(ctx, ws)
	if err != nil {
		return db.Agent{}, fmt.Errorf("workspace has no owner to own the new agent: %w", err)
	}
	agent, err := q.CreateAgent(ctx, db.CreateAgentParams{
		WorkspaceID:          ws,
		Name:                 strings.TrimSpace(p.Name),
		Description:          p.Description,
		Instructions:         instructions,
		RuntimeMode:          rt.RuntimeMode,
		RuntimeConfig:        []byte("{}"),
		RuntimeID:            rt.ID,
		Visibility:           "private",
		PermissionMode:       "private",
		MaxConcurrentTasks:   1,
		OwnerID:              owner.UserID,
		CustomEnv:            []byte("{}"),
		CustomArgs:           []byte("[]"),
		ConversationStarters: []byte("[]"),
	})
	if err != nil {
		return db.Agent{}, fmt.Errorf("create agent: %w", err)
	}
	if err := q.AssignKnowledgeMcpToAgent(ctx, db.AssignKnowledgeMcpToAgentParams{AgentID: agent.ID, WorkspaceID: ws}); err != nil {
		return db.Agent{}, err
	}
	if _, err := WriteAgentInstructionRevision(ctx, q, ws, agent.ID, instructions, InstructionChange{Source: InstructionSourceProposal, ProposalID: proposalID, Actor: actor}); err != nil {
		return db.Agent{}, err
	}
	if _, err := q.AddSquadMember(ctx, db.AddSquadMemberParams{SquadID: sq.ID, MemberType: "agent", MemberID: agent.ID, Role: "member"}); err != nil {
		return db.Agent{}, err
	}
	agent, err = q.SetAgentHomeSquad(ctx, db.SetAgentHomeSquadParams{HomeSquadID: sq.ID, ID: agent.ID, WorkspaceID: ws})
	if err != nil {
		return db.Agent{}, fmt.Errorf("set home squad: %w", err)
	}
	return agent, nil
}

// retireSubject archives an agent or a squad and cancels what it has not started.
// Running work finishes and the knowledge it wrote stays.
func retireSubject(ctx context.Context, q *db.Queries, ws pgtype.UUID, actor GovernanceActor, p retirePayload) (orgSubject, orgBefore, error) {
	if p.AgentID != "" {
		agent, err := orgAgent(ctx, q, ws, p.AgentID)
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		if _, err := q.CancelQueuedTasksByAgent(ctx, agent.ID); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		if _, err := q.ArchiveAgent(ctx, db.ArchiveAgentParams{ID: agent.ID, ArchivedBy: archivedBy(actor)}); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		var probationSquad string
		if agent.HomeSquadID.Valid {
			probationSquad = util.UUIDToString(agent.HomeSquadID)
		}
		return orgSubject{OrgSubjectAgent, agent.ID}, orgBefore{ProbationSquadID: probationSquad}, nil
	}
	sq, err := orgSquad(ctx, q, ws, p.SquadID, "squad_id")
	if err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	if _, err := q.CancelQueuedTasksBySquad(ctx, sq.ID); err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	if _, err := q.ArchiveSquad(ctx, db.ArchiveSquadParams{ID: sq.ID, ArchivedBy: archivedBy(actor)}); err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	return orgSubject{OrgSubjectSquad, sq.ID}, orgBefore{ProbationSquadID: util.UUIDToString(sq.ID)}, nil
}

// archivedBy is the actor a change archives on behalf of, when it has an id.
func archivedBy(actor GovernanceActor) pgtype.UUID {
	if actor.Type == GovernanceActorMember {
		return actor.ID
	}
	return pgtype.UUID{}
}

// merge moves the source squad's members and routines into the target, archives
// the source, and writes the merged charter when the change carries one.
func (s *OrgChangeService) merge(ctx context.Context, q *db.Queries, ws pgtype.UUID, actor GovernanceActor, p mergePayload) (orgSubject, orgBefore, error) {
	from, err := orgSquad(ctx, q, ws, p.FromSquadID, "from_squad_id")
	if err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	into, err := orgSquad(ctx, q, ws, p.IntoSquadID, "into_squad_id")
	if err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	var before orgBefore
	members, err := q.ListSquadMemberAgentIDs(ctx, from.ID)
	if err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	for _, m := range members {
		before.FromMembers = append(before.FromMembers, util.UUIDToString(m))
		member, err := q.IsSquadMember(ctx, db.IsSquadMemberParams{SquadID: into.ID, MemberType: "agent", MemberID: m})
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		if !member {
			if err := ensureSquadMember(ctx, q, into.ID, m); err != nil {
				return orgSubject{}, orgBefore{}, err
			}
			before.AddedToInto = append(before.AddedToInto, util.UUIDToString(m))
		}
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: m, WorkspaceID: ws})
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		if agent.HomeSquadID == from.ID {
			if _, err := q.SetAgentHomeSquad(ctx, db.SetAgentHomeSquadParams{HomeSquadID: into.ID, ID: m, WorkspaceID: ws}); err != nil {
				return orgSubject{}, orgBefore{}, err
			}
			before.RehomedToInto = append(before.RehomedToInto, util.UUIDToString(m))
		}
	}
	routines, err := q.ListSquadRoutineAutopilotIDs(ctx, db.ListSquadRoutineAutopilotIDsParams{WorkspaceID: ws, SquadID: from.ID})
	if err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	for _, r := range routines {
		before.FromRoutines = append(before.FromRoutines, util.UUIDToString(r))
	}
	if len(routines) > 0 {
		if err := q.MoveSquadRoutinesToSquad(ctx, db.MoveSquadRoutinesToSquadParams{ToSquadID: into.ID, WorkspaceID: ws, AutopilotIds: routines}); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
	}
	if p.Charter != nil {
		prior, err := writeCharter(ctx, q, ws, into.ID, p.BaseRevision, *p.Charter, actor)
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		before.IntoCharter = prior
	}
	if _, err := q.ArchiveSquad(ctx, db.ArchiveSquadParams{ID: from.ID, ArchivedBy: archivedBy(actor)}); err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	return orgSubject{OrgSubjectSquad, into.ID}, before, nil
}

// unmerge restores the source of an applied merge: it unarchives the source,
// takes back the members and routines the merge moved, and marks the merge
// undone so it cannot be unmerged twice. The merged squad's charter text stays
// as the boss left it; a merge's charter is not restored here.
func (s *OrgChangeService) unmerge(ctx context.Context, q *db.Queries, ws pgtype.UUID, actor GovernanceActor, p unmergePayload) (orgSubject, orgBefore, error) {
	app, err := mergeApplication(ctx, q, ws, p.ApplicationID)
	if err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	var snap orgBefore
	if err := json.Unmarshal(app.BeforeState, &snap); err != nil {
		return orgSubject{}, orgBefore{}, fmt.Errorf("read merge snapshot: %w", err)
	}
	var merged mergePayload
	if err := json.Unmarshal(app.AfterState, &merged); err != nil {
		return orgSubject{}, orgBefore{}, fmt.Errorf("read merge payload: %w", err)
	}
	from := parseUUIDMust(merged.FromSquadID)
	into, err := orgSquad(ctx, q, ws, merged.IntoSquadID, "into_squad_id")
	if err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	source, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: from, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return orgSubject{}, orgBefore{}, errors.New("the merged source squad no longer exists")
	}
	if err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	if !source.ArchivedAt.Valid {
		return orgSubject{}, orgBefore{}, errors.New("the source squad is not archived; there is nothing to unmerge")
	}
	if _, err := q.UnarchiveSquad(ctx, db.UnarchiveSquadParams{ID: from, WorkspaceID: ws}); err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	// Members and routines go back only where the merge put them, so a later
	// move the boss made stays.
	for _, m := range snap.AddedToInto {
		if _, err := q.RemoveSquadMember(ctx, db.RemoveSquadMemberParams{SquadID: into.ID, MemberType: "agent", MemberID: parseUUIDMust(m)}); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
	}
	for _, m := range snap.FromMembers {
		if err := ensureSquadMember(ctx, q, from, parseUUIDMust(m)); err != nil {
			return orgSubject{}, orgBefore{}, err
		}
	}
	for _, m := range snap.RehomedToInto {
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: parseUUIDMust(m), WorkspaceID: ws})
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		if agent.HomeSquadID == into.ID {
			if _, err := q.SetAgentHomeSquad(ctx, db.SetAgentHomeSquadParams{HomeSquadID: from, ID: agent.ID, WorkspaceID: ws}); err != nil {
				return orgSubject{}, orgBefore{}, err
			}
		}
	}
	if len(snap.FromRoutines) > 0 {
		here, err := q.ListSquadRoutineAutopilotIDs(ctx, db.ListSquadRoutineAutopilotIDsParams{WorkspaceID: ws, SquadID: into.ID})
		if err != nil {
			return orgSubject{}, orgBefore{}, err
		}
		var back []pgtype.UUID
		for _, r := range snap.FromRoutines {
			if id := parseUUIDMust(r); slices.Contains(here, id) {
				back = append(back, id)
			}
		}
		if len(back) > 0 {
			if err := q.MoveSquadRoutinesToSquad(ctx, db.MoveSquadRoutinesToSquadParams{ToSquadID: from, WorkspaceID: ws, AutopilotIds: back}); err != nil {
				return orgSubject{}, orgBefore{}, err
			}
		}
	}
	if _, err := q.MarkOrgChangeApplicationReverted(ctx, db.MarkOrgChangeApplicationRevertedParams{
		ID: app.ID, WorkspaceID: ws, RevertedByType: pgtype.Text{String: actor.Type, Valid: true}, RevertedByID: actor.ID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return orgSubject{}, orgBefore{}, ErrOrgChangeMergeUndone
	} else if err != nil {
		return orgSubject{}, orgBefore{}, err
	}
	return orgSubject{OrgSubjectSquad, from}, orgBefore{}, nil
}

// RevertOrgChange undoes an applied change, restoring what its application
// replaced, and records the revert. The revert of an instruction change is a
// rollback revision. A reverted application cannot be reverted again.
func (s *OrgChangeService) RevertOrgChange(ctx context.Context, q *db.Queries, ws, applicationID pgtype.UUID, actor GovernanceActor) (db.OrgChangeApplication, error) {
	app, err := q.GetOrgChangeApplicationInWorkspace(ctx, db.GetOrgChangeApplicationInWorkspaceParams{ID: applicationID, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.OrgChangeApplication{}, ErrOrgChangeNotFound
	}
	if err != nil {
		return db.OrgChangeApplication{}, err
	}
	if app.RevertedAt.Valid {
		return db.OrgChangeApplication{}, ErrOrgChangeAlreadyReverted
	}
	var before orgBefore
	if err := json.Unmarshal(app.BeforeState, &before); err != nil {
		return db.OrgChangeApplication{}, fmt.Errorf("read before state: %w", err)
	}
	if err := s.revert(ctx, q, ws, app, before, actor); err != nil {
		return db.OrgChangeApplication{}, err
	}
	marked, err := q.MarkOrgChangeApplicationReverted(ctx, db.MarkOrgChangeApplicationRevertedParams{
		ID: app.ID, WorkspaceID: ws, RevertedByType: pgtype.Text{String: actor.Type, Valid: true}, RevertedByID: actor.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.OrgChangeApplication{}, ErrOrgChangeAlreadyReverted
	}
	if err != nil {
		return db.OrgChangeApplication{}, err
	}
	err = NewGovernanceLog().Record(ctx, q, GovernanceEvent{
		WorkspaceID: ws, Kind: GovernanceKindOrgChangeReverted, ActorType: actor.Type, ActorID: actor.ID,
		SubjectType: app.SubjectType, SubjectID: app.SubjectID,
		Summary: fmt.Sprintf("Reverted %s.", strings.ToLower(proposalkinds.OrgChangeTypeLabel(app.ChangeType))),
		Details: map[string]any{"type": app.ChangeType, "proposal_id": util.UUIDToString(app.ProposalID)},
	})
	return marked, err
}

func (s *OrgChangeService) revert(ctx context.Context, q *db.Queries, ws pgtype.UUID, app db.OrgChangeApplication, before orgBefore, actor GovernanceActor) error {
	change := InstructionChange{Source: InstructionSourceRollback, ProposalID: app.ProposalID, Actor: actor}
	switch app.ChangeType {
	case OrgChangeInstruction:
		_, _, err := SetAgentInstructions(ctx, q, app.SubjectID, before.Instructions, change)
		return err
	case OrgChangeSkill:
		for _, id := range before.AddedSkillIDs {
			if err := q.RemoveAgentSkill(ctx, db.RemoveAgentSkillParams{AgentID: app.SubjectID, SkillID: parseUUIDMust(id)}); err != nil {
				return err
			}
		}
		for _, id := range before.RemovedSkillIDs {
			if err := q.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: app.SubjectID, SkillID: parseUUIDMust(id)}); err != nil {
				return err
			}
		}
		return nil
	case OrgChangeCharter:
		if before.Charter == nil {
			return errors.New("the charter had no earlier version to restore")
		}
		_, err := writeCharter(ctx, q, ws, app.SubjectID, mustCharterRevision(ctx, q, ws, app.SubjectID), *before.Charter, actor)
		return err
	case OrgChangeAutonomy:
		if before.Level == nil {
			return errors.New("the autonomy level before the change is unknown")
		}
		if s.ApplyAutonomy == nil {
			return ErrOrgChangeAutonomyUnavailable
		}
		_, err := s.ApplyAutonomy(ctx, q, ws, app.SubjectID, *before.Level, actor, "Reverts an autonomy change.")
		return err
	case OrgChangeHire:
		if _, err := q.CancelQueuedTasksByAgent(ctx, app.SubjectID); err != nil {
			return err
		}
		_, err := q.ArchiveAgent(ctx, db.ArchiveAgentParams{ID: app.SubjectID, ArchivedBy: archivedBy(actor)})
		return err
	case OrgChangeRetire:
		var p retirePayload
		if err := json.Unmarshal(app.AfterState, &p); err != nil {
			return err
		}
		if p.AgentID != "" {
			_, err := q.RestoreAgent(ctx, app.SubjectID)
			return err
		}
		_, err := q.UnarchiveSquad(ctx, db.UnarchiveSquadParams{ID: app.SubjectID, WorkspaceID: ws})
		return err
	case OrgChangeMove:
		var after movePayload
		if err := json.Unmarshal(app.AfterState, &after); err != nil {
			return err
		}
		agentID := app.SubjectID
		toID := parseUUIDMust(after.ToSquadID)
		if _, err := q.RemoveSquadMember(ctx, db.RemoveSquadMemberParams{SquadID: toID, MemberType: "agent", MemberID: agentID}); err != nil {
			return err
		}
		if before.HomeSquadID == "" {
			_, err := q.SetAgentHomeSquad(ctx, db.SetAgentHomeSquadParams{HomeSquadID: pgtype.UUID{}, ID: agentID, WorkspaceID: ws})
			return err
		}
		homeID := parseUUIDMust(before.HomeSquadID)
		if err := ensureSquadMember(ctx, q, homeID, agentID); err != nil {
			return err
		}
		_, err := q.SetAgentHomeSquad(ctx, db.SetAgentHomeSquadParams{HomeSquadID: homeID, ID: agentID, WorkspaceID: ws})
		return err
	case OrgChangeMerge:
		var after mergePayload
		if err := json.Unmarshal(app.AfterState, &after); err != nil {
			return err
		}
		from := parseUUIDMust(after.FromSquadID)
		into := parseUUIDMust(after.IntoSquadID)
		if _, err := q.UnarchiveSquad(ctx, db.UnarchiveSquadParams{ID: from, WorkspaceID: ws}); err != nil {
			return err
		}
		for _, m := range before.AddedToInto {
			if _, err := q.RemoveSquadMember(ctx, db.RemoveSquadMemberParams{SquadID: into, MemberType: "agent", MemberID: parseUUIDMust(m)}); err != nil {
				return err
			}
		}
		for _, m := range before.FromMembers {
			if err := ensureSquadMember(ctx, q, from, parseUUIDMust(m)); err != nil {
				return err
			}
		}
		for _, m := range before.RehomedToInto {
			if _, err := q.SetAgentHomeSquad(ctx, db.SetAgentHomeSquadParams{HomeSquadID: from, ID: parseUUIDMust(m), WorkspaceID: ws}); err != nil {
				return err
			}
		}
		if len(before.FromRoutines) > 0 {
			ids := make([]pgtype.UUID, 0, len(before.FromRoutines))
			for _, r := range before.FromRoutines {
				ids = append(ids, parseUUIDMust(r))
			}
			if err := q.MoveSquadRoutinesToSquad(ctx, db.MoveSquadRoutinesToSquadParams{ToSquadID: from, WorkspaceID: ws, AutopilotIds: ids}); err != nil {
				return err
			}
		}
		if before.IntoCharter != nil {
			_, err := writeCharter(ctx, q, ws, into, mustCharterRevision(ctx, q, ws, into), *before.IntoCharter, actor)
			return err
		}
		return nil
	case OrgChangeUnretire:
		// The revert of an unretire is a retire again: archive and cancel queued work.
		var p unretirePayload
		if err := json.Unmarshal(app.AfterState, &p); err != nil {
			return err
		}
		_, _, err := retireSubject(ctx, q, ws, actor, retirePayload{Type: OrgChangeRetire, AgentID: p.AgentID, SquadID: p.SquadID})
		return err
	case OrgChangeUnmerge:
		return errors.New("an unmerge cannot be reverted; file a merge_squads change to merge the squads again")
	}
	return fmt.Errorf("unknown organisation change %q", app.ChangeType)
}

// mustCharterRevision is the current revision, read for a revert that writes
// the charter back over its current version. A read failure falls back to the
// revision the write will refuse, so the revert fails loudly rather than
// overwriting blind.
func mustCharterRevision(ctx context.Context, q *db.Queries, ws, squadID pgtype.UUID) int64 {
	rev, err := charterRevision(ctx, q, ws, squadID)
	if err != nil {
		return -1
	}
	return rev
}

// parseUUIDMust parses an id this package wrote itself. An id it cannot parse is
// a corrupt record, and an empty id matches no row.
func parseUUIDMust(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}
