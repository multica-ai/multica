// Package approvals implements the approval inbox for FIR-2131 (phase 3 of the
// Multica core permissions stack).
//
// When the permission resolver (internal/cerebro/permissions) returns
// DecisionNeedsApproval for an (actor, capability, resource) request, the
// enforcement gate calls Service.Intake to materialise a pending ask. A human
// then approves, rejects or delegates it from the inbox; every lifecycle event
// is recorded in an append-only audit ledger and broadcast on the event bus so
// the frontend caches invalidate and the requester can be notified.
//
// The terminal transitions (approve / reject / delegate) are race-safe: the
// SQL only mutates a still-pending row, so two concurrent approvers cannot both
// win — the loser gets ErrAlreadyDecided.
package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// TxStarter abstracts transaction creation. *pgxpool.Pool satisfies it. A
// decision and its audit row are committed together so a decided ask without an
// audit trail is impossible.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Requester types.
const (
	RequesterMember = "member"
	RequesterAgent  = "agent"
)

// Statuses.
const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusDelegated = "delegated"
	StatusExpired   = "expired"
	StatusCancelled = "cancelled"
)

// Delegation targets.
const (
	DelegateToMember = "member"
	DelegateToGroup  = "group"
)

// Surfaces accepted in audit rows.
const (
	SurfaceAPI    = "api"
	SurfaceCLI    = "cli"
	SurfaceUI     = "ui"
	SurfaceMCP    = "mcp"
	SurfaceSystem = "system"
)

// Event names published on the bus so frontend query caches can invalidate and
// in-app notifications can fire.
const (
	EventApprovalCreated   = "approval:created"
	EventApprovalDecided   = "approval:decided"
	EventApprovalDelegated = "approval:delegated"
	EventApprovalExpired   = "approval:expired"
)

var (
	// ErrNotFound — no ask with that id in the workspace.
	ErrNotFound = errors.New("approval request not found")
	// ErrAlreadyDecided — the ask is no longer pending; someone got there first.
	ErrAlreadyDecided = errors.New("approval request already decided")
	// ErrInvalidDecision — a Decide call with a non-terminal status.
	ErrInvalidDecision = errors.New("invalid decision status")
)

// Service centralises approval reads/writes, audit logging and bus events.
type Service struct {
	Cerebro *cerebrodb.Queries
	Tx      TxStarter
	Bus     *events.Bus
}

// New constructs a Service.
func New(cerebro *cerebrodb.Queries, tx TxStarter, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, Tx: tx, Bus: bus}
}

// IntakeParams is the input shape for Intake — the seam the enforcement gate
// calls when the resolver returns DecisionNeedsApproval.
type IntakeParams struct {
	WorkspaceID     pgtype.UUID
	RequesterType   string
	RequesterID     pgtype.UUID
	Agent           pgtype.UUID // invalid (null) for direct member calls
	Capability      string
	Resource        string
	Reason          string
	MatchedGrantIDs []pgtype.UUID
	Context         map[string]any
	ExpiresAt       pgtype.Timestamptz // invalid = never expires
	Surface         string
}

// Intake records a new pending ask plus its "created" audit row in one
// transaction, then publishes EventApprovalCreated.
func (s *Service) Intake(ctx context.Context, p IntakeParams) (cerebrodb.CerebroApprovalRequest, error) {
	surface := normaliseSurface(p.Surface)

	grantIDsJSON, err := marshalUUIDs(p.MatchedGrantIDs)
	if err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("marshal grant ids: %w", err)
	}
	contextJSON, err := marshalContext(p.Context)
	if err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("marshal context: %w", err)
	}

	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Cerebro.WithTx(tx)

	row, err := qtx.CreateCerebroApprovalRequest(ctx, cerebrodb.CreateCerebroApprovalRequestParams{
		WorkspaceID:     p.WorkspaceID,
		RequesterType:   p.RequesterType,
		RequesterID:     p.RequesterID,
		AgentID:         p.Agent,
		Capability:      p.Capability,
		Resource:        p.Resource,
		Reason:          p.Reason,
		MatchedGrantIds: grantIDsJSON,
		Context:         contextJSON,
		ExpiresAt:       p.ExpiresAt,
	})
	if err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("create approval: %w", err)
	}

	if err := qtx.InsertCerebroApprovalAudit(ctx, cerebrodb.InsertCerebroApprovalAuditParams{
		WorkspaceID: p.WorkspaceID,
		ApprovalID:  row.ID,
		Action:      "created",
		ActorType:   pgtype.Text{String: p.RequesterType, Valid: p.RequesterType != ""},
		ActorID:     p.RequesterID,
		Surface:     surface,
	}); err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publish(EventApprovalCreated, p.WorkspaceID, p.RequesterType, p.RequesterID, map[string]any{
		"approval_id": util.UUIDToString(row.ID),
		"capability":  row.Capability,
		"status":      row.Status,
	})
	return row, nil
}

