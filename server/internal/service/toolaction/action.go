package toolaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	ErrRawValue          = errors.New("raw action values are forbidden")
	ErrSensitiveMetadata = errors.New("sensitive action metadata is forbidden")
	ErrInvalidMetadata   = errors.New("invalid action metadata")
	ErrStoredMetadata    = errors.New("stored action metadata failed validation")
)

type Event struct {
	ID                string    `json:"id,omitempty"`
	WorkspaceID       string    `json:"workspace_id,omitempty"`
	AgentID           string    `json:"agent_id,omitempty"`
	TaskID            string    `json:"task_id,omitempty"`
	IssueID           string    `json:"issue_id,omitempty"`
	InvocationID      string    `json:"invocation_id,omitempty"`
	ApprovalRequestID string    `json:"approval_request_id,omitempty"`
	TransportKind     string    `json:"transport_kind"`
	ServerKey         string    `json:"server_key"`
	ToolName          string    `json:"tool_name"`
	SchemaDigest      string    `json:"schema_digest"`
	CoverageKind      string    `json:"coverage_kind"`
	EventType         string    `json:"event_type"`
	ArgumentBytes     *int32    `json:"argument_bytes,omitempty"`
	ResultBytes       *int32    `json:"result_bytes,omitempty"`
	DurationMS        *int64    `json:"duration_ms,omitempty"`
	OutcomeCode       string    `json:"outcome_code,omitempty"`
	ErrorClass        string    `json:"error_class,omitempty"`
	ActorUserID       string    `json:"actor_user_id,omitempty"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
}

type ListParams struct {
	WorkspaceID     string
	AgentID         string
	EventType       string
	Since           *time.Time
	CursorCreatedAt *time.Time
	CursorID        string
	Limit           int32
}

type SQLService struct {
	queries *db.Queries
	now     func() time.Time
}

type EventQueries interface {
	CreateOrGetAgentToolActionEvent(context.Context, db.CreateOrGetAgentToolActionEventParams) (db.CreateOrGetAgentToolActionEventRow, error)
}

type InTransactionRecorder interface {
	RecordIn(context.Context, EventQueries, Event) (Event, error)
}

func NewSQLService(queries *db.Queries) *SQLService {
	return &SQLService{queries: queries, now: func() time.Time { return time.Now().UTC() }}
}

func DecodeMetadataOnly(reader io.Reader) (Event, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var event Event
	if err := decoder.Decode(&event); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return Event{}, fmt.Errorf("%w: %v", ErrRawValue, err)
		}
		return Event{}, fmt.Errorf("%w: %v", ErrInvalidMetadata, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Event{}, fmt.Errorf("%w: trailing JSON", ErrInvalidMetadata)
	}
	if err := Validate(event); err != nil {
		return Event{}, err
	}
	return event, nil
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var identityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$`)

func Validate(event Event) error {
	if event.TransportKind != "managed_mcp" && event.TransportKind != "managed_native" {
		return fmt.Errorf("%w: transport_kind", ErrInvalidMetadata)
	}
	if event.CoverageKind != "managed_mcp" && event.CoverageKind != "managed_native" && event.CoverageKind != "declaration_only" {
		return fmt.Errorf("%w: coverage_kind", ErrInvalidMetadata)
	}
	if !validEventType(event.EventType) {
		return fmt.Errorf("%w: event_type", ErrInvalidMetadata)
	}
	if !identityPattern.MatchString(event.ServerKey) || !identityPattern.MatchString(event.ToolName) {
		return fmt.Errorf("%w: raw value in tool identity", ErrRawValue)
	}
	for _, value := range []string{event.ServerKey, event.ToolName, event.OutcomeCode, event.ErrorClass} {
		if containsSensitiveMarker(value) {
			return ErrSensitiveMetadata
		}
	}
	if !digestPattern.MatchString(event.SchemaDigest) {
		return fmt.Errorf("%w: schema_digest", ErrInvalidMetadata)
	}
	if event.ArgumentBytes != nil && (*event.ArgumentBytes < 0 || *event.ArgumentBytes > 134217728) || event.ResultBytes != nil && (*event.ResultBytes < 0 || *event.ResultBytes > 134217728) || event.DurationMS != nil && (*event.DurationMS < 0 || *event.DurationMS > 86400000) {
		return fmt.Errorf("%w: size or duration bounds", ErrInvalidMetadata)
	}
	if !validOutcome(event.OutcomeCode) || !validErrorClass(event.ErrorClass) {
		return fmt.Errorf("%w: outcome metadata", ErrInvalidMetadata)
	}
	if (event.EventType == "approval_approved" || event.EventType == "approval_denied") != (event.ActorUserID != "") {
		return fmt.Errorf("%w: actor_user_id", ErrInvalidMetadata)
	}
	return nil
}

