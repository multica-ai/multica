// Package grouppermissions — backend service + resolution helpers for the
// group permission model landed in JEH-1008 (PR 2 of JEH-1006).
//
// The package owns four pieces of state per cerebro_group:
//
//	capabilities         — toggles (create_runtime, create_agent)
//	runtime allowlist    — which agent_runtime rows the group may use
//	agent allowlist      — which agent rows the group may trigger
//	project group-access — which projects this group can see
//
// Plus a per-workspace default-group pointer that the backfill populates so
// the deny-by-default model lands without breaking existing workflows.
//
// Resolution pattern mirrors cerebro/access.autopilot_scope.Viewer: callers
// (PR 3 enforcement handlers) build a Viewer{UserID, IsAdmin, GroupIDs} once
// per request and ask "can this viewer ___?". Workspace owner/admin always
// answers true; everyone else is gated by their group memberships.
package grouppermissions

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// Capability identifiers — keep in sync with the CHECK constraint on
// cerebro_group_capability. Adding a new value also needs a schema update.
const (
	CapabilityCreateRuntime = "create_runtime"
	CapabilityCreateAgent   = "create_agent"
)

// knownCapabilities mirrors the DB-side CHECK; validating in Go gives a clean
// 400 instead of a constraint-violation 500.
var knownCapabilities = map[string]struct{}{
	CapabilityCreateRuntime: {},
	CapabilityCreateAgent:   {},
}

// IsKnownCapability returns true for the small fixed set of capability
// identifiers we accept on the wire.
func IsKnownCapability(c string) bool {
	_, ok := knownCapabilities[c]
	return ok
}

// Event names published on the events bus so the frontend's TanStack Query
// caches can invalidate after a mutation. Naming follows the existing
// "group:*" namespace from the groups service.
const (
	EventGroupCapabilityChanged = "group:capability_changed"
	EventGroupRuntimeChanged    = "group:runtime_changed"
	EventGroupAgentChanged      = "group:agent_changed"
	EventProjectGroupChanged    = "project:group_access_changed"
)

// Errors surfaced from service methods so callers can map to HTTP codes.
var (
	ErrUnknownCapability   = errors.New("unknown capability")
	ErrCapabilityNotSet    = errors.New("capability is not set on this group")
	ErrRuntimeNotInGroup   = errors.New("runtime is not in this group's allowlist")
	ErrAgentNotInGroup     = errors.New("agent is not in this group's allowlist")
	ErrGroupNotInProject   = errors.New("group does not have access to this project")
	ErrDefaultGroupMissing = errors.New("workspace has no default group")
)

// Viewer mirrors cerebro/access.Viewer but lives here so callers don't have
// to import both packages. GroupIDs MUST be the resolved effective group
// memberships in the current workspace (built via ResolveGroupIDs).
type Viewer struct {
	UserID   pgtype.UUID
	IsAdmin  bool          // workspace owner/admin overrides every group rule.
	GroupIDs []pgtype.UUID // groups this user belongs to in the workspace.
}

// Service centralises permission reads/writes + resolution. Construct one
// per server, stash on the handler struct, and pass it into request flows.
type Service struct {
	Cerebro *cerebrodb.Queries
	Bus     *events.Bus
}

// NewService is a thin constructor so the handler wiring stays explicit.
func NewService(cerebro *cerebrodb.Queries, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, Bus: bus}
}

// ResolveGroupIDs loads the viewer's effective group memberships for a
// workspace. Cheap enough to call once per request; the resulting Viewer
// is reused across multiple CanX calls without re-querying.
func (s *Service) ResolveGroupIDs(ctx context.Context, workspaceID, userID pgtype.UUID) ([]pgtype.UUID, error) {
	ids, err := s.Cerebro.ListCerebroGroupIDsForUser(ctx, cerebrodb.ListCerebroGroupIDsForUserParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return nil, err
	}
	if ids == nil {
		return []pgtype.UUID{}, nil
	}
	return ids, nil
}

// ListCapabilities returns the capability rows attached to a group.
func (s *Service) ListCapabilities(ctx context.Context, groupID pgtype.UUID) ([]cerebrodb.CerebroGroupCapability, error) {
	return s.Cerebro.ListCerebroGroupCapabilities(ctx, groupID)
}

// SetCapability grants a capability on a group. Returns the upserted row.
// publishes group:capability_changed so the frontend invalidates its cache.
func (s *Service) SetCapability(ctx context.Context, workspaceID, actorID, groupID pgtype.UUID, capability string) (cerebrodb.CerebroGroupCapability, error) {
	if !IsKnownCapability(capability) {
		return cerebrodb.CerebroGroupCapability{}, fmt.Errorf("%w: %q", ErrUnknownCapability, capability)
	}
	row, err := s.Cerebro.SetCerebroGroupCapability(ctx, cerebrodb.SetCerebroGroupCapabilityParams{
		GroupID:    groupID,
		Capability: capability,
		GrantedBy:  actorID,
	})
	if err != nil {
		return cerebrodb.CerebroGroupCapability{}, err
	}
	s.publish(EventGroupCapabilityChanged, workspaceID, actorID, map[string]any{
		"group_id":   util.UUIDToString(groupID),
		"capability": capability,
		"action":     "granted",
	})
	return row, nil
}

