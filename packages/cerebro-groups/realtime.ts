import type { QueryClient } from "@tanstack/react-query";
import type { WSClient } from "@multica/core/api/ws-client";
import { getCurrentWsId } from "@multica/core/platform";
import { groupKeys } from "./queries";

// Server emits group:created / group:updated / group:deleted /
// group:member_added / group:member_removed (see
// server/internal/cerebro/groups/service.go), plus the JEH-1009
// permission events: group:capability_changed, group:runtime_changed,
// group:agent_changed, and project:group_access_changed. We treat them
// all as invalidations — the list/member queries are cheap to refetch
// and the data flow is "settings UI is open" so the round-trip cost is
// acceptable.
//
// Returns a teardown function that unsubscribes every handler.
export function registerCerebroGroupHandlers(
  ws: WSClient,
  qc: QueryClient,
): () => void {
  const invalidateGroupList = () => {
    const wsId = getCurrentWsId();
    if (!wsId) return;
    qc.invalidateQueries({ queryKey: groupKeys.list(wsId) });
  };

  const invalidateMembers = (groupId: string | undefined) => {
    const wsId = getCurrentWsId();
    if (!wsId || !groupId) return;
    qc.invalidateQueries({ queryKey: groupKeys.members(wsId, groupId) });
  };

  const invalidateGroupPermissionRows = (
    groupId: string | undefined,
    kind: "capabilities" | "runtimes" | "agents",
  ) => {
    const wsId = getCurrentWsId();
    if (!wsId || !groupId) return;
    qc.invalidateQueries({ queryKey: groupKeys[kind](wsId, groupId) });
  };

  const invalidateProjectGroups = (projectId: string | undefined) => {
    const wsId = getCurrentWsId();
    if (!wsId || !projectId) return;
    qc.invalidateQueries({
      queryKey: groupKeys.projectGroups(wsId, projectId),
    });
  };

  const unsubCreated = ws.on("group:created", invalidateGroupList);
  const unsubUpdated = ws.on("group:updated", invalidateGroupList);
  const unsubDeleted = ws.on("group:deleted", () => {
    invalidateGroupList();
    // The detail/members caches for the deleted group are stale; let them
    // expire naturally — there is no enumeration of group ids to walk.
  });
  const unsubMemberAdded = ws.on("group:member_added", (p) => {
    const payload = p as { group_id?: string };
    invalidateMembers(payload?.group_id);
  });
  const unsubMemberRemoved = ws.on("group:member_removed", (p) => {
    const payload = p as { group_id?: string };
    invalidateMembers(payload?.group_id);
  });
  const unsubCapability = ws.on("group:capability_changed", (p) => {
    const payload = p as { group_id?: string };
    invalidateGroupPermissionRows(payload?.group_id, "capabilities");
  });
  const unsubRuntimeAccess = ws.on("group:runtime_changed", (p) => {
    const payload = p as { group_id?: string };
    invalidateGroupPermissionRows(payload?.group_id, "runtimes");
  });
  const unsubAgentAccess = ws.on("group:agent_changed", (p) => {
    const payload = p as { group_id?: string };
    invalidateGroupPermissionRows(payload?.group_id, "agents");
  });
  const unsubProjectGroup = ws.on("project:group_access_changed", (p) => {
    const payload = p as { project_id?: string };
    invalidateProjectGroups(payload?.project_id);
  });

  return () => {
    unsubCreated();
    unsubUpdated();
    unsubDeleted();
    unsubMemberAdded();
    unsubMemberRemoved();
    unsubCapability();
    unsubRuntimeAccess();
    unsubAgentAccess();
    unsubProjectGroup();
  };
}