func (s *SQLService) RecordIn(ctx context.Context, queries EventQueries, event Event) (Event, error) {
	if err := Validate(event); err != nil {
		return Event{}, err
	}
	workspaceID, err := requiredUUID(event.WorkspaceID, "workspace_id")
	if err != nil {
		return Event{}, err
	}
	agentID, err := requiredUUID(event.AgentID, "agent_id")
	if err != nil {
		return Event{}, err
	}
	taskID, err := requiredUUID(event.TaskID, "task_id")
	if err != nil {
		return Event{}, err
	}
	invocationID, err := requiredUUID(event.InvocationID, "invocation_id")
	if err != nil {
		return Event{}, err
	}
	for field, value := range map[string]string{
		"issue_id":            event.IssueID,
		"approval_request_id": event.ApprovalRequestID,
		"actor_user_id":       event.ActorUserID,
	} {
		if value != "" {
			if _, err := requiredUUID(value, field); err != nil {
				return Event{}, err
			}
		}
	}
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = s.now()
	}
	row, err := queries.CreateOrGetAgentToolActionEvent(ctx, db.CreateOrGetAgentToolActionEventParams{
		WorkspaceID:       workspaceID,
		AgentID:           agentID,
		TaskID:            taskID,
		IssueID:           optionalUUID(event.IssueID),
		InvocationID:      invocationID,
		ApprovalRequestID: optionalUUID(event.ApprovalRequestID),
		TransportKind:     event.TransportKind,
		ServerKey:         event.ServerKey,
		ToolName:          event.ToolName,
		SchemaDigest:      event.SchemaDigest,
		CoverageKind:      event.CoverageKind,
		EventType:         event.EventType,
		ArgumentBytes:     optionalInt4(event.ArgumentBytes),
		ResultBytes:       optionalInt4(event.ResultBytes),
		DurationMs:        optionalInt8(event.DurationMS),
		OutcomeCode:       optionalText(event.OutcomeCode),
		ErrorClass:        optionalText(event.ErrorClass),
		ActorUserID:       optionalUUID(event.ActorUserID),
		CreatedAt:         pgtype.Timestamptz{Time: createdAt, Valid: true},
	})
	if err != nil {
		return Event{}, fmt.Errorf("record action event: %w", err)
	}
	event.ID = util.UUIDToString(row.ID)
	event.CreatedAt = row.CreatedAt.Time
	return event, nil
}

func (s *SQLService) List(ctx context.Context, params ListParams) ([]Event, error) {
	workspaceID, err := requiredUUID(params.WorkspaceID, "workspace_id")
	if err != nil {
		return nil, err
	}
	agentID, err := requiredUUID(params.AgentID, "agent_id")
	if err != nil {
		return nil, err
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		return nil, fmt.Errorf("%w: limit", ErrInvalidMetadata)
	}
	if (params.CursorCreatedAt == nil) != (params.CursorID == "") {
		return nil, fmt.Errorf("%w: cursor", ErrInvalidMetadata)
	}
	if params.CursorID != "" {
		if _, err := requiredUUID(params.CursorID, "cursor_id"); err != nil {
			return nil, err
		}
	}
	rows, err := s.queries.ListAgentToolActionEvents(ctx, db.ListAgentToolActionEventsParams{
		WorkspaceID:     workspaceID,
		AgentID:         agentID,
		FilterEventType: optionalText(params.EventType),
		Since:           optionalTime(params.Since),
		CursorCreatedAt: optionalTime(params.CursorCreatedAt),
		CursorID:        optionalUUID(params.CursorID),
		PageSize:        limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list action events: %w", err)
	}
	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		event := eventFromRow(row)
		if err := Validate(event); err != nil {
			return nil, ErrStoredMetadata
		}
		events = append(events, event)
	}
	return events, nil
}

func IsValidEventType(value string) bool {
	return value == "" || validEventType(value)
}

func eventFromRow(row db.AgentToolActionEvent) Event {
	event := Event{
		ID:                util.UUIDToString(row.ID),
		WorkspaceID:       util.UUIDToString(row.WorkspaceID),
		AgentID:           util.UUIDToString(row.AgentID),
		TaskID:            util.UUIDToString(row.TaskID),
		IssueID:           util.UUIDToString(row.IssueID),
		InvocationID:      util.UUIDToString(row.InvocationID),
		ApprovalRequestID: util.UUIDToString(row.ApprovalRequestID),
		TransportKind:     row.TransportKind,
		ServerKey:         row.ServerKey,
		ToolName:          row.ToolName,
		SchemaDigest:      row.SchemaDigest,
		CoverageKind:      row.CoverageKind,
		EventType:         row.EventType,
		OutcomeCode:       row.OutcomeCode.String,
		ErrorClass:        row.ErrorClass.String,
		ActorUserID:       util.UUIDToString(row.ActorUserID),
		CreatedAt:         row.CreatedAt.Time,
	}
	if row.ArgumentBytes.Valid {
		value := row.ArgumentBytes.Int32
		event.ArgumentBytes = &value
	}
	if row.ResultBytes.Valid {
		value := row.ResultBytes.Int32
		event.ResultBytes = &value
	}
	if row.DurationMs.Valid {
		value := row.DurationMs.Int64
		event.DurationMS = &value
	}
	return event
}

func requiredUUID(value, field string) (pgtype.UUID, error) {
	parsed, err := util.ParseUUID(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("%w: %s", ErrInvalidMetadata, field)
	}
	return parsed, nil
}

func optionalUUID(value string) pgtype.UUID {
	if value == "" {
		return pgtype.UUID{}
	}
	parsed, err := util.ParseUUID(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return parsed
}

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func optionalInt4(value *int32) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *value, Valid: true}
}

func optionalInt8(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}

func optionalTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func validEventType(value string) bool {
	switch value {
	case "requested", "policy_allowed", "policy_denied", "approval_requested", "approval_approved", "approval_denied", "approval_expired", "approval_consumed", "started", "succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func validOutcome(value string) bool {
	switch value {
	case "", "allowed", "denied", "approval_required", "approved", "consumed", "expired", "cancelled", "started", "succeeded", "failed":
		return true
	default:
		return false
	}
}

func validErrorClass(value string) bool {
	switch value {
	case "", "transport", "timeout", "cancelled", "invalid_request", "provider", "internal", "audit", "schema_drift", "unsupported", "policy":
		return true
	default:
		return false
	}
}

func containsSensitiveMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"canary", "bearer ", "password=", "token=", "api_key=", "-----begin", "sk-", "ghp_"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
