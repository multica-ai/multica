package operatingsystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	notetypes "github.com/multica-ai/multica/server/internal/cerebro/note_types"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrProjectNotInWorkspace = errors.New("project not found in workspace")
var ErrOwnerNotInWorkspace = errors.New("owner not found in workspace")
var ErrElementDisabled = errors.New("operating system element is disabled")

var validStrategyKinds = map[string]bool{
	"core_value": true, "core_focus": true, "horizon_goal": true,
}

var validHorizonUnits = map[string]bool{
	"day": true, "week": true, "month": true, "year": true,
}

var validRockHealth = map[string]bool{
	"on_track": true, "at_risk": true, "off_track": true, "unset": true,
}

var validMeetingCadenceUnits = map[string]bool{
	"manual": true, "day": true, "week": true, "month": true, "quarter": true,
}

var validMeetingBindings = map[string]bool{
	"none": true, "scorecard": true, "goals": true, "issues_list": true,
}

type ProjectReader interface {
	GetProjectInWorkspace(context.Context, db.GetProjectInWorkspaceParams) (db.Project, error)
	GetIssueInWorkspace(context.Context, db.GetIssueInWorkspaceParams) (db.Issue, error)
	GetMember(context.Context, pgtype.UUID) (db.Member, error)
	GetAgentInWorkspace(context.Context, db.GetAgentInWorkspaceParams) (db.Agent, error)
}

type Service struct {
	queries  *cerebrodb.Queries
	projects ProjectReader
	now      func() time.Time
}

func NewService(queries *cerebrodb.Queries, projects ProjectReader) *Service {
	return &Service{queries: queries, projects: projects, now: time.Now}
}

func DefaultTerminology() Terminology {
	return Terminology{
		Strategy: "Strategy", Rock: "Goal", Rocks: "Goals",
		VisionPlan: "Vision Plan", Meetings: "Cycles", OrgChart: "Roles",
		Scorecard: "Scorecard", IssuesList: "Issues List", StrategyMap: "Strategy Map",
	}
}

// OsElement is one entry in the code-owned element registry. The database
// stores per-workspace overrides only; defaults live here so new elements do
// not need a migration.
type OsElement struct {
	Key            string
	DefaultEnabled bool
}

// ElementRegistry lists every Operating System element a workspace can turn
// on or off. Elements whose interface has shipped default to enabled.
func ElementRegistry() []OsElement {
	return []OsElement{
		{Key: "vision_plan", DefaultEnabled: true},
		{Key: "goals", DefaultEnabled: true},
		{Key: "meetings", DefaultEnabled: true},
		{Key: "org_chart", DefaultEnabled: true},
		{Key: "scorecard", DefaultEnabled: false},
		{Key: "issues_list", DefaultEnabled: false},
		{Key: "strategy_map", DefaultEnabled: false},
	}
}

func IsRegisteredElement(key string) bool {
	for _, element := range ElementRegistry() {
		if element.Key == key {
			return true
		}
	}
	return false
}

// MergeElementSettings applies workspace overrides on top of the registry
// defaults, preserving registry order.
func MergeElementSettings(overrides map[string]bool) []OsElementResponse {
	registry := ElementRegistry()
	out := make([]OsElementResponse, 0, len(registry))
	for _, element := range registry {
		enabled := element.DefaultEnabled
		if override, ok := overrides[element.Key]; ok {
			enabled = override
		}
		out = append(out, OsElementResponse{Key: element.Key, Enabled: enabled, DefaultEnabled: element.DefaultEnabled})
	}
	return out
}

var goalTypeColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

const defaultGoalTypeColor = "#6366F1"

func ValidateGoalTypeInput(input GoalTypeInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("name is required")
	}
	if input.Color != "" && !goalTypeColorPattern.MatchString(input.Color) {
		return errors.New("color must be a #RRGGBB hex value")
	}
	return nil
}

// ResolvedPeriod is a validated OperatingPeriodInput with calendar bounds and
// a display name filled in.
type ResolvedPeriod struct {
	Name     string
	Unit     string
	StartsOn pgtype.Date
	EndsOn   pgtype.Date
}

// ResolvePeriodInput validates a period and computes bounds and name. Month
// and quarter periods snap to the calendar boundary containing starts_on and
// derive ends_on; custom periods require an explicit name and ends_on.
func ResolvePeriodInput(input OperatingPeriodInput) (ResolvedPeriod, error) {
	start, err := time.Parse("2006-01-02", input.StartsOn)
	if err != nil {
		return ResolvedPeriod{}, errors.New("starts_on must be YYYY-MM-DD")
	}
	name := strings.TrimSpace(input.Name)
	var end time.Time
	switch input.Unit {
	case "month":
		start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, -1)
		if name == "" {
			name = fmt.Sprintf("%s %d", start.Month().String(), start.Year())
		}
	case "quarter":
		quarter := (int(start.Month()) - 1) / 3
		start = time.Date(start.Year(), time.Month(quarter*3+1), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 3, -1)
		if name == "" {
			name = fmt.Sprintf("Q%d %d", quarter+1, start.Year())
		}
	case "custom":
		if name == "" {
			return ResolvedPeriod{}, errors.New("custom periods require a name")
		}
		end, err = time.Parse("2006-01-02", input.EndsOn)
		if err != nil {
			return ResolvedPeriod{}, errors.New("ends_on must be YYYY-MM-DD")
		}
		if end.Before(start) {
			return ResolvedPeriod{}, errors.New("ends_on must not precede starts_on")
		}
	default:
		return ResolvedPeriod{}, errors.New("unit must be month, quarter, or custom")
	}
	return ResolvedPeriod{
		Name: name, Unit: input.Unit,
		StartsOn: pgtype.Date{Time: start, Valid: true},
		EndsOn:   pgtype.Date{Time: end, Valid: true},
	}, nil
}

func NormalizeTerminology(raw []byte) Terminology {
	defaults := DefaultTerminology()
	var values map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return defaults
	}
	read := func(key, fallback string) string {
		value, ok := values[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return fallback
		}
		return strings.TrimSpace(value)
	}
	return Terminology{
		Strategy:    read("strategy", defaults.Strategy),
		Rock:        read("rock", defaults.Rock),
		Rocks:       read("rocks", defaults.Rocks),
		VisionPlan:  read("vision_plan", defaults.VisionPlan),
		Meetings:    read("meetings", defaults.Meetings),
		OrgChart:    read("org_chart", defaults.OrgChart),
		Scorecard:   read("scorecard", defaults.Scorecard),
		IssuesList:  read("issues_list", defaults.IssuesList),
		StrategyMap: read("strategy_map", defaults.StrategyMap),
	}
}

func ValidateStrategyInput(input StrategyItemInput) error {
	if !validStrategyKinds[input.Kind] {
		return fmt.Errorf("invalid strategy kind %q", input.Kind)
	}
	if strings.TrimSpace(input.Title) == "" {
		return errors.New("title is required")
	}
	if input.State != "" && input.State != "active" && input.State != "archived" {
		return fmt.Errorf("invalid strategy state %q", input.State)
	}
	if input.Kind == "horizon_goal" {
		if !validHorizonUnits[input.HorizonUnit] || input.HorizonCount <= 0 {
			return errors.New("horizon goals require a positive day, week, month, or year horizon")
		}
		return nil
	}
	if input.HorizonUnit != "" || input.HorizonCount != 0 {
		return errors.New("only horizon goals accept a horizon")
	}
	return nil
}

func ValidateVisionPlanSectionInput(input VisionPlanSectionInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("name is required")
	}
	if input.SectionType != "list" && input.SectionType != "structured" && input.SectionType != "process" && input.SectionType != "goals" {
		return errors.New("section_type must be list, structured, process, or goals")
	}
	if _, err := util.ParseUUID(input.PageID); err != nil {
		return errors.New("page_id must be a UUID")
	}
	if input.ColumnIndex < 0 || input.ColumnIndex > 2 {
		return errors.New("column_index must be between 0 and 2")
	}
	return nil
}

func ValidateVisionPlanPageInput(input VisionPlanPageInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("name is required")
	}
	if input.ColumnCount < 1 || input.ColumnCount > 3 {
		return errors.New("column_count must be between 1 and 3")
	}
	return nil
}

func ValidateVisionPlanItemInput(input VisionPlanItemInput) error {
	if _, err := util.ParseUUID(input.SectionID); err != nil {
		return errors.New("section_id must be a UUID")
	}
	if strings.TrimSpace(input.Title) == "" {
		return errors.New("title is required")
	}
	if input.State != "" && input.State != "active" && input.State != "archived" {
		return errors.New("state must be active or archived")
	}
	if (input.OwnerType == "") != (input.OwnerID == "") {
		return errors.New("owner_type and owner_id must be provided together")
	}
	if input.OwnerType != "" {
		if input.OwnerType != "member" && input.OwnerType != "agent" {
			return errors.New("owner_type must be member or agent")
		}
		if _, err := util.ParseUUID(input.OwnerID); err != nil {
			return errors.New("owner_id must be a UUID")
		}
	}
	return nil
}

func ValidateMeetingInput(input MeetingConfigInput) error {
	if !validMeetingCadenceUnits[input.CadenceUnit] {
		return fmt.Errorf("cadence_unit must be manual, day, week, month, or quarter")
	}
	if input.CadenceCount <= 0 {
		return errors.New("cadence_count must be > 0")
	}
	seen := make(map[string]bool, len(input.Agenda))
	for _, section := range input.Agenda {
		if strings.TrimSpace(section.ID) == "" {
			return errors.New("agenda section id is required")
		}
		if strings.TrimSpace(section.Name) == "" {
			return errors.New("agenda section name is required")
		}
		if !validMeetingBindings[section.Binding] {
			return fmt.Errorf("invalid agenda binding %q", section.Binding)
		}
		if seen[section.ID] {
			return fmt.Errorf("agenda section %q already exists", section.ID)
		}
		seen[section.ID] = true
	}
	return nil
}

