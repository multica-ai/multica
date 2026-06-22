package credentialpolicy

// store.go is the database-backed half of the credential-policy engine
// (FIR-1479 write-API slice). chain.go holds the pure resolution logic; Store
// loads the per-layer settings one context needs and folds them through Resolve.
//
// The split mirrors toolpolicy.Store (and permissions.Resolver): the decision
// logic lives in the pure Resolve and is exhaustively unit-tested without a
// database, while Store is the thin seam that assembles a chain Input from
// stored rows and writes the authoring rows the permissions interface edits.
//
// It is deliberately the toolpolicy.Store shape minus the dimensions a credential
// grant does not have: no resource_pattern (a box is whole, not pattern-scoped),
// no conditions/WHEN layer, and no Ask setting (reachability is yes/no). What
// remains — Resolve / Set / Clear / ListForSubject plus group expansion — is the
// same so the UI can drive both chains through one mental model.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// Store reads and writes the explicit per-layer credential settings.
type Store struct {
	q *cerebrodb.Queries
}

// NewStore constructs a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{q: cerebrodb.New(pool)}
}

// NewStoreFromQueries constructs a Store from an existing Queries handle — every
// path (Resolve, Set, Clear, ListForSubject) needs only the generated queries,
// so unlike toolpolicy there is no second constructor that also carries a pool.
func NewStoreFromQueries(q *cerebrodb.Queries) *Store {
	return &Store{q: q}
}

// ErrUnknownLayer / ErrUnknownSetting guard the authoring API against values the
// database CHECK constraints would reject anyway, so a bad call fails with a
// clear message instead of a raw constraint-violation error.
var (
	ErrUnknownLayer   = errors.New("credentialpolicy: unknown layer")
	ErrUnknownSetting = errors.New("credentialpolicy: unknown setting")
)

// validLayer reports whether l is one of the chain layers (the five plus the
// System mandate peer of User).
func validLayer(l Layer) bool {
	switch l {
	case LayerWorkspace, LayerRuntime, LayerAgent, LayerGroup, LayerUser, LayerSystem:
		return true
	default:
		return false
	}
}

// validSetting reports whether s is one of the three stored settings. There is
// no Ask: a credential is either reachable or not.
func validSetting(s Setting) bool {
	switch s {
	case SettingInherit, SettingAllow, SettingDeny:
		return true
	default:
		return false
	}
}

// Query identifies the (credential, runtime, agent, user, groups, system) a
// caller wants an effective verdict for. Any of RuntimeID/AgentID/UserID may be
// the zero value (Valid=false) when that layer is not part of the request; that
// layer is then absent and treated as Inherit. GroupIDs may be empty.
type Query struct {
	WorkspaceID   pgtype.UUID
	CredentialKey string
	RuntimeID     pgtype.UUID
	AgentID       pgtype.UUID
	UserID        pgtype.UUID
	GroupIDs      []pgtype.UUID
	// SystemID is the subject of the System layer — the mandate-actor ceiling for
	// a human-less run. Zero (Valid=false) when the run has a human behind it.
	SystemID pgtype.UUID
}

// Resolve loads the explicit per-layer settings for the query and folds them
// into one Effective verdict. A query with no stored rows resolves to the
// deny-by-default base (see Resolve), so a box no layer has granted is
// unreachable — least-privilege.
func (s *Store) Resolve(ctx context.Context, in Query) (Effective, error) {
	input, err := s.loadInput(ctx, in)
	if err != nil {
		return Effective{}, err
	}
	return Resolve(input), nil
}

