package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Hook CRUD errors surfaced to the handler for status mapping. Validation
// problems flow through as automation.ValidationError (→ 400).
var (
	ErrHookNotFound      = errors.New("hook not found")
	ErrHookSystemManaged = errors.New("system-managed hooks cannot be modified through this API")
	ErrHookNoPrincipal   = errors.New("no accountable authorization principal for this hook")
)

// HookAuthor carries the resolved identity for a create/update: who is acting
// (creator, pure audit) and the accountable human whose authority the hook runs
// under (§8). An agent author must resolve to a real member principal.
type HookAuthor struct {
	ActorType       string // member | agent
	ActorID         pgtype.UUID
	PrincipalUserID pgtype.UUID
}

// HookWithRevision pairs a hook row with its active revision so the handler can
// render one complete view. The service returns db rows; the handler shapes JSON.
type HookWithRevision struct {
	Hook     db.Hook
	Revision db.HookRevision
}

// HookService is the store-only policy layer for Event Hooks (MUL-4332 PR2).
// It validates and persists hook specifications and their immutable revisions;
// it performs no matching or execution. Behaviour is gated at the handler by the
// automation_event_hooks feature flag, so creating hooks changes nothing at
// runtime until the executor slice ships and the flag is enabled.
type HookService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

func NewHookService(q *db.Queries, tx TxStarter) *HookService {
	return &HookService{Queries: q, TxStarter: tx}
}

// CreateHook validates the spec, resolves scope + principal, and inserts the
// hook together with revision #1 in one transaction. The two rows reference
// each other, so both ids are generated up front.
func (s *HookService) CreateHook(ctx context.Context, workspaceID pgtype.UUID, spec automation.HookSpec, author HookAuthor) (HookWithRevision, error) {
	if err := automation.Validate(spec); err != nil {
		return HookWithRevision{}, err
	}
	if !author.PrincipalUserID.Valid {
		return HookWithRevision{}, ErrHookNoPrincipal
	}
	scopeType, scopeID, err := resolveScope(spec.Scope)
	if err != nil {
		return HookWithRevision{}, err
	}
	match, conditions, actions, err := marshalRevisionConfig(spec)
	if err != nil {
		return HookWithRevision{}, err
	}

	hookID := util.NewUUID()
	revisionID := util.NewUUID()

	var out HookWithRevision
	err = s.inTx(ctx, func(qtx *db.Queries) error {
		hook, err := qtx.CreateHook(ctx, db.CreateHookParams{
			ID:                           hookID,
			WorkspaceID:                  workspaceID,
			Name:                         spec.Name,
			Enabled:                      true,
			ActiveRevisionID:             revisionID,
			ScopeType:                    scopeType,
			ScopeID:                      scopeID,
			Origin:                       "user",
			CreatorActorType:             author.ActorType,
			CreatorActorID:               author.ActorID,
			AuthorizationPrincipalUserID: author.PrincipalUserID,
		})
		if err != nil {
			return err
		}
		rev, err := qtx.CreateHookRevision(ctx, db.CreateHookRevisionParams{
			ID:            revisionID,
			HookID:        hookID,
			Revision:      1,
			EventType:     spec.When.Event,
			Match:         match,
			Conditions:    conditions,
			FireMode:      spec.Fire.Mode,
			Actions:       actions,
			CreatedByType: author.ActorType,
			CreatedByID:   author.ActorID,
		})
		if err != nil {
			return err
		}
		out = HookWithRevision{Hook: hook, Revision: rev}
		return nil
	})
	if err != nil {
		return HookWithRevision{}, err
	}
	return out, nil
}

