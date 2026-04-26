import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { linkKeys } from "./queries";
import { issueKeys } from "../issues/queries";
import { useWorkspaceId } from "../hooks";
import type { Issue, IssueLink, LinkType } from "../types";

/**
 * Create an issue link.
 *
 * The mutation accepts the optimistic "snapshot" inputs the picker has on
 * hand (target identifier, title, status, etc.) so we can render the new
 * chip immediately without waiting for the round-trip. When the server
 * responds we replace the optimistic entry with the real one — the entry
 * is matched by pair_id once the server returns.
 *
 * No workspace-level link CRUD exists, so cache management is much
 * simpler than labels: we patch the source issue's `links` array in
 * detail caches and invalidate the per-issue links query.
 */
export function useCreateIssueLink() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: ({
      issueId,
      targetIssueId,
      linkType,
    }: {
      issueId: string;
      targetIssueId: string;
      linkType: LinkType;
      // Optimistic snapshot fields — used only for client-side optimism.
      optimisticTarget?: {
        identifier: string;
        title: string;
        status: string;
        number: number;
        workspace_id: string;
        workspace_name: string;
        workspace_slug: string;
      };
    }) =>
      api.createIssueLink(issueId, wsId, {
        target_issue_id: targetIssueId,
        link_type: linkType,
      }),

    onSuccess: (newLink, vars) => {
      // Drop the optimistic placeholder and replace with the server row.
      patchSourceLinks(qc, wsId, vars.issueId, (old) => {
        const withoutOptimistic = old.filter((l) => !l.id.startsWith("optimistic-"));
        return [...withoutOptimistic, newLink];
      });
      // Mirror row also lives on the target — invalidate that issue's caches
      // so its incoming list refreshes if it's currently mounted.
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, vars.targetIssueId) });
      qc.invalidateQueries({
        queryKey: linkKeys.forIssue(wsId, vars.targetIssueId),
      });
      // Source issue's blockers list may have changed.
      qc.invalidateQueries({
        queryKey: linkKeys.blockersForIssue(wsId, vars.issueId),
      });
    },

    onError: (_err, vars) => {
      // Roll back optimistic entry.
      patchSourceLinks(qc, wsId, vars.issueId, (old) =>
        old.filter((l) => !l.id.startsWith("optimistic-")),
      );
    },
  });
}

/**
 * Delete an issue link. We don't know the target issue id from the link id
 * alone before the call, so we read it from the cached detail before
 * mutating; this lets us roll back if the request fails.
 */
export function useDeleteIssueLink() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: ({ issueId, linkId }: { issueId: string; linkId: string }) =>
      api.deleteIssueLink(issueId, linkId, wsId),

    onMutate: ({ issueId, linkId }) => {
      const detail = qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId));
      const removed = detail?.links?.find((l) => l.id === linkId);

      // Optimistic remove.
      patchSourceLinks(qc, wsId, issueId, (old) =>
        old.filter((l) => l.id !== linkId),
      );

      return { removed };
    },

    onSuccess: (_void, vars, ctx) => {
      // The mirror row on the other side also vanished — refresh that issue's
      // caches if we know who it was.
      const targetId = ctx?.removed?.target_issue_id;
      if (targetId) {
        qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, targetId) });
        qc.invalidateQueries({ queryKey: linkKeys.forIssue(wsId, targetId) });
      }
      qc.invalidateQueries({
        queryKey: linkKeys.blockersForIssue(wsId, vars.issueId),
      });
    },

    onError: (_err, vars, ctx) => {
      // Roll back: re-insert the optimistically removed link if we had one.
      if (ctx?.removed) {
        const restore = ctx.removed;
        patchSourceLinks(qc, wsId, vars.issueId, (old) => [...old, restore]);
      }
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, vars.issueId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Cache helpers
// ---------------------------------------------------------------------------

/** Apply a transform to the `links` array on the source issue's detail cache. */
function patchSourceLinks(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  issueId: string,
  transform: (current: IssueLink[]) => IssueLink[],
) {
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? { ...old, links: transform(old.links ?? []) } : old,
  );
}
