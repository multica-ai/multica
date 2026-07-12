package toolpolicy

// store.go is the database-backed half of the tool-policy engine (FIR-2230
// phase 1, persistence). chain.go holds the pure resolution logic; Store loads
// the per-layer settings one context needs and folds them through Resolve.
//
// The split mirrors permissions.Resolver (Can hits the DB, Decide stays pure):
// the interesting decision logic lives in the pure Resolve and is exhaustively
// unit-tested without a database, while Store is the thin seam that assembles a
// chain Input from stored rows.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// Store reads and writes the explicit per-layer tool settings.
type Store struct {
	pool *pgxpool.Pool
	q    *cerebrodb.Queries
	// vaults, when set, lets the credentials table surface Agent Vault boxes as
	// grantable agentvault-vault:<name> rows (FIR-1739). Optional and nil-safe:
	// the claim-time gate, approvals, and registry call sites leave it unset.
	vaults agentvault.VaultLister
}

// NewStore constructs a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: cerebrodb.New(pool)}
}

// WithVaultLister wires an Agent Vault vault lister so the credentials tab also
// surfaces Agent Vault boxes as grantable rows (agentvault-vault:<name>), the
// boxes that live only in Agent Vault and never get a cerebro_credential registry
// row. It is optional and nil-safe: only the credentials table handler wires it;
// the ~8 other NewStore call sites (claim-time gate, approvals, registry fold-in)
// leave it unset and are unaffected. A nil lister degrades to "no Agent Vault
// rows", the same contract the vaults endpoint uses when admin creds are absent.
// Returns the receiver for fluent construction at the call site.
func (s *Store) WithVaultLister(v agentvault.VaultLister) *Store {
	s.vaults = v
	return s
}

// NewStoreFromQueries constructs a Store for the resolution path only — Resolve,
// ListForSubject, Set, Clear, and group expansion all need just the generated
// queries. The Table read model additionally needs a *pgxpool.Pool for its
// custom join query, so callers that render the admin table must use NewStore;
// the runtime enforcement gate, which only resolves, uses this lighter
// constructor and never touches Table.
func NewStoreFromQueries(q *cerebrodb.Queries) *Store {
	return &Store{q: q}
}

// ErrUnknownLayer / ErrUnknownSetting guard the authoring API against values the
// database CHECK constraints would reject anyway, so a bad call fails with a
// clear message instead of a raw constraint-violation error.
var (
	ErrUnknownLayer   = errors.New("toolpolicy: unknown layer")
	ErrUnknownSetting = errors.New("toolpolicy: unknown setting")
)

// validLayer reports whether l is one of the chain layers (the five plus the
// System mandate peer of User, FIR-1609).
func validLayer(l Layer) bool {
	switch l {
	case LayerWorkspace, LayerRuntime, LayerAgent, LayerGroup, LayerUser, LayerOnBehalfOf, LayerSystem:
		return true
	default:
		return false
	}
}

// validSetting reports whether s is one of the five stored settings. Whether
// SettingDisable may be stored at the layer the caller is targeting is a
// separate check — see Set, which rejects it outside LayerWorkspace.
func validSetting(s Setting) bool {
	switch s {
	case SettingInherit, SettingAllow, SettingAsk, SettingDeny, SettingDisable:
		return true
	default:
		return false
	}
}