// RemoveCapability revokes a capability. We don't return an error if the row
// didn't exist — DELETE-and-publish is idempotent enough that the caller can
// treat both "removed" and "wasn't there" as success.
func (s *Service) RemoveCapability(ctx context.Context, workspaceID, actorID, groupID pgtype.UUID, capability string) error {
	if !IsKnownCapability(capability) {
		return fmt.Errorf("%w: %q", ErrUnknownCapability, capability)
	}
	if err := s.Cerebro.RemoveCerebroGroupCapability(ctx, cerebrodb.RemoveCerebroGroupCapabilityParams{
		GroupID:    groupID,
		Capability: capability,
	}); err != nil {
		return err
	}
	s.publish(EventGroupCapabilityChanged, workspaceID, actorID, map[string]any{
		"group_id":   util.UUIDToString(groupID),
		"capability": capability,
		"action":     "revoked",
	})
	return nil
}

// ListRuntimes returns the runtime allowlist rows for a group.
func (s *Service) ListRuntimes(ctx context.Context, groupID pgtype.UUID) ([]cerebrodb.CerebroGroupRuntimeAccess, error) {
	return s.Cerebro.ListCerebroGroupRuntimes(ctx, groupID)
}

// AddRuntime grants a group access to use a specific runtime. The group
// row is assumed to be validated by the caller (we don't want to round-trip
// twice through cerebro_group when the handler just loaded it).
func (s *Service) AddRuntime(ctx context.Context, workspaceID, actorID, groupID, runtimeID pgtype.UUID) (cerebrodb.CerebroGroupRuntimeAccess, error) {
	row, err := s.Cerebro.AddCerebroGroupRuntime(ctx, cerebrodb.AddCerebroGroupRuntimeParams{
		GroupID:   groupID,
		RuntimeID: runtimeID,
		GrantedBy: actorID,
	})
	if err != nil {
		return cerebrodb.CerebroGroupRuntimeAccess{}, err
	}
	s.publish(EventGroupRuntimeChanged, workspaceID, actorID, map[string]any{
		"group_id":   util.UUIDToString(groupID),
		"runtime_id": util.UUIDToString(runtimeID),
		"action":     "granted",
	})
	return row, nil
}

// RemoveRuntime revokes a group's access to a runtime. Same idempotent
// behavior as RemoveCapability.
func (s *Service) RemoveRuntime(ctx context.Context, workspaceID, actorID, groupID, runtimeID pgtype.UUID) error {
	if err := s.Cerebro.RemoveCerebroGroupRuntime(ctx, cerebrodb.RemoveCerebroGroupRuntimeParams{
		GroupID:   groupID,
		RuntimeID: runtimeID,
	}); err != nil {
		return err
	}
	s.publish(EventGroupRuntimeChanged, workspaceID, actorID, map[string]any{
		"group_id":   util.UUIDToString(groupID),
		"runtime_id": util.UUIDToString(runtimeID),
		"action":     "revoked",
	})
	return nil
}

// ListAgents returns the agent allowlist rows for a group.
func (s *Service) ListAgents(ctx context.Context, groupID pgtype.UUID) ([]cerebrodb.CerebroGroupAgentAccess, error) {
	return s.Cerebro.ListCerebroGroupAgents(ctx, groupID)
}

// AddAgent grants a group access to trigger a specific agent.
func (s *Service) AddAgent(ctx context.Context, workspaceID, actorID, groupID, agentID pgtype.UUID) (cerebrodb.CerebroGroupAgentAccess, error) {
	row, err := s.Cerebro.AddCerebroGroupAgent(ctx, cerebrodb.AddCerebroGroupAgentParams{
		GroupID:   groupID,
		AgentID:   agentID,
		GrantedBy: actorID,
	})
	if err != nil {
		return cerebrodb.CerebroGroupAgentAccess{}, err
	}
	s.publish(EventGroupAgentChanged, workspaceID, actorID, map[string]any{
		"group_id": util.UUIDToString(groupID),
		"agent_id": util.UUIDToString(agentID),
		"action":   "granted",
	})
	return row, nil
}