func ValidateOrgChartSeatInput(input OrgChartSeatInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("name is required")
	}
	if (input.OwnerType == "") != (input.OwnerID == "") {
		return errors.New("owner_type and owner_id must be provided together")
	}
	if input.OwnerType != "" && input.OwnerType != "member" && input.OwnerType != "agent" {
		return errors.New("owner_type must be member or agent")
	}
	if input.OwnerID != "" {
		if _, err := util.ParseUUID(input.OwnerID); err != nil {
			return errors.New("owner_id must be a UUID")
		}
	}
	if input.ParentID != "" {
		if _, err := util.ParseUUID(input.ParentID); err != nil {
			return errors.New("parent_id must be a UUID")
		}
	}
	return nil
}

func ValidateRockInput(input RockInput) error {
	legacy := input.ProjectID != "" && input.Title == "" && input.PeriodID == ""
	if legacy {
		if _, err := util.ParseUUID(input.ProjectID); err != nil {
			return errors.New("project_id must be a UUID")
		}
		start, err := time.Parse("2006-01-02", input.PeriodStart)
		if err != nil {
			return errors.New("period_start must be YYYY-MM-DD")
		}
		end, err := time.Parse("2006-01-02", input.PeriodEnd)
		if err != nil {
			return errors.New("period_end must be YYYY-MM-DD")
		}
		if end.Before(start) {
			return errors.New("period_end must not precede period_start")
		}
	} else {
		if strings.TrimSpace(input.Title) == "" {
			return errors.New("title is required")
		}
		if _, err := util.ParseUUID(input.PeriodID); err != nil {
			return errors.New("period_id must be a UUID")
		}
	}
	if input.Confidence < 0 || input.Confidence > 100 {
		return errors.New("confidence must be between 0 and 100")
	}
	if !validRockHealth[input.ReportedHealth] {
		return fmt.Errorf("invalid reported_health %q", input.ReportedHealth)
	}
	if (input.OwnerType == "") != (input.OwnerID == "") {
		return errors.New("owner_type and owner_id must be provided together")
	}
	if input.OwnerType != "" {
		if input.OwnerType != "member" && input.OwnerType != "agent" {
			return errors.New("owner_type must be member or agent")
		}
		if _, err := util.ParseUUID(input.OwnerID); err != nil {
			return errors.New("owner_id must be a UUID")
		}
	}
	seen := make(map[string]bool)
	for _, id := range append(append([]string{}, input.ProjectIDs...), input.IssueIDs...) {
		if _, err := util.ParseUUID(id); err != nil {
			return errors.New("connected object ids must be UUIDs")
		}
		if seen[id] {
			return errors.New("connected object ids must not be duplicated")
		}
		seen[id] = true
	}
	if input.StrategyItemID != "" {
		if _, err := util.ParseUUID(input.StrategyItemID); err != nil {
			return errors.New("strategy_item_id must be a UUID")
		}
	}
	if input.GoalTypeID != "" {
		if _, err := util.ParseUUID(input.GoalTypeID); err != nil {
			return errors.New("goal_type_id must be a UUID")
		}
	}
	return nil
}

func DeriveHealth(total, done, blocked int32, now time.Time) DerivedHealth {
	result := DerivedHealth{CalculatedAt: now.UTC().Format(time.RFC3339)}
	switch {
	case blocked > 0:
		result.State = "off_track"
		result.Reason = fmt.Sprintf("%d of %d Issues are blocked", blocked, total)
	case total > 0 && done == total:
		result.State = "on_track"
		result.Reason = fmt.Sprintf("all %d Issues are done", total)
	case total == 0:
		result.State = "unset"
		result.Reason = "no execution is connected to this Rock"
	default:
		result.State = "at_risk"
		result.Reason = fmt.Sprintf("%d of %d Issues are done", done, total)
	}
	return result
}

func EnsureProjectInWorkspace(ctx context.Context, projects ProjectReader, projectID, workspaceID pgtype.UUID) error {
	_, err := projects.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProjectNotInWorkspace
	}
	return err
}

func EnsureOwnerInWorkspace(ctx context.Context, projects ProjectReader, ownerType string, ownerID, workspaceID pgtype.UUID) error {
	if ownerType == "member" {
		member, err := projects.GetMember(ctx, ownerID)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && member.WorkspaceID != workspaceID {
			return ErrOwnerNotInWorkspace
		}
		return err
	}
	_, err := projects.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: ownerID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrOwnerNotInWorkspace
	}
	return err
}

func IsDuplicateConnection(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Service) GetSettings(ctx context.Context, workspaceID pgtype.UUID) (SettingsResponse, error) {
	row, err := s.queries.GetOperatingSystemSettings(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SettingsResponse{WorkspaceID: util.UUIDToString(workspaceID), Terminology: DefaultTerminology()}, nil
	}
	if err != nil {
		return SettingsResponse{}, err
	}
	return SettingsResponse{
		WorkspaceID: util.UUIDToString(row.WorkspaceID),
		Terminology: NormalizeTerminology(row.Terminology),
		CreatedAt:   timestamp(row.CreatedAt),
		UpdatedAt:   timestamp(row.UpdatedAt),
	}, nil
}

func (s *Service) UpdateSettings(ctx context.Context, workspaceID pgtype.UUID, terminology Terminology) (SettingsResponse, error) {
	normalized := NormalizeTerminology(mustJSON(terminology))
	row, err := s.queries.UpsertOperatingSystemSettings(ctx, cerebrodb.UpsertOperatingSystemSettingsParams{
		WorkspaceID: workspaceID,
		Terminology: mustJSON(normalized),
	})
	if err != nil {
		return SettingsResponse{}, err
	}
	return SettingsResponse{
		WorkspaceID: util.UUIDToString(row.WorkspaceID), Terminology: normalized,
		CreatedAt: timestamp(row.CreatedAt), UpdatedAt: timestamp(row.UpdatedAt),
	}, nil
}

func (s *Service) CreateStrategyItem(ctx context.Context, workspaceID pgtype.UUID, input StrategyItemInput) (StrategyItemResponse, error) {
	if err := ValidateStrategyInput(input); err != nil {
		return StrategyItemResponse{}, err
	}
	row, err := s.queries.CreateStrategyItem(ctx, strategyParams(workspaceID, input))
	return strategyResponse(row.ID, row.WorkspaceID, row.Kind, row.Title, row.Description, row.HorizonUnit, row.HorizonCount, row.HorizonLabel, row.Position, row.State, row.CreatedAt, row.UpdatedAt), err
}

func (s *Service) ListStrategyItems(ctx context.Context, workspaceID pgtype.UUID) ([]StrategyItemResponse, error) {
	rows, err := s.queries.ListStrategyItems(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]StrategyItemResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, strategyResponse(row.ID, row.WorkspaceID, row.Kind, row.Title, row.Description, row.HorizonUnit, row.HorizonCount, row.HorizonLabel, row.Position, row.State, row.CreatedAt, row.UpdatedAt))
	}
	return out, nil
}

func (s *Service) ListStrategyHistory(ctx context.Context, workspaceID pgtype.UUID) ([]StrategyHistoryResponse, error) {
	rows, err := s.queries.ListStrategyItemHistory(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]StrategyHistoryResponse, 0, len(rows))
	for _, row := range rows {
		snapshot := make(map[string]any)
		if err := json.Unmarshal(row.Snapshot, &snapshot); err != nil {
			return nil, err
		}
		out = append(out, StrategyHistoryResponse{
			ID: util.UUIDToString(row.ID), StrategyItemID: util.UUIDToString(row.StrategyItemID),
			Action: row.Action, Title: row.Title, Snapshot: snapshot, ChangedAt: timestamp(row.ChangedAt),
		})
	}
	return out, nil
}

func (s *Service) UpdateStrategyItem(ctx context.Context, workspaceID, id pgtype.UUID, input StrategyItemInput) (StrategyItemResponse, error) {
	if err := ValidateStrategyInput(input); err != nil {
		return StrategyItemResponse{}, err
	}
	params := strategyParams(workspaceID, input)
	row, err := s.queries.UpdateStrategyItem(ctx, cerebrodb.UpdateStrategyItemParams{
		ID: id, WorkspaceID: params.WorkspaceID, Kind: params.Kind, Title: params.Title,
		Description: params.Description, HorizonUnit: params.HorizonUnit,
		HorizonCount: params.HorizonCount, HorizonLabel: params.HorizonLabel, Position: params.Position, State: params.State,
	})
	return strategyResponse(row.ID, row.WorkspaceID, row.Kind, row.Title, row.Description, row.HorizonUnit, row.HorizonCount, row.HorizonLabel, row.Position, row.State, row.CreatedAt, row.UpdatedAt), err
}

func (s *Service) DeleteStrategyItem(ctx context.Context, workspaceID, id pgtype.UUID) (bool, error) {
	count, err := s.queries.DeleteStrategyItem(ctx, cerebrodb.DeleteStrategyItemParams{ID: id, WorkspaceID: workspaceID})
	return count > 0, err
}

