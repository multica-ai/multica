/**
 * Mobile-owned WS cache patchers for the issue domain. These are pure
 * functions over `QueryClient` — no React, no WS plumbing. The hooks in
 * `use-issue-realtime.ts` and `use-my-issues-realtime.ts` translate WS
 * events into calls into this module.
 *
 * Why mobile-owned (and not importing from packages/core/issues/ws-updaters):
 *   - Web's updaters reference `issueKeys` from `packages/core/issues/queries`,
 *     a different runtime instance than mobile's `data/queries/issue-keys.ts`.
 *     TanStack Query keys are compared structurally so this would *appear*
 *     to work, but binding cache mutation to a foreign key factory invites
 *     drift the moment either side adjusts its key shape.
 *   - Mobile cache shapes are simpler: no status-bucketed list, no children
 *     subtree, no childProgress, no label-byIssue cache. A direct mirror
 *     would be ~120 lines of conditional dead-code paths.
 *
 * Cache shapes (the design contract here):
 *   - Issue detail:    Issue                                  (keyed by detail(wsId, id))
 *   - Issue timeline:  TimelineEntry[]                        (keyed by timeline(wsId, id))
 *                      ASC oldest-first; new entries inserted at sorted position.
 *   - My Issues list:  Issue[]                                (keyed by myList(wsId, scope, filter))
 *                      Multiple list caches per wsId (one per scope/filter combo).
 *                      Patch ALL of them via setQueriesData on myAll(wsId).
 *   - Workspace list:  Issue[]                                (keyed by list(wsId))
 *                      Single cache per wsId (no scope/filter in the key —
 *                      filtering happens client-side off the same list).
 */
import type { QueryClient } from "@tanstack/react-query";
import type {
  Comment,
  Issue,
  IssueReaction,
  Label,
  Reaction,
  TimelineEntry,
} from "@multica/core/types";
import { issueKeys } from "@/data/queries/issue-keys";

type TimelinePredicate = (entry: TimelineEntry) => boolean;
type TimelineMutate = (entry: TimelineEntry) => TimelineEntry;

const auxiliaryIssueRevisions = new WeakMap<QueryClient, Map<string, number>>();

type AuxiliaryIssueProjection = "generic" | "labels" | "issue_reactions";

function auxiliaryIssueRevisionPrefix(wsId: string, issueId: string) {
  return `${wsId}:${issueId}:`;
}

function auxiliaryIssueRevisionKey(
  wsId: string,
  issueId: string,
  projection: AuxiliaryIssueProjection,
) {
  return `${auxiliaryIssueRevisionPrefix(wsId, issueId)}${projection}`;
}

function recordAuxiliaryIssueRevision(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  revision: number,
  projection: AuxiliaryIssueProjection,
) {
  let revisions = auxiliaryIssueRevisions.get(qc);
  if (!revisions) {
    revisions = new Map();
    auxiliaryIssueRevisions.set(qc, revisions);
  }
  const key = auxiliaryIssueRevisionKey(wsId, issueId, projection);
  if ((revisions.get(key) ?? 0) < revision) revisions.set(key, revision);
}

function isOlderThanRecordedAuxiliaryProjection(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  revision: number | undefined,
  projection: AuxiliaryIssueProjection,
) {
  if (revision === undefined) return false;
  const recorded = auxiliaryIssueRevisions
    .get(qc)
    ?.get(auxiliaryIssueRevisionKey(wsId, issueId, projection));
  return recorded !== undefined && revision < recorded;
}

export function reconcileIssueFullSnapshotRevision(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  fullRevision: number | undefined,
) {
  if (fullRevision === undefined) return;
  const revisions = auxiliaryIssueRevisions.get(qc);
  if (!revisions) return;
  const prefix = auxiliaryIssueRevisionPrefix(wsId, issueId);
  let newestAuxiliaryRevision: number | undefined;
  for (const [key, revision] of revisions) {
    if (!key.startsWith(prefix)) continue;
    if (fullRevision >= revision) {
      revisions.delete(key);
    } else if (
      newestAuxiliaryRevision === undefined ||
      revision > newestAuxiliaryRevision
    ) {
      newestAuxiliaryRevision = revision;
    }
  }
  if (newestAuxiliaryRevision !== undefined) {
    invalidateStaleIssueOwnerProjections(
      qc,
      wsId,
      issueId,
      newestAuxiliaryRevision,
    );
  }
}

