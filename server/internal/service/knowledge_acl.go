package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Access to restricted knowledge bases (spec 3.3, plan 10.13). A restricted base
// is reachable only through its own read-only WeKnora endpoint, and that endpoint
// is bound in the MCP library only to the agents a grant reaches. The bindings
// are recomputed when a grant, a base's visibility or a squad membership changes.
//
// Humans are not grant subjects. The owner and admins curate every base; other
// members see only workspace-visible bases.

const (
	KnowledgeSubjectSquad = "squad"
	KnowledgeSubjectFloor = "floor"
	KnowledgeSubjectAgent = "agent"

	KnowledgeAccessRead  = "read"
	KnowledgeAccessWrite = "write"

	KnowledgeVisibilityWorkspace  = "workspace"
	KnowledgeVisibilityRestricted = "restricted"

	knowledgeRestrictedEndpointPrefix = "company-restricted-"
)

// ErrKnowledgeSubjectNotFound means a grant names a squad, floor or agent that
// is not in this workspace, or is archived.
var ErrKnowledgeSubjectNotFound = errors.New("the subject is not in this workspace")

// KnowledgeGrantView is one grant on a restricted base.
type KnowledgeGrantView struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Access      string `json:"access"`
}

// KnowledgeAccessView is a base's visibility and its grants.
type KnowledgeAccessView struct {
	Key        string               `json:"key"`
	Visibility string               `json:"visibility"`
	Grants     []KnowledgeGrantView `json:"grants"`
}

// knowledgeRestrictedEndpointName is the WeKnora endpoint and MCP library entry
// of a restricted base.
func knowledgeRestrictedEndpointName(key string) string {
	return knowledgeRestrictedEndpointPrefix + key
}

// curator reports whether the actor may use every base: the system, and an
// owner or admin acting as a human.
func (a KnowledgeActor) curator() bool {
	return a.Type == KnowledgeActorSystem || a.Curator
}

// knowledgeCanRead reports whether an actor may read a base. grants is the
// actor's access per base id, from agentKnowledgeGrants; it is nil for humans.
func knowledgeCanRead(actor KnowledgeActor, visibility, baseID string, grants map[string]string) bool {
	if visibility != KnowledgeVisibilityRestricted || actor.curator() {
		return true
	}
	if actor.Type != KnowledgeActorAgent {
		return false
	}
	return grants[baseID] != ""
}

// knowledgeCanWrite reports whether an actor may write a document into a base.
// Agents write drafts to any base they can reach, and a restricted base needs a
// write grant.
func knowledgeCanWrite(actor KnowledgeActor, visibility, baseID string, grants map[string]string) bool {
	if visibility != KnowledgeVisibilityRestricted || actor.curator() {
		return true
	}
	if actor.Type != KnowledgeActorAgent {
		return false
	}
	return grants[baseID] == KnowledgeAccessWrite
}

