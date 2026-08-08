/**
 * The scrolling timeline.
 *
 * Sort direction
 * --------------
 * Top-level rows can be shown Oldest-first (default, oldest at top) or
 * Newest-first (newest at top). The direction is a persisted user
 * preference (see apps/mobile/lib/use-timeline-sort.ts). Regardless of
 * direction:
 *   - thread-internal `replies` stay ASC (chronological);
 *   - the query cache stays canonical ASC — reversal happens on a copy in
 *     `buildTimelineRows`;
 *   - the "new content" edge is bottom for Oldest, top for Newest. The
 *     new-comment chip, its jump action, and the `markViewed` edge check
 *     all read `computeAtNewestEdge(metrics, direction)`.
 *
 * Backend returns the full timeline in one shot (server-side pagination
 * was dropped in #2322 — p99 ~30 entries per issue, cursor walking only
 * created bugs at reply-thread boundaries).
 *
 * Inbox deep-link — FlashList v2 `startRenderingFromBottom` (mirrors
 * chat-message-list.tsx):
 *   When `highlightCommentId` is set, we pass
 *   `maintainVisibleContentPosition.startRenderingFromBottom` =
 *   (direction === "oldest") to FlashList and remount the list via
 *   `key={`hl-${highlightNonce}-${direction}`}` once timeline data has
 *   arrived. Oldest lands at the bottom (latest end); Newest lands at the
 *   top (latest end). After initial paint MVCP keeps visible content
 *   stable across async resizes.
 *
 *   The matching <CommentCard>'s `RootHighlightOverlay` fires when the
 *   target row ENTERS THE RENDER WINDOW. The 5 s hold timer starts at
 *   that mount event (not at data arrival), so render-ahead can't burn
 *   the window before the user scrolls to the target. The timer is keyed
 *   by (commentId, nonce) so a FlashList recycle/remount of the same
 *   target doesn't reset it.
 *
 * `maintainVisibleContentPosition` is enabled by default on FlashList v2
 * and is implemented inside the C++ shadow tree.
 *
 * List engine: FlashList v2 (Shopify).
 */
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ElementRef,
} from "react";
import {
  ActivityIndicator,
  Pressable,
  RefreshControl,
  View,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from "react-native";
import { FlashList, type FlashListRef } from "@shopify/flash-list";
import { Ionicons } from "@expo/vector-icons";
import { useQuery } from "@tanstack/react-query";
import type { Issue, TimelineEntry } from "@multica/core/types";
import type { TimelineSortDirection } from "@multica/core/issues/timeline-sort";
import { Text } from "@/components/ui/text";
import { useTimelineSort } from "@/lib/use-timeline-sort";
import { IssueHeaderCard } from "./issue-header-card";
import { IssueDescription } from "./issue-description";
import { IssueReactionRow } from "./issue-reaction-row";
import { ActivityRow } from "./activity-row";
import { CommentCard } from "./comment-card";
import { useLastViewedStore } from "@/data/stores/last-viewed-store";
import { coalesceTimeline } from "@/lib/timeline-coalesce";
import { buildTimelineRows, type TimelineRow } from "@/lib/timeline-thread";
import {
  computeAtNewestEdge,
  computeCrossedMarkers,
  shouldMarkViewedOnUnmount,
  type DividerRect,
} from "@/lib/edge-geometry";
import { ImageSequenceProvider } from "@/lib/markdown/image-sequence";
import { issueAttachmentsOptions } from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import type { ImageSequenceBlock } from "@multica/core/attachments/image-sequence";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { useCommentSelectStore } from "@/data/comment-select-store";

interface Props {
  issue: Issue;
  entries: TimelineEntry[] | undefined;
  timelineLoading: boolean;
  refreshing: boolean;
  onRefresh: () => void;
  /** Inbox deep-link target. Root comment id OR reply id. */
  highlightCommentId?: string;
  /** Per-tap nonce. Re-tapping the same inbox row produces the same
   *  `highlightCommentId` but a fresh nonce, which re-triggers the
   *  scroll-and-flash effect. */
  highlightNonce?: string;
  /** Override the persisted sort direction (mostly for tests / previews).
   *  When omitted, TimelineList owns the preference via useTimelineSort
   *  and renders its own inline toggle. */
  direction?: TimelineSortDirection;
}

/** How long the flash stays "claimed" before we let a new highlight take
 *  over. The fade-out itself is driven by the Reanimated sequence inside
 *  CommentCard; this is just the upstream gate. */
const HIGHLIGHT_HOLD_MS = 5000;

/** Sentinel id for the "New since last view" divider row injected into the
 *  FlatList data. Can never collide with a real comment / activity uuid. */
const DIVIDER_ID = "__divider__";

/** Stable id for a top-level divider marker. */
const TOP_MARKER_ID = "__top_marker__";

interface DividerPlan {
  /** Index in the (direction-ordered) `data` array at which to inject the
   *  top-level sentinel row. -1 = no top-level divider. */
  topInsertIdx: number;
  /** Map of root comment id → reply id: an in-thread divider should be
   *  drawn above this reply. */
  unreadReplyByRoot: Map<string, string>;
}

/**
 * Compute where the "new since last view" divider belongs for the given
 * direction-ordered rows.
 *
 * The snapshot is the user's previous `last_viewed_at`. Entries with
 * `created_at <= snapshot` are read; strictly newer are unread. We compute
 * two things in one pass:
 *
 *   1. `topInsertIdx` — where to splice a sentinel row so that every row
 *      on the read side of the divider precedes it and every unread row
 *      follows. In Oldest the unread rows are below; in Newest they are
 *      above. The index is always the position the divider row itself
 *      should occupy in the rendered array.
 *   2. `unreadReplyByRoot` — for a row whose root is read but which
 *      contains unread replies, the id of the first unread reply; the
 *      card draws the divider above that reply.
 *
 * The function is pure and direction-aware.
 */
function planDivider(
  rows: readonly TimelineRow[],
  snapshot: string | null,
  direction: TimelineSortDirection,
): DividerPlan {
  const empty: DividerPlan = { topInsertIdx: -1, unreadReplyByRoot: new Map() };
  if (!snapshot) return empty;

  const unreadReplyByRoot = new Map<string, string>();
  // For each row determine whether it contains any unread entry and, if
  // the root itself is read but a reply is not, the id of that first
  // unread reply (drives the in-thread divider).
  let firstUnreadIdx = -1;
  let lastUnreadIdx = -1;
  for (let i = 0; i < rows.length; i++) {
    const row = rows[i]!;
    const rootTs = row.entry.created_at;
    let rowHasUnread = rootTs > snapshot;
    let firstUnreadReplyId: string | null = null;
    for (const reply of row.replies) {
      if (reply.created_at > snapshot) {
        rowHasUnread = true;
        if (!firstUnreadReplyId) firstUnreadReplyId = reply.id;
      }
    }
    if (rowHasUnread) {
      if (firstUnreadIdx < 0) firstUnreadIdx = i;
      lastUnreadIdx = i;
      if (rootTs <= snapshot && firstUnreadReplyId) {
        unreadReplyByRoot.set(row.entry.id, firstUnreadReplyId);
      }
    }
  }
  if (firstUnreadIdx < 0) return empty;
  // Oldest → rows run old→new; the divider goes above the first unread
  // row. Newest → rows run new→old; unread rows occupy the top block, so
  // the divider goes BELOW them (between unread and read), i.e. splice at
  // lastUnreadIdx + 1. A divider at index 0 or at data.length is
  // redundant with the list edge and is filtered out by the caller.
  const topInsertIdx =
    direction === "newest" ? lastUnreadIdx + 1 : firstUnreadIdx;
  return { topInsertIdx, unreadReplyByRoot };
}

export function TimelineList({
  issue,
  entries,
  timelineLoading,
  refreshing,
  onRefresh,
  highlightCommentId,
  highlightNonce,
  direction: directionProp,
}: Props) {
  // Own the sort preference when not injected. `hydrated` gates first
  // mount so a "newest" user never sees an "oldest" frame (secure-store
  // read is async) — see use-timeline-sort.ts.
  const sortPref = useTimelineSort();
  const direction = directionProp ?? sortPref.direction;
  const hydrated = directionProp ? true : sortPref.hydrated;

  // Top-level selection subscription gates the outer "tap-outside-to-dismiss"
  // Pressable below.
  const selectingId = useCommentSelectStore((s) => s.selectingId);

  // Server returns ASC oldest-first. Pipeline:
  //   1. coalesceTimeline → merge consecutive identical activities
  //   2. buildTimelineRows → group replies under parents, apply top-level
  //      direction (replies stay ASC).
  const data = useMemo<TimelineRow[]>(() => {
    if (!entries) return [];
    return buildTimelineRows(coalesceTimeline(entries), direction);
  }, [entries, direction]);

  // Every image on this screen, in render order: the description first, then
  // each comment row with its replies (MUL-5752). Tapping any of them opens
  // the lightbox at its real position so a swipe walks to the next.
  //
  // The description's attachments come from the same query IssueDescription
  // uses — TanStack Query dedupes it, so this adds no request.
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issueAttachments } = useQuery(
    issueAttachmentsOptions(wsId, issue.id),
  );
  const imageBlocks = useMemo<ImageSequenceBlock[]>(() => {
    const blocks: ImageSequenceBlock[] = [
      { content: issue.description, attachments: issueAttachments },
    ];
    for (const row of data) {
      if (row.entry.type !== "comment") continue;
      blocks.push({
        content: row.entry.content,
        attachments: row.entry.attachments,
      });
      for (const reply of row.replies) {
        blocks.push({ content: reply.content, attachments: reply.attachments });
      }
    }
    return blocks;
  }, [issue.description, issueAttachments, data]);

  const listRef = useRef<FlashListRef<TimelineRow>>(null);
  // Native scroll node captured from the FlashList so marker Views can be
  // measured against the scroll-view's content-y coordinate space.
  const scrollRef = useRef<ElementRef<typeof View> | null>(null);
  useEffect(() => {
    let cancelled = false;
    const tryCapture = () => {
      if (cancelled) return;
      const node = listRef.current?.getNativeScrollRef?.() as
        | ElementRef<typeof View>
        | null;
      if (node) {
        scrollRef.current = node;
        return;
      }
      // Not attached yet (FlashList still mounting) — retry next frame.
      requestAnimationFrame(tryCapture);
    };
    const raf = requestAnimationFrame(tryCapture);
    return () => {
      cancelled = true;
      cancelAnimationFrame(raf);
    };
  }, []);
  const lastStampRef = useRef<string | null>(null);
  const [highlightedId, setHighlightedId] = useState<string | null>(null);

  // ── "New since last view" divider ─────────────────────────────────────
  const lastViewedSnapshotRef = useRef<string | null>(
    useLastViewedStore.getState().getLastViewed(issue.id) ?? null,
  );
  const dividerPlan = useMemo(
    () => planDivider(data, lastViewedSnapshotRef.current, direction),
    [data, direction],
  );

  // Per-marker "has the user scrolled PAST this boundary?" state. Unlike
  // the previous single boolean, we track each marker independently so
  // that with two unread boundaries (an in-thread divider plus a later
  // top-level divider), crossing the first does NOT mark the whole issue
  // read until the user also crosses the second (or reaches the new-
  // content edge). The set is reconciled (not rebuilt) when data changes
  // so a WS arrival can't resurrect a marker the user already crossed:
  // a marker whose id was present in the previous set and has since been
  // removed (user crossed it) stays out; markers new to this render
  // (freshly arrived unread content) start unread.
  const unseenMarkersRef = useRef<Set<string>>(new Set());
  const crossedMarkersRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    const current = new Set<string>();
    if (dividerPlan.topInsertIdx >= 0) current.add(TOP_MARKER_ID);
    for (const replyId of dividerPlan.unreadReplyByRoot.values()) {
      current.add(`reply:${replyId}`);
    }
    const next = new Set<string>();
    for (const key of current) {
      // Skip markers the user already crossed; they remain crossed until
      // the issue is next opened (the set is per-mount, cleared on
      // unmount via markViewed).
      if (crossedMarkersRef.current.has(key)) continue;
      next.add(key);
    }
    unseenMarkersRef.current = next;
  }, [dividerPlan]);

  const markCrossed = useCallback((key: string) => {
    crossedMarkersRef.current.add(key);
    unseenMarkersRef.current.delete(key);
  }, []);

  // ── Runtime divider-rect measurement ──────────────────────────────────
  // Crossing is decided by ACTUAL PIXEL GEOMETRY (isDividerPast /
  // computeCrossedMarkers), not by visible row index. Each rendered divider
  // registers its native View ref here (the top-level sentinel row and each
  // in-thread divider). On scroll/frame we measure each marker and feed its
  // content-y rect to computeCrossedMarkers.
  //
  // We use absolute PAGE coordinates via `measure()` rather than
  // measureLayout(relativeTo: scrollNode): on iOS a ScrollView's content
  // view is translated by contentOffset, so measureLayout relative to the
  // scroll node returns VIEWPORT-relative y (which moves with the scroll)
  // rather than content-y. Absolute pageY is stable under scroll, so:
  //   contentY = markerPageY - scrollPageY + offsetY
  // (scrollPageY is the on-screen top of the scroll viewport, which does
  // not move during scroll). Then isDividerPast's
  // `contentY + height <= offsetY` correctly tests "divider bottom above
  // the viewport top" and is direction-symmetric.
  //
  // A marker whose ref isn't mounted/laid out yet is omitted; we re-measure
  // on the next frame/scroll. We never treat a missing rect as y:0 (that
  // would false-positive on the first layout before the ref is attached).
  const markerRefs = useRef(new Map<string, View>());
  const registerMarkerRef = useCallback(
    (key: string, ref: View | null) => {
      if (ref) markerRefs.current.set(key, ref);
      else markerRefs.current.delete(key);
    },
    [],
  );
  // Latest scroll metrics so newly-mounted markers can be measured against
  // the current position without waiting for the next scroll event.
  const latestMetricsRef = useRef({ offsetY: 0, contentH: 0, viewportH: 0 });
  // Cached on-screen pageY of the scroll viewport (stable during scroll).
  const scrollPageYRef = useRef<number | null>(null);

  const measureAndCrossMarkers = useCallback(() => {
    if (unseenMarkersRef.current.size === 0) return;
    const scrollNode = scrollRef.current;
    if (!scrollNode) {
      // FlashList native scroll ref not attached yet — retry next frame so
      // crossing still fires without another scroll.
      requestAnimationFrame(() => measureAndCrossMarkers());
      return;
    }
    // Ensure we know the scroll viewport's pageY before measuring markers.
    if (scrollPageYRef.current === null) {
      scrollNode.measure(
        (_x, _y, _w, _h, _pageX, pageY) => {
          if (pageY == null || Number.isNaN(pageY)) return;
          scrollPageYRef.current = pageY;
          measureAndCrossMarkers();
        },
      );
      return;
    }
    const scrollPageY = scrollPageYRef.current;
    // Snapshot metrics NOW; measure() callbacks resolve asynchronously and
    // must judge against the position at measurement time, not a later scroll.
    const metrics = latestMetricsRef.current;
    let pending = 0;
    for (const key of unseenMarkersRef.current) {
      const markerView = markerRefs.current.get(key);
      if (!markerView) {
        pending += 1;
        continue;
      }
      // The crossing decision runs INSIDE the async callback because the
      // rect isn't available synchronously. We pass a one-entry map so the
      // runtime stays wired to the exact helper under test rather than
      // re-deriving the formula inline.
      markerView.measure((_x, _y, _w, h, _pageX, pageY) => {
        // The marker may have been crossed/removed by another path (e.g.
        // reaching the edge) between scheduling and this callback.
        if (!unseenMarkersRef.current.has(key)) return;
        if (pageY == null || Number.isNaN(pageY) || h == null) return;
        const contentY = pageY - scrollPageY + metrics.offsetY;
        const rects = new Map<string, DividerRect>([
          [key, { y: contentY, height: h }],
        ]);
        const crossed = computeCrossedMarkers(rects, metrics, direction);
        if (crossed.has(key)) markCrossed(key);
      });
    }
    // If any marker ref wasn't mounted yet, re-measure next frame so crossing
    // still fires without another scroll event.
    if (pending > 0) {
      requestAnimationFrame(() => measureAndCrossMarkers());
    }
  }, [direction, markCrossed]);

  // When the marker set changes (new unread content arrives), measure once
  // against the current position — the divider may already be scrolled past.
  useEffect(() => {
    const raf = requestAnimationFrame(() => measureAndCrossMarkers());
    return () => cancelAnimationFrame(raf);
  }, [measureAndCrossMarkers, dividerPlan]);

  // ── Highlight timer: start when target row MOUNTS, not when data
  //    arrives. Keyed by (commentId, nonce) so recycle/remount of the
  //    same target cannot reset the 5 s window. ───────────────────────
  const highlightArmedNonceRef = useRef<string | null>(null);
  const handleHighlightMounted = useCallback(
    (commentId: string) => {
      const nonce = highlightNonce ?? "";
      const armKey = `${commentId}:${nonce}`;
      if (highlightArmedNonceRef.current === armKey) return;
      highlightArmedNonceRef.current = armKey;
      setHighlightedId(commentId);
      // Reset any prior timer.
      if (highlightTimerRef.current !== null) {
        clearTimeout(highlightTimerRef.current);
      }
      highlightTimerRef.current = setTimeout(() => {
        // Only clear if this armed key is still the latest.
        if (highlightArmedNonceRef.current === armKey) {
          setHighlightedId(null);
          highlightTimerRef.current = null;
        }
      }, HIGHLIGHT_HOLD_MS);
    },
    [highlightNonce],
  );
  const highlightTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    return () => {
      if (highlightTimerRef.current !== null) {
        clearTimeout(highlightTimerRef.current);
        highlightTimerRef.current = null;
      }
    };
  }, []);

  // Reset armed-key gate when a fresh deep-link arrives (different id or
  // nonce) so the new target can re-arm even if a prior target shared an
  // id (re-tap → new nonce).
  useEffect(() => {
    if (!highlightCommentId) {
      highlightArmedNonceRef.current = null;
      return;
    }
    const stamp = `${highlightCommentId}:${highlightNonce ?? ""}`;
    if (lastStampRef.current !== stamp) {
      lastStampRef.current = stamp;
      highlightArmedNonceRef.current = null;
      setHighlightedId(null);
    }
  }, [highlightCommentId, highlightNonce]);

  // ── New-comment-while-reading chip ────────────────────────────────────
  const [newCount, setNewCount] = useState(0);
  // atNewestEdge defaults to true so initial 0→N load is silent; but
  // `userReachedEdgeRef` tracks whether the USER has actually interacted
  // with / arrived at the edge. We do NOT want a just-opened screen
  // (atEdge=true by default, no scroll) to be treated as "caught up" on
  // unmount when there's an unread divider present.
  const atNewestEdgeRef = useRef(true);
  const userReachedEdgeRef = useRef(false);
  const lastTotalCountRef = useRef(0);

  const totalEntryCount = useMemo(
    () => data.reduce((n, r) => n + 1 + r.replies.length, 0),
    [data],
  );
  useEffect(() => {
    const grew = totalEntryCount > lastTotalCountRef.current;
    const diff = totalEntryCount - lastTotalCountRef.current;
    lastTotalCountRef.current = totalEntryCount;
    if (!grew) return;
    if (atNewestEdgeRef.current) return;
    setNewCount((prev) => prev + diff);
  }, [totalEntryCount]);

  const handleScroll = useCallback(
    (e: NativeSyntheticEvent<NativeScrollEvent>) => {
      const { contentOffset, contentSize, layoutMeasurement } = e.nativeEvent;
      const metrics = {
        offsetY: contentOffset.y,
        contentH: contentSize.height,
        viewportH: layoutMeasurement.height,
      };
      latestMetricsRef.current = metrics;
      const wasAtEdge = atNewestEdgeRef.current;
      const atEdge = computeAtNewestEdge(metrics, direction);
      atNewestEdgeRef.current = atEdge;
      if (atEdge) userReachedEdgeRef.current = true;
      // Reaching the newest edge clears the unread-new chip and marks
      // every outstanding divider as crossed ("caught up").
      if (!wasAtEdge && atEdge && newCount > 0) {
        setNewCount(0);
      }
      if (atEdge) {
        // Reaching the new-content edge counts as "caught up" — every
        // outstanding divider is crossed.
        for (const key of unseenMarkersRef.current) {
          crossedMarkersRef.current.add(key);
        }
        unseenMarkersRef.current.clear();
      } else {
        // Per-divider crossing for both top-level and in-thread markers is
        // decided by actual divider rect geometry (isDividerPast), not by
        // visible row index.
        measureAndCrossMarkers();
      }
    },
    [direction, newCount, measureAndCrossMarkers],
  );

  // When direction changes, snapshot whether the user was at the OLD
  // direction's new-content edge BEFORE recomputing, then on the next
  // frame recompute against the physical scroll position and optionally
  // scroll to the new edge. Without the snapshot, Oldest→Newest would
  // read the same physical offset, see "not at top" (because the user
  // was at the BOTTOM in Oldest), and fail to perform the programmatic
  // jump to top that the "was at the newest end" intuition expects.
  const prevDirectionRef = useRef<TimelineSortDirection>(direction);
  useEffect(() => {
    if (prevDirectionRef.current === direction) return;
    const wasAtOldEdge = atNewestEdgeRef.current;
    prevDirectionRef.current = direction;
    // Wait one frame for FlashList to re-layout with the reversed data
    // before reading metrics / scrolling.
    const raf = requestAnimationFrame(() => {
      // If the user was at the old edge, jump them to the new edge —
      // "I was caught up, keep me caught up". Otherwise leave the
      // physical position where it is; recompute edge state on the next
      // real scroll.
      if (wasAtOldEdge) {
        if (direction === "newest") {
          listRef.current?.scrollToOffset({ offset: 0, animated: false });
        } else {
          listRef.current?.scrollToEnd({ animated: false });
        }
        atNewestEdgeRef.current = true;
        userReachedEdgeRef.current = true;
        setNewCount(0);
        unseenMarkersRef.current.clear();
      }
      // If not at the old edge, the next handleScroll will recompute
      // atNewestEdgeRef from the same physical offset against the new
      // direction; the chip/icon flips naturally.
    });
    return () => cancelAnimationFrame(raf);
  }, [direction]);

  const onJumpToNew = useCallback(() => {
    if (direction === "newest") {
      listRef.current?.scrollToOffset({ offset: 0, animated: true });
    } else {
      listRef.current?.scrollToEnd({ animated: true });
    }
    setNewCount(0);
    atNewestEdgeRef.current = true;
    userReachedEdgeRef.current = true;
    unseenMarkersRef.current.clear();
  }, [direction]);

  // ── Inject the top-level divider sentinel at the planned index ────────
  const dataWithDivider = useMemo<TimelineRow[]>(() => {
    if (dividerPlan.topInsertIdx < 0) return data;
    if (dividerPlan.topInsertIdx === 0 || dividerPlan.topInsertIdx >= data.length) {
      // A divider at the very top (all unread in newest) or very bottom
      // (all unread in oldest) is redundant with the edge of the list —
      // skip it. The markViewed edge behavior still handles "caught up".
      return data;
    }
    const divider: TimelineRow = {
      entry: {
        id: DIVIDER_ID,
        type: "activity",
        created_at: "",
        actor_type: "",
        actor_id: "",
      } as unknown as TimelineEntry,
      replies: [],
    };
    return [
      ...data.slice(0, dividerPlan.topInsertIdx),
      divider,
      ...data.slice(dividerPlan.topInsertIdx),
    ];
  }, [data, dividerPlan.topInsertIdx]);

  // On unmount, bump last-viewed to now only if the user actually caught
  // up. The decision is delegated to pure shouldMarkViewedOnUnmount (unit
  // tested in lib/edge-geometry.test.ts). CRITICAL: read dividerPlan from a
  // LIVE ref, not the first-render closure — on a cold load the first render
  // has no entries yet (noDividers === true), and a cleanup bound to that
  // closure would false-positive markViewed when the user leaves without
  // scrolling, even after unread entries had arrived. The ref is reassigned
  // every render so cleanup always sees the latest plan.
  const dividerPlanRef = useRef(dividerPlan);
  dividerPlanRef.current = dividerPlan;
  const markViewed = useLastViewedStore((s) => s.markViewed);
  useEffect(() => {
    const issueId = issue.id;
    return () => {
      const plan = dividerPlanRef.current;
      const hasDividers =
        plan.topInsertIdx >= 0 || plan.unreadReplyByRoot.size > 0;
      if (
        shouldMarkViewedOnUnmount({
          hasDividers,
          unseenMarkerCount: unseenMarkersRef.current.size,
          userReachedEdge: userReachedEdgeRef.current,
        })
      ) {
        markViewed(issueId);
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [issue.id]);

  const ListHeader = (
    <View>
      <IssueHeaderCard issue={issue} />
      <IssueDescription issueId={issue.id} description={issue.description} />
      <IssueReactionRow issue={issue} />
      <View className="px-4 pt-4 pb-2 border-t border-border flex-row items-center justify-between">
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          Activity
        </Text>
        {!directionProp ? (
          <SortToggle
            direction={direction}
            onToggle={sortPref.toggle}
          />
        ) : null}
      </View>
      {timelineLoading && (!entries || entries.length === 0) ? (
        <View className="py-6 items-center">
          <ActivityIndicator />
        </View>
      ) : null}
    </View>
  );

  // FlashList remount key: include direction so switching sort re-arms the
  // initial scroll position (startRenderingFromBottom depends on it).
  const hasData = dataWithDivider.length > 0;
  const flashListKey =
    highlightCommentId && hasData
      ? `hl-${highlightNonce ?? "0"}-${direction}`
      : `list-${direction}`;

  if (!hydrated) {
    return (
      <View className="flex-1 items-center justify-center">
        <ActivityIndicator />
      </View>
    );
  }

  return (
    <ImageSequenceProvider blocks={imageBlocks}>
    <View className="flex-1">
      <Pressable
        onPress={
          selectingId
            ? () => useCommentSelectStore.getState().clear()
            : undefined
        }
        disabled={!selectingId}
        style={{ flex: 1 }}
      >
      <FlashList
        key={flashListKey}
        ref={listRef}
        data={dataWithDivider}
        keyExtractor={(row) => row.entry.id}
        ListHeaderComponent={ListHeader}
        keyboardDismissMode="on-drag"
        keyboardShouldPersistTaps="handled"
        maintainVisibleContentPosition={{
          // Oldest deep-link → land at bottom (latest end); Newest → top.
          startRenderingFromBottom:
            !!highlightCommentId && direction === "oldest",
        }}
        ListHeaderComponentStyle={{ marginBottom: 4 }}
        ItemSeparatorComponent={RowSeparator}
        renderItem={({ item }) => {
          if (item.entry.id === DIVIDER_ID) {
            return (
              <UnreadDivider
                markerRef={(ref) => registerMarkerRef(TOP_MARKER_ID, ref)}
                onLayout={() => measureAndCrossMarkers()}
              />
            );
          }
          if (item.entry.type === "comment") {
            const unreadReplyId =
              dividerPlan.unreadReplyByRoot.get(item.entry.id) ?? null;
            return (
              <CommentCard
                entry={item.entry}
                replies={item.replies}
                issueId={issue.id}
                issueIdentifier={issue.identifier}
                highlightedCommentId={highlightedId}
                unreadBeforeReplyId={unreadReplyId}
                unreadMarkerRef={
                  unreadReplyId
                    ? (ref) =>
                        registerMarkerRef(`reply:${unreadReplyId}`, ref)
                    : null
                }
                onUnreadMarkerLayout={() => measureAndCrossMarkers()}
                onHighlightMounted={handleHighlightMounted}
              />
            );
          }
          return <ActivityRow entry={item.entry} />;
        }}
        onScroll={handleScroll}
        onScrollBeginDrag={() =>
          useCommentSelectStore.getState().clear()
        }
        onMomentumScrollBegin={() =>
          useCommentSelectStore.getState().clear()
        }
        refreshControl={
          <RefreshControl refreshing={refreshing} onRefresh={onRefresh} />
        }
        contentContainerStyle={{ paddingBottom: 16 }}
      />
      </Pressable>
      {newCount > 0 ? (
        <NewCommentChip
          count={newCount}
          direction={direction}
          onPress={onJumpToNew}
        />
      ) : null}
    </View>
    </ImageSequenceProvider>
  );
}

function RowSeparator() {
  return <View style={{ height: 12 }} />;
}

function UnreadDivider({
  markerRef,
  onLayout,
}: {
  markerRef?: (ref: View | null) => void;
  onLayout?: () => void;
}) {
  return (
    <View
      ref={markerRef}
      onLayout={onLayout}
      className="flex-row items-center gap-2 px-4"
    >
      <View className="flex-1 h-px bg-destructive/40" />
      <Text className="text-[10px] uppercase tracking-wider font-medium text-destructive">
        New
      </Text>
      <View className="flex-1 h-px bg-destructive/40" />
    </View>
  );
}

/**
 * Compact Oldest/Newest toggle in the list header.
 */
function SortToggle({
  direction,
  onToggle,
}: {
  direction: TimelineSortDirection;
  onToggle: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const muted = THEME[colorScheme].mutedForeground;
  const newest = direction === "newest";
  return (
    <Pressable
      onPress={onToggle}
      hitSlop={8}
      accessibilityRole="button"
      accessibilityLabel="Toggle comment sort order"
      className="flex-row items-center gap-1 px-2 py-1 rounded-md active:opacity-70"
    >
      <Ionicons
        name={newest ? "arrow-up" : "arrow-down"}
        size={12}
        color={muted}
      />
      <Text className="text-[11px] text-muted-foreground font-medium">
        {newest ? "Newest" : "Oldest"}
      </Text>
    </Pressable>
  );
}

function NewCommentChip({
  count,
  direction,
  onPress,
}: {
  count: number;
  direction: TimelineSortDirection;
  onPress: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const fg = THEME[colorScheme].primaryForeground;
  // Newest: chip at TOP pointing up (new content above). Oldest: at
  // bottom pointing down (new content below).
  const atTop = direction === "newest";
  return (
    <Pressable
      onPress={onPress}
      className={
        atTop
          ? "absolute top-3 self-center px-3.5 py-1.5 rounded-full bg-primary active:opacity-80 flex-row items-center gap-1.5"
          : "absolute bottom-3 self-center px-3.5 py-1.5 rounded-full bg-primary active:opacity-80 flex-row items-center gap-1.5"
      }
      accessibilityRole="button"
      accessibilityLabel={`Jump to ${count} new ${count === 1 ? "message" : "messages"}`}
      style={{
        shadowColor: "#000",
        shadowOffset: { width: 0, height: 2 },
        shadowOpacity: 0.18,
        shadowRadius: 6,
        elevation: 4,
      }}
    >
      <Ionicons
        name={atTop ? "arrow-up" : "arrow-down"}
        size={14}
        color={fg}
      />
      <Text className="text-xs font-semibold text-primary-foreground">
        {count} new
      </Text>
    </Pressable>
  );
}
