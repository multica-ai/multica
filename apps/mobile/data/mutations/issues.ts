/**
 * Comment creation mutation. Mirrors the optimistic + invalidate pattern of
 * apps/mobile/data/mutations/inbox.ts:17 — operates on the flat
 * `TimelineEntry[]` timeline cache (ASC, oldest first; server-side
 * pagination was dropped in #2322).
 *
 * Optimistic strategy:
 *   - Cancel timeline refetches.
 *   - Snapshot the current cache.
 *   - Append a synthetic comment-typed TimelineEntry to the end of the list
 *     (newest position, since the array is ASC).
 *   - On error: roll back to the snapshot.
 *   - On settled: invalidate so the server's real comment row replaces the
 *     synthetic one (real id, real created_at).
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  CreateIssueRequest,
  Issue,
  IssueReaction,
  Label,
  Reaction,
  TimelineEntry,
  UpdateIssueRequest,
} from "@multica/core/types";
import { api } from "@/data/api";
import { issueKeys } from "@/data/queries/issues";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";

export type ToggleCommentReactionVars = {
  commentId: string;
  emoji: string;
  /** Pass the existing Reaction from the entry to indicate this is a remove.
   *  Undefined means "add". Mirrors web's ToggleCommentReactionVars shape so
   *  call sites stay portable across clients. */
  existing?: Reaction;
};

export type ToggleIssueReactionVars = {
  emoji: string;
  /** See above. */
  existing?: IssueReaction;
};

export type CreateCommentVars = {
  content: string;
  /** When set, the new comment is a threaded reply to this comment id. */
  parentId?: string;
};

export function useCreateComment(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationFn: ({ content, parentId }: CreateCommentVars) =>
      api.createComment(issueId, content, parentId),
    onMutate: async ({ content, parentId }) => {
      const key = issueKeys.timeline(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      if (!userId) return { prev, key };

      const optimistic: TimelineEntry = {
        type: "comment",
        id: `optimistic-${Date.now()}`,
        actor_type: "member",
        actor_id: userId,
        content,
        parent_id: parentId ?? null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        comment_type: "comment",
        reactions: [],
        attachments: [],
      };

      // ASC list: new comment goes to the end (newest position on screen).
      qc.setQueryData<TimelineEntry[]>(key, (old) =>
        old ? [...old, optimistic] : [optimistic],
      );

      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: issueKeys.timeline(wsId, issueId),
      });
    },
  });
}

/**
 * Toggle a reaction on a comment. Cache target is the timeline infinite
 * query — comment reactions ride on `TimelineEntry.reactions[]` inside each
 * page.entries, so no separate query is involved.
 *
 * Optimistic strategy mirrors useCreateComment: cancel → snapshot → mutate
 * cache → on error rollback → on settle invalidate (so the synthetic
 * reaction id is replaced by the server's real id).
 *
 * Argument shape mirrors web's `ToggleCommentReactionVars` so the eventual
 * migration to the web pattern (useMutationState-derived optimistic state,
 * once WS lands) does not require changing trigger code.
 */
export function useToggleCommentReaction(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationKey: ["toggleCommentReaction", issueId] as const,
    mutationFn: async ({
      commentId,
      emoji,
      existing,
    }: ToggleCommentReactionVars) => {
      if (existing) {
        await api.removeReaction(commentId, emoji);
        return null;
      }
      return api.addReaction(commentId, emoji);
    },
    onMutate: async ({ commentId, emoji, existing }) => {
      const key = issueKeys.timeline(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      if (!userId) return { prev, key };

      qc.setQueryData<TimelineEntry[]>(key, (old) => {
        if (!old) return old;
        return old.map((entry) => {
          if (entry.id !== commentId) return entry;
          const reactions = entry.reactions ?? [];
          if (existing) {
            return {
              ...entry,
              reactions: reactions.filter((r) => r.id !== existing.id),
            };
          }
          const optimistic: Reaction = {
            id: `optimistic-${emoji}-${Date.now()}`,
            comment_id: commentId,
            actor_type: "member",
            actor_id: userId,
            emoji,
            created_at: new Date().toISOString(),
          };
          return { ...entry, reactions: [...reactions, optimistic] };
        });
      });
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
    },
  });
}

/**
 * Toggle a reaction on the issue itself. Cache target is the issue detail
 * query — Issue.reactions is an optional array on the Issue object.
 *
 * Mobile reads issue reactions directly off the detail cache (no separate
 * query like web's issueReactionsOptions). Single source of truth, less
 * code, fewer requests.
 */