// agentKnowledgeGrants returns the access an agent holds per base, from its own
// grants, its squads' and its squads' floors'. Write beats read.
func (s *KnowledgeService) agentKnowledgeGrants(ctx context.Context, workspaceID, agentID pgtype.UUID) (map[string]string, error) {
	rows, err := s.Queries.ListKnowledgeACLForAgent(ctx, db.ListKnowledgeACLForAgentParams{WorkspaceID: workspaceID, AgentID: agentID})
	if err != nil {
		return nil, fmt.Errorf("list knowledge grants: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		id := util.UUIDToString(r.KnowledgeBaseID)
		if out[id] != KnowledgeAccessWrite {
			out[id] = r.Access
		}
	}
	return out, nil
}

// actorKnowledgeGrants is agentKnowledgeGrants for an agent actor, and nil for
// anyone else (their access does not depend on grants).
func (s *KnowledgeService) actorKnowledgeGrants(ctx context.Context, workspaceID pgtype.UUID, actor KnowledgeActor) (map[string]string, error) {
	if actor.Type != KnowledgeActorAgent || !actor.ID.Valid {
		return nil, nil
	}
	return s.agentKnowledgeGrants(ctx, workspaceID, actor.ID)
}

// dropUnreadable removes the bases an actor may not read, so every later step
// (listing, searching, selecting a base by key) cannot reach them. A restricted
// base the actor may not read is treated as absent, so its existence stays hidden.
func (s *KnowledgeService) dropUnreadable(ctx context.Context, workspaceID pgtype.UUID, actor KnowledgeActor, bases map[string]db.KnowledgeBase) error {
	if actor.curator() {
		return nil
	}
	grants, err := s.actorKnowledgeGrants(ctx, workspaceID, actor)
	if err != nil {
		return err
	}
	for id, b := range bases {
		if !knowledgeCanRead(actor, b.Visibility, id, grants) {
			delete(bases, id)
		}
	}
	return nil
}

// baseByKey returns the live base with this key.
func (s *KnowledgeService) baseByKey(ctx context.Context, workspaceID pgtype.UUID, key string) (db.KnowledgeBase, error) {
	b, err := s.Queries.GetKnowledgeBaseByKey(ctx, db.GetKnowledgeBaseByKeyParams{WorkspaceID: workspaceID, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.KnowledgeBase{}, ErrKnowledgeBaseNotFound
	}
	if err != nil {
		return db.KnowledgeBase{}, fmt.Errorf("load knowledge base: %w", err)
	}
	return b, nil
}

// KnowledgeAccess returns a base's visibility and grants. Owners and admins only
// reach it, through the handler.
func (s *KnowledgeService) KnowledgeAccess(ctx context.Context, workspaceID pgtype.UUID, key string) (KnowledgeAccessView, error) {
	if ok, _ := s.offeredReason(); !ok {
		return KnowledgeAccessView{}, &KnowledgeNotReadyError{State: KnowledgeStateDisabled}
	}
	base, err := s.baseByKey(ctx, workspaceID, key)
	if err != nil {
		return KnowledgeAccessView{}, err
	}
	rows, err := s.Queries.ListKnowledgeACLForBase(ctx, db.ListKnowledgeACLForBaseParams{WorkspaceID: workspaceID, KnowledgeBaseID: base.ID})
	if err != nil {
		return KnowledgeAccessView{}, fmt.Errorf("list grants: %w", err)
	}
	out := KnowledgeAccessView{Key: key, Visibility: base.Visibility, Grants: []KnowledgeGrantView{}}
	for _, r := range rows {
		out.Grants = append(out.Grants, KnowledgeGrantView{
			SubjectType: r.SubjectType, SubjectID: util.UUIDToString(r.SubjectID), Access: r.Access,
		})
	}
	return out, nil
}

// subjectInWorkspace reports whether a grant's subject is live in the workspace.
func (s *KnowledgeService) subjectInWorkspace(ctx context.Context, workspaceID pgtype.UUID, subjectType string, id pgtype.UUID) (bool, error) {
	var ok bool
	var err error
	switch subjectType {
	case KnowledgeSubjectSquad:
		ok, err = s.Queries.KnowledgeACLSquadExists(ctx, db.KnowledgeACLSquadExistsParams{ID: id, WorkspaceID: workspaceID})
	case KnowledgeSubjectFloor:
		ok, err = s.Queries.KnowledgeACLFloorExists(ctx, db.KnowledgeACLFloorExistsParams{ID: id, WorkspaceID: workspaceID})
	case KnowledgeSubjectAgent:
		ok, err = s.Queries.KnowledgeACLAgentExists(ctx, db.KnowledgeACLAgentExistsParams{ID: id, WorkspaceID: workspaceID})
	default:
		return false, &KnowledgeInvalidError{"subject_type must be squad, floor or agent"}
	}
	if err != nil {
		return false, fmt.Errorf("check grant subject: %w", err)
	}
	return ok, nil
}

// SetKnowledgeGrant gives a squad, floor or agent read or write access to a
// restricted base, and updates the bindings of the agents it reaches.
func (s *KnowledgeService) SetKnowledgeGrant(ctx context.Context, workspaceID pgtype.UUID, key, subjectType, subjectID, access string) (KnowledgeAccessView, error) {
	if access != KnowledgeAccessRead && access != KnowledgeAccessWrite {
		return KnowledgeAccessView{}, &KnowledgeInvalidError{"access must be read or write"}
	}
	id, err := util.ParseUUID(subjectID)
	if err != nil {
		return KnowledgeAccessView{}, &KnowledgeInvalidError{"subject_id must be an id"}
	}
	if _, err := s.workspaceClient(ctx, workspaceID); err != nil {
		return KnowledgeAccessView{}, err
	}
	base, err := s.baseByKey(ctx, workspaceID, key)
	if err != nil {
		return KnowledgeAccessView{}, err
	}
	if base.Visibility != KnowledgeVisibilityRestricted {
		return KnowledgeAccessView{}, &KnowledgeConflictError{"make the knowledge base restricted before granting access to it"}
	}
	live, err := s.subjectInWorkspace(ctx, workspaceID, subjectType, id)
	if err != nil {
		return KnowledgeAccessView{}, err
	}
	if !live {
		return KnowledgeAccessView{}, ErrKnowledgeSubjectNotFound
	}
	if err := s.Queries.UpsertKnowledgeACL(ctx, db.UpsertKnowledgeACLParams{
		WorkspaceID: workspaceID, KnowledgeBaseID: base.ID, SubjectType: subjectType, SubjectID: id, Access: access,
	}); err != nil {
		return KnowledgeAccessView{}, fmt.Errorf("save grant: %w", err)
	}
	if err := s.syncRestrictedBindings(ctx, workspaceID, base); err != nil {
		return KnowledgeAccessView{}, err
	}
	return s.KnowledgeAccess(ctx, workspaceID, key)
}

// RemoveKnowledgeGrant withdraws a grant. The agents it reached lose the
// endpoint unless another grant still reaches them.
func (s *KnowledgeService) RemoveKnowledgeGrant(ctx context.Context, workspaceID pgtype.UUID, key, subjectType, subjectID string) (KnowledgeAccessView, error) {
	id, err := util.ParseUUID(subjectID)
	if err != nil {
		return KnowledgeAccessView{}, &KnowledgeInvalidError{"subject_id must be an id"}
	}
	if _, err := s.workspaceClient(ctx, workspaceID); err != nil {
		return KnowledgeAccessView{}, err
	}
	base, err := s.baseByKey(ctx, workspaceID, key)
	if err != nil {
		return KnowledgeAccessView{}, err
	}
	if _, err := s.Queries.DeleteKnowledgeACL(ctx, db.DeleteKnowledgeACLParams{
		WorkspaceID: workspaceID, KnowledgeBaseID: base.ID, SubjectType: subjectType, SubjectID: id,
	}); err != nil {
		return KnowledgeAccessView{}, fmt.Errorf("remove grant: %w", err)
	}
	if err := s.syncRestrictedBindings(ctx, workspaceID, base); err != nil {
		return KnowledgeAccessView{}, err
	}
	return s.KnowledgeAccess(ctx, workspaceID, key)
}

// SetKnowledgeVisibility makes a base restricted or workspace-visible.
//
// Restricting creates the base's endpoint and binds it to the agents its grants
// reach. Opening it again removes the endpoint from the library and rotates its
// WeKnora token, which the library held, so the old token stops working. Either
// way company-read is updated to match.
func (s *KnowledgeService) SetKnowledgeVisibility(ctx context.Context, workspaceID, actorID pgtype.UUID, key, visibility string) (KnowledgeAccessView, error) {
	if visibility != KnowledgeVisibilityWorkspace && visibility != KnowledgeVisibilityRestricted {
		return KnowledgeAccessView{}, &KnowledgeInvalidError{"visibility must be workspace or restricted"}
	}
	ws, err := s.workspaceClient(ctx, workspaceID)
	if err != nil {
		return KnowledgeAccessView{}, err
	}
	base, err := s.baseByKey(ctx, workspaceID, key)
	if err != nil {
		return KnowledgeAccessView{}, err
	}
	if base.Visibility == visibility {
		return s.KnowledgeAccess(ctx, workspaceID, key)
	}
	if _, err := s.Queries.SetKnowledgeBaseVisibility(ctx, db.SetKnowledgeBaseVisibilityParams{
		ID: base.ID, WorkspaceID: workspaceID, Visibility: visibility,
	}); err != nil {
		return KnowledgeAccessView{}, fmt.Errorf("set visibility: %w", err)
	}
	base.Visibility = visibility

	if visibility == KnowledgeVisibilityRestricted {
		if _, err := s.ensureReadEndpoint(ctx, workspaceID, actorID, ws, knowledgeRestrictedEndpointName(key),
			"Read-only access to a restricted knowledge base", []string{base.WeknoraKbID}); err != nil {
			return KnowledgeAccessView{}, fmt.Errorf("create restricted endpoint: %w", err)
		}
		if err := s.syncRestrictedBindings(ctx, workspaceID, base); err != nil {
			return KnowledgeAccessView{}, err
		}
	} else if err := s.revokeRestrictedEndpoint(ctx, workspaceID, ws, key); err != nil {
		return KnowledgeAccessView{}, err
	}
	if _, err := s.ensureMCPServer(ctx, workspaceID, actorID, ws); err != nil {
		return KnowledgeAccessView{}, fmt.Errorf("update company-read: %w", err)
	}
	return s.KnowledgeAccess(ctx, workspaceID, key)
}

// revokeRestrictedEndpoint takes a base's endpoint away from every agent and
// from the library, and rotates its WeKnora token so the one the library held
// stops working. A missing endpoint is already revoked.
func (s *KnowledgeService) revokeRestrictedEndpoint(ctx context.Context, workspaceID pgtype.UUID, ws knowledge.Workspace, key string) error {
	name := knowledgeRestrictedEndpointName(key)
	entry, err := s.Queries.GetWorkspaceMcpServerByName(ctx, db.GetWorkspaceMcpServerByNameParams{WorkspaceID: workspaceID, Name: name})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load library entry: %w", err)
	}
	if err == nil {
		if err := s.removeLibraryEntry(ctx, workspaceID, entry.ID); err != nil {
			return err
		}
	}
	endpoints, err := ws.ListMCPEndpoints(ctx)
	if err != nil {
		return fmt.Errorf("list mcp endpoints: %w", err)
	}
	for _, e := range endpoints {
		if e.Name == name {
			if _, err := ws.RotateMCPEndpointToken(ctx, e.ID); err != nil {
				return fmt.Errorf("revoke restricted endpoint: %w", err)
			}
		}
	}
	return nil
}

// removeLibraryEntry deletes a library entry and its bindings under the same
// locks the MCP API uses for a server delete.
func (s *KnowledgeService) removeLibraryEntry(ctx context.Context, workspaceID, serverID pgtype.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin library removal: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	if _, err := q.LockWorkspaceForChatSessionCreate(ctx, workspaceID); err != nil {
		return fmt.Errorf("lock workspace: %w", err)
	}
	if _, err := q.LockWorkspaceMcpServerForShare(ctx, db.LockWorkspaceMcpServerForShareParams{ID: serverID, WorkspaceID: workspaceID}); err != nil {
		return fmt.Errorf("lock mcp server: %w", err)
	}
	if err := q.DeleteAgentMcpServersByServer(ctx, serverID); err != nil {
		return fmt.Errorf("unbind restricted endpoint: %w", err)
	}
	if _, err := q.DeleteWorkspaceMcpServer(ctx, db.DeleteWorkspaceMcpServerParams{ID: serverID, WorkspaceID: workspaceID}); err != nil {
		return fmt.Errorf("remove library entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit library removal: %w", err)
	}
	return nil
}

// syncRestrictedBindings makes the agents bound to a restricted base's library
// entry exactly the agents its grants reach. It runs in one transaction with the
// same locks as assignToLiveAgents, so no binding can outlive a deleted workspace
// or server (the junction has no foreign keys). A base without a library entry has
// no endpoint yet, so there is nothing to bind.
func (s *KnowledgeService) syncRestrictedBindings(ctx context.Context, workspaceID pgtype.UUID, base db.KnowledgeBase) error {
	entry, err := s.Queries.GetWorkspaceMcpServerByName(ctx, db.GetWorkspaceMcpServerByNameParams{WorkspaceID: workspaceID, Name: knowledgeRestrictedEndpointName(base.Key)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load library entry: %w", err)
	}
	agents, err := s.Queries.ListLiveAgentIDsForKnowledge(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	reach := make(map[string]bool, len(agents))
	for _, agent := range agents {
		grants, err := s.agentKnowledgeGrants(ctx, workspaceID, agent)
		if err != nil {
			return err
		}
		reach[util.UUIDToString(agent)] = grants[util.UUIDToString(base.ID)] != ""
	}
	return s.writeBindings(ctx, workspaceID, entry.ID, agents, reach)
}

// RecomputeAgentKnowledgeAccess updates an agent's bindings to every restricted
// base after its squad membership changed. An agent removed from a squad loses
// the endpoints that squad's grants reached.
func (s *KnowledgeService) RecomputeAgentKnowledgeAccess(ctx context.Context, workspaceID, agentID pgtype.UUID) error {
	if s == nil || s.Queries == nil || !agentID.Valid {
		return nil
	}
	bases, err := s.Queries.ListKnowledgeBases(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list knowledge bases: %w", err)
	}
	grants, err := s.agentKnowledgeGrants(ctx, workspaceID, agentID)
	if err != nil {
		return err
	}
	for _, b := range bases {
		if b.Visibility != KnowledgeVisibilityRestricted {
			continue
		}
		entry, err := s.Queries.GetWorkspaceMcpServerByName(ctx, db.GetWorkspaceMcpServerByNameParams{WorkspaceID: workspaceID, Name: knowledgeRestrictedEndpointName(b.Key)})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("load library entry: %w", err)
		}
		reach := map[string]bool{util.UUIDToString(agentID): grants[util.UUIDToString(b.ID)] != ""}
		if err := s.writeBindings(ctx, workspaceID, entry.ID, []pgtype.UUID{agentID}, reach); err != nil {
			return err
		}
	}
	return nil
}

// writeBindings binds the server to each agent in reach and unbinds the others,
// under the assignment locks.
func (s *KnowledgeService) writeBindings(ctx context.Context, workspaceID, serverID pgtype.UUID, agents []pgtype.UUID, reach map[string]bool) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin knowledge bindings: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	if _, err := q.LockWorkspaceForChatSessionCreate(ctx, workspaceID); err != nil {
		return fmt.Errorf("lock workspace: %w", err)
	}
	if _, err := q.LockWorkspaceMcpServerForShare(ctx, db.LockWorkspaceMcpServerForShareParams{ID: serverID, WorkspaceID: workspaceID}); err != nil {
		return fmt.Errorf("lock mcp server: %w", err)
	}
	for _, agent := range agents {
		if reach[util.UUIDToString(agent)] {
			if err := q.AddAgentMcpServer(ctx, db.AddAgentMcpServerParams{AgentID: agent, ServerID: serverID}); err != nil {
				return fmt.Errorf("bind restricted endpoint: %w", err)
			}
			continue
		}
		if _, err := q.RemoveAgentMcpServer(ctx, db.RemoveAgentMcpServerParams{AgentID: agent, ServerID: serverID}); err != nil {
			return fmt.Errorf("unbind restricted endpoint: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit knowledge bindings: %w", err)
	}
	return nil
}
