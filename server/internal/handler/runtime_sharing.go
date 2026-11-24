package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

var errRuntimeManagedByConnection = errors.New("runtime managed by connection")

var errInvalidRuntimeShare = errors.New("invalid runtime sharing")

// errRuntimeFeatureLocked rejects an addition to runtime_member_sharing; the
// rejection is already recorded and the caller answers feature_locked.
var errRuntimeFeatureLocked = errors.New("runtime member sharing is not included in the plan")

const runtimeSharingOpAddMembers = "add_members"

// runtimeMemberSharingAdds reports whether moving a runtime's access from
// previous to next adds to the paid feature runtime_member_sharing (MUL-9491):
// next shares the private runtime with someone previous did not. Switching
// from "only me" or public to specific members is an addition; making it
// public, making it "only me", removing people, and re-saving the same list
// are not. Runtime claim and binding checks never consult the feature, so an
// existing list keeps working after a downgrade.
func runtimeMemberSharingAdds(previous, next db.AgentRuntime) bool {
	if next.Visibility != "private" {
		return false
	}
	var had []pgtype.UUID
	if previous.Visibility == "private" {
		had = previous.SharedWith
	}
	for _, id := range next.SharedWith {
		if id != next.OwnerID && !slices.Contains(had, id) {
			return true
		}
	}
	return false
}

// runtimeShareRequestMayAdd reports whether a request names any recipient, the
// only way it can add to runtime_member_sharing. Requests that cannot add skip
// reading the feature.
func runtimeShareRequestMayAdd(req UpdateAgentRuntimeRequest) bool {
	return len(req.sharedWithIDs) > 0
}

func parseRuntimeSharedWith(w http.ResponseWriter, raw []string) ([]pgtype.UUID, bool) {
	if len(raw) > 1000 {
		writeError(w, http.StatusBadRequest, "shared_with has too many members")
		return nil, false
	}
	ids := make([]pgtype.UUID, 0, len(raw))
	for _, value := range raw {
		id, ok := parseUUIDOrBadRequest(w, value, "shared_with")
		if !ok {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func runtimeAccessNarrows(previous, next db.AgentRuntime) bool {
	if next.Visibility == "public" {
		return false
	}
	if previous.Visibility == "public" {
		return true
	}
	for _, id := range previous.SharedWith {
		if id != next.OwnerID && !slices.Contains(next.SharedWith, id) {
			return true
		}
	}
	return false
}

func runtimeSharedWithStrings(ids []pgtype.UUID) []string {
	result := make([]string, len(ids))
	for i, id := range ids {
		result[i] = uuidToString(id)
	}
	return result
}

func sameRuntimeAccess(a, b db.AgentRuntime) bool {
	return a.Visibility == b.Visibility && slices.Equal(a.SharedWith, b.SharedWith)
}

// Resolve PATCH fields from the locked row. Explicit private/public requests
// without a recipient list clear grants, preserving old clients' privacy action.
func resolveRuntimeAccess(ctx context.Context, q *db.Queries, rt db.AgentRuntime, req UpdateAgentRuntimeRequest) (db.AgentRuntime, error) {
	next := rt
	if req.Visibility != nil {
		next.Visibility = *req.Visibility
		next.SharedWith = []pgtype.UUID{}
	}
	if req.SharedWith != nil {
		next.SharedWith = []pgtype.UUID{}
		for _, id := range req.sharedWithIDs {
			if id == rt.OwnerID || slices.Contains(next.SharedWith, id) {
				continue
			}
			next.SharedWith = append(next.SharedWith, id)
		}
		sort.Slice(next.SharedWith, func(i, j int) bool { return uuidToString(next.SharedWith[i]) < uuidToString(next.SharedWith[j]) })
	}
	if next.Visibility == "public" && len(next.SharedWith) > 0 {
		return next, fmt.Errorf("%w: shared_with requires private visibility", errInvalidRuntimeShare)
	}
	if len(next.SharedWith) > 0 {
		members, err := q.LockRuntimeShareMembers(ctx, db.LockRuntimeShareMembersParams{WorkspaceID: rt.WorkspaceID, UserIds: next.SharedWith})
		if err != nil {
			return next, err
		}
		if len(members) != len(next.SharedWith) {
			return next, fmt.Errorf("%w: every recipient must be a workspace member", errInvalidRuntimeShare)
		}
	}
	return next, nil
}

// Both explicit access changes and workspace departure use the same lock order
// and cancellation settlement, inside their caller's transaction.
func lockRuntimeRevocationPlan(ctx context.Context, qtx *db.Queries, next db.AgentRuntime) (runtimeWorkspaceAccessRevocationPlan, error) {
	agents, err := qtx.ListNonOwnerAgentsByRuntimeForUpdate(ctx, db.ListNonOwnerAgentsByRuntimeForUpdateParams{
		RuntimeID: next.ID, RuntimeOwnerID: next.OwnerID, SharedWith: next.SharedWith,
	})
	if err != nil {
		return runtimeWorkspaceAccessRevocationPlan{}, err
	}
	if _, err := qtx.LockRuntimeRevocationChatSessions(ctx, db.LockRuntimeRevocationChatSessionsParams{
		RuntimeID: next.ID, RuntimeOwnerID: next.OwnerID, SharedWith: next.SharedWith,
	}); err != nil {
		return runtimeWorkspaceAccessRevocationPlan{}, err
	}
	tasks, err := qtx.ListNonOwnerActiveTasksByRuntimeForUpdate(ctx, db.ListNonOwnerActiveTasksByRuntimeForUpdateParams{
		RuntimeID: next.ID, RuntimeOwnerID: next.OwnerID, SharedWith: next.SharedWith,
	})
	return runtimeWorkspaceAccessRevocationPlan{Agents: agents, Tasks: tasks}, err
}

func applyRuntimeAccessRevocation(ctx context.Context, qtx *db.Queries, next db.AgentRuntime) (db.AgentRuntime, service.RuntimeTeardownResult, []pgtype.UUID, error) {
	var result service.RuntimeTeardownResult
	updated, err := qtx.UpdateAgentRuntimeVisibility(ctx, db.UpdateAgentRuntimeVisibilityParams{
		ID: next.ID, Visibility: next.Visibility, SharedWith: next.SharedWith,
	})
	if err != nil {
		return updated, result, nil, err
	}
	result.CancelledTasks, err = qtx.CancelNonOwnerTasksByRuntime(ctx, db.CancelNonOwnerTasksByRuntimeParams{
		RuntimeID: next.ID, RuntimeOwnerID: next.OwnerID, SharedWith: next.SharedWith,
		FailureReason: string(taskfailure.ReasonRuntimeAccessRevoked),
	})
	if err != nil {
		return updated, result, nil, err
	}
	readyChats, err := service.PrepareRuntimeAccessCancellation(ctx, qtx, result.CancelledTasks)
	if err != nil {
		return updated, result, nil, err
	}
	result.UnboundAgents, err = qtx.UnbindNonOwnerAgentsFromRuntime(ctx, db.UnbindNonOwnerAgentsFromRuntimeParams{
		RuntimeID: next.ID, RuntimeOwnerID: next.OwnerID, SharedWith: next.SharedWith,
	})
	if err != nil {
		return updated, result, nil, err
	}
	ids := make([]pgtype.UUID, len(result.UnboundAgents))
	for i, a := range result.UnboundAgents {
		ids[i] = a.ID
	}
	result.PausedAutopilots, err = qtx.PauseAutopilotsByUnboundAgents(ctx, ids)
	return updated, result, readyChats, err
}