// RemoveAgent revokes a group's access to an agent.
func (s *Service) RemoveAgent(ctx context.Context, workspaceID, actorID, groupID, agentID pgtype.UUID) error {
	if err := s.Cerebro.RemoveCerebroGroupAgent(ctx, cerebrodb.RemoveCerebroGroupAgentParams{
		GroupID: groupID,
		AgentID: agentID,
	}); err != nil {
		return err
	}
	s.publish(EventGroupAgentChanged, workspaceID, actorID, map[string]any{
		"group_id": util.UUIDToString(groupID),
		"agent_id": util.UUIDToString(agentID),
		"action":   "revoked",
	})
	return nil
}

// ListProjectGroups returns the group-access rows for a project.
func (s *Service) ListProjectGroups(ctx context.Context, projectID pgtype.UUID) ([]cerebrodb.CerebroProjectGroupMember, error) {
	return s.Cerebro.ListCerebroProjectGroups(ctx, projectID)
}

// AddProjectGroup grants a group access to a project. This is OR-ed with the
// existing project_member user rows when computing project visibility.
func (s *Service) AddProjectGroup(ctx context.Context, workspaceID, actorID, projectID, groupID pgtype.UUID) (cerebrodb.CerebroProjectGroupMember, error) {
	row, err := s.Cerebro.AddCerebroProjectGroup(ctx, cerebrodb.AddCerebroProjectGroupParams{
		ProjectID: projectID,
		GroupID:   groupID,
		AddedBy:   actorID,
	})
	if err != nil {
		return cerebrodb.CerebroProjectGroupMember{}, err
	}
	s.publish(EventProjectGroupChanged, workspaceID, actorID, map[string]any{
		"project_id": util.UUIDToString(projectID),
		"group_id":   util.UUIDToString(groupID),
		"action":     "granted",
	})
	return row, nil
}

// RemoveProjectGroup revokes a group's access to a project.
func (s *Service) RemoveProjectGroup(ctx context.Context, workspaceID, actorID, projectID, groupID pgtype.UUID) error {
	if err := s.Cerebro.RemoveCerebroProjectGroup(ctx, cerebrodb.RemoveCerebroProjectGroupParams{
		ProjectID: projectID,
		GroupID:   groupID,
	}); err != nil {
		return err
	}
	s.publish(EventProjectGroupChanged, workspaceID, actorID, map[string]any{
		"project_id": util.UUIDToString(projectID),
		"group_id":   util.UUIDToString(groupID),
		"action":     "revoked",
	})
	return nil
}

// DefaultGroupID returns the workspace's default-group pointer or pgx.ErrNoRows
// if the workspace has no default (typically only the case if the migration
// hasn't run yet on this DB).
func (s *Service) DefaultGroupID(ctx context.Context, workspaceID pgtype.UUID) (pgtype.UUID, error) {
	return s.Cerebro.GetCerebroDefaultGroupID(ctx, workspaceID)
}

// IsDefaultGroup tells callers whether a group row is the default for its
// workspace. The groups service uses this to refuse deletion.
func (s *Service) IsDefaultGroup(ctx context.Context, groupID pgtype.UUID) (bool, error) {
	return s.Cerebro.IsCerebroDefaultGroup(ctx, groupID)
}

// CanCreateRuntime returns true when the viewer is allowed to create a new
// runtime in the workspace. Admin override + capability-based gating.
func (s *Service) CanCreateRuntime(ctx context.Context, viewer Viewer, workspaceID pgtype.UUID) (bool, error) {
	return s.hasCapability(ctx, viewer, workspaceID, CapabilityCreateRuntime)
}

// CanCreateAgent — same shape as CanCreateRuntime but for agent creation.
func (s *Service) CanCreateAgent(ctx context.Context, viewer Viewer, workspaceID pgtype.UUID) (bool, error) {
	return s.hasCapability(ctx, viewer, workspaceID, CapabilityCreateAgent)
}

func (s *Service) hasCapability(ctx context.Context, viewer Viewer, workspaceID pgtype.UUID, capability string) (bool, error) {
	if viewer.IsAdmin {
		return true, nil
	}
	if !viewer.UserID.Valid {
		return false, nil
	}
	return s.Cerebro.HasCerebroCapability(ctx, cerebrodb.HasCerebroCapabilityParams{
		WorkspaceID: workspaceID,
		UserID:      viewer.UserID,
		Capability:  capability,
	})
}

// CanUseRuntime returns true when the viewer is allowed to use the given
// runtime. Owner of the runtime always wins (the owner check is up to the
// caller — the runtime row's OwnerID lives in the upstream agent_runtime
// table, which this package intentionally does not depend on).
func (s *Service) CanUseRuntime(ctx context.Context, viewer Viewer, runtimeID pgtype.UUID) (bool, error) {
	if viewer.IsAdmin {
		return true, nil
	}
	if !viewer.UserID.Valid {
		return false, nil
	}
	return s.Cerebro.HasCerebroRuntimeAccess(ctx, cerebrodb.HasCerebroRuntimeAccessParams{
		RuntimeID: runtimeID,
		UserID:    viewer.UserID,
	})
}

