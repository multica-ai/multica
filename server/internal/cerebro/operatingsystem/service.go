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
	if input.Confidence < 0 || input.Confidence > 100 {
		return errors.New("confidence must be between 0 and 100")
	}
	if !validRockHealth[input.ReportedHealth] {
		return fmt.Errorf("invalid reported_health %q", input.ReportedHealth)
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
		result.State = "at_risk"
		result.Reason = "no Issues are connected to this Project"
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
	return strategyResponse(row), err
}

func (s *Service) ListStrategyItems(ctx context.Context, workspaceID pgtype.UUID) ([]StrategyItemResponse, error) {
	rows, err := s.queries.ListStrategyItems(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]StrategyItemResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, strategyResponse(row))
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
		HorizonCount: params.HorizonCount, Position: params.Position, State: params.State,
	})
	return strategyResponse(row), err
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
	_, err := s.queries.UpsertRock(ctx, cerebrodb.UpsertRockParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
		PeriodStart: pgtype.Date{Time: start, Valid: true}, PeriodEnd: pgtype.Date{Time: end, Valid: true},
		Confidence: input.Confidence, ReportedHealth: input.ReportedHealth,
	})
	return err
}

func (s *Service) DeleteRock(ctx context.Context, workspaceID, projectID pgtype.UUID) (bool, error) {
	count, err := s.queries.DeleteRock(ctx, cerebrodb.DeleteRockParams{ProjectID: projectID, WorkspaceID: workspaceID})
	return count > 0, err
}

func (s *Service) ListRocks(ctx context.Context, workspaceID pgtype.UUID) ([]RockResponse, error) {
	rows, err := s.queries.ListRockRollups(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]RockResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, rockResponse(row, s.now()))
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

func strategyParams(workspaceID pgtype.UUID, input StrategyItemInput) cerebrodb.CreateStrategyItemParams {
	return cerebrodb.CreateStrategyItemParams{
		WorkspaceID: workspaceID, Kind: input.Kind, Title: strings.TrimSpace(input.Title),
		Description: input.Description, HorizonUnit: text(input.HorizonUnit),
		HorizonCount: integer(input.HorizonCount), Position: input.Position, State: defaultState(input.State),
	}
}

func strategyResponse(row cerebrodb.CerebroStrategyItem) StrategyItemResponse {
	return StrategyItemResponse{
		ID: util.UUIDToString(row.ID), WorkspaceID: util.UUIDToString(row.WorkspaceID), Kind: row.Kind,
		Title: row.Title, Description: row.Description, HorizonUnit: row.HorizonUnit.String,
		HorizonCount: row.HorizonCount.Int32, Position: row.Position, State: row.State,
		CreatedAt: timestamp(row.CreatedAt), UpdatedAt: timestamp(row.UpdatedAt),
	}
}

func rockResponse(row cerebrodb.ListRockRollupsRow, now time.Time) RockResponse {
	return RockResponse{
		ProjectID: util.UUIDToString(row.ProjectID), WorkspaceID: util.UUIDToString(row.WorkspaceID),
		ProjectTitle: row.ProjectTitle, ProjectDescription: row.ProjectDescription.String,
		ProjectStatus: row.ProjectStatus, LeadType: row.LeadType.String, LeadID: util.UUIDToString(row.LeadID),
		PeriodStart: date(row.PeriodStart), PeriodEnd: date(row.PeriodEnd), Confidence: row.Confidence,
		ReportedHealth: row.ReportedHealth,
		DerivedHealth:  DeriveHealth(row.IssueCount, row.DoneIssueCount, row.BlockedIssueCount, now),
		IssueCount:     row.IssueCount, DoneIssueCount: row.DoneIssueCount, BlockedIssueCount: row.BlockedIssueCount,
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
