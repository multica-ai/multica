// Package grants implements the Persona grant control plane for JEH-1179.
//
// A grant expresses "subject S may perform capability C on resources matching
// pattern P in workspace W", with optional constraints (classification ceiling,
// time window, approval requirement). Each mutation is recorded in an
// append-only audit ledger.
package grants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TxStarter abstracts transaction creation. *pgxpool.Pool satisfies it.
// Used to make mutation + audit insert atomic so a successful mutation
// without an audit row is impossible (JEH-1213).
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Subject types accepted on the wire and stored in subject_type.
const (
	SubjectTypeMember           = "member"
	SubjectTypeAgent            = "agent"
	SubjectTypeGroup            = "group"
	SubjectTypeRole             = "role"
	SubjectTypeWorkspaceDefault = "workspace_default"
)

var knownSubjectTypes = map[string]struct{}{
	SubjectTypeMember:           {},
	SubjectTypeAgent:            {},
	SubjectTypeGroup:            {},
	SubjectTypeRole:             {},
	SubjectTypeWorkspaceDefault: {},
}

// capabilityPattern validates capability identifiers: lowercase letters,
// digits, underscores, and optional dot/colon-namespacing (e.g. "issue.read").
var capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9_.:]*$`)

// resourcePatternPattern validates resource patterns: path segments, wildcards,
// and placeholder tokens ({id}). Examples: "*", "issues/*", "projects/{id}".
var resourcePatternPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-/*{}]+$`)

// Event names published on the bus so frontend query caches can invalidate.
const (
	EventGrantCreated = "grant:created"
	EventGrantUpdated = "grant:updated"
	EventGrantRevoked = "grant:revoked"
	EventGrantDeleted = "grant:deleted"
)

// Surfaces accepted in audit rows.
const (
	SurfaceAPI = "api"
	SurfaceCLI = "cli"
	SurfaceUI  = "ui"
	SurfaceMCP = "mcp"
)

var (
	ErrUnknownSubjectType     = errors.New("unknown subject_type")
	ErrSubjectIDRequired      = errors.New("subject_id required for this subject_type")
	ErrInvalidCapability      = errors.New("invalid capability identifier")
	ErrInvalidResourcePattern = errors.New("invalid resource_pattern")
	ErrGrantNotFound          = errors.New("grant not found")
	ErrSubjectNotFound        = errors.New("subject not found in workspace")
)

// Service centralises grant reads/writes and audit logging.
type Service struct {
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries
	Tx       TxStarter
	Bus      *events.Bus
}

func New(cerebro *cerebrodb.Queries, upstream *db.Queries, tx TxStarter, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, Upstream: upstream, Tx: tx, Bus: bus}
}

// CreateParams is the input shape for Create.
type CreateParams struct {
	WorkspaceID           pgtype.UUID
	SubjectType           string
	SubjectID             pgtype.UUID // invalid (null) when SubjectType == workspace_default
	ResourcePattern       string
	Capability            string
	ClassificationCeiling pgtype.Text
	TimeWindowStart       pgtype.Timestamptz
	TimeWindowEnd         pgtype.Timestamptz
	ApprovalRequired      bool
	GrantedByType         pgtype.Text
	GrantedByID           pgtype.UUID
	Surface               string
}

// UpdateParams is the input shape for Update. Zero/invalid values are left
// unchanged (COALESCE in SQL).
type UpdateParams struct {
	ID                    pgtype.UUID
	WorkspaceID           pgtype.UUID
	ResourcePattern       pgtype.Text
	Capability            pgtype.Text
	ClassificationCeiling pgtype.Text
	TimeWindowStart       pgtype.Timestamptz
	TimeWindowEnd         pgtype.Timestamptz
	ApprovalRequired      pgtype.Bool
	ActorType             pgtype.Text
	ActorID               pgtype.UUID
	Surface               string
}

func (s *Service) Validate(subjectType, resourcePattern, capability string) error {
	if _, ok := knownSubjectTypes[subjectType]; !ok {
		return ErrUnknownSubjectType
	}
	if resourcePattern == "" || !resourcePatternPattern.MatchString(resourcePattern) {
		return ErrInvalidResourcePattern
	}
	if capability == "" || !capabilityPattern.MatchString(capability) {
		return ErrInvalidCapability
	}
	return nil
}

