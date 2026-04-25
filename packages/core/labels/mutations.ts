import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { labelKeys } from "./queries";
import { issueKeys } from "../issues/queries";
import { patchIssueInBuckets } from "../issues/cache-helpers";
import { useWorkspaceId } from "../hooks";
import type { Issue, IssueLabel, LabelColor, ListIssuesCache } from "../types";

// ---------------------------------------------------------------------------
// Workspace-level label CRUD (members only — agents get 403 on POST/PATCH/DELETE)
// ---------------------------------------------------------------------------

export function useCreateLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: { name: string; color: LabelColor }) =>
      api.createLabel(wsId, data),
    onSuccess: (newLabel) => {
      qc.setQueryData<IssueLabel[]>(labelKeys.list(wsId), (old) => {
        if (!old) return [newLabel];
        if (old.some((l) => l.id === newLabel.id)) return old;
        // Maintain alphabetical order to match server ordering.
        return [...old, newLabel].sort((a, b) =>
          a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
        );
      });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.list(wsId) });
    },
  });
}

export function useUpdateLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      id,
      ...data
    }: { id: string; name?: string; color?: LabelColor }) =>
      api.updateLabel(wsId, id, data),
    onMutate: ({ id, ...data }) => {
      qc.cancelQueries({ queryKey: labelKeys.list(wsId) });
      const prev = qc.getQueryData<IssueLabel[]>(labelKeys.list(wsId));
      qc.setQueryData<IssueLabel[]>(labelKeys.list(wsId), (old) =>
        old ? old.map((l) => (l.id === id ? { ...l, ...data } : l)) : old,
      );
      // Patch attached label fragments inside any cached issue so chips update
      // immediately on board/list/detail.
      patchLabelAcrossIssueCaches(qc, wsId, id, data);
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(labelKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useDeleteLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteLabel(wsId, id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: labelKeys.list(wsId) });
      const prev = qc.getQueryData<IssueLabel[]>(labelKeys.list(wsId));
      qc.setQueryData<IssueLabel[]>(labelKeys.list(wsId), (old) =>
        old ? old.filter((l) => l.id !== id) : old,
      );
      // Strip the deleted label from every cached issue.
      removeLabelAcrossIssueCaches(qc, wsId, id);
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(labelKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Issue-level attach/detach (members + agents both allowed)
// ---------------------------------------------------------------------------

/** Attach a label to an issue. Server returns the full updated label list. */
export function useAttachIssueLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ issueId, labelId }: { issueId: string; labelId: string }) =>
      api.attachIssueLabel(issueId, labelId, wsId),
    onMutate: async ({ issueId, labelId }) => {
      await qc.cancelQueries({ queryKey: issueKeys.detail(wsId, issueId) });
      const labels = qc.getQueryData<IssueLabel[]>(labelKeys.list(wsId)) ?? [];
      const label = labels.find((l) => l.id === labelId);
      if (!label) return;
      // Optimistic — append unless already present.
      patchIssueLabelsAcrossCaches(qc, wsId, issueId, (current) =>
        current.some((l) => l.id === labelId)
          ? current
          : [...current, label].sort((a, b) =>
              a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
            ),
      );
    },
    onSuccess: (resp, vars) => {
      // Server-confirmed list — overwrite cache so we drop any stale optimistic state.
      patchIssueLabelsAcrossCaches(qc, wsId, vars.issueId, () => resp.labels);
    },
    onError: (_err, vars) => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, vars.issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
    },
  });
}

export function useDetachIssueLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ issueId, labelId }: { issueId: string; labelId: string }) =>
      api.detachIssueLabel(issueId, labelId, wsId),
    onMutate: ({ issueId, labelId }) => {
      patchIssueLabelsAcrossCaches(qc, wsId, issueId, (current) =>
        current.filter((l) => l.id !== labelId),
      );
    },
    onError: (_err, vars) => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, vars.issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Cache patching helpers
// ---------------------------------------------------------------------------

/**
 * Walk every issue cache (workspace list, my-issues lists, and detail caches)
 * and replace the labels on the given issue using `transform`.
 */