func (s *Service) ensureVisionPlanEnabled(ctx context.Context, workspaceID pgtype.UUID) error {
	elements, err := s.ListElements(ctx, workspaceID)
	if err != nil {
		return err
	}
	for _, element := range elements {
		if element.Key == "vision_plan" && element.Enabled {
			return nil
		}
	}
	return ErrElementDisabled
}

func (s *Service) ensureElementEnabled(ctx context.Context, workspaceID pgtype.UUID, key string) error {
	elements, err := s.ListElements(ctx, workspaceID)
	if err != nil {
		return err
	}
	for _, element := range elements {
		if element.Key == key && element.Enabled {
			return nil
		}
	}
	return ErrElementDisabled
}

func defaultMeetingAgenda() []MeetingAgendaSectionResponse {
	return []MeetingAgendaSectionResponse{
		{ID: "check-in", Name: "Check-in", Position: 0, Binding: "none"},
		{ID: "scorecard-review", Name: "Scorecard review", Position: 1, Binding: "scorecard"},
		{ID: "goal-review", Name: "Goal review", Position: 2, Binding: "goals"},
		{ID: "headlines", Name: "Headlines", Position: 3, Binding: "none"},
		{ID: "todos", Name: "Todos", Position: 4, Binding: "none"},
		{ID: "issue-solving", Name: "Issue solving", Position: 5, Binding: "issues_list"},
		{ID: "conclude", Name: "Conclude", Position: 6, Binding: "none"},
	}
}

func (s *Service) meetingNoteTypes(ctx context.Context, workspaceID pgtype.UUID) ([]MeetingNoteTypeResponse, error) {
	rows, err := s.queries.ListCerebroNoteTypesByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]MeetingNoteTypeResponse, 0, len(rows))
	for _, row := range rows {
		if !row.Enabled || row.CadenceUnit == "manual" {
			continue
		}
		item := MeetingNoteTypeResponse{
			ID: util.UUIDToString(row.ID), Name: row.Name, Icon: row.Icon,
			CadenceUnit: row.CadenceUnit, CadenceCount: row.CadenceCount, Enabled: row.Enabled,
			CurrentNoteID: s.currentNoteLink(ctx, row),
		}
		if row.AnchorWeekday.Valid {
			v := row.AnchorWeekday.Int16
			item.AnchorWeekday = &v
		}
		if row.AnchorWeekOfMonth.Valid {
			v := row.AnchorWeekOfMonth.Int16
			item.AnchorWeekOfMonth = &v
		}
		upcoming := notetypes.Upcoming(row, now, 4)
		dates := make([]string, 0, len(upcoming))
		for _, d := range upcoming {
			dates = append(dates, d.Format("2006-01-02"))
		}
		if len(dates) > 0 {
			item.NextMeetingDate = dates[0]
			item.UpcomingDates = dates
		}
		yearAhead := notetypes.UpcomingUntil(row, now, now.AddDate(1, 0, 0), 60)
		year := make([]string, 0, len(yearAhead))
		for _, d := range yearAhead {
			year = append(year, d.Format("2006-01-02"))
		}
		if len(year) > 0 {
			item.YearDates = year
		}
		if len(row.Participants) > 0 {
			var refs []MeetingParticipant
			if err := json.Unmarshal(row.Participants, &refs); err == nil {
				item.Participants = refs
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// currentNoteLink resolves the note to open from the planner: the single
// rolling document for running_doc types, otherwise the newest materialised
// note for new_note types. Empty when the type has never materialised a note.
func (s *Service) currentNoteLink(ctx context.Context, row cerebrodb.CerebroNoteType) string {
	if row.RunningDocArtifactID.Valid {
		return util.UUIDToString(row.RunningDocArtifactID)
	}
	id, err := s.queries.GetLatestNoteTypeArtifact(ctx, row.ID)
	if err != nil || !id.Valid {
		return ""
	}
	return util.UUIDToString(id)
}

func applyMeetingNoteType(response *MeetingConfigResponse, noteTypes []MeetingNoteTypeResponse) {
	if response.NoteTypeID == "" {
		return
	}
	for _, noteType := range noteTypes {
		if noteType.ID != response.NoteTypeID {
			continue
		}
		response.NoteTypeName = noteType.Name
		response.CurrentNoteID = noteType.CurrentNoteID
		response.CadenceUnit = noteType.CadenceUnit
		response.CadenceCount = noteType.CadenceCount
		return
	}
}

func parseMeetingAgenda(raw []byte) ([]MeetingAgendaSectionResponse, error) {
	if len(raw) == 0 {
		return []MeetingAgendaSectionResponse{}, nil
	}
	var agenda []MeetingAgendaSectionResponse
	if err := json.Unmarshal(raw, &agenda); err != nil {
		return nil, errors.New("meeting agenda is invalid")
	}
	return agenda, nil
}

func (s *Service) meetingResponse(ctx context.Context, workspaceID pgtype.UUID, row *cerebrodb.CerebroOperatingMeeting, agenda []MeetingAgendaSectionResponse) (MeetingConfigResponse, error) {
	noteTypes, err := s.meetingNoteTypes(ctx, workspaceID)
	if err != nil {
		return MeetingConfigResponse{}, err
	}
	response := MeetingConfigResponse{
		WorkspaceID: util.UUIDToString(workspaceID), CadenceUnit: "manual", CadenceCount: 1,
		Agenda: agenda, AvailableNoteTypes: noteTypes,
	}
	if row != nil {
		response.CadenceUnit = row.CadenceUnit
		response.CadenceCount = row.CadenceCount
		if row.NoteTypeID.Valid {
			response.NoteTypeID = util.UUIDToString(row.NoteTypeID)
		}
	}
	applyMeetingNoteType(&response, noteTypes)
	return response, nil
}

func (s *Service) GetMeeting(ctx context.Context, workspaceID pgtype.UUID) (MeetingConfigResponse, error) {
	if err := s.ensureElementEnabled(ctx, workspaceID, "meetings"); err != nil {
		return MeetingConfigResponse{}, err
	}
	row, err := s.queries.GetOperatingMeeting(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.meetingResponse(ctx, workspaceID, nil, defaultMeetingAgenda())
	}
	if err != nil {
		return MeetingConfigResponse{}, err
	}
	agenda, err := parseMeetingAgenda(row.Agenda)
	if err != nil {
		return MeetingConfigResponse{}, err
	}
	return s.meetingResponse(ctx, workspaceID, &row, agenda)
}

func (s *Service) UpdateMeeting(ctx context.Context, workspaceID pgtype.UUID, input MeetingConfigInput) (MeetingConfigResponse, error) {
	if err := s.ensureElementEnabled(ctx, workspaceID, "meetings"); err != nil {
		return MeetingConfigResponse{}, err
	}
	if err := ValidateMeetingInput(input); err != nil {
		return MeetingConfigResponse{}, err
	}
	noteTypeID := pgtype.UUID{}
	cadenceUnit := input.CadenceUnit
	cadenceCount := input.CadenceCount
	if input.NoteTypeID != "" {
		parsed, err := util.ParseUUID(input.NoteTypeID)
		if err != nil {
			return MeetingConfigResponse{}, errors.New("note_type_id must be a UUID")
		}
		noteType, err := s.queries.GetCerebroNoteType(ctx, parsed)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return MeetingConfigResponse{}, pgx.ErrNoRows
			}
			return MeetingConfigResponse{}, err
		}
		if noteType.WorkspaceID != workspaceID {
			return MeetingConfigResponse{}, pgx.ErrNoRows
		}
		if !noteType.Enabled || noteType.CadenceUnit == "manual" {
			return MeetingConfigResponse{}, errors.New("note_type_id must reference an enabled recurring note")
		}
		noteTypeID = parsed
		cadenceUnit = noteType.CadenceUnit
		cadenceCount = noteType.CadenceCount
	}
	agenda := make([]MeetingAgendaSectionResponse, 0, len(input.Agenda))
	for _, section := range input.Agenda {
		agenda = append(agenda, MeetingAgendaSectionResponse{
			ID: strings.TrimSpace(section.ID), Name: strings.TrimSpace(section.Name),
			Position: section.Position, Binding: section.Binding,
		})
	}
	rawAgenda, err := json.Marshal(agenda)
	if err != nil {
		return MeetingConfigResponse{}, err
	}
	row, err := s.queries.UpsertOperatingMeeting(ctx, cerebrodb.UpsertOperatingMeetingParams{
		WorkspaceID: workspaceID, NoteTypeID: noteTypeID, CadenceUnit: cadenceUnit,
		CadenceCount: cadenceCount, Agenda: rawAgenda,
	})
	if err != nil {
		return MeetingConfigResponse{}, err
	}
	return s.meetingResponse(ctx, workspaceID, &row, agenda)
}

func responsibilities(raw []byte) []string {
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return []string{}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func orgSeatResponse(id, workspaceID, parentID pgtype.UUID, name string, rawResponsibilities []byte, ownerType pgtype.Text, ownerID pgtype.UUID, ownerName string, position int32) OrgChartSeatResponse {
	return OrgChartSeatResponse{
		ID: util.UUIDToString(id), WorkspaceID: util.UUIDToString(workspaceID), ParentID: nullableUUIDString(parentID),
		Name: name, Responsibilities: responsibilities(rawResponsibilities), OwnerType: ownerType.String,
		OwnerID: nullableUUIDString(ownerID), OwnerName: ownerName, Vacant: !ownerID.Valid, Position: position,
	}
}

func nullableUUIDString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return util.UUIDToString(id)
}

func (s *Service) validateOrgChartParent(ctx context.Context, workspaceID, seatID, parentID pgtype.UUID) error {
	if !parentID.Valid {
		return nil
	}
	rows, err := s.queries.ListOrgChartSeats(ctx, workspaceID)
	if err != nil {
		return err
	}
	parents := make(map[string]pgtype.UUID, len(rows))
	for _, row := range rows {
		parents[util.UUIDToString(row.ID)] = row.ParentID
		if row.ID == parentID && row.WorkspaceID != workspaceID {
			return pgx.ErrNoRows
		}
	}
	if _, ok := parents[util.UUIDToString(parentID)]; !ok {
		return pgx.ErrNoRows
	}
	if seatID.Valid {
		current := parentID
		for current.Valid {
			if current == seatID {
				return errors.New("parent_id would create a cycle")
			}
			var ok bool
			current, ok = parents[util.UUIDToString(current)]
			if !ok {
				break
			}
		}
	}
	return nil
}

func (s *Service) ListOrgChartSeats(ctx context.Context, workspaceID pgtype.UUID) ([]OrgChartSeatResponse, error) {
	if err := s.ensureElementEnabled(ctx, workspaceID, "org_chart"); err != nil {
		return nil, err
	}
	rows, err := s.queries.ListOrgChartSeats(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]OrgChartSeatResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, orgSeatResponse(row.ID, row.WorkspaceID, row.ParentID, row.Name, row.Responsibilities, row.OwnerType, row.OwnerID, row.OwnerName, row.Position))
	}
	return out, nil
}

