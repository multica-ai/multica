import { queryOptions } from "@tanstack/react-query";
import { api } from "@multica/core/api";

/**
 * Query keys for the skill ownership / versioning / change-request / fork
 * sub-resources (JEH-216). Kept under a `cerebro` prefix so they never collide
 * with the upstream `["workspaces", wsId, "skills", …]` cache, and so a single
 * `invalidateQueries({ queryKey: skillOwnershipKeys.skill(wsId, skillId) })`
 * refreshes every sub-resource of one skill at once.
 */
export const skillOwnershipKeys = {
  skill: (wsId: string, skillId: string) =>
    ["cerebro", "skill-ownership", wsId, skillId] as const,
  versions: (wsId: string, skillId: string) =>
    [...skillOwnershipKeys.skill(wsId, skillId), "versions"] as const,
  changeRequests: (wsId: string, skillId: string) =>
    [...skillOwnershipKeys.skill(wsId, skillId), "change-requests"] as const,
  forks: (wsId: string, skillId: string) =>
    [...skillOwnershipKeys.skill(wsId, skillId), "forks"] as const,
  forkParent: (wsId: string, skillId: string) =>
    [...skillOwnershipKeys.skill(wsId, skillId), "fork-parent"] as const,
};

export function skillVersionsOptions(wsId: string, skillId: string) {
  return queryOptions({
    queryKey: skillOwnershipKeys.versions(wsId, skillId),
    queryFn: () => api.listSkillVersions(skillId),
    enabled: !!wsId && !!skillId,
  });
}

export function skillChangeRequestsOptions(wsId: string, skillId: string) {
  return queryOptions({
    queryKey: skillOwnershipKeys.changeRequests(wsId, skillId),
    queryFn: () => api.listSkillChangeRequests(skillId),
    enabled: !!wsId && !!skillId,
  });
}

export function skillForksOptions(wsId: string, skillId: string) {
  return queryOptions({
    queryKey: skillOwnershipKeys.forks(wsId, skillId),
    queryFn: () => api.listSkillForks(skillId),
    enabled: !!wsId && !!skillId,
  });
}

/**
 * The skill this one was forked from ("forked from" lineage), or null when the
 * skill is an original. The client maps the backend 404 to null, so this query
 * resolves rather than errors for non-forks.
 */
export function skillForkParentOptions(wsId: string, skillId: string) {
  return queryOptions({
    queryKey: skillOwnershipKeys.forkParent(wsId, skillId),
    queryFn: () => api.getSkillForkParent(skillId),
    enabled: !!wsId && !!skillId,
  });
}