// ListFilter carries the optional list filters.
type ListFilter struct {
	Status string
	Limit  int32
	Offset int32
}

// List returns asks for a workspace, pending first. A zero Limit defaults to 50.
func (s *Service) List(ctx context.Context, workspaceID pgtype.UUID, f ListFilter) ([]cerebrodb.ListCerebroApprovalRequestsRow, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.Cerebro.ListCerebroApprovalRequests(ctx, cerebrodb.ListCerebroApprovalRequestsParams{
		WorkspaceID: workspaceID,
		Column2:     f.Status,
		Limit:       limit,
		Offset:      f.Offset,
	})
}

// CountPending returns the number of pending asks — used for the inbox badge.
func (s *Service) CountPending(ctx context.Context, workspaceID pgtype.UUID) (int32, error) {
	return s.Cerebro.CountPendingCerebroApprovals(ctx, workspaceID)
}

// Get fetches one ask scoped to its workspace.
func (s *Service) Get(ctx context.Context, id, workspaceID pgtype.UUID) (cerebrodb.CerebroApprovalRequest, error) {
	row, err := s.Cerebro.GetCerebroApprovalRequest(ctx, cerebrodb.GetCerebroApprovalRequestParams{
		ID:          id,
		WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return cerebrodb.CerebroApprovalRequest{}, ErrNotFound
	}
	return row, err
}

// ReusableQuery identifies the request whose approval a poll-based caller wants
// to rejoin on retry instead of raising a duplicate (FIR-2586).
type ReusableQuery struct {
	WorkspaceID pgtype.UUID
	Agent       pgtype.UUID
	Capability  string
	Resource    string
}

// FindReusable returns the most recent still-actionable approval for the given
// (agent, capability, resource), with ok=false when none exists. A 'pending'
// match lets a retried poll-based request (e.g. a daemon repo checkout) rejoin
// the same ask; an 'approved', unexpired match lets the retry through. This is
// the dedup that stops every checkout retry from spawning a fresh inbox request
// and makes an ask approved after the poll budget actually honourable. Callers
// that block in-process (agent tool loop, credentials) do not need it — they
// hold one Await across the whole window, so there is no retry to dedup.
func (s *Service) FindReusable(ctx context.Context, q ReusableQuery) (cerebrodb.CerebroApprovalRequest, bool, error) {
	row, err := s.Cerebro.FindReusableCerebroApprovalRequest(ctx, cerebrodb.FindReusableCerebroApprovalRequestParams{
		WorkspaceID: q.WorkspaceID,
		AgentID:     q.Agent,
		Capability:  q.Capability,
		Resource:    q.Resource,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return cerebrodb.CerebroApprovalRequest{}, false, nil
	}
	if err != nil {
		return cerebrodb.CerebroApprovalRequest{}, false, fmt.Errorf("find reusable approval: %w", err)
	}
	return row, true, nil
}

// Approve marks a pending ask approved. Race-safe: ErrAlreadyDecided if the ask
// is no longer pending, ErrNotFound if it does not exist. expiresAt time-boxes
// the grant for FindReusable (TECH-3498): the caller computes it — now() for a
// one-shot approve (not reusable for a future call), now()+duration for a period
// grant, or an invalid value for never-expires. An invalid value writes NULL.
func (s *Service) Approve(ctx context.Context, id, workspaceID, actorID pgtype.UUID, note, surface string, expiresAt pgtype.Timestamptz) (cerebrodb.CerebroApprovalRequest, error) {
	return s.decide(ctx, id, workspaceID, actorID, StatusApproved, note, surface, expiresAt)
}

// Reject marks a pending ask rejected. A rejected ask is never reusable, so it
// passes a zero (NULL) expiry.
func (s *Service) Reject(ctx context.Context, id, workspaceID, actorID pgtype.UUID, note, surface string) (cerebrodb.CerebroApprovalRequest, error) {
	return s.decide(ctx, id, workspaceID, actorID, StatusRejected, note, surface, pgtype.Timestamptz{})
}

func (s *Service) decide(ctx context.Context, id, workspaceID, actorID pgtype.UUID, status, note, surface string, expiresAt pgtype.Timestamptz) (cerebrodb.CerebroApprovalRequest, error) {
	if status != StatusApproved && status != StatusRejected {
		return cerebrodb.CerebroApprovalRequest{}, ErrInvalidDecision
	}
	surface = normaliseSurface(surface)

	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Cerebro.WithTx(tx)

	row, err := qtx.DecideCerebroApprovalRequest(ctx, cerebrodb.DecideCerebroApprovalRequestParams{
		ID:           id,
		WorkspaceID:  workspaceID,
		Status:       status,
		DecidedByID:  actorID,
		DecisionNote: pgtype.Text{String: note, Valid: note != ""},
		ExpiresAt:    expiresAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return cerebrodb.CerebroApprovalRequest{}, s.classifyMissing(ctx, qtx, id, workspaceID)
	}
	if err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("decide approval: %w", err)
	}

	if err := qtx.InsertCerebroApprovalAudit(ctx, cerebrodb.InsertCerebroApprovalAuditParams{
		WorkspaceID: workspaceID,
		ApprovalID:  id,
		Action:      status, // "approved" | "rejected"
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     actorID,
		Surface:     surface,
		Note:        pgtype.Text{String: note, Valid: note != ""},
	}); err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publish(EventApprovalDecided, workspaceID, "member", actorID, map[string]any{
		"approval_id": util.UUIDToString(id),
		"status":      row.Status,
	})
	return row, nil
}

// Delegate reassigns a pending ask to another approver (member or group),
// keeping it actionable.
func (s *Service) Delegate(ctx context.Context, id, workspaceID, actorID pgtype.UUID, toType string, toID pgtype.UUID, note, surface string) (cerebrodb.CerebroApprovalRequest, error) {
	if toType != DelegateToMember && toType != DelegateToGroup {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("invalid delegate target type %q", toType)
	}
	if !toID.Valid {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("delegate target id required")
	}
	surface = normaliseSurface(surface)

	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Cerebro.WithTx(tx)

	row, err := qtx.DelegateCerebroApprovalRequest(ctx, cerebrodb.DelegateCerebroApprovalRequestParams{
		ID:              id,
		WorkspaceID:     workspaceID,
		DelegatedToType: pgtype.Text{String: toType, Valid: true},
		DelegatedToID:   toID,
		DecidedByID:     actorID,
		DecisionNote:    pgtype.Text{String: note, Valid: note != ""},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return cerebrodb.CerebroApprovalRequest{}, s.classifyMissing(ctx, qtx, id, workspaceID)
	}
	if err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("delegate approval: %w", err)
	}

	if err := qtx.InsertCerebroApprovalAudit(ctx, cerebrodb.InsertCerebroApprovalAuditParams{
		WorkspaceID: workspaceID,
		ApprovalID:  id,
		Action:      "delegated",
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     actorID,
		Surface:     surface,
		Note:        pgtype.Text{String: note, Valid: note != ""},
	}); err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return cerebrodb.CerebroApprovalRequest{}, fmt.Errorf("commit tx: %w", err)
	}

	s.publish(EventApprovalDelegated, workspaceID, "member", actorID, map[string]any{
		"approval_id":       util.UUIDToString(id),
		"delegated_to_type": toType,
		"delegated_to_id":   util.UUIDToString(toID),
	})
	return row, nil
}

// ExpireDue sweeps every pending ask past its deadline to 'expired', writes one
// audit row per expiry, and publishes EventApprovalExpired. Safe to call from a
// periodic job. Returns the number expired.
func (s *Service) ExpireDue(ctx context.Context) (int, error) {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Cerebro.WithTx(tx)

	expired, err := qtx.ExpireDueCerebroApprovalRequests(ctx)
	if err != nil {
		return 0, fmt.Errorf("expire due: %w", err)
	}
	for _, e := range expired {
		if err := qtx.InsertCerebroApprovalAudit(ctx, cerebrodb.InsertCerebroApprovalAuditParams{
			WorkspaceID: e.WorkspaceID,
			ApprovalID:  e.ID,
			Action:      "expired",
			Surface:     SurfaceSystem,
		}); err != nil {
			return 0, fmt.Errorf("insert expiry audit: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}

	for _, e := range expired {
		s.publish(EventApprovalExpired, e.WorkspaceID, "system", pgtype.UUID{}, map[string]any{
			"approval_id": util.UUIDToString(e.ID),
			"status":      StatusExpired,
		})
	}
	return len(expired), nil
}

// classifyMissing distinguishes "already decided" from "never existed" after a
// conditional update matched no row. It MUST run on the caller's transaction
// (qtx), not on s.Cerebro/the pool: the caller still holds its tx connection,
// so reading via the pool would acquire a second connection and deadlock under
// concurrency once in-flight decisions exhaust the pool (see FIR-2131).
func (s *Service) classifyMissing(ctx context.Context, q *cerebrodb.Queries, id, workspaceID pgtype.UUID) error {
	if _, err := q.GetCerebroApprovalRequest(ctx, cerebrodb.GetCerebroApprovalRequestParams{
		ID:          id,
		WorkspaceID: workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return ErrAlreadyDecided
}

func (s *Service) publish(eventType string, workspaceID pgtype.UUID, actorType string, actorID pgtype.UUID, payload map[string]any) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   actorType,
		ActorID:     util.UUIDToString(actorID),
		Payload:     payload,
	})
}

func normaliseSurface(s string) string {
	switch s {
	case SurfaceCLI, SurfaceUI, SurfaceMCP, SurfaceSystem:
		return s
	default:
		return SurfaceAPI
	}
}

func marshalUUIDs(ids []pgtype.UUID) ([]byte, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id.Valid {
			out = append(out, util.UUIDToString(id))
		}
	}
	return json.Marshal(out)
}

func marshalContext(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}
