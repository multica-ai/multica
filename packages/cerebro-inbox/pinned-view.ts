"use client";

// FIR-2653 — "Pinned" inbox view. Shows only the inbox entries whose subject
// is pinned by the current user: the item's own issue, that issue's direct
// parent, or its project. Pins live in a separate per-user table (sidebar
// pins), so this resolves the pin set client-side and joins it against each
// merged inbox entry — no server change, no new field on the inbox item.
import { useCallback, useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { pinListOptions } from "@multica/core/pins";
import { childrenByParentsOptions } from "@multica/core/issues";
import type { InboxItem, Channel } from "@multica/core/types";

/**
 * Structural subset of the inbox-page `MergedEntry` union that the pinned
 * filter needs. Kept structural so the upstream inbox page can pass its own
 * entries without importing a cerebro type.
 */
export type PinnableEntry =
  | { kind: "chat" }
  | { kind: "notif"; item: Pick<InboxItem, "issue_id" | "project_id"> }
  | { kind: "channel"; channel: Pick<Channel, "id"> };

/** The resolved pin context an entry is matched against. */
export interface PinnedContext {
  /** ids of issues the user pinned directly. */
  pinnedIssueIds: ReadonlySet<string>;
  /** ids of projects the user pinned directly. */
  pinnedProjectIds: ReadonlySet<string>;
  /** ids of channels / DMs the user pinned directly. */
  pinnedChannelIds: ReadonlySet<string>;
  /** ids of issues whose direct parent is a pinned issue. */
  childOfPinnedIds: ReadonlySet<string>;
}

/**
 * Pure decision: does this inbox entry belong in the "Pinned" view?
 * Exported (and unit-tested) separately so the rule is verifiable without
 * standing up react-query.
 */
export function entryMatchesPins(entry: PinnableEntry, ctx: PinnedContext): boolean {
  if (entry.kind === "notif") {
    const { issue_id, project_id } = entry.item;
    if (issue_id && (ctx.pinnedIssueIds.has(issue_id) || ctx.childOfPinnedIds.has(issue_id)))
      return true;
    if (project_id && ctx.pinnedProjectIds.has(project_id)) return true;
    return false;
  }
  if (entry.kind === "channel") return ctx.pinnedChannelIds.has(entry.channel.id);
  return false; // agent chat sessions have no pin representation
}

/**
 * Returns a predicate that decides whether an inbox entry belongs in the
 * "Pinned" view. An entry matches when:
 *  - its issue is pinned, or
 *  - its issue's direct parent is pinned ("parent issue has a pin"), or
 *  - its project is pinned, or
 *  - it is a pinned channel / DM.
 * Agent chat sessions have no pin representation, so they never match.
 *
 * Parent resolution uses the batched children-by-parents query (the same one
 * the swim-lane view uses) keyed on the small, sorted set of pinned issue ids
 * — so it costs one chunked request, not an N-item fan-out.
 */
export function useInboxPinnedMatcher(
  wsId: string,
  userId: string,
): (entry: PinnableEntry) => boolean {
  const qc = useQueryClient();
  const { data: pins } = useQuery(pinListOptions(wsId, userId));

  const { pinnedIssueIds, pinnedProjectIds, pinnedChannelIds, sortedIssueIds } = useMemo(() => {
    const issue = new Set<string>();
    const project = new Set<string>();
    const channel = new Set<string>();
    for (const pin of pins ?? []) {
      if (pin.item_type === "issue") issue.add(pin.item_id);
      else if (pin.item_type === "project") project.add(pin.item_id);
      else channel.add(pin.item_id); // "channel" | "dm"
    }
    return {
      pinnedIssueIds: issue,
      pinnedProjectIds: project,
      pinnedChannelIds: channel,
      // Sorted for a stable react-query cache key.
      sortedIssueIds: [...issue].sort(),
    };
  }, [pins]);

  const { data: childrenByParent } = useQuery(
    childrenByParentsOptions(wsId, sortedIssueIds, qc),
  );

  const childOfPinnedIds = useMemo(() => {
    const set = new Set<string>();
    if (childrenByParent) {
      for (const children of childrenByParent.values()) {
        for (const child of children) set.add(child.id);
      }
    }
    return set;
  }, [childrenByParent]);

  return useCallback(
    (entry: PinnableEntry): boolean =>
      entryMatchesPins(entry, {
        pinnedIssueIds,
        pinnedProjectIds,
        pinnedChannelIds,
        childOfPinnedIds,
      }),
    [pinnedIssueIds, pinnedProjectIds, pinnedChannelIds, childOfPinnedIds],
  );
}