// ValidateSubjectExists confirms the subject UUID refers to a real entity in the workspace.
// workspace_default and role subject types are skipped (no per-ID table to check).
func (s *Service) ValidateSubjectExists(ctx context.Context, subjectType string, subjectID, workspaceID pgtype.UUID) error {
	switch subjectType {
	case SubjectTypeMember:
		_, err := s.Upstream.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      subjectID,
			WorkspaceID: workspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSubjectNotFound
		}
		return err
	case SubjectTypeAgent:
		_, err := s.Upstream.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          subjectID,
			WorkspaceID: workspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSubjectNotFound
		}
		return err
	case SubjectTypeGroup:
		g, err := s.Cerebro.GetCerebroGroup(ctx, subjectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSubjectNotFound
		}
		if err != nil {
			return err
		}
		if g.WorkspaceID != workspaceID {
			return ErrSubjectNotFound
		}
		return nil
	}
	return nil
}

func (s *Service) Create(ctx context.Context, p CreateParams) (cerebrodb.CerebroWorkspaceGrant, error) {
	surface := p.Surface
	if surface == "" {
		surface = SurfaceAPI
	}
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Cerebro.WithTx(tx)

	row, err := qtx.CreateCerebroWorkspaceGrant(ctx, cerebrodb.CreateCerebroWorkspaceGrantParams{
		WorkspaceID:           p.WorkspaceID,
		SubjectType:           p.SubjectType,
		SubjectID:             p.SubjectID,
		ResourcePattern:       p.ResourcePattern,
		Capability:            p.Capability,
		ClassificationCeiling: p.ClassificationCeiling,
		TimeWindowStart:       p.TimeWindowStart,
		TimeWindowEnd:         p.TimeWindowEnd,
		ApprovalRequired:      p.ApprovalRequired,
		GrantedByType:         p.GrantedByType,
		GrantedByID:           p.GrantedByID,
	})
	if err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, err
	}

	if err := qtx.InsertCerebroGrantAudit(ctx, cerebrodb.InsertCerebroGrantAuditParams{
		WorkspaceID: p.WorkspaceID,
		GrantID:     row.ID,
		Action:      "created",
		ActorType:   p.GrantedByType,
		ActorID:     p.GrantedByID,
		Surface:     surface,
		Diff:        grantDiff(nil, &row),
	}); err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publish(EventGrantCreated, p.WorkspaceID, p.GrantedByID, map[string]any{
		"grant_id": util.UUIDToString(row.ID),
		"action":   "created",
	})
	return row, nil
}

func (s *Service) Get(ctx context.Context, grantID, workspaceID pgtype.UUID) (cerebrodb.CerebroWorkspaceGrant, error) {
	return s.Cerebro.GetCerebroWorkspaceGrant(ctx, cerebrodb.GetCerebroWorkspaceGrantParams{
		ID:          grantID,
		WorkspaceID: workspaceID,
	})
}

// ListFilter carries the optional filter fields for List.
type ListFilter struct {
	SubjectType string
	SubjectID   pgtype.UUID
	Status      string
}

func (s *Service) List(ctx context.Context, workspaceID pgtype.UUID, f ListFilter) ([]cerebrodb.CerebroWorkspaceGrant, error) {
	return s.Cerebro.ListCerebroWorkspaceGrants(ctx, cerebrodb.ListCerebroWorkspaceGrantsParams{
		WorkspaceID: workspaceID,
		Column2:     f.SubjectType,
		Column3:     f.SubjectID,
		Column4:     f.Status,
	})
}

func (s *Service) Update(ctx context.Context, p UpdateParams) (cerebrodb.CerebroWorkspaceGrant, error) {
	surface := p.Surface
	if surface == "" {
		surface = SurfaceAPI
	}

	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Cerebro.WithTx(tx)

	before, err := qtx.GetCerebroWorkspaceGrant(ctx, cerebrodb.GetCerebroWorkspaceGrantParams{
		ID: p.ID, WorkspaceID: p.WorkspaceID,
	})
	if err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, err
	}

	resourcePattern := ""
	if p.ResourcePattern.Valid {
		resourcePattern = p.ResourcePattern.String
	}
	capability := ""
	if p.Capability.Valid {
		capability = p.Capability.String
	}
	approvalRequired := false
	if p.ApprovalRequired.Valid {
		approvalRequired = p.ApprovalRequired.Bool
	}

	row, err := qtx.UpdateCerebroWorkspaceGrant(ctx, cerebrodb.UpdateCerebroWorkspaceGrantParams{
		ID:                    p.ID,
		WorkspaceID:           p.WorkspaceID,
		ResourcePattern:       resourcePattern,
		Capability:            capability,
		ClassificationCeiling: p.ClassificationCeiling,
		TimeWindowStart:       p.TimeWindowStart,
		TimeWindowEnd:         p.TimeWindowEnd,
		ApprovalRequired:      approvalRequired,
	})
	if err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, err
	}

	if err := qtx.InsertCerebroGrantAudit(ctx, cerebrodb.InsertCerebroGrantAuditParams{
		WorkspaceID: p.WorkspaceID,
		GrantID:     p.ID,
		Action:      "updated",
		ActorType:   p.ActorType,
		ActorID:     p.ActorID,
		Surface:     surface,
		Diff:        grantDiff(&before, &row),
	}); err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publish(EventGrantUpdated, p.WorkspaceID, p.ActorID, map[string]any{
		"grant_id": util.UUIDToString(p.ID),
		"action":   "updated",
	})
	return row, nil
}