export function useToggleIssueReaction(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationKey: ["toggleIssueReaction", issueId] as const,
    mutationFn: async ({ emoji, existing }: ToggleIssueReactionVars) => {
      if (existing) {
        await api.removeIssueReaction(issueId, emoji);
        return null;
      }
      return api.addIssueReaction(issueId, emoji);
    },
    onMutate: async ({ emoji, existing }) => {
      const key = issueKeys.detail(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Issue>(key);
      if (!userId || !prev) return { prev, key };

      const reactions = prev.reactions ?? [];
      let nextReactions: IssueReaction[];
      if (existing) {
        nextReactions = reactions.filter((r) => r.id !== existing.id);
      } else {
        const optimistic: IssueReaction = {
          id: `optimistic-${emoji}-${Date.now()}`,
          issue_id: issueId,
          actor_type: "member",
          actor_id: userId,
          emoji,
          created_at: new Date().toISOString(),
        };
        nextReactions = [...reactions, optimistic];
      }
      qc.setQueryData<Issue>(key, { ...prev, reactions: nextReactions });
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
    },
  });
}

/**
 * Update an issue's editable fields (status / priority / assignee / due_date /
 * project_id / etc). Optimistic merge into the detail cache; settle invalidates
 * the my-issues list so a status change re-buckets the SectionList in
 * (tabs)/my-issues.tsx automatically.
 *
 * Mobile cache is flat `Issue[]` (not bucketed `byStatus`), so we DON'T mirror
 * web's `patchIssueInBuckets` rebalancing — settling via `invalidate` is
 * cheaper and produces the same end state.
 */
export function useUpdateIssue(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["updateIssue", issueId] as const,
    mutationFn: (patch: UpdateIssueRequest) => api.updateIssue(issueId, patch),
    onMutate: async (patch) => {
      const key = issueKeys.detail(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Issue>(key);
      if (prev) {
        qc.setQueryData<Issue>(key, { ...prev, ...patch });
      }
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (server) => {
      qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), server);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
    },
  });
}

/**
 * Attach a label to the issue. Caller already has the full Label object
 * from the picker, so we don't need to fetch it. Optimistic append to
 * `issue.labels[]`; on success, replace with server-returned full array
 * (handles ordering / dup safety).
 */
export function useAttachLabel(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["attachLabel", issueId] as const,
    mutationFn: ({ label }: { label: Label }) =>
      api.attachLabel(issueId, label.id),
    onMutate: async ({ label }) => {
      const key = issueKeys.detail(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Issue>(key);
      if (prev) {
        const existing = prev.labels ?? [];
        // Skip dup append — the optimistic case must be idempotent because
        // the picker can fire twice on rapid taps before the request lands.
        if (!existing.some((l) => l.id === label.id)) {
          qc.setQueryData<Issue>(key, {
            ...prev,
            labels: [...existing, label],
          });
        }
      }
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (server) => {
      const key = issueKeys.detail(wsId, issueId);
      const current = qc.getQueryData<Issue>(key);
      if (current) {
        qc.setQueryData<Issue>(key, { ...current, labels: server.labels });
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
    },
  });
}

/** Detach a label. Mirror of useAttachLabel — same invalidation surface. */
export function useDetachLabel(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["detachLabel", issueId] as const,
    mutationFn: ({ labelId }: { labelId: string }) =>
      api.detachLabel(issueId, labelId),
    onMutate: async ({ labelId }) => {
      const key = issueKeys.detail(wsId, issueId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Issue>(key);
      if (prev) {
        const existing = prev.labels ?? [];
        qc.setQueryData<Issue>(key, {
          ...prev,
          labels: existing.filter((l) => l.id !== labelId),
        });
      }
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (server) => {
      const key = issueKeys.detail(wsId, issueId);
      const current = qc.getQueryData<Issue>(key);
      if (current) {
        qc.setQueryData<Issue>(key, { ...current, labels: server.labels });
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
    },
  });
}

/**
 * Issue creation mutation. No optimistic insert — the my-issues list is
 * status-bucketed + scope-filtered (assigned/created/agents), so optimism
 * needs to decide which bucket + scope the row lands in, with rollback.
 * Invalidation is simpler and the hosted server returns in <300ms.
 *
 * Invalidates:
 *  - issueKeys.myAll(wsId)        my-issues list (all three scopes)
 *  - ["inbox", wsId]              inbox (assignment notification if any)
 */
export function useCreateIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (body: CreateIssueRequest) => api.createIssue(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
      qc.invalidateQueries({ queryKey: ["inbox", wsId] });
    },
  });
}