function patchIssueLabelsAcrossCaches(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  issueId: string,
  transform: (current: IssueLabel[]) => IssueLabel[],
) {
  // Detail cache
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, labels: transform(old.labels ?? []) } : old,
  );
  // Workspace list (bucketed) + every my-issues bucket cache.
  for (const queryKey of [
    issueKeys.list(wsId),
    ...findMyIssueListKeys(qc, wsId),
  ]) {
    qc.setQueryData<ListIssuesCache>(queryKey, (old) => {
      if (!old) return old;
      const loc = findIssue(old, issueId);
      if (!loc) return old;
      return patchIssueInBuckets(old, issueId, {
        labels: transform(loc.labels ?? []),
      });
    });
  }
}

/** Apply a label-fragment patch to every issue across every cache that has it attached. */
function patchLabelAcrossIssueCaches(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  labelId: string,
  patch: Partial<IssueLabel>,
) {
  const apply = (labels: IssueLabel[]) =>
    labels.map((l) => (l.id === labelId ? { ...l, ...patch } : l));

  // Detail caches — we must scan all issue detail caches in this workspace.
  forEachDetailCache(qc, wsId, (issue) => {
    if (issue.labels?.some((l) => l.id === labelId)) {
      qc.setQueryData<Issue>(issueKeys.detail(wsId, issue.id), {
        ...issue,
        labels: apply(issue.labels),
      });
    }
  });

  // List caches — walk buckets in place.
  for (const queryKey of [
    issueKeys.list(wsId),
    ...findMyIssueListKeys(qc, wsId),
  ]) {
    qc.setQueryData<ListIssuesCache>(queryKey, (old) => {
      if (!old) return old;
      let next = old;
      for (const status of Object.keys(old.byStatus) as (keyof typeof old.byStatus)[]) {
        const bucket = old.byStatus[status];
        if (!bucket) continue;
        for (const issue of bucket.issues) {
          if (issue.labels?.some((l) => l.id === labelId)) {
            next = patchIssueInBuckets(next, issue.id, { labels: apply(issue.labels) });
          }
        }
      }
      return next;
    });
  }
}

/** Strip the deleted label out of every cached issue. */
function removeLabelAcrossIssueCaches(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  labelId: string,
) {
  const strip = (labels: IssueLabel[]) => labels.filter((l) => l.id !== labelId);

  forEachDetailCache(qc, wsId, (issue) => {
    if (issue.labels?.some((l) => l.id === labelId)) {
      qc.setQueryData<Issue>(issueKeys.detail(wsId, issue.id), {
        ...issue,
        labels: strip(issue.labels),
      });
    }
  });

  for (const queryKey of [
    issueKeys.list(wsId),
    ...findMyIssueListKeys(qc, wsId),
  ]) {
    qc.setQueryData<ListIssuesCache>(queryKey, (old) => {
      if (!old) return old;
      let next = old;
      for (const status of Object.keys(old.byStatus) as (keyof typeof old.byStatus)[]) {
        const bucket = old.byStatus[status];
        if (!bucket) continue;
        for (const issue of bucket.issues) {
          if (issue.labels?.some((l) => l.id === labelId)) {
            next = patchIssueInBuckets(next, issue.id, { labels: strip(issue.labels) });
          }
        }
      }
      return next;
    });
  }
}

/** Find an issue inside a bucketed cache without rebuilding it. */
function findIssue(cache: ListIssuesCache, issueId: string): Issue | null {
  for (const status of Object.keys(cache.byStatus) as (keyof typeof cache.byStatus)[]) {
    const bucket = cache.byStatus[status];
    if (!bucket) continue;
    const issue = bucket.issues.find((i) => i.id === issueId);
    if (issue) return issue;
  }
  return null;
}

/** Walk every cached `[issues, wsId, detail, *]` cache. */
function forEachDetailCache(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  fn: (issue: Issue) => void,
) {
  const cache = qc.getQueryCache();
  cache.findAll({ queryKey: ["issues", wsId, "detail"] }).forEach((q) => {
    const data = q.state.data as Issue | undefined;
    if (data) fn(data);
  });
}

/** Find every cached `[issues, wsId, my, ...]` list key currently in cache. */
function findMyIssueListKeys(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
) {
  return qc
    .getQueryCache()
    .findAll({ queryKey: issueKeys.myAll(wsId) })
    .map((q) => q.queryKey);
}