// CanUseAgent returns true when the viewer is allowed to trigger the agent.
func (s *Service) CanUseAgent(ctx context.Context, viewer Viewer, agentID pgtype.UUID) (bool, error) {
	if viewer.IsAdmin {
		return true, nil
	}
	if !viewer.UserID.Valid {
		return false, nil
	}
	return s.Cerebro.HasCerebroAgentAccess(ctx, cerebrodb.HasCerebroAgentAccessParams{
		AgentID: agentID,
		UserID:  viewer.UserID,
	})
}

// CanSeeProjectViaGroup returns true when one of the viewer's groups grants
// access to the project. Callers OR this with project.access == 'workspace',
// project_member rows, and admin override to compute final visibility.
func (s *Service) CanSeeProjectViaGroup(ctx context.Context, viewer Viewer, projectID pgtype.UUID) (bool, error) {
	if viewer.IsAdmin {
		return true, nil
	}
	if !viewer.UserID.Valid {
		return false, nil
	}
	return s.Cerebro.HasCerebroProjectAccessViaGroup(ctx, cerebrodb.HasCerebroProjectAccessViaGroupParams{
		ProjectID: projectID,
		UserID:    viewer.UserID,
	})
}

// VisibleAgentIDs returns the set of agent IDs the viewer is granted access to
// via any group membership in the workspace. Returns (nil, nil) when no
// filtering should be applied — either because the viewer is an admin (sees
// everything) or because the user ID is invalid (caller already denied access
// upstream; returning nil here keeps the contract simple — no filter).
//
// The empty slice (vs nil) is the "no grants" case: the viewer is a non-admin
// member with zero accessible resources. Callers must distinguish nil
// ("no filter") from []{} ("explicit empty allowlist") so an admin sees every
// agent while a non-admin member with no group grant sees none.
func (s *Service) VisibleAgentIDs(ctx context.Context, viewer Viewer, workspaceID pgtype.UUID) ([]pgtype.UUID, error) {
	if viewer.IsAdmin {
		return nil, nil
	}
	if !viewer.UserID.Valid {
		return nil, nil
	}
	ids, err := s.Cerebro.ListCerebroAgentIDsForUser(ctx, cerebrodb.ListCerebroAgentIDsForUserParams{
		WorkspaceID: workspaceID,
		UserID:      viewer.UserID,
	})
	if err != nil {
		return nil, err
	}
	if ids == nil {
		return []pgtype.UUID{}, nil
	}
	return ids, nil
}

// VisibleRuntimeIDs mirrors VisibleAgentIDs for the runtime allowlist.
func (s *Service) VisibleRuntimeIDs(ctx context.Context, viewer Viewer, workspaceID pgtype.UUID) ([]pgtype.UUID, error) {
	if viewer.IsAdmin {
		return nil, nil
	}
	if !viewer.UserID.Valid {
		return nil, nil
	}
	ids, err := s.Cerebro.ListCerebroRuntimeIDsForUser(ctx, cerebrodb.ListCerebroRuntimeIDsForUserParams{
		WorkspaceID: workspaceID,
		UserID:      viewer.UserID,
	})
	if err != nil {
		return nil, err
	}
	if ids == nil {
		return []pgtype.UUID{}, nil
	}
	return ids, nil
}

// VisibleProjectIDs returns the project IDs the viewer has group-level access
// to. Callers OR these IDs with the existing project_member + workspace-access
// results to compute the final ListProjects payload.
func (s *Service) VisibleProjectIDs(ctx context.Context, viewer Viewer, workspaceID pgtype.UUID) ([]pgtype.UUID, error) {
	if viewer.IsAdmin {
		return nil, nil
	}
	if !viewer.UserID.Valid {
		return nil, nil
	}
	ids, err := s.Cerebro.ListCerebroProjectIDsForUser(ctx, cerebrodb.ListCerebroProjectIDsForUserParams{
		WorkspaceID: workspaceID,
		UserID:      viewer.UserID,
	})
	if err != nil {
		return nil, err
	}
	if ids == nil {
		return []pgtype.UUID{}, nil
	}
	return ids, nil
}

// ProjectAudienceUserIDs returns every user with group-level access to the
// project. Used by the WS audience builder so realtime events reach group-
// only viewers (otherwise a non-admin non-member with group access would
// only see changes after a manual refresh).
func (s *Service) ProjectAudienceUserIDs(ctx context.Context, projectID pgtype.UUID) ([]pgtype.UUID, error) {
	ids, err := s.Cerebro.ListCerebroUserIDsForProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if ids == nil {
		return []pgtype.UUID{}, nil
	}
	return ids, nil
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