func (s *Service) saveOrgChartSeat(ctx context.Context, workspaceID, seatID pgtype.UUID, input OrgChartSeatInput) (OrgChartSeatResponse, error) {
	if err := s.ensureElementEnabled(ctx, workspaceID, "org_chart"); err != nil {
		return OrgChartSeatResponse{}, err
	}
	if err := ValidateOrgChartSeatInput(input); err != nil {
		return OrgChartSeatResponse{}, err
	}
	parentID := pgtype.UUID{}
	if input.ParentID != "" {
		parentID, _ = util.ParseUUID(input.ParentID)
	}
	if err := s.validateOrgChartParent(ctx, workspaceID, seatID, parentID); err != nil {
		return OrgChartSeatResponse{}, err
	}
	ownerID := pgtype.UUID{}
	ownerType := pgtype.Text{}
	if input.OwnerID != "" {
		ownerID, _ = util.ParseUUID(input.OwnerID)
		if err := EnsureOwnerInWorkspace(ctx, s.projects, input.OwnerType, ownerID, workspaceID); err != nil {
			return OrgChartSeatResponse{}, err
		}
		ownerType = pgtype.Text{String: input.OwnerType, Valid: true}
	}
	rawResponsibilities, err := json.Marshal(responsibilitiesFromInput(input.Responsibilities))
	if err != nil {
		return OrgChartSeatResponse{}, err
	}
	if seatID.Valid {
		row, err := s.queries.UpdateOrgChartSeat(ctx, cerebrodb.UpdateOrgChartSeatParams{
			ID: seatID, WorkspaceID: workspaceID, ParentID: parentID, Name: strings.TrimSpace(input.Name),
			Responsibilities: rawResponsibilities, OwnerType: ownerType, OwnerID: ownerID, Position: input.Position,
		})
		if err != nil {
			return OrgChartSeatResponse{}, err
		}
		return orgSeatResponse(row.ID, row.WorkspaceID, row.ParentID, row.Name, row.Responsibilities, row.OwnerType, row.OwnerID, row.OwnerName, row.Position), nil
	}
	row, err := s.queries.CreateOrgChartSeat(ctx, cerebrodb.CreateOrgChartSeatParams{
		WorkspaceID: workspaceID, ParentID: parentID, Name: strings.TrimSpace(input.Name),
		Responsibilities: rawResponsibilities, OwnerType: ownerType, OwnerID: ownerID, Position: input.Position,
	})
	if err != nil {
		return OrgChartSeatResponse{}, err
	}
	return orgSeatResponse(row.ID, row.WorkspaceID, row.ParentID, row.Name, row.Responsibilities, row.OwnerType, row.OwnerID, row.OwnerName, row.Position), nil
}

