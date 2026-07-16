package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/admission"
	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Hook CRUD errors surfaced to the handler for status mapping. Validation
// problems (shape or unresolvable/forbidden target) flow through as
// *automation.ValidationError (→ 400).
var (
	ErrHookNotFound          = errors.New("hook not found")
	ErrHookSystemManaged     = errors.New("system-managed hooks cannot be modified through this API")
	ErrHookNoPrincipal       = errors.New("no accountable authorization principal for this hook")
	ErrHookForbidden         = errors.New("only the hook's principal or a workspace admin may modify it")
	ErrHookPrincipalDeparted = errors.New("the hook's authorization principal is no longer a member of this workspace")
)

// HookAuthor carries the resolved identity for a hook write: who is acting
// (creator, pure audit) and the accountable human whose authority the hook runs
// under (§8). Membership and role are NOT carried here — the service re-derives
// them inside the write transaction so a stale snapshot can never authorize a
// write (review round 3, point 3).
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

// CreateHook validates the spec (shape + workspace-scoped, principal-gated
// targets) and inserts the hook together with revision #1 in one transaction.
// The writer's membership and every target admission are (re)checked inside that
// transaction against the accountable principal, so an illegal configuration
// never enters the store (§13) and a stale role/membership snapshot cannot
// authorize the write. The two rows reference each other, so both ids are
// generated up front.
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
		// The creator's principal must be a current member of the workspace.
		creator, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID: author.PrincipalUserID, WorkspaceID: workspaceID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrHookNoPrincipal
			}
			return err
		}
		// A newly created hook runs under the creator's authority.
		if err := validateTargets(ctx, qtx, workspaceID, spec, creator); err != nil {
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
// hook's active revision (§5.1), all inside one transaction that first locks the
// hook row (so concurrent PATCHes serialize and MAX(revision)+1 cannot collide),
// then re-checks archived/origin, the editor's live membership/role, and the
// edit-authorization gate. Target admission is judged against the hook's LOCKED
// stored principal — never the editor — so an admin editing another member's
// hook can only change configuration, never grant the stored principal reach it
// lacks (review round 3, point 1). Scope is immutable.
func (s *HookService) UpdateHook(ctx context.Context, workspaceID, hookID pgtype.UUID, spec automation.HookSpec, author HookAuthor) (HookWithRevision, error) {
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
		existing, err := s.lockEditableHook(ctx, qtx, workspaceID, hookID, author)
		if err != nil {
			return err
		}
		// Editing re-arms the hook, so its STORED principal must still be a live
		// member — an admin cannot re-point a departed principal's hook at new
		// targets (review round 4, point 1). Admission then runs against that
		// principal, never the editor.
		principal, err := s.requireLivePrincipal(ctx, qtx, workspaceID, existing.AuthorizationPrincipalUserID)
		if err != nil {
			return err
		}
		if err := validateTargets(ctx, qtx, workspaceID, spec, principal); err != nil {
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
// does not cancel queued/running executions (§5.1). Load, authorization and the
// mutation all happen inside one transaction against the locked row.
func (s *HookService) SetEnabled(ctx context.Context, workspaceID, hookID pgtype.UUID, enabled bool, reason string, author HookAuthor) (HookWithRevision, error) {
	disabledReason := pgtype.Text{}
	if !enabled && reason != "" {
		disabledReason = pgtype.Text{String: reason, Valid: true}
	}
	var out HookWithRevision
	err := s.inTx(ctx, func(qtx *db.Queries) error {
		existing, err := s.lockEditableHook(ctx, qtx, workspaceID, hookID, author)
		if err != nil {
			return err
		}
		// Enable re-arms the hook, so (like update) its stored principal must still
		// be a live member. Disable is a degrading op: an admin may safely disable a
		// departed principal's hook (review round 4, point 1).
		if enabled {
			if _, err := s.requireLivePrincipal(ctx, qtx, workspaceID, existing.AuthorizationPrincipalUserID); err != nil {
				return err
			}
		}
		hook, err := qtx.SetHookEnabled(ctx, db.SetHookEnabledParams{
			ID:             hookID,
			WorkspaceID:    workspaceID,
			Enabled:        enabled,
			DisabledReason: disabledReason,
		})
		if err != nil {
			return err
		}
		rev, err := qtx.GetHookRevision(ctx, hook.ActiveRevisionID)
		if err != nil {
			return err
		}
		out = HookWithRevision{Hook: hook, Revision: rev}
		return nil
	})
	return out, err
}

// ArchiveHook soft-deletes a hook (§5.1); revisions/executions/effects are kept.
// Load, authorization and the mutation all happen inside one transaction.
func (s *HookService) ArchiveHook(ctx context.Context, workspaceID, hookID pgtype.UUID, author HookAuthor) error {
	return s.inTx(ctx, func(qtx *db.Queries) error {
		if _, err := s.lockEditableHook(ctx, qtx, workspaceID, hookID, author); err != nil {
			return err
		}
		if _, err := qtx.ArchiveHook(ctx, db.ArchiveHookParams{ID: hookID, WorkspaceID: workspaceID}); err != nil {
			return err
		}
		return nil
	})
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

// lockEditableHook loads and row-locks a non-archived, non-system hook and
// enforces the edit-authorization gate against the editor's LIVE membership and
// role read inside the same transaction. Returns the locked row for callers that
// need its stored principal.
func (s *HookService) lockEditableHook(ctx context.Context, qtx *db.Queries, workspaceID, hookID pgtype.UUID, author HookAuthor) (db.Hook, error) {
	existing, err := qtx.GetHookForUpdate(ctx, db.GetHookForUpdateParams{ID: hookID, WorkspaceID: workspaceID})
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
	editor, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID: author.PrincipalUserID, WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The editor is not (or no longer) a member of this workspace.
			return db.Hook{}, ErrHookForbidden
		}
		return db.Hook{}, err
	}
	if err := authorizeHookEdit(existing, editor); err != nil {
		return db.Hook{}, err
	}
	return existing, nil
}

// requireLivePrincipal loads the hook's stored principal as a workspace member,
// making live membership a hard precondition for re-arming a hook (update/enable).
// A departed principal fails closed; a real DB error is propagated.
func (s *HookService) requireLivePrincipal(ctx context.Context, qtx *db.Queries, workspaceID, principal pgtype.UUID) (db.Member, error) {
	member, err := qtx.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID: principal, WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Member{}, ErrHookPrincipalDeparted
		}
		return db.Member{}, err
	}
	return member, nil
}