// Query identifies the (tool, resource_pattern, runtime, agent, user, groups)
// a caller wants an effective verdict for. Any of RuntimeID/AgentID/UserID may
// be the zero value (Valid=false) when that layer is not part of the request;
// that layer is then absent and treated as Inherit. GroupIDs may be empty.
// ResourcePattern is the per-resource scoping added in FIR-2505 slice 1: an
// empty string selects the capability-wide rows (the legacy semantics) and a
// non-empty value selects rows that target that exact pattern verbatim.
type Query struct {
	WorkspaceID     pgtype.UUID
	ToolKey         string
	ResourcePattern string
	RuntimeID       pgtype.UUID
	AgentID         pgtype.UUID
	UserID          pgtype.UUID
	GroupIDs        []pgtype.UUID
	// SystemID is the subject of the System layer — the mandate-actor ceiling for
	// a human-less run (FIR-1609). The subject identity of a System actor is the
	// autopilot that drives it (the autopilot is the human-less actor, born from
	// its owner). Zero (Valid=false) when the run has a human behind it, so the
	// System layer is then absent and treated as Inherit. The owner's User ceiling
	// loads regardless; System only ever tightens on top of it.
	SystemID pgtype.UUID
	// OnBehalfOfID is the subject of the on_behalf_of layer — the delegated member
	// (the task initiator) the work is performed for, as a real tighten-only actor
	// level distinct from UserID (the agent owner) (FIR-2441). Zero (Valid=false)
	// when the run has no delegation, so the layer is then absent and treated as
	// Inherit. It can restrict a delegated member's access across every agent they
	// drive but never widen it. Threading it here keeps Resolve (which powers the
	// claim brief) at parity with the TableQuery path that already honours it.
	OnBehalfOfID pgtype.UUID
	// Base is the workspace/system default applied when even the runtime layer
	// inherits. Empty defaults to Allow (see Resolve).
	Base Setting
	// IsSystem marks a run with no human behind it (autopilot / system-triggered).
	// Such a run has no one to answer an approval prompt, so any Ask it resolves to
	// becomes Deny — fail-safe (FIR-1609). The caller sets this from the run's
	// origin (an empty trigger user means no human). The owner's User ceiling is
	// still loaded and still caps the result; IsSystem only adds the Ask→Deny
	// fail-safe on top, so a system run can never be looser than a human one.
	IsSystem bool
	// RequestContext carries the request attributes (host, action) the WHEN layer
	// (a row's Conditions) is matched against during resolution (FIR-1609). The
	// zero value matches any condition that constrains nothing; a row that
	// constrains host/action against an empty context simply does not apply and
	// is dropped. Callers that do not yet thread request attributes leave this
	// zero — unconditioned rows (every row written before conditions existed)
	// resolve identically regardless, so wiring this in is behaviour-preserving.
	RequestContext RequestContext
	// Eval evaluates a CEL expression carried on a Condition. nil is valid: a row
	// with a non-empty Expr and no evaluator is undecidable and fails closed by
	// effect (see ConditionedSetting). Structured conditions (host/action) need
	// no evaluator.
	Eval ExprEvaluator
}

// Resolve loads the explicit per-layer settings for the query and folds them
// into one Effective verdict. A query with no stored rows resolves to Base
// (Allow by default), so an unconfigured workspace keeps working unchanged.
func (s *Store) Resolve(ctx context.Context, in Query) (Effective, error) {
	input, err := s.loadInput(ctx, in)
	if err != nil {
		return Effective{}, err
	}
	return Resolve(input), nil
}

// FlagMemberOverride is the workspace feature-flag key that switches the GENERAL
// tool-policy gate from the pure tighten-only Resolve to ResolveMemberOverride
// (FIR-2175). Default OFF: an unset flag keeps the tighten-only chain, so the
// gate is behaviour-identical until an admin opts the workspace in. The handlers
// read it next to their policyCELEvaluator flag read and pass the result into
// ResolveGeneral.
const FlagMemberOverride = "cerebro_member_override"

// ResolveGeneral folds the chain for the GENERAL tool-policy gate — the visible
// per-tool Allow / Ask / Deny permissions an operator authors in the policy
// table. When memberOverride is true (the workspace cerebro_member_override flag
// is on) it resolves through ResolveMemberOverride, the two-stage model where a
// member's own setting overrides an inherited group/workspace default by
// specificity and can therefore LOOSEN a group Deny. When false it is identical
// to Resolve (pure most-restrictive-wins), so the flag's default is a no-op.
//
// SECURITY — read before adding a caller. Only the general tool-policy gate may
// pass memberOverride=true. The deny-by-default FLOORS — credentials, the OS
// sandbox, repo checkout, and the repo-approval cap — MUST keep calling Resolve
// directly and MUST NEVER reach this method, because ResolveMemberOverride can
// loosen and a floor that can be loosened is not a floor: a member could grant
// themselves access to a secret their group denied. Resolve stays the single
// load-bearing resolver for every floor; this method is for the visible gate
// only. See ResolveMemberOverride for the model and chain.go for the contrast.
func (s *Store) ResolveGeneral(ctx context.Context, in Query, memberOverride bool) (Effective, error) {
	input, err := s.loadInput(ctx, in)
	if err != nil {
		return Effective{}, err
	}
	mode := ModeHardFloor
	if memberOverride {
		mode = ModeOpenable
	}
	return ResolveWithMode(mode, input), nil
}

