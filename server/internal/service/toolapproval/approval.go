package toolapproval

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
)

var (
	ErrForbidden       = errors.New("tool approval operation forbidden")
	ErrInvalidMetadata = errors.New("invalid tool approval metadata")
	ErrRawMetadata     = errors.New("raw tool approval metadata is forbidden")
	ErrInvalidDecision = errors.New("invalid tool approval decision")
)

type ActorKind string

const (
	ActorHuman  ActorKind = "human"
	ActorAgent  ActorKind = "agent"
	ActorTask   ActorKind = "task"
	ActorDaemon ActorKind = "daemon"
)

type Actor struct {
	Kind          ActorKind
	UserID        string
	AgentID       string
	WorkspaceRole string
}

const (
	DecisionApproved = "approved"
	DecisionDenied   = "denied"
	DecisionApprove  = "approve"
	DecisionDeny     = "deny"
	StatusPending    = "pending"
	StatusConsumed   = "consumed"
	StatusCancelled  = "cancelled"
)

type Approval struct {
	ID               string     `json:"id"`
	WorkspaceID      string     `json:"workspace_id"`
	AgentID          string     `json:"agent_id,omitempty"`
	TaskID           string     `json:"task_id,omitempty"`
	IssueID          string     `json:"issue_id,omitempty"`
	ChatSessionID    string     `json:"chat_session_id,omitempty"`
	InvocationID     string     `json:"invocation_id,omitempty"`
	TransportKind    string     `json:"transport_kind,omitempty"`
	ServerKey        string     `json:"server_key,omitempty"`
	ToolName         string     `json:"tool_name,omitempty"`
	SchemaDigest     string     `json:"schema_digest,omitempty"`
	PolicyRevision   int64      `json:"policy_revision,omitempty"`
	SchemaFieldNames []string   `json:"schema_field_names,omitempty"`
	ArgumentBytes    int32      `json:"argument_bytes,omitempty"`
	Status           string     `json:"status"`
	ReasonCode       string     `json:"reason_code,omitempty"`
	RequestedAt      time.Time  `json:"requested_at,omitempty"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
	ConsumedAt       *time.Time `json:"consumed_at,omitempty"`
	CancelledAt      *time.Time `json:"cancelled_at,omitempty"`
	ExpiresAt        time.Time  `json:"expires_at,omitempty"`
	DeciderUserID    string     `json:"decider_user_id,omitempty"`
	DecidedByUserID  string     `json:"-"`
}

type Creation struct {
	WorkspaceID      string
	AgentID          string
	TaskID           string
	IssueID          string
	ChatSessionID    string
	InvocationID     string
	IdempotencyKey   string
	TransportKind    string
	ServerKey        string
	ToolName         string
	SchemaDigest     string
	PolicyRevision   int64
	SchemaFieldNames []string
	ArgumentBytes    int32
	RequestedAt      time.Time
	ExpiresAt        time.Time
	Actor            Actor
}

type Decision struct {
	WorkspaceID    string
	ApprovalID     string
	Actor          Actor
	Decision       string
	ReasonCode     string
	ExpectedStatus string
	DecidedAt      time.Time
}

type Consumption struct {
	WorkspaceID    string
	TaskID         string
	ApprovalID     string
	InvocationID   string
	TransportKind  string
	ServerKey      string
	ToolName       string
	SchemaDigest   string
	PolicyRevision int64
	Actor          Actor
	ConsumedAt     time.Time
}

type Cancellation struct {
	WorkspaceID string
	TaskID      string
	ApprovalID  string
	Actor       Actor
	ReasonCode  string
	CancelledAt time.Time
}

type Lookup struct {
	WorkspaceID string
	TaskID      string
	ApprovalID  string
	Actor       Actor
}

type OperatorLookup struct {
	WorkspaceID string
	ApprovalID  string
	Actor       Actor
}

type PendingQuery struct {
	WorkspaceID string
	AgentID     string
	Actor       Actor
	Limit       int32
	AsOf        time.Time
}

type Repository interface {
	CreateOrGet(context.Context, Creation) (Approval, error)
	Decide(context.Context, Decision) (Approval, error)
	Consume(context.Context, Consumption) (Approval, error)
	Cancel(context.Context, Cancellation) (Approval, error)
	Get(context.Context, Lookup) (Approval, error)
	ListPending(context.Context, PendingQuery) ([]Approval, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: func() time.Time { return time.Now().UTC() }}
}

var approvalDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var approvalIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$`)

