// FIR-1576 (follow-up) — decide whether an inbox notification's issue is a
// channel/DM from the issue's OWN `kind`, not from membership in the channel-
// list cache.
//
// Why this exists: an inbox row for a channel/DM is a *folded* view whose
// click/detail routing must open the in-pane ChannelDetail, never the generic
// IssueDetail — IssueDetail's FIR-2452 effect redirects channel/DM issues out
// of the inbox to /channels/<id>, kicking the user out. Both inboxes used to
// detect channel-ness purely via `channelMap` (and later `knownChannelIds`).
// That cache is empty on first load and after back-navigation (the channel
// list refetches), so for a beat a channel notif looks like a plain issue,
// falls through to IssueDetail, and the redirect fires before the cache
// repopulates. Jesper hit exactly this clicking the "General" channel and on
// the browser back button — the kind detection was "error prone".
//
// `InboxItem` carries no issue-kind, so the authoritative signal is the issue
// itself. We fetch the selected notif's issue (sharing IssueDetail's query
// cache, so a plain issue notif pays no extra request) and route on its real
// `kind`. `knownChannelIds` stays as a zero-latency fast path. While the kind
// is still unknown we report `kindPending` so the caller can hold the pane on a
// placeholder instead of mounting the redirecting IssueDetail.
import { useQuery } from "@tanstack/react-query";
import { issueDetailOptions } from "@multica/core/issues/queries";

export interface SelectedNotifChannel {
  /** The selected notif's issue is a channel/DM → open ChannelDetail in-pane. */
  isChannel: boolean;
  /** Kind not yet resolved (and not a fast-path channel): hold off IssueDetail. */
  kindPending: boolean;
}

/**
 * Pure decision: given the selected notif's issue id, its resolved `kind` (or
 * undefined while the issue query is still in flight / errored), the query
 * loading flag, and the fast-path known-channel set, decide how the detail pane
 * should route. Extracted from the hook so the routing rule is unit-testable
 * without rendering.
 */
export function resolveNotifChannel(
  issueId: string | null,
  issueKind: string | undefined,
  isLoading: boolean,
  knownChannelIds: ReadonlySet<string>,
): SelectedNotifChannel {
  const isChannel =
    !!issueId &&
    (knownChannelIds.has(issueId) ||
      issueKind === "channel" ||
      issueKind === "dm");
  const kindPending = !!issueId && !isChannel && isLoading;
  return { isChannel, kindPending };
}

/**
 * Resolves whether the currently selected notification's issue is a channel/DM.
 *
 * @param wsId            current workspace id (query cache key).
 * @param issueId         the selected notif's issue id, or null when the
 *                        selection is not a notif (or has no issue).
 * @param knownChannelIds session-persistent fast-path set from useKnownChannelIds.
 */
export function useSelectedNotifChannel(
  wsId: string,
  issueId: string | null,
  knownChannelIds: ReadonlySet<string>,
): SelectedNotifChannel {
  const { data: issue, isLoading } = useQuery({
    ...issueDetailOptions(wsId, issueId ?? ""),
    enabled: !!issueId,
  });
  return resolveNotifChannel(issueId, issue?.kind, isLoading, knownChannelIds);
}