// MemberOverrideEnabled reports whether the default-on cerebro_member_override
// flag is on for the workspace — read from the workspace-level row (the all-zero
// sentinel user_id), exactly like the gateway (runtime.memberOverrideEnabled) and
// local-runtime (daemonMemberOverrideEnabled) gates, so a display surface that
// consults it can never disagree with enforcement. A lookup miss or DB error
// resolves to OFF (the flag's default): a path that can LOOSEN access must never
// switch itself on by accident.
func (s *Store) MemberOverrideEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if s == nil || s.q == nil {
		return false
	}
	enabled, err := s.q.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     FlagMemberOverride,
	})
	if err != nil || !enabled {
		return false
	}
	return true
}

// ResolveOptIn loads the explicit settings for the query's (workspace, user,
// groups, tool) context and decides an OFF-by-default capability gate: false
// unless an explicit Allow has been granted at the user or group layer. Unlike
// Resolve (tighten-only, default Allow), this cannot be expressed by the chain
// — see ResolveOptIn for why a Deny base can never be lifted by a grant. in.Base
// is ignored; the gate is opt-in by definition.
func (s *Store) ResolveOptIn(ctx context.Context, in Query) (bool, error) {
	input, err := s.loadInput(ctx, in)
	if err != nil {
		return false, err
	}
	return ResolveOptIn(input), nil
}

// loadInput fetches the rows for the query and assembles a chain Input. Several
// group rows are collapsed with CombineGroups (most permissive group wins)
// before entering the chain at LayerGroup.
func (s *Store) loadInput(ctx context.Context, in Query) (Input, error) {
	groupIDs, err := s.resolveGroupIDs(ctx, in.WorkspaceID, in.UserID, in.GroupIDs)
	if err != nil {
		return Input{}, err
	}
	rows, err := s.q.ListCerebroToolPolicyForContext(ctx, cerebrodb.ListCerebroToolPolicyForContextParams{
		WorkspaceID:     in.WorkspaceID,
		ToolKey:         in.ToolKey,
		ResourcePattern: in.ResourcePattern,
		RuntimeID:       in.RuntimeID,
		AgentID:         in.AgentID,
		UserID:          in.UserID,
		GroupIds:        groupIDs,
		SystemID:        in.SystemID,
		OnBehalfOfID:    in.OnBehalfOfID,
	})
	if err != nil {
		return Input{}, fmt.Errorf("toolpolicy: load context settings: %w", err)
	}

	input := Input{Settings: map[Layer]Setting{}, Base: in.Base, IsSystem: in.IsSystem}
	var groupSettings []Setting
	for _, r := range rows {
		layer := Layer(r.Layer)
		setting := Setting(r.Setting)
		cond, err := decodeCondition(r.Conditions)
		if err != nil {
			return Input{}, fmt.Errorf("toolpolicy: decode conditions for layer %q: %w", layer, err)
		}
		setting, applies := ConditionedSetting(setting, cond, in.RequestContext, in.Eval)
		if !applies {
			// The WHEN layer's terms are not met (or the row fails closed): the
			// row drops out, so this layer inherits as if the row were absent.
			continue
		}
		if layer == LayerGroup {
			groupSettings = append(groupSettings, setting)
			continue
		}
		input.Settings[layer] = setting
	}
	if len(groupSettings) > 0 {
		input.Settings[LayerGroup] = CombineGroups(groupSettings...)
	}
	return input, nil
}