// loadInput fetches the rows for the query and assembles a chain Input. Several
// group rows are collapsed with CombineGroups (a grant in any group wins) before
// entering the chain at LayerGroup.
func (s *Store) loadInput(ctx context.Context, in Query) (Input, error) {
	groupIDs, err := s.resolveGroupIDs(ctx, in.WorkspaceID, in.UserID, in.GroupIDs)
	if err != nil {
		return Input{}, err
	}
	rows, err := s.q.ListCerebroCredentialPolicyForContext(ctx, cerebrodb.ListCerebroCredentialPolicyForContextParams{
		WorkspaceID:   in.WorkspaceID,
		CredentialKey: in.CredentialKey,
		RuntimeID:     in.RuntimeID,
		AgentID:       in.AgentID,
		UserID:        in.UserID,
		GroupIds:      groupIDs,
		SystemID:      in.SystemID,
	})
	if err != nil {
		return Input{}, fmt.Errorf("credentialpolicy: load context settings: %w", err)
	}

	input := Input{Settings: map[Layer]Setting{}}
	var groupSettings []Setting
	for _, r := range rows {
		layer := Layer(r.Layer)
		setting := Setting(r.Setting)
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
// memberships are loaded — so the Group layer always reflects the real groups of
// the user whose ceiling we are resolving, without the caller having to
// enumerate them and without trusting a client-supplied group list. A user not
// in scope yields no groups (the layer inherits).
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
		return nil, fmt.Errorf("credentialpolicy: load user groups: %w", err)
	}
	ids := make([]pgtype.UUID, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	return ids, nil
}

// SetParams is the argument bundle for Set.
type SetParams struct {
	WorkspaceID   pgtype.UUID
	CredentialKey string
	Layer         Layer
	SubjectID     pgtype.UUID
	Setting       Setting
	UpdatedBy     pgtype.UUID // optional; the zero value leaves the column NULL
}

// Set upserts one layer's explicit choice for one credential. Storing
// SettingInherit is allowed (it behaves like an absent row but lets the UI
// render a deliberate "follow the layer below"); use Clear to drop the row.
func (s *Store) Set(ctx context.Context, p SetParams) (cerebrodb.CerebroCredentialPolicy, error) {
	if !validLayer(p.Layer) {
		return cerebrodb.CerebroCredentialPolicy{}, fmt.Errorf("%w: %q", ErrUnknownLayer, p.Layer)
	}
	if !validSetting(p.Setting) {
		return cerebrodb.CerebroCredentialPolicy{}, fmt.Errorf("%w: %q", ErrUnknownSetting, p.Setting)
	}
	row, err := s.q.UpsertCerebroCredentialPolicy(ctx, cerebrodb.UpsertCerebroCredentialPolicyParams{
		WorkspaceID:   p.WorkspaceID,
		CredentialKey: p.CredentialKey,
		Layer:         string(p.Layer),
		SubjectID:     p.SubjectID,
		Setting:       string(p.Setting),
		UpdatedBy:     p.UpdatedBy,
	})
	if err != nil {
		return cerebrodb.CerebroCredentialPolicy{}, fmt.Errorf("credentialpolicy: set: %w", err)
	}
	return row, nil
}

// Clear removes one layer's explicit choice for one credential so that layer
// reverts to Inherit. Clearing an absent row is a no-op.
func (s *Store) Clear(ctx context.Context, workspaceID pgtype.UUID, credentialKey string, layer Layer, subjectID pgtype.UUID) error {
	if !validLayer(layer) {
		return fmt.Errorf("%w: %q", ErrUnknownLayer, layer)
	}
	if err := s.q.DeleteCerebroCredentialPolicy(ctx, cerebrodb.DeleteCerebroCredentialPolicyParams{
		WorkspaceID:   workspaceID,
		CredentialKey: credentialKey,
		Layer:         string(layer),
		SubjectID:     subjectID,
	}); err != nil {
		return fmt.Errorf("credentialpolicy: clear: %w", err)
	}
	return nil
}

// SubjectSetting is one (credential, setting) pair a subject holds at one layer,
// used to render the per-agent / per-runtime column of the permissions table.
type SubjectSetting struct {
	CredentialKey string
	Setting       Setting
	UpdatedBy     pgtype.UUID
	UpdatedAt     pgtype.Timestamptz
}

// ListForSubject returns every explicit setting one subject holds at one layer,
// across all credentials.
func (s *Store) ListForSubject(ctx context.Context, workspaceID pgtype.UUID, layer Layer, subjectID pgtype.UUID) ([]SubjectSetting, error) {
	if !validLayer(layer) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownLayer, layer)
	}
	rows, err := s.q.ListCerebroCredentialPolicyForSubject(ctx, cerebrodb.ListCerebroCredentialPolicyForSubjectParams{
		WorkspaceID: workspaceID,
		Layer:       string(layer),
		SubjectID:   subjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("credentialpolicy: list for subject: %w", err)
	}
	out := make([]SubjectSetting, 0, len(rows))
	for _, r := range rows {
		out = append(out, SubjectSetting{
			CredentialKey: r.CredentialKey,
			Setting:       Setting(r.Setting),
			UpdatedBy:     r.UpdatedBy,
			UpdatedAt:     r.UpdatedAt,
		})
	}
	return out, nil
}
