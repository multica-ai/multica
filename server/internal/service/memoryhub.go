// Package service: MemoryHub binding lifecycle (Plan v1.2 section 4 state
// machine + v1.3 A2.1). Owner: ALL-16.
//
// Every transition is CAS (`WHERE id=$id AND status=$from AND version=$version`);
// zero updated rows maps to 409 binding_transition_conflict, invalid
// transitions map to 409 binding_transition_invalid, and no transition ever
// changes an issue stage or issue metadata.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// BindingStatus mirrors the frozen six-state binding enum.
type BindingStatus string

const (
	BindingUnbound      BindingStatus = "unbound"
	BindingBinding      BindingStatus = "binding"
	BindingBound        BindingStatus = "bound"
	BindingSyncFailed   BindingStatus = "sync_failed"
	BindingCompensating BindingStatus = "compensating"
	BindingBlocked      BindingStatus = "blocked"
)

// Binding errors surfaced to the handler layer.
var (
	ErrBindingNotFound           = errors.New("memoryhub: binding not found")
	ErrBindingTransitionInvalid  = errors.New("memoryhub: binding_transition_invalid")
	ErrBindingTransitionConflict = errors.New("memoryhub: binding_transition_conflict")
	ErrBindingDuplicateScope     = errors.New("memoryhub: binding_duplicate_scope")
	ErrBindingIdemMismatch       = errors.New("memoryhub: binding_idempotency_mismatch")
)

// bindingTransitionTable is the frozen legal-transition set (v1.3 A2.1).
var bindingTransitionTable = map[BindingStatus]map[BindingStatus]bool{
	BindingUnbound: {BindingBinding: true},
	BindingBinding: {
		BindingBinding: true, BindingBound: true,
		BindingSyncFailed: true, BindingCompensating: true,
	},
	BindingBound: {
		BindingUnbound: true, BindingBinding: true, BindingBound: true,
		BindingSyncFailed: true, BindingCompensating: true,
	},
	BindingSyncFailed: {
		BindingBinding: true, BindingBound: true, BindingSyncFailed: true,
		BindingCompensating: true, BindingBlocked: true,
	},
	BindingCompensating: {
		BindingUnbound: true, BindingBinding: true, BindingBound: true,
		BindingCompensating: true, BindingBlocked: true,
	},
	BindingBlocked: {
		// Only manual unlock; blocked -> unbound is invalid.
		BindingBinding: true, BindingBlocked: true,
	},
}

// ValidBindingTransition reports whether from -> to is legal. Idempotent
// re-entry is legal for binding/bound/sync_failed/compensating/blocked.
func ValidBindingTransition(from, to BindingStatus) bool {
	if to == from {
		switch from {
		case BindingBinding, BindingBound, BindingSyncFailed, BindingCompensating, BindingBlocked:
			return true
		}
		return false
	}
	return bindingTransitionTable[from][to]
}

// bindingIdempotencyKey derives the stable idempotency key:
// sha256(workspace_id|scope_kind|scope_id|subject_type|subject_id).
func bindingIdempotencyKey(workspaceID, scopeKind string, scopeID pgtype.UUID, subjectType, subjectID string) string {
	scope := ""
	if scopeID.Valid {
		scope = uuidString(scopeID)
	}
	sum := sha256.Sum256([]byte(workspaceID + "|" + scopeKind + "|" + scope + "|" + subjectType + "|" + subjectID))
	return hex.EncodeToString(sum[:])
}

// RemoteClient is the remote-boundary interface used by the binding lifecycle.
// Tests substitute a fake; production wires the integrations/memoryhub client.
type RemoteClient interface {
	FindOrCreateTeam(ctx context.Context, kind, remoteID string) (string, error)
	FindOrCreateAgent(ctx context.Context, kind, remoteID string) (string, error)
	FindOrCreateTask(ctx context.Context, kind, remoteID string) (string, error)
	FindRemote(ctx context.Context, kind, remoteID string) (string, error)
	DeleteRemote(ctx context.Context, remoteID string) error
}

// MemoryHubService owns the binding lifecycle. It depends on the sqlc Queries
// and the MemoryHub client boundary.
type MemoryHubService struct {
	Queries *db.Queries
	Remote  RemoteClient
}

// NewMemoryHubService builds the service.
func NewMemoryHubService(q *db.Queries, remote RemoteClient) *MemoryHubService {
	return &MemoryHubService{Queries: q, Remote: remote}
}