func (s *Service) CreateOrGet(ctx context.Context, creation Creation) (Approval, error) {
	if creation.Actor.Kind != ActorDaemon {
		return Approval{}, ErrForbidden
	}
	creation.IdempotencyKey = strings.TrimSpace(creation.IdempotencyKey)
	creation.TransportKind = strings.ToLower(strings.TrimSpace(creation.TransportKind))
	creation.ServerKey = strings.TrimSpace(creation.ServerKey)
	creation.ToolName = strings.TrimSpace(creation.ToolName)
	creation.SchemaDigest = strings.ToLower(strings.TrimSpace(creation.SchemaDigest))
	fields := make([]string, len(creation.SchemaFieldNames))
	copy(fields, creation.SchemaFieldNames)
	creation.SchemaFieldNames = fields
	for i := range creation.SchemaFieldNames {
		creation.SchemaFieldNames[i] = strings.TrimSpace(creation.SchemaFieldNames[i])
	}
	sort.Strings(creation.SchemaFieldNames)

	now := s.now().UTC()
	creation.RequestedAt = now
	if err := validateCreation(creation, now); err != nil {
		return Approval{}, err
	}
	return s.repository.CreateOrGet(ctx, creation)
}

func validateCreation(creation Creation, now time.Time) error {
	for name, value := range map[string]string{
		"workspace_id":  creation.WorkspaceID,
		"agent_id":      creation.AgentID,
		"task_id":       creation.TaskID,
		"invocation_id": creation.InvocationID,
	} {
		if _, err := util.ParseUUID(value); err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidMetadata, name)
		}
	}
	for name, value := range map[string]string{"issue_id": creation.IssueID, "chat_session_id": creation.ChatSessionID} {
		if value != "" {
			if _, err := util.ParseUUID(value); err != nil {
				return fmt.Errorf("%w: %s", ErrInvalidMetadata, name)
			}
		}
	}
	if len(creation.IdempotencyKey) < 1 || len(creation.IdempotencyKey) > 128 || containsSensitiveMarker(creation.IdempotencyKey) {
		return fmt.Errorf("%w: idempotency_key", ErrRawMetadata)
	}
	if creation.TransportKind != "managed_mcp" && creation.TransportKind != "managed_native" {
		return fmt.Errorf("%w: transport_kind", ErrInvalidMetadata)
	}
	if !approvalIdentityPattern.MatchString(creation.ServerKey) || !approvalIdentityPattern.MatchString(creation.ToolName) {
		return fmt.Errorf("%w: tool identity", ErrRawMetadata)
	}
	if containsSensitiveMarker(creation.ServerKey) || containsSensitiveMarker(creation.ToolName) {
		return fmt.Errorf("%w: tool identity", ErrRawMetadata)
	}
	if !approvalDigestPattern.MatchString(creation.SchemaDigest) || creation.PolicyRevision < 1 {
		return fmt.Errorf("%w: policy identity", ErrInvalidMetadata)
	}
	if len(creation.SchemaFieldNames) > 128 {
		return fmt.Errorf("%w: schema_field_names", ErrInvalidMetadata)
	}
	for i, field := range creation.SchemaFieldNames {
		if !approvalIdentityPattern.MatchString(field) || containsSensitiveMarker(field) {
			return fmt.Errorf("%w: schema_field_names", ErrRawMetadata)
		}
		if i > 0 && field == creation.SchemaFieldNames[i-1] {
			return fmt.Errorf("%w: duplicate schema_field_names", ErrInvalidMetadata)
		}
	}
	if creation.ArgumentBytes < 0 || creation.ArgumentBytes > 134217728 {
		return fmt.Errorf("%w: argument_bytes", ErrInvalidMetadata)
	}
	if !creation.ExpiresAt.After(now) || creation.ExpiresAt.After(now.Add(24*time.Hour)) {
		return fmt.Errorf("%w: expires_at", ErrInvalidMetadata)
	}
	return nil
}

func containsSensitiveMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"canary", "bearer ", "password=", "token=", "api_key=", "-----begin", "sk-", "ghp_", "http://", "https://"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (s *Service) Decide(ctx context.Context, decision Decision) (Approval, error) {
	if decision.Actor.Kind != ActorHuman || (decision.Actor.WorkspaceRole != "owner" && decision.Actor.WorkspaceRole != "admin") {
		return Approval{}, ErrForbidden
	}
	for name, value := range map[string]string{
		"workspace_id":  decision.WorkspaceID,
		"approval_id":   decision.ApprovalID,
		"actor_user_id": decision.Actor.UserID,
	} {
		if _, err := util.ParseUUID(value); err != nil {
			return Approval{}, fmt.Errorf("%w: %s", ErrInvalidMetadata, name)
		}
	}
	if decision.ExpectedStatus == "" {
		decision.ExpectedStatus = StatusPending
	}
	if decision.ExpectedStatus != StatusPending {
		return Approval{}, ErrStateConflict
	}
	switch decision.Decision {
	case DecisionApprove, DecisionApproved:
		decision.Decision = DecisionApproved
		if decision.ReasonCode != "" && decision.ReasonCode != "operator_approved" {
			return Approval{}, ErrInvalidDecision
		}
		decision.ReasonCode = "operator_approved"
	case DecisionDeny, DecisionDenied:
		decision.Decision = DecisionDenied
		if decision.ReasonCode == "" {
			decision.ReasonCode = "operator_denied"
		}
		switch decision.ReasonCode {
		case "operator_denied", "unexpected_action", "risk_too_high", "not_needed":
		default:
			return Approval{}, ErrInvalidDecision
		}
	default:
		return Approval{}, ErrInvalidDecision
	}
	decision.DecidedAt = s.now().UTC()
	return s.repository.Decide(ctx, decision)
}

