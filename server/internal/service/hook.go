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
// problems (shape or unresolvable target) flow through as
// *automation.ValidationError (→ 400).
var (
	ErrHookNotFound      = errors.New("hook not found")
	ErrHookSystemManaged = errors.New("system-managed hooks cannot be modified through this API")
	ErrHookNoPrincipal   = errors.New("no accountable authorization principal for this hook")
	ErrHookForbidden     = errors.New("only the hook's principal or a workspace admin may modify it")
)

// HookAuthor carries the resolved identity for a hook write: who is acting
// (creator, pure audit), the accountable human whose authority the hook runs
// under (§8), and whether that human is a workspace owner/admin. An agent author
// must resolve to a real member principal.
type HookAuthor struct {
	ActorType        string // member | agent
	ActorID          pgtype.UUID
	PrincipalUserID  pgtype.UUID
	IsWorkspaceAdmin bool
}

// CanInvokeAgent is the admission predicate the handler supplies (wrapping
// Handler.canInvokeAgent) so the service can fail-closed on a trigger_agent
// target without importing request context. A nil predicate denies every
// trigger_agent target (fail-closed).
type CanInvokeAgent func(agent db.Agent) bool

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

// CreateHook validates the spec (shape + workspace-scoped targets), resolves
// scope + principal, and inserts the hook together with revision #1 in one
// transaction. The two rows reference each other, so both ids are generated up
// front.
func (s *HookService) CreateHook(ctx context.Context, workspaceID pgtype.UUID, spec automation.HookSpec, author HookAuthor, canInvoke CanInvokeAgent) (HookWithRevision, error) {
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
		if err := validateTargets(ctx, qtx, workspaceID, spec, canInvoke); err != nil {
			return err
		}
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
// hook's active revision (§5.1). It locks the hook row first so concurrent
// PATCHes serialize and MAX(revision)+1 can never collide (review point 4), and
// re-checks archived/origin/authorization inside the lock. Only the hook's
// principal or a workspace admin may edit (review point 1). Scope is immutable.
func (s *HookService) UpdateHook(ctx context.Context, workspaceID, hookID pgtype.UUID, spec automation.HookSpec, author HookAuthor, canInvoke CanInvokeAgent) (HookWithRevision, error) {
	if err := automation.Validate(spec); err != nil {
		return HookWithRevision{}, err
	}
	match, conditions, actions, err := marshalRevisionConfig(spec)
	if err != nil {
		return HookWithRevision{}, err
	}

	revisionID := util.NewUUID()
	var out HookWithRevision
	err = s.inTx(ctx, func(qtx *db.Queries) error {
		existing, err := qtx.GetHookForUpdate(ctx, db.GetHookForUpdateParams{ID: hookID, WorkspaceID: workspaceID})
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
		if err := authorizeHookEdit(existing, author); err != nil {
			return err
		}
		if err := validateTargets(ctx, qtx, workspaceID, spec, canInvoke); err != nil {
			return err
		}
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
// does not cancel queued/running executions (§5.1). Only the principal or a
// workspace admin may toggle it.
func (s *HookService) SetEnabled(ctx context.Context, workspaceID, hookID pgtype.UUID, enabled bool, reason string, author HookAuthor) (HookWithRevision, error) {
	if _, err := s.loadEditableHook(ctx, workspaceID, hookID, author); err != nil {
		return HookWithRevision{}, err
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
// Only the principal or a workspace admin may archive it.
func (s *HookService) ArchiveHook(ctx context.Context, workspaceID, hookID pgtype.UUID, author HookAuthor) error {
	if _, err := s.loadEditableHook(ctx, workspaceID, hookID, author); err != nil {
		return err
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

// loadEditableHook loads a non-archived, non-system hook and enforces the edit
// authorization gate, for the enable/disable/archive paths.
func (s *HookService) loadEditableHook(ctx context.Context, workspaceID, hookID pgtype.UUID, author HookAuthor) (db.Hook, error) {
	existing, err := s.Queries.GetHookInWorkspace(ctx, db.GetHookInWorkspaceParams{ID: hookID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Hook{}, ErrHookNotFound
		}
		return db.Hook{}, err
	}
	if existing.ArchivedAt.Valid {
		return db.Hook{}, ErrHookNotFound
	}
	if existing.Origin == "system" {
		return db.Hook{}, ErrHookSystemManaged
	}
	if err := authorizeHookEdit(existing, author); err != nil {
		return db.Hook{}, err
	}
	return existing, nil
}

// authorizeHookEdit implements the edit gate (review point 1): a workspace
// owner/admin may edit any hook; otherwise only the hook's original
// authorization principal may. The principal is NOT transferred on edit, so an
// arbitrary member can never rewrite a rule that keeps running under someone
// else's authority.
func authorizeHookEdit(hook db.Hook, author HookAuthor) error {
	if author.IsWorkspaceAdmin {
		return nil
	}
	if principalMatches(hook.AuthorizationPrincipalUserID, author.PrincipalUserID) {
		return nil
	}
	return ErrHookForbidden
}

func principalMatches(a, b pgtype.UUID) bool {
	return a.Valid && b.Valid && a.Bytes == b.Bytes
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

// validateTargets fail-closed checks that every id the spec references exists in
// the hook's workspace under the current principal (review point 2). §13 requires
// this at create/update time so an illegal configuration never enters the store
// and never reaches a worker. Uses the tx-bound queries so the checks share the
// write transaction.
func validateTargets(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, spec automation.HookSpec, canInvoke CanInvokeAgent) error {
	if spec.Scope != nil && spec.Scope.Type == automation.ScopeIssue {
		if err := requireIssue(ctx, qtx, workspaceID, spec.Scope.ID, "scope.id"); err != nil {
			return err
		}
	}
	for i, cond := range spec.If {
		if cond.IssuesStatus != nil {
			for _, id := range cond.IssuesStatus.IDs {
				if err := requireIssue(ctx, qtx, workspaceID, id, fmt.Sprintf("if[%d].issues_status.ids", i)); err != nil {
					return err
				}
			}
		}
		if cond.IssueField != nil {
			if err := requireIssue(ctx, qtx, workspaceID, cond.IssueField.ID, fmt.Sprintf("if[%d].issue_field.id", i)); err != nil {
				return err
			}
		}
	}
	for i, action := range spec.Do {
		if err := validateActionTargets(ctx, qtx, workspaceID, i, action, canInvoke); err != nil {
			return err
		}
	}
	return nil
}

func validateActionTargets(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, i int, a automation.ActionSpec, canInvoke CanInvokeAgent) error {
	where := fmt.Sprintf("do[%d].%s", i, a.Type)
	switch a.Type {
	case automation.ActionSetIssueStatus, automation.ActionAddComment:
		return requireIssue(ctx, qtx, workspaceID, a.IssueID, where+".issue_id")
	case automation.ActionTriggerAgent:
		if err := requireIssue(ctx, qtx, workspaceID, a.IssueID, where+".issue_id"); err != nil {
			return err
		}
		return requireInvokableAgent(ctx, qtx, workspaceID, a.AgentID, where+".agent_id", canInvoke)
	case automation.ActionSendInbox:
		return requireMember(ctx, qtx, workspaceID, a.MemberID, where+".member_id")
	case automation.ActionRunAutopilot:
		return requireAutopilot(ctx, qtx, workspaceID, a.AutopilotID, where+".autopilot_id")
	}
	return nil
}

func requireIssue(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, id, field string) error {
	uid, err := util.ParseUUID(id)
	if err != nil {
		return automation.NewValidationError("%s must be a uuid", field)
	}
	if _, err := qtx.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: uid, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.NewValidationError("%s references issue %s which does not exist in this workspace", field, id)
		}
		return err
	}
	return nil
}

func requireMember(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, id, field string) error {
	uid, err := util.ParseUUID(id)
	if err != nil {
		return automation.NewValidationError("%s must be a uuid", field)
	}
	if _, err := qtx.GetMemberInWorkspace(ctx, db.GetMemberInWorkspaceParams{ID: uid, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.NewValidationError("%s references member %s which is not in this workspace", field, id)
		}
		return err
	}
	return nil
}

func requireAutopilot(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, id, field string) error {
	uid, err := util.ParseUUID(id)
	if err != nil {
		return automation.NewValidationError("%s must be a uuid", field)
	}
	if _, err := qtx.GetAutopilotInWorkspace(ctx, db.GetAutopilotInWorkspaceParams{ID: uid, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.NewValidationError("%s references autopilot %s which does not exist in this workspace", field, id)
		}
		return err
	}
	return nil
}

// requireInvokableAgent confirms the agent exists in the workspace, is not
// archived, has a runtime, and is invokable by the current principal — the same
// admission the interactive trigger path enforces, applied fail-closed at save.
func requireInvokableAgent(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, id, field string, canInvoke CanInvokeAgent) error {
	uid, err := util.ParseUUID(id)
	if err != nil {
		return automation.NewValidationError("%s must be a uuid", field)
	}
	agent, err := qtx.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: uid, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.NewValidationError("%s references agent %s which does not exist in this workspace", field, id)
		}
		return err
	}
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		return automation.NewValidationError("%s references agent %s which is archived or has no runtime", field, id)
	}
	if canInvoke == nil || !canInvoke(agent) {
		return automation.NewValidationError("%s references agent %s which the hook's principal may not invoke", field, id)
	}
	return nil
}