// UpdateHook appends a new immutable revision from the spec and repoints the
// hook's active revision (§5.1). Existing executions stay pinned to their
// original revision. System-managed hooks are not editable here.
func (s *HookService) UpdateHook(ctx context.Context, workspaceID, hookID pgtype.UUID, spec automation.HookSpec, author HookAuthor) (HookWithRevision, error) {
	if err := automation.Validate(spec); err != nil {
		return HookWithRevision{}, err
	}
	// Scope is immutable after creation; UpdateHook only replaces the revision
	// config and the display name.
	match, conditions, actions, err := marshalRevisionConfig(spec)
	if err != nil {
		return HookWithRevision{}, err
	}

	existing, err := s.Queries.GetHookInWorkspace(ctx, db.GetHookInWorkspaceParams{ID: hookID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return HookWithRevision{}, ErrHookNotFound
		}
		return HookWithRevision{}, err
	}
	if existing.ArchivedAt.Valid {
		return HookWithRevision{}, ErrHookNotFound
	}
	if existing.Origin == "system" {
		return HookWithRevision{}, ErrHookSystemManaged
	}

	revisionID := util.NewUUID()
	var out HookWithRevision
	err = s.inTx(ctx, func(qtx *db.Queries) error {
		maxRev, err := qtx.GetMaxHookRevision(ctx, hookID)
		if err != nil {
			return err
		}
		rev, err := qtx.CreateHookRevision(ctx, db.CreateHookRevisionParams{
			ID:            revisionID,
			HookID:        hookID,
			Revision:      maxRev + 1,
			EventType:     spec.When.Event,
			Match:         match,
			Conditions:    conditions,
			FireMode:      spec.Fire.Mode,
			Actions:       actions,
			CreatedByType: author.ActorType,
			CreatedByID:   author.ActorID,
		})
		if err != nil {
			return err
		}
		hook, err := qtx.SetHookActiveRevision(ctx, db.SetHookActiveRevisionParams{
			ID:               hookID,
			WorkspaceID:      workspaceID,
			ActiveRevisionID: revisionID,
			Name:             spec.Name,
		})
		if err != nil {
			return err
		}
		out = HookWithRevision{Hook: hook, Revision: rev}
		return nil
	})
	if err != nil {
		return HookWithRevision{}, err
	}
	return out, nil
}

// GetHook loads a hook and its active revision.
func (s *HookService) GetHook(ctx context.Context, workspaceID, hookID pgtype.UUID) (HookWithRevision, error) {
	hook, err := s.Queries.GetHookInWorkspace(ctx, db.GetHookInWorkspaceParams{ID: hookID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return HookWithRevision{}, ErrHookNotFound
		}
		return HookWithRevision{}, err
	}
	if hook.ArchivedAt.Valid {
		return HookWithRevision{}, ErrHookNotFound
	}
	rev, err := s.Queries.GetHookRevision(ctx, hook.ActiveRevisionID)
	if err != nil {
		return HookWithRevision{}, err
	}
	return HookWithRevision{Hook: hook, Revision: rev}, nil
}

// ListHooks returns every non-archived hook in the workspace with its active
// revision. Hook counts per workspace are small (guardrails cap fan-out, not the
// number of rules), so the per-hook revision lookup is acceptable.
func (s *HookService) ListHooks(ctx context.Context, workspaceID pgtype.UUID) ([]HookWithRevision, error) {
	hooks, err := s.Queries.ListHooksByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]HookWithRevision, 0, len(hooks))
	for _, hook := range hooks {
		rev, err := s.Queries.GetHookRevision(ctx, hook.ActiveRevisionID)
		if err != nil {
			return nil, err
		}
		out = append(out, HookWithRevision{Hook: hook, Revision: rev})
	}
	return out, nil
}

