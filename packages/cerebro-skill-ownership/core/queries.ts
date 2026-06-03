import { queryOptions } from "@tanstack/react-query";
import { api } from "@multica/core/api";

export const skillOwnershipKeys = {
  all: (skillId: string) => ["cerebro", "skill-ownership", skillId] as const,
  versions: (skillId: string) =>
    [...skillOwnershipKeys.all(skillId), "versions"] as const,
  changeRequests: (skillId: string) =>
    [...skillOwnershipKeys.all(skillId), "change-requests"] as const,
  pendingAll: () => ["cerebro", "skill-ownership", "pending"] as const,
  forks: (skillId: string) =>
    [...skillOwnershipKeys.all(skillId), "forks"] as const,
};

export function skillVersionsOptions(skillId: string) {
  return queryOptions({
    queryKey: skillOwnershipKeys.versions(skillId),
    queryFn: () => api.listSkillVersions(skillId),
    enabled: !!skillId,
    staleTime: 60 * 1000,
  });
}

export function skillChangeRequestsOptions(skillId: string) {
  return queryOptions({
    queryKey: skillOwnershipKeys.changeRequests(skillId),
    queryFn: () => api.listSkillChangeRequests(skillId),
    enabled: !!skillId,
    staleTime: 30 * 1000,
  });
}

export function pendingSkillChangeRequestsOptions() {
  return queryOptions({
    queryKey: skillOwnershipKeys.pendingAll(),
    queryFn: () => api.listPendingSkillChangeRequests(),
    staleTime: 30 * 1000,
  });
}

export function skillForksOptions(skillId: string) {
  return queryOptions({
    queryKey: skillOwnershipKeys.forks(skillId),
    queryFn: () => api.listSkillForks(skillId),
    enabled: !!skillId,
    staleTime: 60 * 1000,
  });
}