// resolveGroupIDs returns the group ids that enter the chain at LayerGroup.
//
// When the caller passes explicit group ids they are used verbatim. When none
// are given but a user is in scope, the user's actual workspace group
// memberships are loaded — so the Group layer always reflects the real groups
// of the user whose ceiling we are resolving, without the caller (the admin
// table or the runtime enforcement gate) having to enumerate them and without
// trusting a client-supplied group list. A user not in scope yields no groups
// (the layer inherits).
func (s *Store) resolveGroupIDs(ctx context.Context, workspaceID, userID pgtype.UUID, explicit []pgtype.UUID) ([]pgtype.UUID, error) {
	if len(explicit) > 0 {
		return explicit, nil
	}
	if !userID.Valid {
		return []pgtype.UUID{}, nil
	}
	groups, err := s.q.ListCerebroGroupsForUser(ctx, cerebrodb.ListCerebroGroupsForUserParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load user groups: %w", err)
	}
	ids := make([]pgtype.UUID, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	return ids, nil
}

// SetParams is the argument bundle for Set. ResourcePattern is the per-resource
// scope (e.g. one repo URL); leave empty for the capability-wide row that
// existing pre-FIR-2505 callers wrote.
type SetParams struct {
	WorkspaceID     pgtype.UUID
	ToolKey         string
	Layer           Layer
	SubjectID       pgtype.UUID
	ResourcePattern string
	Setting         Setting
	// Conditions is the optional WHEN layer on this rule (FIR-1609). nil — or a
	// zero-value Condition — stores NULL, meaning "no condition, always applies".
	Conditions *Condition
	UpdatedBy  pgtype.UUID // optional; the zero value leaves the column NULL
}

// Set upserts one layer's explicit choice for one (tool, resource_pattern).
// Storing SettingInherit is allowed (it behaves like an absent row but lets the
// UI render a deliberate "follow the layer below"); use Clear to drop the row
// entirely.
func (s *Store) Set(ctx context.Context, p SetParams) (cerebrodb.UpsertCerebroToolPolicyRow, error) {
	if !validLayer(p.Layer) {
		return cerebrodb.UpsertCerebroToolPolicyRow{}, fmt.Errorf("%w: %q", ErrUnknownLayer, p.Layer)
	}
	if !validSetting(p.Setting) {
		return cerebrodb.UpsertCerebroToolPolicyRow{}, fmt.Errorf("%w: %q", ErrUnknownSetting, p.Setting)
	}
	// SettingDisable is a workspace-only state (product decision 2026-07-06,
	// FIR-2351 follow-up): it makes a workspace Deny an unopenable floor for one
	// permission. Authoring it at any other layer would be meaningless (Disable
	// is normalized away by resolveOpenable/resolveHardFloor unless it sits at
	// LayerWorkspace) and would silently behave like an ordinary Deny instead —
	// reject it outright so the write path fails loudly rather than the row
	// quietly doing nothing.
	if p.Setting == SettingDisable && p.Layer != LayerWorkspace {
		return cerebrodb.UpsertCerebroToolPolicyRow{}, fmt.Errorf("%w: disable is only valid at the workspace layer, got %q", ErrUnknownSetting, p.Layer)
	}
	conditions, err := encodeCondition(p.Conditions)
	if err != nil {
		return cerebrodb.UpsertCerebroToolPolicyRow{}, fmt.Errorf("toolpolicy: set: %w", err)
	}
	// Read the prior explicit setting first so the change-log row can record the
	// old -> new transition (FIR-3091 punkt 8 fase 2). "" means the layer held no
	// explicit row (Inherit). A read failure degrades to "" rather than blocking
	// the write — the transition detail is lost, the change itself is still logged.
	oldSetting, err := s.q.GetCerebroToolPolicySetting(ctx, cerebrodb.GetCerebroToolPolicySettingParams{
		WorkspaceID:     p.WorkspaceID,
		ToolKey:         p.ToolKey,
		Layer:           string(p.Layer),
		SubjectID:       p.SubjectID,
		ResourcePattern: p.ResourcePattern,
	})
	if err != nil {
		oldSetting = ""
	}
	row, err := s.q.UpsertCerebroToolPolicy(ctx, cerebrodb.UpsertCerebroToolPolicyParams{
		WorkspaceID:     p.WorkspaceID,
		ToolKey:         p.ToolKey,
		Layer:           string(p.Layer),
		SubjectID:       p.SubjectID,
		ResourcePattern: p.ResourcePattern,
		Setting:         string(p.Setting),
		Conditions:      conditions,
		UpdatedBy:       p.UpdatedBy,
	})
	if err != nil {
		return cerebrodb.UpsertCerebroToolPolicyRow{}, fmt.Errorf("toolpolicy: set: %w", err)
	}
	s.recordAudit(ctx, p.WorkspaceID, p.ToolKey, p.Layer, p.SubjectID, p.ResourcePattern, "set", oldSetting, string(p.Setting), p.UpdatedBy)
	return row, nil
}

// recordAudit appends one change-log row after a completed Set/Clear (FIR-3091
// punkt 8 fase 2). The error is swallowed deliberately — the policy write has
// already happened, and failing the request over a missing history row would be
// worse than a gap in the log (the same trade-off cerebro_credential_audit
// makes for its allow-side rows). actorID identifies the member who authored
// the change; the zero value records a system write (e.g. the enablement
// migration pinning legacy grants).
func (s *Store) recordAudit(ctx context.Context, workspaceID pgtype.UUID, toolKey string, layer Layer, subjectID pgtype.UUID, resourcePattern, action, oldSetting, newSetting string, actorID pgtype.UUID) {
	actorType := "member"
	if !actorID.Valid {
		actorType = "system"
	}
	_ = s.q.RecordCerebroToolPolicyAudit(ctx, cerebrodb.RecordCerebroToolPolicyAuditParams{
		WorkspaceID:     workspaceID,
		ToolKey:         toolKey,
		Layer:           string(layer),
		SubjectID:       subjectID,
		ResourcePattern: resourcePattern,
		Action:          action,
		OldSetting:      oldSetting,
		NewSetting:      newSetting,
		ActorType:       actorType,
		ActorID:         actorID,
	})
}

// Clear removes one layer's explicit choice for one (tool, resource_pattern)
// so that layer reverts to Inherit for that pattern. Clearing an absent row is
// a no-op and leaves no change-log row. clearedBy identifies the member who
// authored the change for the change log (FIR-3091 punkt 8 fase 2); the zero
// value records a system write.
func (s *Store) Clear(ctx context.Context, workspaceID pgtype.UUID, toolKey string, layer Layer, subjectID pgtype.UUID, resourcePattern string, clearedBy pgtype.UUID) error {
	if !validLayer(layer) {
		return fmt.Errorf("%w: %q", ErrUnknownLayer, layer)
	}
	// Read the row being cleared first so the change-log entry records what was
	// dropped — and so a no-op clear (no row) logs nothing.
	oldSetting, getErr := s.q.GetCerebroToolPolicySetting(ctx, cerebrodb.GetCerebroToolPolicySettingParams{
		WorkspaceID:     workspaceID,
		ToolKey:         toolKey,
		Layer:           string(layer),
		SubjectID:       subjectID,
		ResourcePattern: resourcePattern,
	})
	if err := s.q.DeleteCerebroToolPolicy(ctx, cerebrodb.DeleteCerebroToolPolicyParams{
		WorkspaceID:     workspaceID,
		ToolKey:         toolKey,
		Layer:           string(layer),
		SubjectID:       subjectID,
		ResourcePattern: resourcePattern,
	}); err != nil {
		return fmt.Errorf("toolpolicy: clear: %w", err)
	}
	if getErr == nil {
		s.recordAudit(ctx, workspaceID, toolKey, layer, subjectID, resourcePattern, "clear", oldSetting, "", clearedBy)
	}
	return nil
}

// SubjectSetting is one (tool, resource_pattern, setting) triple a subject
// holds at one layer, used to render the per-agent / per-runtime column of the
// admin table. ResourcePattern is "" for capability-wide rows.
type SubjectSetting struct {
	ToolKey         string
	ResourcePattern string
	Setting         Setting
	Conditions      *Condition // optional WHEN layer; nil when the row has none.
	UpdatedBy       pgtype.UUID
	UpdatedAt       pgtype.Timestamptz
}

// ListForSubject returns every explicit setting one subject holds at one layer,
// across all tools and resource patterns.
func (s *Store) ListForSubject(ctx context.Context, workspaceID pgtype.UUID, layer Layer, subjectID pgtype.UUID) ([]SubjectSetting, error) {
	if !validLayer(layer) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownLayer, layer)
	}
	rows, err := s.q.ListCerebroToolPolicyForSubject(ctx, cerebrodb.ListCerebroToolPolicyForSubjectParams{
		WorkspaceID: workspaceID,
		Layer:       string(layer),
		SubjectID:   subjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: list for subject: %w", err)
	}
	out := make([]SubjectSetting, 0, len(rows))
	for _, r := range rows {
		cond, err := decodeCondition(r.Conditions)
		if err != nil {
			return nil, fmt.Errorf("toolpolicy: list for subject: decode conditions for %q: %w", r.ToolKey, err)
		}
		out = append(out, SubjectSetting{
			ToolKey:         r.ToolKey,
			ResourcePattern: r.ResourcePattern,
			Setting:         Setting(r.Setting),
			Conditions:      cond,
			UpdatedBy:       r.UpdatedBy,
			UpdatedAt:       r.UpdatedAt,
		})
	}
	return out, nil
}