func responsibilitiesFromInput(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (s *Service) CreateOrgChartSeat(ctx context.Context, workspaceID pgtype.UUID, input OrgChartSeatInput) (OrgChartSeatResponse, error) {
	return s.saveOrgChartSeat(ctx, workspaceID, pgtype.UUID{}, input)
}

func (s *Service) UpdateOrgChartSeat(ctx context.Context, workspaceID, seatID pgtype.UUID, input OrgChartSeatInput) (OrgChartSeatResponse, error) {
	return s.saveOrgChartSeat(ctx, workspaceID, seatID, input)
}

func (s *Service) DeleteOrgChartSeat(ctx context.Context, workspaceID, seatID pgtype.UUID) (bool, error) {
	if err := s.ensureElementEnabled(ctx, workspaceID, "org_chart"); err != nil {
		return false, err
	}
	count, err := s.queries.DeleteOrgChartSeat(ctx, cerebrodb.DeleteOrgChartSeatParams{ID: seatID, WorkspaceID: workspaceID})
	return count > 0, err
}

func (s *Service) ListVisionPlan(ctx context.Context, workspaceID pgtype.UUID) (VisionPlanResponse, error) {
	if err := s.ensureVisionPlanEnabled(ctx, workspaceID); err != nil {
		return VisionPlanResponse{}, err
	}
	if err := s.queries.EnsureDefaultVisionPlanSections(ctx, workspaceID); err != nil {
		return VisionPlanResponse{}, err
	}
	if err := s.queries.EnsureLegacyVisionPlanSections(ctx, workspaceID); err != nil {
		return VisionPlanResponse{}, err
	}
	if err := s.queries.AssignLegacyVisionPlanItems(ctx, workspaceID); err != nil {
		return VisionPlanResponse{}, err
	}
	if err := s.queries.EnsureDefaultVisionPlanPages(ctx, workspaceID); err != nil {
		return VisionPlanResponse{}, err
	}
	if err := s.queries.EnsureDefaultVisionPlanGoalsBlock(ctx, workspaceID); err != nil {
		return VisionPlanResponse{}, err
	}
	if err := s.queries.AssignDefaultVisionPlanSectionPages(ctx, workspaceID); err != nil {
		return VisionPlanResponse{}, err
	}
	if err := s.queries.AssignRemainingVisionPlanSectionPages(ctx, workspaceID); err != nil {
		return VisionPlanResponse{}, err
	}
	pageRows, err := s.queries.ListVisionPlanPages(ctx, workspaceID)
	if err != nil {
		return VisionPlanResponse{}, err
	}
	sectionRows, err := s.queries.ListVisionPlanSections(ctx, workspaceID)
	if err != nil {
		return VisionPlanResponse{}, err
	}
	itemRows, err := s.queries.ListVisionPlanItems(ctx, workspaceID)
	if err != nil {
		return VisionPlanResponse{}, err
	}
	connectionRows, err := s.queries.ListVisionPlanGoalConnections(ctx, workspaceID)
	if err != nil {
		return VisionPlanResponse{}, err
	}

	linkRows, err := s.queries.ListVisionPlanObjectLinks(ctx, workspaceID)
	if err != nil {
		return VisionPlanResponse{}, err
	}

	connections := make(map[string][]VisionPlanGoalConnection)
	for _, row := range connectionRows {
		itemID := util.UUIDToString(row.StrategyItemID)
		connections[itemID] = append(connections[itemID], VisionPlanGoalConnection{
			ConnectionID: util.UUIDToString(row.ID), GoalID: util.UUIDToString(row.GoalID),
		})
	}
	links := make(map[string][]VisionPlanObjectLink)
	for _, row := range linkRows {
		itemID := util.UUIDToString(row.StrategyItemID)
		links[itemID] = append(links[itemID], VisionPlanObjectLink{
			ConnectionID: util.UUIDToString(row.ID), TargetType: row.TargetType,
			TargetID: util.UUIDToString(row.TargetID), Title: row.TargetTitle, Identifier: row.TargetIdentifier,
		})
	}
	items := make(map[string][]VisionPlanItemResponse)
	for _, row := range itemRows {
		response := visionPlanItemResponse(
			row.ID, row.WorkspaceID, row.SectionID, row.Title, row.Description, row.PartLabel,
			row.OwnerType, row.OwnerID, row.OwnerName, row.Position, row.State,
			row.CreatedAt, row.UpdatedAt,
		)
		response.GoalConnections = connections[response.ID]
		if response.GoalConnections == nil {
			response.GoalConnections = []VisionPlanGoalConnection{}
		}
		response.Links = links[response.ID]
		if response.Links == nil {
			response.Links = []VisionPlanObjectLink{}
		}
		items[response.SectionID] = append(items[response.SectionID], response)
	}
	sections := make([]VisionPlanSectionResponse, 0, len(sectionRows))
	for _, row := range sectionRows {
		section := visionPlanSectionResponse(row.ID, row.WorkspaceID, row.Key, row.Name, row.SectionType, row.Position, row.PageID, row.ColumnIndex, row.CreatedAt, row.UpdatedAt)
		section.Items = items[section.ID]
		if section.Items == nil {
			section.Items = []VisionPlanItemResponse{}
		}
		sections = append(sections, section)
	}
	pages := make([]VisionPlanPageResponse, 0, len(pageRows))
	for _, row := range pageRows {
		pages = append(pages, visionPlanPageResponse(row))
	}
	return VisionPlanResponse{Pages: pages, Sections: sections}, nil
}

func (s *Service) CreateVisionPlanPage(ctx context.Context, workspaceID pgtype.UUID, input VisionPlanPageInput) (VisionPlanPageResponse, error) {
	if err := s.ensureVisionPlanEnabled(ctx, workspaceID); err != nil {
		return VisionPlanPageResponse{}, err
	}
	if err := ValidateVisionPlanPageInput(input); err != nil {
		return VisionPlanPageResponse{}, err
	}
	row, err := s.queries.CreateVisionPlanPage(ctx, cerebrodb.CreateVisionPlanPageParams{
		WorkspaceID: workspaceID, Name: strings.TrimSpace(input.Name), ColumnCount: input.ColumnCount, Position: input.Position,
	})
	if err != nil {
		return VisionPlanPageResponse{}, err
	}
	return visionPlanPageResponse(row), nil
}

func (s *Service) UpdateVisionPlanPage(ctx context.Context, workspaceID, id pgtype.UUID, input VisionPlanPageInput) (VisionPlanPageResponse, error) {
	if err := s.ensureVisionPlanEnabled(ctx, workspaceID); err != nil {
		return VisionPlanPageResponse{}, err
	}
	if err := ValidateVisionPlanPageInput(input); err != nil {
		return VisionPlanPageResponse{}, err
	}
	row, err := s.queries.UpdateVisionPlanPage(ctx, cerebrodb.UpdateVisionPlanPageParams{
		ID: id, WorkspaceID: workspaceID, Name: strings.TrimSpace(input.Name), ColumnCount: input.ColumnCount, Position: input.Position,
	})
	if err != nil {
		return VisionPlanPageResponse{}, err
	}
	return visionPlanPageResponse(row), nil
}

// Deleting a page takes its blocks with it, so the last page is never removable
// — a workspace always keeps somewhere to put a block.
func (s *Service) DeleteVisionPlanPage(ctx context.Context, workspaceID, id pgtype.UUID) (bool, error) {
	if err := s.ensureVisionPlanEnabled(ctx, workspaceID); err != nil {
		return false, err
	}
	count, err := s.queries.DeleteVisionPlanPage(ctx, cerebrodb.DeleteVisionPlanPageParams{ID: id, WorkspaceID: workspaceID})
	return count > 0, err
}

func (s *Service) CreateVisionPlanSection(ctx context.Context, workspaceID pgtype.UUID, input VisionPlanSectionInput) (VisionPlanSectionResponse, error) {
	if err := s.ensureVisionPlanEnabled(ctx, workspaceID); err != nil {
		return VisionPlanSectionResponse{}, err
	}
	if err := ValidateVisionPlanSectionInput(input); err != nil {
		return VisionPlanSectionResponse{}, err
	}
	pageID, err := util.ParseUUID(input.PageID)
	if err != nil {
		return VisionPlanSectionResponse{}, err
	}
	row, err := s.queries.CreateVisionPlanSection(ctx, cerebrodb.CreateVisionPlanSectionParams{
		WorkspaceID: workspaceID, Name: strings.TrimSpace(input.Name), SectionType: input.SectionType,
		Position: input.Position, PageID: pageID, ColumnIndex: input.ColumnIndex,
	})
	if err != nil {
		return VisionPlanSectionResponse{}, err
	}
	return visionPlanSectionResponse(row.ID, row.WorkspaceID, row.Key, row.Name, row.SectionType, row.Position, row.PageID, row.ColumnIndex, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Service) UpdateVisionPlanSection(ctx context.Context, workspaceID, id pgtype.UUID, input VisionPlanSectionInput) (VisionPlanSectionResponse, error) {
	if err := s.ensureVisionPlanEnabled(ctx, workspaceID); err != nil {
		return VisionPlanSectionResponse{}, err
	}
	if err := ValidateVisionPlanSectionInput(input); err != nil {
		return VisionPlanSectionResponse{}, err
	}
	pageID, err := util.ParseUUID(input.PageID)
	if err != nil {
		return VisionPlanSectionResponse{}, err
	}
	row, err := s.queries.UpdateVisionPlanSection(ctx, cerebrodb.UpdateVisionPlanSectionParams{
		ID: id, WorkspaceID: workspaceID, Name: strings.TrimSpace(input.Name), SectionType: input.SectionType,
		Position: input.Position, PageID: pageID, ColumnIndex: input.ColumnIndex,
	})
	if err != nil {
		return VisionPlanSectionResponse{}, err
	}
	return visionPlanSectionResponse(row.ID, row.WorkspaceID, row.Key, row.Name, row.SectionType, row.Position, row.PageID, row.ColumnIndex, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Service) DeleteVisionPlanSection(ctx context.Context, workspaceID, id pgtype.UUID) (bool, error) {
	if err := s.ensureVisionPlanEnabled(ctx, workspaceID); err != nil {
		return false, err
	}
	count, err := s.queries.DeleteVisionPlanSection(ctx, cerebrodb.DeleteVisionPlanSectionParams{ID: id, WorkspaceID: workspaceID})
	return count > 0, err
}

func (s *Service) CreateVisionPlanItem(ctx context.Context, workspaceID pgtype.UUID, input VisionPlanItemInput) (VisionPlanItemResponse, error) {
	if err := s.ensureVisionPlanEnabled(ctx, workspaceID); err != nil {
		return VisionPlanItemResponse{}, err
	}
	if err := ValidateVisionPlanItemInput(input); err != nil {
		return VisionPlanItemResponse{}, err
	}
	sectionID, _ := util.ParseUUID(input.SectionID)
	ownerID := pgtype.UUID{}
	if input.OwnerID != "" {
		ownerID, _ = util.ParseUUID(input.OwnerID)
		if err := EnsureOwnerInWorkspace(ctx, s.projects, input.OwnerType, ownerID, workspaceID); err != nil {
			return VisionPlanItemResponse{}, err
		}
	}
	row, err := s.queries.CreateVisionPlanItem(ctx, cerebrodb.CreateVisionPlanItemParams{
		ID: sectionID, WorkspaceID: workspaceID, Title: strings.TrimSpace(input.Title),
		Description: strings.TrimSpace(input.Description), Position: input.Position, State: defaultState(input.State),
		PartLabel: strings.TrimSpace(input.PartLabel), OwnerType: text(input.OwnerType), OwnerID: ownerID,
	})
	if err != nil {
		return VisionPlanItemResponse{}, err
	}
	return visionPlanItemResponse(row.ID, row.WorkspaceID, row.SectionID, row.Title, row.Description, row.PartLabel, row.OwnerType, row.OwnerID, "", row.Position, row.State, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Service) UpdateVisionPlanItem(ctx context.Context, workspaceID, id pgtype.UUID, input VisionPlanItemInput) (VisionPlanItemResponse, error) {
	if err := s.ensureVisionPlanEnabled(ctx, workspaceID); err != nil {
		return VisionPlanItemResponse{}, err
	}
	if err := ValidateVisionPlanItemInput(input); err != nil {
		return VisionPlanItemResponse{}, err
	}
	sectionID, _ := util.ParseUUID(input.SectionID)
	ownerID := pgtype.UUID{}
	if input.OwnerID != "" {
		ownerID, _ = util.ParseUUID(input.OwnerID)
		if err := EnsureOwnerInWorkspace(ctx, s.projects, input.OwnerType, ownerID, workspaceID); err != nil {
			return VisionPlanItemResponse{}, err
		}
	}
	row, err := s.queries.UpdateVisionPlanItem(ctx, cerebrodb.UpdateVisionPlanItemParams{
		ID: id, WorkspaceID: workspaceID, SectionID: sectionID, Title: strings.TrimSpace(input.Title),
		Description: strings.TrimSpace(input.Description), Position: input.Position, State: defaultState(input.State),
		PartLabel: strings.TrimSpace(input.PartLabel), OwnerType: text(input.OwnerType), OwnerID: ownerID,
	})
	if err != nil {
		return VisionPlanItemResponse{}, err
	}
	return visionPlanItemResponse(row.ID, row.WorkspaceID, row.SectionID, row.Title, row.Description, row.PartLabel, row.OwnerType, row.OwnerID, "", row.Position, row.State, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Service) DeleteVisionPlanItem(ctx context.Context, workspaceID, id pgtype.UUID) (bool, error) {
	if err := s.ensureVisionPlanEnabled(ctx, workspaceID); err != nil {
		return false, err
	}
	return s.DeleteStrategyItem(ctx, workspaceID, id)
}

func (s *Service) UpsertRock(ctx context.Context, workspaceID pgtype.UUID, input RockInput) error {
	if err := ValidateRockInput(input); err != nil {
		return err
	}
	projectID, _ := util.ParseUUID(input.ProjectID)
	if err := EnsureProjectInWorkspace(ctx, s.projects, projectID, workspaceID); err != nil {
		return err
	}
	start, _ := time.Parse("2006-01-02", input.PeriodStart)
	end, _ := time.Parse("2006-01-02", input.PeriodEnd)
	period, err := s.queries.UpsertOperatingPeriod(ctx, cerebrodb.UpsertOperatingPeriodParams{
		WorkspaceID: workspaceID, Name: quarterName(start),
		StartsOn: pgtype.Date{Time: start, Valid: true}, EndsOn: pgtype.Date{Time: end, Valid: true},
	})
	if err != nil {
		return err
	}
	row, err := s.queries.UpsertLegacyRock(ctx, cerebrodb.UpsertLegacyRockParams{
		ID: projectID, WorkspaceID: workspaceID, ID_2: period.ID,
		Confidence: input.Confidence, ReportedHealth: input.ReportedHealth,
	})
	if err != nil {
		return err
	}
	_, err = s.queries.CreateObjectConnection(ctx, cerebrodb.CreateObjectConnectionParams{
		WorkspaceID: workspaceID, SourceType: "rock", SourceID: row.ID,
		TargetType: "project", TargetID: projectID, RelationshipType: "contains",
		Provenance: "system", CreatedByType: "system", CreatedByID: row.ID,
	})
	if IsDuplicateConnection(err) {
		return nil
	}
	return err
}

func (s *Service) DeleteRock(ctx context.Context, workspaceID, rockID pgtype.UUID) (bool, error) {
	count, err := s.queries.DeleteRock(ctx, cerebrodb.DeleteRockParams{ID: rockID, WorkspaceID: workspaceID})
	return count > 0, err
}

func (s *Service) ListPeriods(ctx context.Context, workspaceID pgtype.UUID) ([]OperatingPeriodResponse, error) {
	rows, err := s.queries.ListOperatingPeriods(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]OperatingPeriodResponse, 0, len(rows)+1)
	for _, row := range rows {
		out = append(out, periodResponse(row.ID, row.WorkspaceID, row.Name, row.Unit, row.StartsOn, row.EndsOn))
	}
	if len(out) == 0 {
		name, start, end := quarterPeriod(s.now().UTC())
		row, createErr := s.queries.CreateOperatingPeriod(ctx, cerebrodb.CreateOperatingPeriodParams{
			WorkspaceID: workspaceID,
			Name:        name,
			Unit:        "quarter",
			StartsOn:    pgtype.Date{Time: start, Valid: true},
			EndsOn:      pgtype.Date{Time: end, Valid: true},
		})
		if createErr != nil {
			return nil, createErr
		}
		out = append(out, periodResponse(row.ID, row.WorkspaceID, row.Name, row.Unit, row.StartsOn, row.EndsOn))
	}
	return out, nil
}

func (s *Service) CreatePeriod(ctx context.Context, workspaceID pgtype.UUID, input OperatingPeriodInput) (OperatingPeriodResponse, error) {
	resolved, err := ResolvePeriodInput(input)
	if err != nil {
		return OperatingPeriodResponse{}, err
	}
	row, err := s.queries.CreateOperatingPeriod(ctx, cerebrodb.CreateOperatingPeriodParams{
		WorkspaceID: workspaceID, Name: resolved.Name, Unit: resolved.Unit,
		StartsOn: resolved.StartsOn, EndsOn: resolved.EndsOn,
	})
	if err != nil {
		return OperatingPeriodResponse{}, err
	}
	return periodResponse(row.ID, row.WorkspaceID, row.Name, row.Unit, row.StartsOn, row.EndsOn), nil
}

func (s *Service) UpdatePeriod(ctx context.Context, workspaceID, id pgtype.UUID, input OperatingPeriodInput) (OperatingPeriodResponse, error) {
	resolved, err := ResolvePeriodInput(input)
	if err != nil {
		return OperatingPeriodResponse{}, err
	}
	row, err := s.queries.UpdateOperatingPeriod(ctx, cerebrodb.UpdateOperatingPeriodParams{
		ID: id, WorkspaceID: workspaceID, Name: resolved.Name, Unit: resolved.Unit,
		StartsOn: resolved.StartsOn, EndsOn: resolved.EndsOn,
	})
	if err != nil {
		return OperatingPeriodResponse{}, err
	}
	return periodResponse(row.ID, row.WorkspaceID, row.Name, row.Unit, row.StartsOn, row.EndsOn), nil
}

func (s *Service) DeletePeriod(ctx context.Context, workspaceID, id pgtype.UUID) (bool, error) {
	count, err := s.queries.DeleteOperatingPeriod(ctx, cerebrodb.DeleteOperatingPeriodParams{ID: id, WorkspaceID: workspaceID})
	if isForeignKeyViolation(err) {
		return false, errors.New("period is still referenced and must be emptied first")
	}
	return count > 0, err
}

func (s *Service) ListElements(ctx context.Context, workspaceID pgtype.UUID) ([]OsElementResponse, error) {
	rows, err := s.queries.ListOsElementSettings(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	overrides := make(map[string]bool, len(rows))
	for _, row := range rows {
		overrides[row.ElementKey] = row.Enabled
	}
	return MergeElementSettings(overrides), nil
}

func (s *Service) UpdateElement(ctx context.Context, workspaceID pgtype.UUID, key string, enabled bool) (OsElementResponse, error) {
	if !IsRegisteredElement(key) {
		return OsElementResponse{}, fmt.Errorf("invalid element %q", key)
	}
	row, err := s.queries.UpsertOsElementSetting(ctx, cerebrodb.UpsertOsElementSettingParams{
		WorkspaceID: workspaceID, ElementKey: key, Enabled: enabled,
	})
	if err != nil {
		return OsElementResponse{}, err
	}
	defaultEnabled := false
	for _, element := range ElementRegistry() {
		if element.Key == key {
			defaultEnabled = element.DefaultEnabled
		}
	}
	return OsElementResponse{Key: row.ElementKey, Enabled: row.Enabled, DefaultEnabled: defaultEnabled}, nil
}

func (s *Service) CreateGoalType(ctx context.Context, workspaceID pgtype.UUID, input GoalTypeInput) (GoalTypeResponse, error) {
	if err := ValidateGoalTypeInput(input); err != nil {
		return GoalTypeResponse{}, err
	}
	row, err := s.queries.CreateGoalType(ctx, goalTypeParams(workspaceID, input))
	if IsDuplicateConnection(err) {
		return GoalTypeResponse{}, errors.New("a goal type with this name already exists")
	}
	if err != nil {
		return GoalTypeResponse{}, err
	}
	return goalTypeResponse(row), nil
}

func (s *Service) ListGoalTypes(ctx context.Context, workspaceID pgtype.UUID) ([]GoalTypeResponse, error) {
	rows, err := s.queries.ListGoalTypes(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]GoalTypeResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, goalTypeResponse(row))
	}
	return out, nil
}

func (s *Service) UpdateGoalType(ctx context.Context, workspaceID, id pgtype.UUID, input GoalTypeInput) (GoalTypeResponse, error) {
	if err := ValidateGoalTypeInput(input); err != nil {
		return GoalTypeResponse{}, err
	}
	params := goalTypeParams(workspaceID, input)
	row, err := s.queries.UpdateGoalType(ctx, cerebrodb.UpdateGoalTypeParams{
		ID: id, WorkspaceID: workspaceID, Name: params.Name, Color: params.Color,
		ScopeLabel: params.ScopeLabel, Position: params.Position,
	})
	if IsDuplicateConnection(err) {
		return GoalTypeResponse{}, errors.New("a goal type with this name already exists")
	}
	if err != nil {
		return GoalTypeResponse{}, err
	}
	return goalTypeResponse(row), nil
}

func (s *Service) DeleteGoalType(ctx context.Context, workspaceID, id pgtype.UUID) (bool, error) {
	count, err := s.queries.DeleteGoalType(ctx, cerebrodb.DeleteGoalTypeParams{ID: id, WorkspaceID: workspaceID})
	return count > 0, err
}

func quarterPeriod(now time.Time) (string, time.Time, time.Time) {
	quarter := (int(now.Month())-1)/3 + 1
	startMonth := time.Month((quarter-1)*3 + 1)
	start := time.Date(now.Year(), startMonth, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 3, 0).AddDate(0, 0, -1)
	return fmt.Sprintf("Q%d %d", quarter, now.Year()), start, end
}

func (s *Service) SaveRock(ctx context.Context, workspaceID pgtype.UUID, actorType string, actorID pgtype.UUID, rockID *pgtype.UUID, input RockInput) (RockResponse, error) {
	if err := ValidateRockInput(input); err != nil {
		return RockResponse{}, err
	}
	periodID, _ := util.ParseUUID(input.PeriodID)
	if _, err := s.queries.GetOperatingPeriod(ctx, cerebrodb.GetOperatingPeriodParams{ID: periodID, WorkspaceID: workspaceID}); err != nil {
		return RockResponse{}, err
	}
	ownerID := pgtype.UUID{}
	if input.OwnerID != "" {
		ownerID, _ = util.ParseUUID(input.OwnerID)
		if err := EnsureOwnerInWorkspace(ctx, s.projects, input.OwnerType, ownerID, workspaceID); err != nil {
			return RockResponse{}, err
		}
	}
	goalTypeID := pgtype.UUID{}
	if input.GoalTypeID != "" {
		goalTypeID, _ = util.ParseUUID(input.GoalTypeID)
		if _, err := s.queries.GetGoalType(ctx, cerebrodb.GetGoalTypeParams{ID: goalTypeID, WorkspaceID: workspaceID}); err != nil {
			return RockResponse{}, err
		}
	}
	var id pgtype.UUID
	if rockID == nil {
		row, err := s.queries.CreateRock(ctx, cerebrodb.CreateRockParams{
			WorkspaceID: workspaceID, Title: strings.TrimSpace(input.Title), Description: strings.TrimSpace(input.Description),
			OwnerType: text(input.OwnerType), OwnerID: ownerID, PeriodID: periodID,
			Confidence: input.Confidence, ReportedHealth: input.ReportedHealth, GoalTypeID: goalTypeID,
		})
		if err != nil {
			return RockResponse{}, err
		}
		id = row.ID
	} else {
		row, err := s.queries.UpdateRock(ctx, cerebrodb.UpdateRockParams{
			ID: *rockID, WorkspaceID: workspaceID, Title: strings.TrimSpace(input.Title), Description: strings.TrimSpace(input.Description),
			OwnerType: text(input.OwnerType), OwnerID: ownerID, PeriodID: periodID,
			Confidence: input.Confidence, ReportedHealth: input.ReportedHealth, GoalTypeID: goalTypeID,
		})
		if err != nil {
			return RockResponse{}, err
		}
		id = row.ID
	}
	if err := s.replaceRockConnections(ctx, workspaceID, actorType, actorID, id, input); err != nil {
		return RockResponse{}, err
	}
	return s.getRockResponse(ctx, workspaceID, id)
}

func (s *Service) AddRockCheckIn(ctx context.Context, workspaceID, rockID pgtype.UUID, actorType string, actorID pgtype.UUID, input RockCheckInInput) (RockCheckIn, error) {
	if input.Confidence < 0 || input.Confidence > 100 || !validRockHealth[input.ReportedHealth] {
		return RockCheckIn{}, errors.New("confidence and reported_health are invalid")
	}
	row, err := s.queries.CreateRockCheckIn(ctx, cerebrodb.CreateRockCheckInParams{
		WorkspaceID: workspaceID, ID: rockID, Confidence: input.Confidence,
		ReportedHealth: input.ReportedHealth, Note: strings.TrimSpace(input.Note),
		CreatedByType: actorType, CreatedByID: actorID,
	})
	if err != nil {
		return RockCheckIn{}, err
	}
	if _, err := s.queries.ApplyRockCheckIn(ctx, cerebrodb.ApplyRockCheckInParams{
		ID: rockID, WorkspaceID: workspaceID, Confidence: input.Confidence, ReportedHealth: input.ReportedHealth,
	}); err != nil {
		return RockCheckIn{}, err
	}
	return checkInResponse(row), nil
}

func (s *Service) ListRocks(ctx context.Context, workspaceID pgtype.UUID) ([]RockResponse, error) {
	rows, err := s.queries.ListRockRollups(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]RockResponse, 0, len(rows))
	for _, row := range rows {
		rock, err := s.enrichRockResponse(ctx, workspaceID, rockResponse(row, s.now()))
		if err != nil {
			return nil, err
		}
		out = append(out, rock)
	}
	return out, nil
}

func (s *Service) CreateConnection(ctx context.Context, workspaceID pgtype.UUID, actorType string, actorID pgtype.UUID, input ObjectConnectionInput) (ObjectConnectionResponse, error) {
	params, err := connectionParams(workspaceID, actorType, actorID, input)
	if err != nil {
		return ObjectConnectionResponse{}, err
	}
	if input.SourceType == "strategy_item" {
		switch input.TargetType {
		case "rock", "project", "issue":
			if _, err := s.queries.GetStrategyItem(ctx, cerebrodb.GetStrategyItemParams{ID: params.SourceID, WorkspaceID: workspaceID}); err != nil {
				return ObjectConnectionResponse{}, err
			}
		}
		switch input.TargetType {
		case "rock":
			if _, err := s.queries.GetRock(ctx, cerebrodb.GetRockParams{ID: params.TargetID, WorkspaceID: workspaceID}); err != nil {
				return ObjectConnectionResponse{}, err
			}
		case "project":
			if err := EnsureProjectInWorkspace(ctx, s.projects, params.TargetID, workspaceID); err != nil {
				return ObjectConnectionResponse{}, err
			}
		case "issue":
			if _, err := s.projects.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: params.TargetID, WorkspaceID: workspaceID}); err != nil {
				return ObjectConnectionResponse{}, err
			}
		}
	}
	row, err := s.queries.CreateObjectConnection(ctx, params)
	if IsDuplicateConnection(err) {
		return ObjectConnectionResponse{}, fmt.Errorf("duplicate connection: %w", err)
	}
	return connectionResponse(row), err
}