// authorizeHookEdit implements the edit gate (review point 1): a workspace
// owner/admin may edit any hook's configuration; otherwise only the hook's
// original authorization principal may. The principal is NOT transferred on
// edit, so an arbitrary member can never rewrite a rule that keeps running under
// someone else's authority.
func authorizeHookEdit(hook db.Hook, editor db.Member) error {
	if admission.RoleAllowed(editor.Role, "owner", "admin") {
		return nil
	}
	if principalMatches(hook.AuthorizationPrincipalUserID, editor.UserID) {
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

// targetChecker fail-closed validates every id a spec references, against the
// workspace and the hook's stored principal, inside the write transaction
// (review round 3, points 1 & 2). §13 requires this at create/update time so an
// illegal configuration never enters the store and never reaches a worker.
type targetChecker struct {
	ctx               context.Context
	qtx               *db.Queries
	workspaceID       pgtype.UUID
	principalUserID   string
	principalMember   db.Member
	principalIsMember bool
}

// validateTargets is always called with the hook's stored principal already
// resolved to a LIVE workspace member (the create/update paths make that a hard
// gate first), so every target admission runs against a known-member principal.
func validateTargets(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID, spec automation.HookSpec, principal db.Member) error {
	tc := &targetChecker{
		ctx:               ctx,
		qtx:               qtx,
		workspaceID:       workspaceID,
		principalUserID:   util.UUIDToString(principal.UserID),
		principalMember:   principal,
		principalIsMember: true,
	}
	return tc.validate(spec)
}

func (tc *targetChecker) validate(spec automation.HookSpec) error {
	if spec.Scope != nil && spec.Scope.Type == automation.ScopeIssue {
		if err := tc.requireIssue(spec.Scope.ID, "scope.id"); err != nil {
			return err
		}
	}
	for i, cond := range spec.If {
		if err := tc.validateConditionTargets(i, cond); err != nil {
			return err
		}
	}
	for i, action := range spec.Do {
		if err := tc.validateActionTargets(i, action); err != nil {
			return err
		}
	}
	return nil
}

func (tc *targetChecker) validateConditionTargets(i int, c automation.ConditionSpec) error {
	if c.IssuesStatus != nil {
		for _, id := range c.IssuesStatus.IDs {
			if err := tc.requireIssue(id, fmt.Sprintf("if[%d].issues_status.ids", i)); err != nil {
				return err
			}
		}
	}
	if c.IssueField != nil {
		if err := tc.requireIssue(c.IssueField.ID, fmt.Sprintf("if[%d].issue_field.id", i)); err != nil {
			return err
		}
		// The operand ids must also resolve to workspace resources.
		operands := collectFieldOperands(*c.IssueField)
		where := fmt.Sprintf("if[%d].issue_field", i)
		switch c.IssueField.Field {
		case automation.IssueFieldParentIssueID:
			for _, v := range operands {
				if err := tc.requireIssue(v, where); err != nil {
					return err
				}
			}
		case automation.IssueFieldAssigneeID:
			for _, v := range operands {
				if err := tc.requireAssignee(v, where); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (tc *targetChecker) validateActionTargets(i int, a automation.ActionSpec) error {
	where := fmt.Sprintf("do[%d].%s", i, a.Type)
	switch a.Type {
	case automation.ActionSetIssueStatus, automation.ActionAddComment:
		return tc.requireIssue(a.IssueID, where+".issue_id")
	case automation.ActionTriggerAgent:
		if err := tc.requireIssue(a.IssueID, where+".issue_id"); err != nil {
			return err
		}
		return tc.requireInvokableAgent(a.AgentID, where+".agent_id")
	case automation.ActionSendInbox:
		return tc.requireMember(a.MemberID, where+".member_id")
	case automation.ActionRunAutopilot:
		return tc.requireWritableAutopilot(a.AutopilotID, where+".autopilot_id")
	}
	return nil
}

func collectFieldOperands(c automation.IssueFieldCond) []string {
	out := make([]string, 0, len(c.In)+1)
	if c.Eq != "" {
		out = append(out, c.Eq)
	}
	out = append(out, c.In...)
	return out
}

func (tc *targetChecker) requireIssue(id, field string) error {
	uid, err := util.ParseUUID(id)
	if err != nil {
		return automation.NewValidationError("%s must be a uuid", field)
	}
	if _, err := tc.qtx.GetIssueInWorkspace(tc.ctx, db.GetIssueInWorkspaceParams{ID: uid, WorkspaceID: tc.workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.NewValidationError("%s references issue %s which does not exist in this workspace", field, id)
		}
		return err
	}
	return nil
}

func (tc *targetChecker) requireMember(id, field string) error {
	uid, err := util.ParseUUID(id)
	if err != nil {
		return automation.NewValidationError("%s must be a uuid", field)
	}
	if _, err := tc.qtx.GetMemberInWorkspace(tc.ctx, db.GetMemberInWorkspaceParams{ID: uid, WorkspaceID: tc.workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.NewValidationError("%s references member %s which is not in this workspace", field, id)
		}
		return err
	}
	return nil
}

// requireAssignee validates an issue-assignee operand. Issue assignees are
// polymorphic (member | agent | squad, §Domain): a MEMBER assignee id is a USER
// id (looked up via GetMemberByUserAndWorkspace, NOT a member-row id), an agent
// id is a workspace agent, and a squad id is a non-archived workspace squad
// (review round 4, point 3).
func (tc *targetChecker) requireAssignee(id, field string) error {
	uid, err := util.ParseUUID(id)
	if err != nil {
		return automation.NewValidationError("%s must be a uuid", field)
	}
	if _, err := tc.qtx.GetMemberByUserAndWorkspace(tc.ctx, db.GetMemberByUserAndWorkspaceParams{UserID: uid, WorkspaceID: tc.workspaceID}); err == nil {
		return nil
	}
	if _, err := tc.qtx.GetAgentInWorkspace(tc.ctx, db.GetAgentInWorkspaceParams{ID: uid, WorkspaceID: tc.workspaceID}); err == nil {
		return nil
	}
	if squad, err := tc.qtx.GetSquadInWorkspace(tc.ctx, db.GetSquadInWorkspaceParams{ID: uid, WorkspaceID: tc.workspaceID}); err == nil && !squad.ArchivedAt.Valid {
		return nil
	}
	return automation.NewValidationError("%s references assignee %s which is not a member, agent, or squad in this workspace", field, id)
}

func (tc *targetChecker) requireWritableAutopilot(id, field string) error {
	uid, err := util.ParseUUID(id)
	if err != nil {
		return automation.NewValidationError("%s must be a uuid", field)
	}
	ap, err := tc.qtx.GetAutopilotInWorkspace(tc.ctx, db.GetAutopilotInWorkspaceParams{ID: uid, WorkspaceID: tc.workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.NewValidationError("%s references autopilot %s which does not exist in this workspace", field, id)
		}
		return err
	}
	// Write permission is judged for the hook's stored principal: role/authorship
	// or an explicit collaborator grant — the same rule the interactive autopilot
	// write path enforces (review round 3, point 2).
	if !tc.principalIsMember {
		return automation.NewValidationError("%s references autopilot %s which the hook's principal may not write", field, id)
	}
	if !admission.AutopilotWriteByOwnership(ap, tc.principalMember) {
		granted, err := tc.qtx.IsAutopilotCollaborator(tc.ctx, db.IsAutopilotCollaboratorParams{AutopilotID: ap.ID, UserID: tc.principalMember.UserID})
		if err != nil {
			return err
		}
		if !granted {
			return automation.NewValidationError("%s references autopilot %s which the hook's principal may not write", field, id)
		}
	}
	// Write permission is not enough: running the autopilot invokes its assignee
	// (an agent, or a squad's leader agent), so the principal must also be able to
	// invoke that agent — otherwise execution would be admission-skipped (review
	// round 4, point 2). PR4 re-validates dynamic state at execution time.
	return tc.requireInvocableAutopilotAssignee(ap, field)
}

// requireInvocableAutopilotAssignee resolves the autopilot's assignee to its
// executing agent (the agent itself, or a non-archived squad's leader) and
// requires the stored principal be able to invoke it. A dangling/archived
// assignee fails closed.
func (tc *targetChecker) requireInvocableAutopilotAssignee(ap db.Autopilot, field string) error {
	switch ap.AssigneeType {
	case "agent":
		agent, err := tc.qtx.GetAgentInWorkspace(tc.ctx, db.GetAgentInWorkspaceParams{ID: ap.AssigneeID, WorkspaceID: tc.workspaceID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return automation.NewValidationError("%s references autopilot %s whose assignee agent is missing or not in this workspace", field, util.UUIDToString(ap.ID))
			}
			return err
		}
		return tc.checkAgentInvocable(agent, field)
	case "squad":
		squad, err := tc.qtx.GetSquadInWorkspace(tc.ctx, db.GetSquadInWorkspaceParams{ID: ap.AssigneeID, WorkspaceID: tc.workspaceID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return automation.NewValidationError("%s references autopilot %s whose squad is missing or not in this workspace", field, util.UUIDToString(ap.ID))
			}
			return err
		}
		if squad.ArchivedAt.Valid {
			return automation.NewValidationError("%s references autopilot %s whose squad is archived", field, util.UUIDToString(ap.ID))
		}
		leader, err := tc.qtx.GetAgentInWorkspace(tc.ctx, db.GetAgentInWorkspaceParams{ID: squad.LeaderID, WorkspaceID: tc.workspaceID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return automation.NewValidationError("%s references autopilot %s whose squad leader is missing", field, util.UUIDToString(ap.ID))
			}
			return err
		}
		return tc.checkAgentInvocable(leader, field)
	default:
		return automation.NewValidationError("%s references autopilot %s with an unsupported assignee", field, util.UUIDToString(ap.ID))
	}
}

// requireInvokableAgent confirms the agent exists in the workspace, is not
// archived, has a runtime, and is invokable by the hook's STORED principal — the
// same admission the interactive trigger path enforces, applied fail-closed at
// save against the principal, never the editor (review round 3, point 1).
func (tc *targetChecker) requireInvokableAgent(id, field string) error {
	uid, err := util.ParseUUID(id)
	if err != nil {
		return automation.NewValidationError("%s must be a uuid", field)
	}
	agent, err := tc.qtx.GetAgentInWorkspace(tc.ctx, db.GetAgentInWorkspaceParams{ID: uid, WorkspaceID: tc.workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.NewValidationError("%s references agent %s which does not exist in this workspace", field, id)
		}
		return err
	}
	return tc.checkAgentInvocable(agent, field)
}

// checkAgentInvocable fail-closed asserts an already-loaded workspace agent is
// live (not archived, has a runtime) and invocable by the hook's stored
// principal. Shared by trigger_agent and run_autopilot assignee validation.
func (tc *targetChecker) checkAgentInvocable(agent db.Agent, field string) error {
	id := util.UUIDToString(agent.ID)
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		return automation.NewValidationError("%s references agent %s which is archived or has no runtime", field, id)
	}
	targets, err := tc.qtx.ListAgentInvocationTargets(tc.ctx, agent.ID)
	if err != nil {
		return err
	}
	if !admission.AgentInvocableByMember(agent, targets, tc.principalUserID, tc.principalIsMember) {
		return automation.NewValidationError("%s references agent %s which the hook's principal may not invoke", field, id)
	}
	return nil
}