// Holder is one explicit policy row for a single tool: which layer set it, on
// which subject, for which resource pattern, and to what setting. It is the
// reverse of SubjectSetting (one subject, all tools) — one tool, all subjects
// and layers — and backs the per-permission detail page's "who has this
// permission and which layer grants it" tab (FIR-3091 punkt 8).
type Holder struct {
	Layer           string
	SubjectID       pgtype.UUID
	ResourcePattern string
	Setting         Setting
	UpdatedBy       pgtype.UUID
	UpdatedAt       time.Time
}

// Holders returns every explicit setting authored for one tool across all
// layers and subjects in the workspace, ordered by layer, subject, then
// resource pattern. An unconfigured tool returns an empty slice, never an error.
func (s *Store) Holders(ctx context.Context, workspaceID pgtype.UUID, toolKey string) ([]Holder, error) {
	rows, err := s.q.ListCerebroToolPolicyHolders(ctx, cerebrodb.ListCerebroToolPolicyHoldersParams{
		WorkspaceID: workspaceID,
		ToolKey:     toolKey,
	})
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: list holders: %w", err)
	}
	out := make([]Holder, 0, len(rows))
	for _, r := range rows {
		out = append(out, Holder{
			Layer:           r.Layer,
			SubjectID:       r.SubjectID,
			ResourcePattern: r.ResourcePattern,
			Setting:         Setting(r.Setting),
			UpdatedBy:       r.UpdatedBy,
			UpdatedAt:       r.UpdatedAt.Time,
		})
	}
	return out, nil
}