function acceptsRevision(current: number | undefined, incoming: number | undefined) {
  if (current === undefined) return true;
  return incoming !== undefined && incoming >= current;
}

// =====================================================
// Issue detail cache (single Issue per id)
// =====================================================

export function patchIssueDetail(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Issue> & { id: string },
) {
  if (partial.revision === undefined) {
    qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, partial.id) });
  }
  qc.setQueryData<Issue>(issueKeys.detail(wsId, partial.id), (old) =>
    old && acceptsRevision(old.revision, partial.revision)
      ? { ...old, ...partial }
      : old,
  );
  reconcileIssueFullSnapshotRevision(qc, wsId, partial.id, partial.revision);
}

export function onIssueAuxiliaryRevision(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  revision: number | undefined,
  projection: AuxiliaryIssueProjection = "generic",
) {
  if (revision === undefined) return;
  recordAuxiliaryIssueRevision(qc, wsId, issueId, revision, projection);
  invalidateStaleIssueOwnerProjections(qc, wsId, issueId, revision);
}

function invalidateStaleIssueOwnerProjections(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  revision: number,
) {
  const isStale = (issue: Issue | undefined) =>
    issue !== undefined &&
    (issue.revision === undefined || issue.revision < revision);
  const detailKey = issueKeys.detail(wsId, issueId);
  if (isStale(qc.getQueryData<Issue>(detailKey))) {
    qc.invalidateQueries({ queryKey: detailKey, exact: true });
  }
  for (const [key, data] of qc.getQueriesData<Issue[]>({
    queryKey: issueKeys.myAll(wsId),
  })) {
    if (data?.some((issue) => issue.id === issueId && isStale(issue))) {
      qc.invalidateQueries({ queryKey: key, exact: true });
    }
  }
  const listKey = issueKeys.list(wsId);
  if (qc.getQueryData<Issue[]>(listKey)?.some(
    (issue) => issue.id === issueId && isStale(issue),
  )) {
    qc.invalidateQueries({ queryKey: listKey, exact: true });
  }
}

export function clearIssueDetail(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.removeQueries({ queryKey: issueKeys.detail(wsId, issueId) });
  qc.removeQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
}

export function invalidateIssueAfterReconnect(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.attachments(wsId, issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.activeTasks(wsId, issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.tasks(wsId, issueId) });
}

// =====================================================
// Issue timeline (flat TimelineEntry[], ASC oldest-first)
// =====================================================

export function appendTimelineEntry(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  entry: TimelineEntry,
) {
  qc.setQueryData<TimelineEntry[]>(
    issueKeys.timeline(wsId, issueId),
    (old) => {
      if (!old) return old;
      // Skip if the entry is already present — backend can re-emit on
      // reconnect or two clients can echo the same comment.
      if (old.some((e) => e.id === entry.id && e.type === entry.type)) {
        return old;
      }
      const next = [...old, entry];
      next.sort((a, b) => {
        if (a.created_at !== b.created_at) return a.created_at < b.created_at ? -1 : 1;
        return a.id < b.id ? -1 : 1;
      });
      return next;
    },
  );
}

export function patchTimelineEntry(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  predicate: TimelinePredicate,
  mutate: TimelineMutate,
) {
  qc.setQueryData<TimelineEntry[]>(
    issueKeys.timeline(wsId, issueId),
    (old) => (old ? old.map((e) => (predicate(e) ? mutate(e) : e)) : old),
  );
}

export function replaceCommentTimelineEntry(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  entry: TimelineEntry,
) {
  if (entry.revision === undefined) {
    qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
  }
  patchTimelineEntry(
    qc,
    wsId,
    issueId,
    (current) => current.type === "comment" && current.id === entry.id,
    (current) =>
      acceptsRevision(current.revision, entry.revision)
        ? {
            ...entry,
            actor_name: entry.actor_name ?? current.actor_name,
            actor_avatar_url:
              entry.actor_avatar_url ?? current.actor_avatar_url,
          }
        : current,
  );
}

export function advanceCommentRevision(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  commentId: string,
  revision: number | undefined,
) {
  if (revision === undefined) return;
  patchTimelineEntry(
    qc,
    wsId,
    issueId,
    (entry) => entry.type === "comment" && entry.id === commentId,
    (entry) => entry.revision === undefined || revision > entry.revision
      ? { ...entry, revision }
      : entry,
  );
}

export function removeTimelineEntry(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  predicate: TimelinePredicate,
) {
  qc.setQueryData<TimelineEntry[]>(
    issueKeys.timeline(wsId, issueId),
    (old) => (old ? old.filter((e) => !predicate(e)) : old),
  );
}

