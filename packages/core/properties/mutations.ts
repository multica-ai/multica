import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { propertyKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import {
  invalidatePropertyWindowQueries,
  onIssuePropertiesChanged,
  patchIssueProperties,
} from "../issues/ws-updaters";
import { findIssueLocation } from "../issues/cache-helpers";
import type { IssueFlatCache } from "../issues/cache-coordinator";
import type {
  CreatePropertyRequest,
  UpdatePropertyRequest,
  Issue,
  IssuePropertyValue,
  IssuePropertyValues,
  ListIssuesCache,
} from "../types";

export function useCreateProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreatePropertyRequest) => api.createProperty(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
    },
  });
}

/**
 * Definition updates (rename, options, archive). No optimistic patch: edits
 * happen in the settings dialog where a round-trip is acceptable, and config
 * canonicalization (option id assignment) is server-side anyway.
 */
export function useUpdateProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdatePropertyRequest) =>
      api.updateProperty(id, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
      // Issue caches embed the value bag; a definition change (rename,
      // option edits) changes how values render, and rows referencing an
      // archived definition must drop out of "+ Add property" menus.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

/**
 * Read the issue's current property bag from whichever cache holds it: the
 * detail cache first, then any list cache bucket. Board/list surfaces never
 * populate the detail cache, so stopping there would make the optimistic
 * merge below overwrite the whole bag with a single key.
 */
function readIssueProperties(qc: ReturnType<typeof useQueryClient>, wsId: string, issueId: string): IssuePropertyValues | undefined {
  const detail = qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId));
  if (detail) return detail.properties ?? {};
  for (const [, data] of qc.getQueriesData<ListIssuesCache>({ queryKey: issueKeys.list(wsId) })) {
    if (!data) continue;
    const location = findIssueLocation(data, issueId);
    if (location) return location.issue.properties ?? {};
  }
  for (const [, data] of qc.getQueriesData<IssueFlatCache>({
    queryKey: issueKeys.flatAll(wsId),
  })) {
    for (const page of data?.pages ?? []) {
      const issue = page.issues.find((candidate) => candidate.id === issueId);
      if (issue) return issue.properties ?? {};
    }
  }
  for (const [, data] of qc.getQueriesData<Issue[]>({
    queryKey: issueKeys.childrenAll(wsId),
  })) {
    const issue = data?.find((candidate) => candidate.id === issueId);
    if (issue) return issue.properties ?? {};
  }
  return undefined;
}

async function cancelIssuePropertyMutationQueries(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  issueId: string,
) {
  await Promise.all([
    qc.cancelQueries({ queryKey: issueKeys.detail(wsId, issueId) }),
    qc.cancelQueries({ queryKey: issueKeys.list(wsId) }),
    qc.cancelQueries({ queryKey: issueKeys.flatAll(wsId) }),
    qc.cancelQueries({ queryKey: issueKeys.childrenAll(wsId) }),
    qc.cancelQueries({ queryKey: issueKeys.childrenByParentsAll(wsId) }),
  ]);
}

/**
 * Optimistic single-property write on an issue.
 *
 * Concurrency contract (MUL-4463 review round 1/2):
 *   - Mutations for the SAME issue serialize via TanStack's mutation
 *     `scope`, so full-bag responses cannot land out of order and rapid
 *     multi-select toggles cannot interleave.
 *   - The optimistic patch merges into the bag read from ANY cache
 *     (detail or list) — never replaces it — so board-only surfaces keep
 *     the issue's other property values.
 *   - Failure restores the snapshot; if no snapshot existed, it falls back
 *     to invalidation.
 *   - The LAST settled mutation for the issue does an authoritative
 *     invalidate of the detail cache plus the definition catalog (usage
 *     counts), reconciling any raced WS snapshots under staleTime:Infinity.
 */
export function useSetIssueProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ issueId, propertyId, value }: { issueId: string; propertyId: string; value: IssuePropertyValue }) =>
      api.setIssueProperty(issueId, propertyId, value),
    scope: { id: `issue-properties:${wsId}` },
    mutationKey: ["issue-properties", wsId],
    onMutate: async ({ issueId, propertyId, value }) => {
      // A response snapshotted before this write must not land after the
      // optimistic patch and revert any denormalized issue projection.
      await cancelIssuePropertyMutationQueries(qc, wsId, issueId);
      const prev = readIssueProperties(qc, wsId, issueId);
      patchIssueProperties(qc, wsId, issueId, { ...(prev ?? {}), [propertyId]: value });
      return { prevValue: prev?.[propertyId], hadBag: prev !== undefined, issueId, propertyId };
    },
    onError: (_err, _vars, ctx) => {
      if (!ctx) return;
      rollbackSingleKey(qc, wsId, ctx);
    },
    onSuccess: (data, { issueId }) => {
      onIssuePropertiesChanged(qc, wsId, issueId, data.properties ?? {});
    },
    onSettled: (_data, _err, { issueId }) => {
      settleIssuePropertyCaches(qc, wsId, issueId);
    },
  });
}

export function useUnsetIssueProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ issueId, propertyId }: { issueId: string; propertyId: string }) =>
      api.unsetIssueProperty(issueId, propertyId),
    scope: { id: `issue-properties:${wsId}` },
    mutationKey: ["issue-properties", wsId],
    onMutate: async ({ issueId, propertyId }) => {
      await cancelIssuePropertyMutationQueries(qc, wsId, issueId);
      const prev = readIssueProperties(qc, wsId, issueId);
      if (prev) {
        const next = { ...prev };
        delete next[propertyId];
        patchIssueProperties(qc, wsId, issueId, next);
      }
      return { prevValue: prev?.[propertyId], hadBag: prev !== undefined, issueId, propertyId };
    },
    onError: (_err, _vars, ctx) => {
      if (!ctx) return;
      rollbackSingleKey(qc, wsId, ctx);
    },
    onSuccess: (data, { issueId }) => {
      onIssuePropertiesChanged(qc, wsId, issueId, data.properties ?? {});
    },
    onSettled: (_data, _err, { issueId }) => {
      settleIssuePropertyCaches(qc, wsId, issueId);
    },
  });
}

/**
 * Roll back exactly the key this mutation touched, against the CURRENT bag.
 * Restoring the whole onMutate snapshot would erase concurrent remote writes
 * to other keys that landed via WS while this request was in flight
 * (clean-room review F2 interleave B).
 */
function rollbackSingleKey(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  ctx: { prevValue: IssuePropertyValue | undefined; hadBag: boolean; issueId: string; propertyId: string },
) {
  if (!ctx.hadBag) {
    qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, ctx.issueId) });
    return;
  }
  const current = readIssueProperties(qc, wsId, ctx.issueId) ?? {};
  const next = { ...current };
  if (ctx.prevValue === undefined) delete next[ctx.propertyId];
  else next[ctx.propertyId] = ctx.prevValue;
  patchIssueProperties(qc, wsId, ctx.issueId, next);
}

/** Authoritative reconcile once the LAST in-flight property write settles. */
function settleIssuePropertyCaches(qc: ReturnType<typeof useQueryClient>, wsId: string, issueId: string) {
  if (qc.isMutating({ mutationKey: ["issue-properties", wsId] }) > 1) return;
  qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
  qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
  // Server-filtered/-sorted windows must refetch: the write may have moved
  // this issue across pages or reordered it (onIssuePropertiesChanged
  // already fires this for the WS path; local mutations need it on settle
  // because the self-emitted WS event may be suppressed or raced).
  invalidatePropertyWindowQueries(qc, wsId);
}