func (s *Service) ListConnections(ctx context.Context, workspaceID pgtype.UUID, objectType, objectID string) ([]ObjectConnectionResponse, error) {
	id, err := util.ParseUUID(objectID)
	if err != nil || strings.TrimSpace(objectType) == "" {
		return nil, errors.New("object type and UUID are required")
	}
	rows, err := s.queries.ListObjectConnections(ctx, cerebrodb.ListObjectConnectionsParams{
		WorkspaceID: workspaceID, SourceType: objectType, SourceID: id,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ObjectConnectionResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, connectionResponse(row))
	}
	return out, nil
}

func (s *Service) DeleteConnection(ctx context.Context, workspaceID, id pgtype.UUID) (bool, error) {
	count, err := s.queries.DeleteObjectConnection(ctx, cerebrodb.DeleteObjectConnectionParams{ID: id, WorkspaceID: workspaceID})
	return count > 0, err
}

func (s *Service) replaceRockConnections(ctx context.Context, workspaceID pgtype.UUID, actorType string, actorID, rockID pgtype.UUID, input RockInput) error {
	if actorType != "member" && actorType != "agent" {
		return errors.New("invalid actor type")
	}
	for _, raw := range input.ProjectIDs {
		id, _ := util.ParseUUID(raw)
		if err := EnsureProjectInWorkspace(ctx, s.projects, id, workspaceID); err != nil {
			return err
		}
	}
	for _, raw := range input.IssueIDs {
		id, _ := util.ParseUUID(raw)
		if _, err := s.projects.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: id, WorkspaceID: workspaceID}); err != nil {
			return err
		}
	}
	var strategyID pgtype.UUID
	if input.StrategyItemID != "" {
		strategyID, _ = util.ParseUUID(input.StrategyItemID)
		if _, err := s.queries.GetStrategyItem(ctx, cerebrodb.GetStrategyItemParams{ID: strategyID, WorkspaceID: workspaceID}); err != nil {
			return err
		}
	}
	if err := s.queries.DeleteObjectConnectionsForSource(ctx, cerebrodb.DeleteObjectConnectionsForSourceParams{
		WorkspaceID: workspaceID, SourceType: "rock", SourceID: rockID, TargetTypes: []string{"project", "issue"},
	}); err != nil {
		return err
	}
	for _, target := range []struct {
		typeName string
		ids      []string
	}{{"project", input.ProjectIDs}, {"issue", input.IssueIDs}} {
		for _, raw := range target.ids {
			id, _ := util.ParseUUID(raw)
			if _, err := s.queries.CreateObjectConnection(ctx, cerebrodb.CreateObjectConnectionParams{
				WorkspaceID: workspaceID, SourceType: "rock", SourceID: rockID,
				TargetType: target.typeName, TargetID: id, RelationshipType: "contains",
				Provenance: "manual", CreatedByType: actorType, CreatedByID: actorID,
			}); err != nil {
				return err
			}
		}
	}
	if err := s.queries.DeleteRockStrategyConnections(ctx, cerebrodb.DeleteRockStrategyConnectionsParams{WorkspaceID: workspaceID, TargetID: rockID}); err != nil {
		return err
	}
	if input.StrategyItemID != "" {
		_, err := s.queries.CreateObjectConnection(ctx, cerebrodb.CreateObjectConnectionParams{
			WorkspaceID: workspaceID, SourceType: "strategy_item", SourceID: strategyID,
			TargetType: "rock", TargetID: rockID, RelationshipType: "supports",
			Provenance: "manual", CreatedByType: actorType, CreatedByID: actorID,
		})
		return err
	}
	return nil
}