func (s *Service) Consume(ctx context.Context, consumption Consumption) (Approval, error) {
	if consumption.Actor.Kind != ActorDaemon {
		return Approval{}, ErrForbidden
	}
	if err := validateOperationIDs(consumption.WorkspaceID, consumption.TaskID, consumption.ApprovalID); err != nil {
		return Approval{}, err
	}
	consumption.ConsumedAt = s.now().UTC()
	return s.repository.Consume(ctx, consumption)
}

func (s *Service) Cancel(ctx context.Context, cancellation Cancellation) (Approval, error) {
	if cancellation.Actor.Kind != ActorDaemon {
		return Approval{}, ErrForbidden
	}
	if err := validateOperationIDs(cancellation.WorkspaceID, cancellation.TaskID, cancellation.ApprovalID); err != nil {
		return Approval{}, err
	}
	if cancellation.ReasonCode != "task_cancelled" {
		return Approval{}, ErrInvalidDecision
	}
	cancellation.CancelledAt = s.now().UTC()
	return s.repository.Cancel(ctx, cancellation)
}

func (s *Service) Get(ctx context.Context, lookup Lookup) (Approval, error) {
	if lookup.Actor.Kind != ActorDaemon {
		return Approval{}, ErrForbidden
	}
	if err := validateOperationIDs(lookup.WorkspaceID, lookup.TaskID, lookup.ApprovalID); err != nil {
		return Approval{}, err
	}
	return s.repository.Get(ctx, lookup)
}

func (s *Service) GetOperator(ctx context.Context, lookup OperatorLookup) (Approval, error) {
	if lookup.Actor.Kind != ActorHuman || (lookup.Actor.WorkspaceRole != "owner" && lookup.Actor.WorkspaceRole != "admin") {
		return Approval{}, ErrForbidden
	}
	for _, value := range []string{lookup.WorkspaceID, lookup.ApprovalID, lookup.Actor.UserID} {
		if _, err := util.ParseUUID(value); err != nil {
			return Approval{}, ErrInvalidMetadata
		}
	}
	repository, ok := s.repository.(interface {
		GetOperator(context.Context, OperatorLookup) (Approval, error)
	})
	if !ok {
		return Approval{}, ErrForbidden
	}
	return repository.GetOperator(ctx, lookup)
}

func (s *Service) ListPending(ctx context.Context, query PendingQuery) ([]Approval, error) {
	if query.Actor.Kind != ActorHuman || (query.Actor.WorkspaceRole != "owner" && query.Actor.WorkspaceRole != "admin") {
		return nil, ErrForbidden
	}
	if _, err := util.ParseUUID(query.WorkspaceID); err != nil {
		return nil, fmt.Errorf("%w: workspace_id", ErrInvalidMetadata)
	}
	if _, err := util.ParseUUID(query.Actor.UserID); err != nil {
		return nil, fmt.Errorf("%w: actor_user_id", ErrInvalidMetadata)
	}
	if query.AgentID != "" {
		if _, err := util.ParseUUID(query.AgentID); err != nil {
			return nil, fmt.Errorf("%w: agent_id", ErrInvalidMetadata)
		}
	}
	if query.Limit == 0 {
		query.Limit = 50
	}
	if query.Limit < 1 || query.Limit > 100 {
		return nil, fmt.Errorf("%w: limit", ErrInvalidMetadata)
	}
	query.AsOf = s.now().UTC()
	return s.repository.ListPending(ctx, query)
}

func validateOperationIDs(workspaceID, taskID, approvalID string) error {
	for name, value := range map[string]string{
		"workspace_id": workspaceID,
		"task_id":      taskID,
		"approval_id":  approvalID,
	} {
		if _, err := util.ParseUUID(value); err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidMetadata, name)
		}
	}
	return nil
}