/**
 * Cascade-delete a comment and every descendant reply (reply-to-reply
 * chains included). Mirrors the server's cascade in `comment.go:DeleteComment`
 * and web's `comment:deleted` handler at
 * `packages/views/issues/hooks/use-issue-timeline.ts:164-194`.
 *
 * Without this, removing only the root entry leaves the replies as
 * "orphans" — `buildTimelineRows` then promotes them to top-level rows
 * (its orphan-rescue branch), so the user would see ghost replies after a
 * thread delete on another client. Same-N rule violation.
 *
 * BFS rather than recursive Set-union: cheaper for arbitrary depth chains
 * and avoids recursion-depth concerns on large threads.
 */
export function removeCommentCascade(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  commentId: string,
) {
  qc.setQueryData<TimelineEntry[]>(
    issueKeys.timeline(wsId, issueId),
    (old) => {
      if (!old) return old;
      const removed = new Set<string>([commentId]);
      // Iterate to fixed point — a single forward pass catches direct
      // children; later passes catch reply-to-reply chains. Bounded by
      // the timeline length, so worst case O(N²) on a degenerate chain
      // but N is p99 30 and chains are typically depth 1-2.
      let changed = true;
      while (changed) {
        changed = false;
        for (const e of old) {
          if (
            e.type === "comment" &&
            e.parent_id &&
            removed.has(e.parent_id) &&
            !removed.has(e.id)
          ) {
            removed.add(e.id);
            changed = true;
          }
        }
      }
      return old.filter(
        (e) => !(e.type === "comment" && removed.has(e.id)),
      );
    },
  );
}

// =====================================================
// My Issues list (flat Issue[] across many keys)
// =====================================================

export function patchMyIssuesList(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Issue> & { id: string },
) {
  if (partial.revision === undefined) {
    qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
  }
  // myList is keyed by (wsId, scope, filter); we don't know which entries
  // the issue belongs to, so update every cached one. Any not-yet-loaded
  // list will fetch fresh on mount.
  qc.setQueriesData<Issue[]>({ queryKey: issueKeys.myAll(wsId) }, (old) =>
    old ? old.map((i) =>
      i.id === partial.id && acceptsRevision(i.revision, partial.revision)
        ? { ...i, ...partial }
        : i,
    ) : old,
  );
  reconcileIssueFullSnapshotRevision(qc, wsId, partial.id, partial.revision);
}

export function removeFromMyIssuesList(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.setQueriesData<Issue[]>({ queryKey: issueKeys.myAll(wsId) }, (old) =>
    old ? old.filter((i) => i.id !== issueId) : old,
  );
}

// =====================================================
// Workspace Issues list (flat Issue[] under list(wsId))
// =====================================================

export function patchIssuesList(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Issue> & { id: string },
) {
  if (partial.revision === undefined) {
    qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
  }
  qc.setQueryData<Issue[]>(issueKeys.list(wsId), (old) =>
    old ? old.map((i) =>
      i.id === partial.id && acceptsRevision(i.revision, partial.revision)
        ? { ...i, ...partial }
        : i,
    ) : old,
  );
  reconcileIssueFullSnapshotRevision(qc, wsId, partial.id, partial.revision);
}

export function prependToIssuesList(
  qc: QueryClient,
  wsId: string,
  issue: Issue,
) {
  qc.setQueryData<Issue[]>(issueKeys.list(wsId), (old) => {
    if (!old) return old;
    if (old.some((i) => i.id === issue.id)) return old;
    return [issue, ...old];
  });
}

export function removeFromIssuesList(
  qc: QueryClient,
  wsId: string,
  issueId: string,
) {
  qc.setQueryData<Issue[]>(issueKeys.list(wsId), (old) =>
    old ? old.filter((i) => i.id !== issueId) : old,
  );
}

// =====================================================
// Reactions
// =====================================================

export function addCommentReaction(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  commentId: string,
  reaction: Reaction,
  commentRevision?: number,
) {
  if (commentRevision === undefined) {
    qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
  }
  patchTimelineEntry(
    qc,
    wsId,
    issueId,
    (e) => e.type === "comment" && e.id === commentId,
    (e) => acceptsRevision(e.revision, commentRevision) ? ({
      ...e,
      revision: commentRevision ?? e.revision,
      reactions: [...(e.reactions ?? []).filter((r) => r.id !== reaction.id), reaction],
    }) : e,
  );
}