func (s *Service) getRockResponse(ctx context.Context, workspaceID, rockID pgtype.UUID) (RockResponse, error) {
	rows, err := s.ListRocks(ctx, workspaceID)
	if err != nil {
		return RockResponse{}, err
	}
	for _, rock := range rows {
		if rock.ID == util.UUIDToString(rockID) {
			return rock, nil
		}
	}
	return RockResponse{}, pgx.ErrNoRows
}

func (s *Service) enrichRockResponse(ctx context.Context, workspaceID pgtype.UUID, rock RockResponse) (RockResponse, error) {
	rockID, _ := util.ParseUUID(rock.ID)
	projects, err := s.queries.ListRockProjects(ctx, cerebrodb.ListRockProjectsParams{WorkspaceID: workspaceID, SourceID: rockID})
	if err != nil {
		return RockResponse{}, err
	}
	for _, project := range projects {
		rock.Projects = append(rock.Projects, RockProject{ID: util.UUIDToString(project.ID), Title: project.Title, IssueCount: project.IssueCount, DoneIssueCount: project.DoneIssueCount})
	}
	issues, err := s.queries.ListRockIssues(ctx, cerebrodb.ListRockIssuesParams{WorkspaceID: workspaceID, SourceID: rockID})
	if err != nil {
		return RockResponse{}, err
	}
	for _, issue := range issues {
		rock.Issues = append(rock.Issues, RockIssue{
			ID: util.UUIDToString(issue.ID), Identifier: issue.Identifier, Title: issue.Title,
			Status: issue.Status, ProjectID: util.UUIDToString(issue.ProjectID), ProjectTitle: issue.ProjectTitle,
		})
	}
	checkIns, err := s.queries.ListRockCheckIns(ctx, cerebrodb.ListRockCheckInsParams{WorkspaceID: workspaceID, RockID: rockID})
	if err != nil {
		return RockResponse{}, err
	}
	for _, checkIn := range checkIns {
		rock.CheckIns = append(rock.CheckIns, checkInResponse(checkIn))
	}
	return rock, nil
}

func checkInResponse(row cerebrodb.CerebroRockCheckIn) RockCheckIn {
	return RockCheckIn{
		ID: util.UUIDToString(row.ID), Confidence: row.Confidence, ReportedHealth: row.ReportedHealth,
		Note: row.Note, CreatedByType: row.CreatedByType, CreatedByID: util.UUIDToString(row.CreatedByID), CreatedAt: timestamp(row.CreatedAt),
	}
}