// SetEnabled enables/disables a hook. Disable only blocks future matches; it
// does not cancel queued/running executions (§5.1).
func (s *HookService) SetEnabled(ctx context.Context, workspaceID, hookID pgtype.UUID, enabled bool, reason string) (HookWithRevision, error) {
	existing, err := s.Queries.GetHookInWorkspace(ctx, db.GetHookInWorkspaceParams{ID: hookID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return HookWithRevision{}, ErrHookNotFound
		}
		return HookWithRevision{}, err
	}
	if existing.ArchivedAt.Valid {
		return HookWithRevision{}, ErrHookNotFound
	}
	if existing.Origin == "system" {
		// Enabling/disabling a system hook is an admin-only lifecycle op reserved
		// for the PR5 system-hook management path, not this user CRUD API.
		return HookWithRevision{}, ErrHookSystemManaged
	}
	disabledReason := pgtype.Text{}
	if !enabled && reason != "" {
		disabledReason = pgtype.Text{String: reason, Valid: true}
	}
	hook, err := s.Queries.SetHookEnabled(ctx, db.SetHookEnabledParams{
		ID:             hookID,
		WorkspaceID:    workspaceID,
		Enabled:        enabled,
		DisabledReason: disabledReason,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return HookWithRevision{}, ErrHookNotFound
		}
		return HookWithRevision{}, err
	}
	rev, err := s.Queries.GetHookRevision(ctx, hook.ActiveRevisionID)
	if err != nil {
		return HookWithRevision{}, err
	}
	return HookWithRevision{Hook: hook, Revision: rev}, nil
}

// ArchiveHook soft-deletes a hook (§5.1); revisions/executions/effects are kept.
func (s *HookService) ArchiveHook(ctx context.Context, workspaceID, hookID pgtype.UUID) error {
	existing, err := s.Queries.GetHookInWorkspace(ctx, db.GetHookInWorkspaceParams{ID: hookID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrHookNotFound
		}
		return err
	}
	if existing.ArchivedAt.Valid {
		return ErrHookNotFound
	}
	if existing.Origin == "system" {
		return ErrHookSystemManaged
	}
	if _, err := s.Queries.ArchiveHook(ctx, db.ArchiveHookParams{ID: hookID, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrHookNotFound
		}
		return err
	}
	return nil
}

// ListExecutions returns the newest execution-trace rows for a hook (bounded).
func (s *HookService) ListExecutions(ctx context.Context, workspaceID, hookID pgtype.UUID, limit int32) ([]db.HookExecution, error) {
	// Confirm the hook belongs to the workspace before exposing its trace.
	if _, err := s.Queries.GetHookInWorkspace(ctx, db.GetHookInWorkspaceParams{ID: hookID, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrHookNotFound
		}
		return nil, err
	}
	return s.Queries.ListHookExecutionsByHook(ctx, db.ListHookExecutionsByHookParams{HookID: hookID, Limit: limit})
}

func (s *HookService) inTx(ctx context.Context, fn func(qtx *db.Queries) error) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// resolveScope maps the optional scope spec to (scope_type, scope_id). The spec
// has already passed automation.Validate, so an issue scope always has a valid id.
func resolveScope(scope *automation.ScopeSpec) (string, pgtype.UUID, error) {
	if scope == nil || scope.Type == automation.ScopeWorkspace {
		return automation.ScopeWorkspace, pgtype.UUID{}, nil
	}
	id, err := util.ParseUUID(scope.ID)
	if err != nil {
		return "", pgtype.UUID{}, err
	}
	return automation.ScopeIssue, id, nil
}

// marshalRevisionConfig produces the JSONB payloads stored on a revision.
// conditions and actions are always stored as (possibly empty) arrays; match as
// an object.
func marshalRevisionConfig(spec automation.HookSpec) (match, conditions, actions []byte, err error) {
	match = spec.When.Match
	if len(match) == 0 {
		match = []byte("{}")
	}
	if len(spec.If) == 0 {
		conditions = []byte("[]")
	} else if conditions, err = json.Marshal(spec.If); err != nil {
		return nil, nil, nil, err
	}
	if actions, err = json.Marshal(spec.Do); err != nil {
		return nil, nil, nil, err
	}
	return match, conditions, actions, nil
}