// CreateBindingRequest is the service-layer create input.
type CreateBindingRequest struct {
	WorkspaceID string
	ScopeKind   string
	ScopeID     pgtype.UUID
	SubjectType string
	SubjectID   string
}

// CreateBinding inserts a binding in unbound state. Idempotent replay by
// idempotency key returns the existing row.
func (s *MemoryHubService) CreateBinding(ctx context.Context, req CreateBindingRequest) (*db.MemoryhubBinding, error) {
	key := bindingIdempotencyKey(req.WorkspaceID, req.ScopeKind, req.ScopeID, req.SubjectType, req.SubjectID)

	existing, err := s.Queries.GetMemoryHubBindingByIDempotencyKey(ctx, key)
	if err == nil {
		return &existing, nil // idempotent replay
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	binding, err := s.Queries.InsertMemoryHubBinding(ctx, db.InsertMemoryHubBindingParams{
		WorkspaceID:    uuidFromString(req.WorkspaceID),
		ScopeKind:      req.ScopeKind,
		ScopeID:        req.ScopeID,
		SubjectType:    req.SubjectType,
		SubjectID:      uuidFromString(req.SubjectID),
		IdempotencyKey: key,
	})
	if err != nil {
		return nil, classifyBindingInsertError(err)
	}
	return &binding, nil
}

// Transition moves a binding under CAS. Remote effects happen in the caller
// after a successful local transition.
func (s *MemoryHubService) Transition(ctx context.Context, id pgtype.UUID, version int32, from, to BindingStatus) (*db.MemoryhubBinding, error) {
	if !ValidBindingTransition(from, to) {
		return nil, ErrBindingTransitionInvalid
	}
	binding, err := s.Queries.UpdateMemoryHubBindingStateCAS(ctx, db.UpdateMemoryHubBindingStateCASParams{
		ID:       id,
		Status:   string(to),
		Status_2: string(from),
		Version:  version,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBindingTransitionConflict
		}
		return nil, err
	}
	return &binding, nil
}

// Bind marks a binding bound and records remote refs + evidence under CAS.
func (s *MemoryHubService) Bind(ctx context.Context, id pgtype.UUID, version int32, remote RemoteRefValue, evidenceRef pgtype.UUID) (*db.MemoryhubBinding, error) {
	binding, err := s.Queries.UpdateMemoryHubBindingRemoteStateCAS(ctx, db.UpdateMemoryHubBindingRemoteStateCASParams{
		ID:           id,
		Status:       string(BindingBound),
		Status_2:     string(BindingBinding),
		Version:      version,
		RemoteTeamID: pgtype.Text{String: remote.TeamID, Valid: remote.TeamID != ""},
		RemoteAgentID: pgtype.Text{String: remote.AgentID, Valid: remote.AgentID != ""},
		RemoteTaskID:  pgtype.Text{String: remote.TaskID, Valid: remote.TaskID != ""},
		RemoteName:    pgtype.Text{String: remote.Name, Valid: remote.Name != ""},
		EvidenceRef:   evidenceRef,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBindingTransitionConflict
		}
		return nil, err
	}
	return &binding, nil
}

// Unbind performs a local unbind only (remote untouched).
func (s *MemoryHubService) Unbind(ctx context.Context, id pgtype.UUID, version int32) (*db.MemoryhubBinding, error) {
	binding, err := s.Queries.UpdateMemoryHubBindingStateCAS(ctx, db.UpdateMemoryHubBindingStateCASParams{
		ID:       id,
		Status:   string(BindingUnbound),
		Status_2: string(BindingBound),
		Version:  version,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBindingTransitionConflict
		}
		return nil, err
	}
	return &binding, nil
}

// RemoteRefValue carries the resolved remote identity.
type RemoteRefValue struct {
	TeamID  string
	AgentID string
	TaskID  string
	Name    string
}

func classifyBindingInsertError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if containsSub(msg, "memoryhub_binding_project_subject_uidx") ||
		containsSub(msg, "memoryhub_binding_workspace_subject_uidx") {
		return ErrBindingDuplicateScope
	}
	if containsSub(msg, "memoryhub_binding_idempotency_uidx") {
		return ErrBindingIdemMismatch
	}
	return err
}

func containsSub(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func uuidFromString(s string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(s)
	return u
}

func uuidString(u pgtype.UUID) string {
	return u.String()
}
