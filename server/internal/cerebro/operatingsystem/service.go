package operatingsystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrProjectNotInWorkspace = errors.New("project not found in workspace")
var ErrOwnerNotInWorkspace = errors.New("owner not found in workspace")

var validStrategyKinds = map[string]bool{
	"core_value": true, "core_focus": true, "horizon_goal": true,
}

var validHorizonUnits = map[string]bool{
	"day": true, "week": true, "month": true, "year": true,
}

var validRockHealth = map[string]bool{
	"on_track": true, "at_risk": true, "off_track": true, "unset": true,
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
	return Terminology{Strategy: "Strategy", Rock: "Rock", Rocks: "Rocks"}
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
		Strategy: read("strategy", defaults.Strategy),
		Rock:     read("rock", defaults.Rock),
		Rocks:    read("rocks", defaults.Rocks),
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
	if len(rows) == 0 {
		name, start, end := quarterPeriod(s.now().UTC())
		row, createErr := s.queries.UpsertOperatingPeriod(ctx, cerebrodb.UpsertOperatingPeriodParams{
			WorkspaceID: workspaceID,
			Name:        name,
			StartsOn:    pgtype.Date{Time: start, Valid: true},
			EndsOn:      pgtype.Date{Time: end, Valid: true},
		})
		if createErr != nil {
			return nil, createErr
		}
		rows = append(rows, row)
	}
	out := make([]OperatingPeriodResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, OperatingPeriodResponse{
			ID: util.UUIDToString(row.ID), WorkspaceID: util.UUIDToString(row.WorkspaceID),
			Name: row.Name, StartsOn: date(row.StartsOn), EndsOn: date(row.EndsOn),
		})
	}
	return out, nil
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
	var id pgtype.UUID
	if rockID == nil {
		row, err := s.queries.CreateRock(ctx, cerebrodb.CreateRockParams{
			WorkspaceID: workspaceID, Title: strings.TrimSpace(input.Title), Description: strings.TrimSpace(input.Description),
			OwnerType: text(input.OwnerType), OwnerID: ownerID, PeriodID: periodID,
			Confidence: input.Confidence, ReportedHealth: input.ReportedHealth,
		})
		if err != nil {
			return RockResponse{}, err
		}
		id = row.ID
	} else {
		row, err := s.queries.UpdateRock(ctx, cerebrodb.UpdateRockParams{
			ID: *rockID, WorkspaceID: workspaceID, Title: strings.TrimSpace(input.Title), Description: strings.TrimSpace(input.Description),
			OwnerType: text(input.OwnerType), OwnerID: ownerID, PeriodID: periodID,
			Confidence: input.Confidence, ReportedHealth: input.ReportedHealth,
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

func rockResponse(row cerebrodb.ListRockRollupsRow, now time.Time) RockResponse {
	return RockResponse{
		ID: util.UUIDToString(row.ID), Title: row.Title, Description: row.Description,
		OwnerType: row.OwnerType.String, OwnerID: util.UUIDToString(row.OwnerID), OwnerName: row.OwnerName,
		PeriodID: util.UUIDToString(row.PeriodID), PeriodName: row.PeriodName,
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