export function removeCommentReaction(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  commentId: string,
  emoji: string,
  actorId: string,
  commentRevision?: number,
) {
  if (commentRevision === undefined) {
    qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
  }
  patchTimelineEntry(
    qc,
    wsId,
    issueId,
    (e) => e.type === "comment" && e.id === commentId,
    (e) => acceptsRevision(e.revision, commentRevision) ? ({
      ...e,
      revision: commentRevision ?? e.revision,
      reactions: (e.reactions ?? []).filter(
        (r) => !(r.emoji === emoji && r.actor_id === actorId),
      ),
    }) : e,
  );
}

export function addIssueReaction(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  reaction: IssueReaction,
  issueRevision?: number,
) {
  if (
    isOlderThanRecordedAuxiliaryProjection(
      qc,
      wsId,
      issueId,
      issueRevision,
      "issue_reactions",
    )
  ) {
    return;
  }
  if (issueRevision === undefined) {
    qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
  }
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) => {
    if (!old) return old;
    if (!acceptsRevision(old.revision, issueRevision)) return old;
    const existing = old.reactions ?? [];
    if (existing.some((r) => r.id === reaction.id)) {
      return old;
    }
    return { ...old, reactions: [...existing, reaction] };
  });
  onIssueAuxiliaryRevision(
    qc,
    wsId,
    issueId,
    issueRevision,
    "issue_reactions",
  );
}

export function removeIssueReaction(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  emoji: string,
  actorId: string,
  issueRevision?: number,
) {
  if (
    isOlderThanRecordedAuxiliaryProjection(
      qc,
      wsId,
      issueId,
      issueRevision,
      "issue_reactions",
    )
  ) {
    return;
  }
  if (issueRevision === undefined) {
    qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
  }
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old && acceptsRevision(old.revision, issueRevision)
      ? {
          ...old,
          reactions: (old.reactions ?? []).filter(
            (r) => !(r.emoji === emoji && r.actor_id === actorId),
          ),
        }
      : old,
  );
  onIssueAuxiliaryRevision(
    qc,
    wsId,
    issueId,
    issueRevision,
    "issue_reactions",
  );
}

// =====================================================
// Labels
// =====================================================

export function patchIssueLabels(
  qc: QueryClient,
  wsId: string,
  issueId: string,
  labels: Label[],
  issueRevision?: number,
) {
  if (
    isOlderThanRecordedAuxiliaryProjection(
      qc,
      wsId,
      issueId,
      issueRevision,
      "labels",
    )
  ) {
    return;
  }
  const applyLabels = (issue: Issue) =>
    issue.id === issueId && acceptsRevision(issue.revision, issueRevision)
      ? { ...issue, labels }
      : issue;
  qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
    old ? applyLabels(old) : old,
  );
  qc.setQueriesData<Issue[]>({ queryKey: issueKeys.myAll(wsId) }, (old) =>
    old?.map(applyLabels),
  );
  qc.setQueryData<Issue[]>(issueKeys.list(wsId), (old) =>
    old?.map(applyLabels),
  );
  onIssueAuxiliaryRevision(qc, wsId, issueId, issueRevision, "labels");
}

// =====================================================
// Helpers — payload normalization
// =====================================================

/**
 * Convert a Comment WS payload into a TimelineEntry. The two types share
 * most fields but use different actor-key names (Comment uses
 * `author_type/author_id`; TimelineEntry uses `actor_type/actor_id`).
 */
export function commentToTimelineEntry(comment: Comment): TimelineEntry {
  return {
    type: "comment",
    id: comment.id,
    actor_type: comment.author_type,
    actor_id: comment.author_id,
    created_at: comment.created_at,
    content: comment.content,
    parent_id: comment.parent_id,
    updated_at: comment.updated_at,
    comment_type: comment.type,
    reactions: comment.reactions,
    attachments: comment.attachments,
    // Carry resolve state through. Web's commentToTimelineEntry includes
    // these (`packages/views/issues/hooks/use-issue-timeline.ts:42-58`); the
    // earlier mobile copy dropped them, so a `comment:updated` event on a
    // resolved comment would silently strip the resolved flag from the
    // cache and the UI would un-dim.
    resolved_at: comment.resolved_at,
    resolved_by_type: comment.resolved_by_type,
    resolved_by_id: comment.resolved_by_id,
    source_task_id: comment.source_task_id,
    revision: comment.revision,
  };
}
