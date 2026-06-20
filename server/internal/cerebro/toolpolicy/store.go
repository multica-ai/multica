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

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// Store reads and writes the explicit per-layer tool settings.
type Store struct {
	pool *pgxpool.Pool
	q    *cerebrodb.Queries
}

// NewStore constructs a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: cerebrodb.New(pool)}
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
	case LayerWorkspace, LayerRuntime, LayerAgent, LayerGroup, LayerUser, LayerSystem:
		return true
	default:
		return false
	}
}

// validSetting reports whether s is one of the four stored settings.
func validSetting(s Setting) bool {
	switch s {
	case SettingInherit, SettingAllow, SettingAsk, SettingDeny:
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
	conditions, err := encodeCondition(p.Conditions)
	if err != nil {
		return cerebrodb.UpsertCerebroToolPolicyRow{}, fmt.Errorf("toolpolicy: set: %w", err)
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
	return row, nil
}

// Clear removes one layer's explicit choice for one (tool, resource_pattern)
// so that layer reverts to Inherit for that pattern. Clearing an absent row is
// a no-op.
func (s *Store) Clear(ctx context.Context, workspaceID pgtype.UUID, toolKey string, layer Layer, subjectID pgtype.UUID, resourcePattern string) error {
	if !validLayer(layer) {
		return fmt.Errorf("%w: %q", ErrUnknownLayer, layer)
	}
	if err := s.q.DeleteCerebroToolPolicy(ctx, cerebrodb.DeleteCerebroToolPolicyParams{
		WorkspaceID:     workspaceID,
		ToolKey:         toolKey,
		Layer:           string(layer),
		SubjectID:       subjectID,
		ResourcePattern: resourcePattern,
	}); err != nil {
		return fmt.Errorf("toolpolicy: clear: %w", err)
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