func healthScore(total, done, blocked int32) int32 {
	if total == 0 {
		return 0
	}
	score := done * 100 / total
	if blocked > 0 && score > 49 {
		return 49
	}
	return score
}

func quarterName(start time.Time) string {
	return fmt.Sprintf("Q%d %d", (int(start.Month())-1)/3+1, start.Year())
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func goalTypeParams(workspaceID pgtype.UUID, input GoalTypeInput) cerebrodb.CreateGoalTypeParams {
	color := input.Color
	if color == "" {
		color = defaultGoalTypeColor
	}
	return cerebrodb.CreateGoalTypeParams{
		WorkspaceID: workspaceID, Name: strings.TrimSpace(input.Name), Color: color,
		ScopeLabel: strings.TrimSpace(input.ScopeLabel), Position: input.Position,
	}
}

func goalTypeResponse(row cerebrodb.CerebroGoalType) GoalTypeResponse {
	return GoalTypeResponse{
		ID: util.UUIDToString(row.ID), WorkspaceID: util.UUIDToString(row.WorkspaceID),
		Name: row.Name, Color: row.Color, ScopeLabel: row.ScopeLabel, Position: row.Position,
		CreatedAt: timestamp(row.CreatedAt), UpdatedAt: timestamp(row.UpdatedAt),
	}
}

func periodResponse(id, workspaceID pgtype.UUID, name, unit string, startsOn, endsOn pgtype.Date) OperatingPeriodResponse {
	return OperatingPeriodResponse{
		ID: util.UUIDToString(id), WorkspaceID: util.UUIDToString(workspaceID),
		Name: name, Unit: unit, StartsOn: date(startsOn), EndsOn: date(endsOn),
	}
}

func strategyParams(workspaceID pgtype.UUID, input StrategyItemInput) cerebrodb.CreateStrategyItemParams {
	return cerebrodb.CreateStrategyItemParams{
		WorkspaceID: workspaceID, Kind: input.Kind, Title: strings.TrimSpace(input.Title),
		Description: input.Description, HorizonUnit: text(input.HorizonUnit),
		HorizonCount: integer(input.HorizonCount), HorizonLabel: text(input.HorizonLabel), Position: input.Position, State: defaultState(input.State),
	}
}

func strategyResponse(id, workspaceID pgtype.UUID, kind, title, description string, horizonUnit pgtype.Text, horizonCount pgtype.Int4, horizonLabel pgtype.Text, position int32, state string, createdAt, updatedAt pgtype.Timestamptz) StrategyItemResponse {
	return StrategyItemResponse{
		ID: util.UUIDToString(id), WorkspaceID: util.UUIDToString(workspaceID), Kind: kind,
		Title: title, Description: description, HorizonUnit: horizonUnit.String,
		HorizonCount: horizonCount.Int32, HorizonLabel: horizonLabel.String, Position: position, State: state,
		CreatedAt: timestamp(createdAt), UpdatedAt: timestamp(updatedAt),
	}
}

func visionPlanSectionResponse(id, workspaceID pgtype.UUID, key, name, sectionType string, position int32, pageID pgtype.UUID, columnIndex int32, createdAt, updatedAt pgtype.Timestamptz) VisionPlanSectionResponse {
	return VisionPlanSectionResponse{
		ID: util.UUIDToString(id), WorkspaceID: util.UUIDToString(workspaceID),
		Key: key, Name: name, SectionType: sectionType, Position: position,
		PageID: util.UUIDToString(pageID), ColumnIndex: columnIndex,
		Items: []VisionPlanItemResponse{}, CreatedAt: timestamp(createdAt), UpdatedAt: timestamp(updatedAt),
	}
}

func visionPlanPageResponse(row cerebrodb.CerebroVisionPlanPage) VisionPlanPageResponse {
	return VisionPlanPageResponse{
		ID: util.UUIDToString(row.ID), WorkspaceID: util.UUIDToString(row.WorkspaceID),
		Key: row.Key, Name: row.Name, ColumnCount: row.ColumnCount, Position: row.Position,
		CreatedAt: timestamp(row.CreatedAt), UpdatedAt: timestamp(row.UpdatedAt),
	}
}

func visionPlanItemResponse(id, workspaceID, sectionID pgtype.UUID, title, description, partLabel string, ownerType pgtype.Text, ownerID pgtype.UUID, ownerName string, position int32, state string, createdAt, updatedAt pgtype.Timestamptz) VisionPlanItemResponse {
	return VisionPlanItemResponse{
		ID: util.UUIDToString(id), WorkspaceID: util.UUIDToString(workspaceID), SectionID: util.UUIDToString(sectionID),
		Title: title, Description: description, PartLabel: partLabel,
		OwnerType: ownerType.String, OwnerID: util.UUIDToString(ownerID), OwnerName: ownerName,
		Position: position, State: state, GoalConnections: []VisionPlanGoalConnection{}, Links: []VisionPlanObjectLink{},
		CreatedAt: timestamp(createdAt), UpdatedAt: timestamp(updatedAt),
	}
}

func rockResponse(row cerebrodb.ListRockRollupsRow, now time.Time) RockResponse {
	return RockResponse{
		ID: util.UUIDToString(row.ID), Title: row.Title, Description: row.Description,
		OwnerType: row.OwnerType.String, OwnerID: util.UUIDToString(row.OwnerID), OwnerName: row.OwnerName,
		PeriodID: util.UUIDToString(row.PeriodID), PeriodName: row.PeriodName,
		GoalTypeID: util.UUIDToString(row.GoalTypeID), GoalTypeName: row.GoalTypeName,
		GoalTypeColor: row.GoalTypeColor, GoalTypeScopeLabel: row.GoalTypeScopeLabel,
		ProjectID: util.UUIDToString(row.ProjectID), WorkspaceID: util.UUIDToString(row.WorkspaceID),
		ProjectTitle: row.ProjectTitle, ProjectDescription: row.ProjectDescription,
		ProjectStatus: row.ProjectStatus, LeadType: row.OwnerType.String, LeadID: util.UUIDToString(row.OwnerID),
		PeriodStart: date(row.PeriodStart), PeriodEnd: date(row.PeriodEnd), Confidence: row.Confidence,
		ReportedHealth: row.ReportedHealth,
		DerivedHealth:  DeriveHealth(row.IssueCount, row.DoneIssueCount, row.BlockedIssueCount, now),
		IssueCount:     row.IssueCount, DoneIssueCount: row.DoneIssueCount, BlockedIssueCount: row.BlockedIssueCount,
		ProjectCount: row.ProjectCount, HealthScore: healthScore(row.IssueCount, row.DoneIssueCount, row.BlockedIssueCount),
		StrategyItemID: util.UUIDToString(row.StrategyItemID), StrategyItemTitle: row.StrategyItemTitle,
		Projects: []RockProject{}, Issues: []RockIssue{}, CheckIns: []RockCheckIn{},
		CreatedAt: timestamp(row.CreatedAt), UpdatedAt: timestamp(row.UpdatedAt),
	}
}

func connectionParams(workspaceID pgtype.UUID, actorType string, actorID pgtype.UUID, input ObjectConnectionInput) (cerebrodb.CreateObjectConnectionParams, error) {
	if actorType != "member" && actorType != "agent" {
		return cerebrodb.CreateObjectConnectionParams{}, errors.New("invalid actor type")
	}
	sourceID, sourceErr := util.ParseUUID(input.SourceID)
	targetID, targetErr := util.ParseUUID(input.TargetID)
	if sourceErr != nil || targetErr != nil || strings.TrimSpace(input.SourceType) == "" || strings.TrimSpace(input.TargetType) == "" {
		return cerebrodb.CreateObjectConnectionParams{}, errors.New("source and target types and UUIDs are required")
	}
	if input.RelationshipType == "" {
		input.RelationshipType = "relates_to"
	}
	if input.Provenance == "" {
		input.Provenance = "manual"
	}
	if input.Provenance != "manual" && input.Provenance != "agent" && input.Provenance != "system" {
		return cerebrodb.CreateObjectConnectionParams{}, errors.New("invalid provenance")
	}
	return cerebrodb.CreateObjectConnectionParams{
		WorkspaceID: workspaceID, SourceType: input.SourceType, SourceID: sourceID,
		TargetType: input.TargetType, TargetID: targetID, RelationshipType: input.RelationshipType,
		Provenance: input.Provenance, CreatedByType: actorType, CreatedByID: actorID,
	}, nil
}

func connectionResponse(row cerebrodb.CerebroObjectConnection) ObjectConnectionResponse {
	return ObjectConnectionResponse{
		ID: util.UUIDToString(row.ID), WorkspaceID: util.UUIDToString(row.WorkspaceID),
		SourceType: row.SourceType, SourceID: util.UUIDToString(row.SourceID),
		TargetType: row.TargetType, TargetID: util.UUIDToString(row.TargetID),
		RelationshipType: row.RelationshipType, Provenance: row.Provenance,
		CreatedByType: row.CreatedByType, CreatedByID: util.UUIDToString(row.CreatedByID),
		CreatedAt: timestamp(row.CreatedAt),
	}
}

func defaultState(state string) string {
	if state == "" {
		return "active"
	}
	return state
}

func text(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func integer(value int32) pgtype.Int4 {
	return pgtype.Int4{Int32: value, Valid: value > 0}
}

func timestamp(value pgtype.Timestamptz) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339)
}

func date(value pgtype.Date) string {
	if !value.Valid {
		return ""
	}
	return value.Time.Format("2006-01-02")
}

func mustJSON(value any) []byte {
	out, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return out
}