// Change is one change-log row for a tool: which layer/subject/pattern was
// written, whether it was a set or a clear, the old -> new setting transition,
// and who did it (FIR-3091 punkt 8 fase 2). OldSetting "" means the layer held
// no explicit row before the write; NewSetting "" means the row was cleared.
type Change struct {
	Layer           string
	SubjectID       pgtype.UUID
	ResourcePattern string
	Action          string
	OldSetting      string
	NewSetting      string
	ActorType       string
	ActorID         pgtype.UUID
	CreatedAt       time.Time
}

// Changes returns one tool's change history, newest first, capped at limit
// rows. A tool with no recorded changes returns an empty slice, never an error.
func (s *Store) Changes(ctx context.Context, workspaceID pgtype.UUID, toolKey string, limit int32) ([]Change, error) {
	rows, err := s.q.ListCerebroToolPolicyAuditForTool(ctx, cerebrodb.ListCerebroToolPolicyAuditForToolParams{
		WorkspaceID: workspaceID,
		ToolKey:     toolKey,
		RowLimit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: list changes: %w", err)
	}
	out := make([]Change, 0, len(rows))
	for _, r := range rows {
		out = append(out, Change{
			Layer:           r.Layer,
			SubjectID:       r.SubjectID,
			ResourcePattern: r.ResourcePattern,
			Action:          r.Action,
			OldSetting:      r.OldSetting,
			NewSetting:      r.NewSetting,
			ActorType:       r.ActorType,
			ActorID:         r.ActorID,
			CreatedAt:       r.CreatedAt.Time,
		})
	}
	return out, nil
}

// UsageParams describes one applied permission decision for the usage log
// (FIR-3091 punkt 8 fase 3): which tool was enforced, at which gate, for whom,
// on which concrete resource, and what the decision was. DecidedBy is the layer
// whose rule decided; "" means no explicit rule matched and the gate's
// base/baseline answered.
type UsageParams struct {
	WorkspaceID      pgtype.UUID
	ToolKey          string
	EnforcementPoint string
	SubjectType      string // "member" | "agent" | "system"
	SubjectID        pgtype.UUID
	Resource         string
	Decision         Setting
	DecidedBy        string
}

// RecordUsage appends one usage-log row after a permission decision was
// applied at an enforcement point (FIR-3091 punkt 8 fase 3). Best-effort by
// design: the decision has already been applied, and failing the guarded
// action over a missing history row would be worse than a gap in the log (the
// same trade-off recordAudit makes). A subject without a valid id is recorded
// as a system subject; a Disable verdict is recorded as the deny it acts as.
func (s *Store) RecordUsage(ctx context.Context, p UsageParams) {
	if s == nil || s.q == nil {
		return
	}
	subjectType := p.SubjectType
	if !p.SubjectID.Valid {
		subjectType = "system"
	}
	decision := p.Decision
	if decision == SettingDisable {
		decision = SettingDeny
	}
	_ = s.q.RecordCerebroToolPolicyUsage(ctx, cerebrodb.RecordCerebroToolPolicyUsageParams{
		WorkspaceID:      p.WorkspaceID,
		ToolKey:          p.ToolKey,
		EnforcementPoint: p.EnforcementPoint,
		SubjectType:      subjectType,
		SubjectID:        p.SubjectID,
		Resource:         p.Resource,
		Decision:         string(decision),
		DecidedBy:        p.DecidedBy,
	})
}

// UsageRow is one usage-log row for a tool: which gate applied a decision, to
// whom, on which resource, and what was decided (FIR-3091 punkt 8 fase 3).
type UsageRow struct {
	EnforcementPoint string
	SubjectType      string
	SubjectID        pgtype.UUID
	Resource         string
	Decision         string
	DecidedBy        string
	CreatedAt        time.Time
}

// Usage returns one tool's usage history, newest first, capped at limit rows.
// A tool with no recorded usage returns an empty slice, never an error.
func (s *Store) Usage(ctx context.Context, workspaceID pgtype.UUID, toolKey string, limit int32) ([]UsageRow, error) {
	rows, err := s.q.ListCerebroToolPolicyUsageForTool(ctx, cerebrodb.ListCerebroToolPolicyUsageForToolParams{
		WorkspaceID: workspaceID,
		ToolKey:     toolKey,
		RowLimit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: list usage: %w", err)
	}
	out := make([]UsageRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, UsageRow{
			EnforcementPoint: r.EnforcementPoint,
			SubjectType:      r.SubjectType,
			SubjectID:        r.SubjectID,
			Resource:         r.Resource,
			Decision:         r.Decision,
			DecidedBy:        r.DecidedBy,
			CreatedAt:        r.CreatedAt.Time,
		})
	}
	return out, nil
}

// encodeCondition marshals an optional Condition for the conditions JSONB
// column. A nil or zero-value Condition encodes to NULL (no constraint).
func encodeCondition(c *Condition) ([]byte, error) {
	if c == nil || c.IsZero() {
		return nil, nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal conditions: %w", err)
	}
	return b, nil
}

// decodeCondition unmarshals a stored conditions JSONB blob. NULL / empty / a
// zero-value Condition all decode to nil (no constraint).
func decodeCondition(raw []byte) (*Condition, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var c Condition
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("unmarshal conditions: %w", err)
	}
	if c.IsZero() {
		return nil, nil
	}
	return &c, nil
}