func (s *Service) Revoke(ctx context.Context, grantID, workspaceID, actorID pgtype.UUID, actorType, surface string) (cerebrodb.CerebroWorkspaceGrant, error) {
	if surface == "" {
		surface = SurfaceAPI
	}

	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Cerebro.WithTx(tx)

	row, err := qtx.RevokeCerebroWorkspaceGrant(ctx, cerebrodb.RevokeCerebroWorkspaceGrantParams{
		ID:          grantID,
		WorkspaceID: workspaceID,
		RevokedByID: actorID,
	})
	if err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, err
	}
	actorTypePG := pgtype.Text{String: actorType, Valid: actorType != ""}
	if err := qtx.InsertCerebroGrantAudit(ctx, cerebrodb.InsertCerebroGrantAuditParams{
		WorkspaceID: workspaceID,
		GrantID:     grantID,
		Action:      "revoked",
		ActorType:   actorTypePG,
		ActorID:     actorID,
		Surface:     surface,
	}); err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return cerebrodb.CerebroWorkspaceGrant{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publish(EventGrantRevoked, workspaceID, actorID, map[string]any{
		"grant_id": util.UUIDToString(grantID),
		"action":   "revoked",
	})
	return row, nil
}

func (s *Service) Delete(ctx context.Context, grantID, workspaceID, actorID pgtype.UUID, actorType, surface string) error {
	if surface == "" {
		surface = SurfaceAPI
	}

	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Cerebro.WithTx(tx)

	actorTypePG := pgtype.Text{String: actorType, Valid: actorType != ""}
	if err := qtx.InsertCerebroGrantAudit(ctx, cerebrodb.InsertCerebroGrantAuditParams{
		WorkspaceID: workspaceID,
		GrantID:     grantID,
		Action:      "deleted",
		ActorType:   actorTypePG,
		ActorID:     actorID,
		Surface:     surface,
	}); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	if err := qtx.DeleteCerebroWorkspaceGrant(ctx, cerebrodb.DeleteCerebroWorkspaceGrantParams{
		ID:          grantID,
		WorkspaceID: workspaceID,
	}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	s.publish(EventGrantDeleted, workspaceID, actorID, map[string]any{
		"grant_id": util.UUIDToString(grantID),
		"action":   "deleted",
	})
	return nil
}

func (s *Service) publish(eventType string, workspaceID, actorID pgtype.UUID, payload map[string]any) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "member",
		ActorID:     util.UUIDToString(actorID),
		Payload:     payload,
	})
}

// grantDiff produces a compact JSON diff summary for the audit ledger.
func grantDiff(before, after *cerebrodb.CerebroWorkspaceGrant) []byte {
	type snap struct {
		ResourcePattern string `json:"resource_pattern,omitempty"`
		Capability      string `json:"capability,omitempty"`
		Status          string `json:"status,omitempty"`
	}
	m := map[string]any{}
	if after != nil {
		m["after"] = snap{
			ResourcePattern: after.ResourcePattern,
			Capability:      after.Capability,
			Status:          after.Status,
		}
	}
	if before != nil {
		m["before"] = snap{
			ResourcePattern: before.ResourcePattern,
			Capability:      before.Capability,
			Status:          before.Status,
		}
	}
	b, _ := json.Marshal(m)
	return b
}

// surfaceFromHeader extracts the X-Cerebro-Surface request header value or
// returns the default surface. Call it from handler to pass to service.
func surfaceFromHeader(h string) string {
	switch strings.ToLower(h) {
	case SurfaceCLI, SurfaceUI, SurfaceMCP:
		return strings.ToLower(h)
	}
	return SurfaceAPI
}
