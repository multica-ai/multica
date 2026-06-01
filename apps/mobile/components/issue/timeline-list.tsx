/**
 * The scrolling timeline. ASC chronological — oldest at top, newest near the
 * bottom (above the composer). Pull-to-refresh refetches issue + timeline.
 *
 * Backend returns the full timeline in one shot (server-side pagination
 * was dropped in #2322 — p99 ~30 entries per issue, cursor walking only
 * created bugs at reply-thread boundaries). The previous "Pull to load
 * older" UX and top-edge `fetchOlder` trigger are gone.
 *
 * Inbox deep-link — FlashList v2 `startRenderingFromBottom` (mirrors
 * chat-message-list.tsx):
 *   When `highlightCommentId` is set, we pass
 *   `maintainVisibleContentPosition.startRenderingFromBottom: true` to
 *   FlashList and remount the list via `key={`hl-${highlightNonce}`}` once
 *   timeline data has arrived. FlashList's `getInitialScrollIndex()` then
 *   lands on the last data item; after the initial paint MVCP keeps the
 *   visible content stable across async resizes (Shiki highlight, image
 *   natural-size, WS `comment:updated`). No JS-side scroll dance.
 *
 *   The previous implementation hand-rolled a `landed` state gate that
 *   re-fired `scrollToEnd` on every `onContentSizeChange`. That deadlocked
 *   the user at the bottom: `onScrollBeginDrag` queued `setLanded(true)`
 *   but a concurrent `onContentSizeChange` (from MVCP's own ScrollAnchor
 *   reposition, or from a markdown pass finishing) read the stale React
 *   closure, saw `landed === false`, and slammed the user back to bottom
 *   before the commit landed. State-vs-event races don't survive iOS
 *   markdown rendering hogging the JS thread.
 *
 *   The matching <CommentCard>'s `RootHighlightOverlay` fires when the
 *   target row enters the render window — so for a deep-link pointing
 *   at an old comment, the user scrolls up and the flash plays as the
 *   row mounts. `HIGHLIGHT_HOLD_MS` (5s) is the window for that.
 *
 *   Why not `scrollToIndex`: it requires accurate height estimates that
 *   variable-height markdown bubbles can't provide, even with
 *   `onScrollToIndexFailed` retry. Same lesson web learned (see
 *   `packages/views/issues/components/issue-detail.tsx:1822-1850` — they
 *   split the path into virtualized-for-browse / flat-for-deep-link
 *   precisely because "virtualization and 'land precisely on a target'
 *   have fundamentally opposed contracts"). Mobile sidesteps the split
 *   by not pretending to land precisely.
 *
 * `maintainVisibleContentPosition` is enabled by default on FlashList v2
 * and is implemented inside the C++ shadow tree — it compensates the
 * scroll offset both when a row is INSERTED above the viewport AND when
 * an upper row RESIZES (a WS `comment:updated` / `reaction:added` /
 * `resolved` event on an older comment used to push the user's read
 * position down by the delta). On FlatList the same prop was iOS-only
 * + JS-side; the FlashList path is steadier and animates the offset
 * compensation rather than snapping it.
 *
 * List engine: FlashList v2 (Shopify). Migrated from FlatList because:
 *   1. Cell recycling — markdown bubbles (Shiki highlight, image natural-
 *      size, lightbox provider injection) are expensive to mount; FlashList
 *      keeps them in recycled cells when scrolling through history rather
 *      than re-running the multi-pass render each time a row re-enters the
 *      window.
 *   2. Native MVCP — see paragraph above. Smoother behavior than FlatList's
 *      JS-side implementation when WS events resize an upper row.
 *   3. Async-render stability — async markdown size changes inside the
 *      viewport no longer cause the visual "twitch" we saw with FlatList,
 *      because FlashList re-layouts inside the shadow tree without
 *      surfacing a JS-side onContentSizeChange storm.
 *
 * What FlashList v2 does NOT change about this file:
 *   - The deep-link "land at bottom + flash on row mount" pattern (Linear
 *     iOS). FlashList's `scrollToIndex` is still estimate-based, so we
 *     intentionally don't try to land precisely on a specific comment —
 *     we land at the bottom and let `RootHighlightOverlay` claim the
 *     target when the user scrolls past it. `startRenderingFromBottom`
 *     is NOT enabled: a normal issue-open should show the header (title,
 *     description, status) first, not jump straight to the latest reply.
 *   - Spacing between rows. FlashList ignores `gap-*` on
 *     `contentContainer` the same way it does in chat-message-list.tsx —
 *     we use `ItemSeparatorComponent` for the 12 px breathing room and
 *     `ListHeaderComponentStyle` to add the same gap below the header.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Pressable,
  RefreshControl,
  View,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type ViewToken,
} from "react-native";
import { FlashList, type FlashListRef } from "@shopify/flash-list";
import { Ionicons } from "@expo/vector-icons";
import type { Issue, TimelineEntry } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { IssueHeaderCard } from "./issue-header-card";
import { IssueDescription } from "./issue-description";
import { IssueReactionRow } from "./issue-reaction-row";
import { ActivityRow } from "./activity-row";
import { CommentCard } from "./comment-card";
import { useLastViewedStore } from "@/data/stores/last-viewed-store";
import { coalesceTimeline } from "@/lib/timeline-coalesce";
import { buildTimelineRows, type TimelineRow } from "@/lib/timeline-thread";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { useCommentSelectStore } from "@/data/comment-select-store";

interface Props {
  issue: Issue;
  entries: TimelineEntry[] | undefined;
  timelineLoading: boolean;
  refreshing: boolean;
  onRefresh: () => void;
  /** Inbox deep-link target. Root comment id OR reply id — replies live
   *  inline inside their parent's CommentCard, so a reply target scrolls
   *  to the parent's row and the card highlights the matching child. */
  highlightCommentId?: string;
  /** Per-tap nonce. Re-tapping the same inbox row produces the same
   *  `highlightCommentId` but a fresh nonce, which re-triggers the
   *  scroll-and-flash effect (without this, identical props short-circuit). */
  highlightNonce?: string;
}

/** How long the flash stays "claimed" before we let a new highlight take
 *  over. The fade-out itself is driven by the Reanimated sequence inside
 *  CommentCard; this is just the upstream gate. 5s gives the user time
 *  to land at the bottom, realise the target is an older comment, and
 *  scroll up to it — the overlay still fires when the row mounts. */
const HIGHLIGHT_HOLD_MS = 5000;

/** Pixel slack at the bottom edge — inside this band we treat the user as
 *  "already at bottom" so the new-comment chip doesn't fire for entries
 *  the user is already about to see. */
const AT_BOTTOM_SLACK_PX = 80;

/** Sentinel id for the "New since last view" divider row injected into the
 *  FlatList data. Picked because it can never collide with a real comment
 *  / activity uuid. */
const DIVIDER_ID = "__divider__";

export function TimelineList({
  issue,
  entries,
  timelineLoading,
  refreshing,
  onRefresh,
  highlightCommentId,
  highlightNonce,
}: Props) {
  // Top-level selection subscription gates the outer "tap-outside-to-dismiss"
  // Pressable below. When null, the Pressable stays disabled and every tap
  // passes through to comment cards / chip rows / reactions normally.
  const selectingId = useCommentSelectStore((s) => s.selectingId);

  // Server already returns ASC oldest-first. Pipeline:
  //   1. coalesceTimeline → merge consecutive identical activities
  //   2. buildTimelineRows → reorder so replies sit adjacent to their parent
  //      and tag each reply with `replyTo` for the card to render the
  //      "↪ Replying to" header + thread-line border. This is the mobile
  //      flat-list interpretation of web's recursive reply tree.
  const data = useMemo<TimelineRow[]>(() => {
    if (!entries) return [];
    return buildTimelineRows(coalesceTimeline(entries));
  }, [entries]);

  const listRef = useRef<FlashListRef<TimelineRow>>(null);
  // Gates single-shot per (commentId, nonce) tuple. Re-tap from inbox
  // bumps the nonce → ref no longer matches → effect re-fires.
  const lastStampRef = useRef<string | null>(null);
  const [highlightedId, setHighlightedId] = useState<string | null>(null);

  // ── "New since last view" divider ─────────────────────────────────────
  // Snapshot the last-viewed timestamp ONCE on mount. Subsequent WS
  // appends shouldn't shift the divider — the user wants a stable
  // "where I was when I came back" boundary. The store update happens on
  // unmount, gated on the user having actually scrolled past the divider.
  const lastViewedSnapshotRef = useRef<string | null | undefined>(undefined);
  if (lastViewedSnapshotRef.current === undefined) {
    lastViewedSnapshotRef.current =
      useLastViewedStore.getState().getLastViewed(issue.id) ?? null;
  }
  const dividerAnchorId = useMemo(() => {
    const snapshot = lastViewedSnapshotRef.current;
    if (!snapshot) return null;
    // First entry strictly newer than the snapshot anchors the divider;
    // divider draws ABOVE this row. If everything is older, no divider.
    const found = data.find((r) => r.entry.created_at > snapshot);
    return found ? found.entry.id : null;
  }, [data]);
  const dividerScrolledPastRef = useRef(false);

  useEffect(() => {
    if (!highlightCommentId || data.length === 0) return;
    const stamp = `${highlightCommentId}:${highlightNonce ?? ""}`;
    if (lastStampRef.current === stamp) return;
    lastStampRef.current = stamp;

    setHighlightedId(highlightCommentId);

    const fade = setTimeout(() => setHighlightedId(null), HIGHLIGHT_HOLD_MS);
    return () => clearTimeout(fade);
  }, [highlightCommentId, highlightNonce, data.length]);

  // ── New-comment-while-reading chip ────────────────────────────────────
  // After landing, if WS appends new entries while the user is NOT at the
  // bottom, surface a floating "↓ N new" chip instead of silently shifting
  // content below the viewport. Tapping the chip scrolls to bottom and
  // clears the counter; reaching the bottom by hand also clears it.
  const [newCount, setNewCount] = useState(0);
  const isAtBottomRef = useRef(true);
  const lastDataLenRef = useRef(0);
  useEffect(() => {
    const grew = data.length > lastDataLenRef.current;
    const diff = data.length - lastDataLenRef.current;
    lastDataLenRef.current = data.length;
    if (!grew) return;
    // `isAtBottomRef` defaults to `true` so the initial 0→N load is treated
    // as "user is already at the bottom" and the chip stays silent until a
    // later WS append arrives while the user is scrolled up.
    if (isAtBottomRef.current) return;
    setNewCount((prev) => prev + diff);
  }, [data.length]);

  const handleScroll = useCallback(
    (e: NativeSyntheticEvent<NativeScrollEvent>) => {
      const { contentOffset, contentSize, layoutMeasurement } = e.nativeEvent;
      const distFromBottom =
        contentSize.height - (contentOffset.y + layoutMeasurement.height);
      const wasAtBottom = isAtBottomRef.current;
      isAtBottomRef.current = distFromBottom < AT_BOTTOM_SLACK_PX;
      // Reaching the bottom clears the unread-new chip — same iMessage /
      // chat-app semantic: "I've caught up".
      if (!wasAtBottom && isAtBottomRef.current && newCount > 0) {
        setNewCount(0);
      }
    },
    [newCount],
  );

  const onJumpToNew = useCallback(() => {
    listRef.current?.scrollToEnd({ animated: true });
    setNewCount(0);
  }, []);

  // ── Inject divider as a sentinel row before its anchor entry ──────────
  // FlatList wants a flat data[] and a stable key per row. Rather than
  // teach the renderer about "items + dividers" via a union type, fake a
  // TimelineRow with a sentinel id; renderItem checks the id first.
  const dataWithDivider = useMemo<TimelineRow[]>(() => {
    if (!dividerAnchorId) return data;
    const anchorIdx = data.findIndex((r) => r.entry.id === dividerAnchorId);
    if (anchorIdx <= 0) return data;
    const divider: TimelineRow = {
      // Cast: this entry is a synthetic marker, not a real TimelineEntry —
      // renderItem keys off `id === DIVIDER_ID` and never reads other fields.
      entry: {
        id: DIVIDER_ID,
        type: "activity",
        created_at: "",
        actor_type: "",
        actor_id: "",
      } as unknown as TimelineEntry,
      replies: [],
    };
    return [...data.slice(0, anchorIdx), divider, ...data.slice(anchorIdx)];
  }, [data, dividerAnchorId]);

  // Mark "scrolled past" once the divider row leaves the viewport — used
  // by the unmount effect below to decide whether to bump last-viewed.
  const viewabilityConfig = useMemo(
    () => ({ itemVisiblePercentThreshold: 1 }),
    [],
  );
  const handleViewableItemsChanged = useCallback(
    ({ viewableItems }: { viewableItems: ViewToken[] }) => {
      if (!dividerAnchorId) return;
      if (dividerScrolledPastRef.current) return;
      const dividerIdx = dataWithDivider.findIndex(
        (r) => r.entry.id === DIVIDER_ID,
      );
      if (dividerIdx < 0) return;
      const minVisibleIdx = viewableItems.reduce(
        (acc, v) => (v.index != null && v.index < acc ? v.index : acc),
        Number.POSITIVE_INFINITY,
      );
      if (minVisibleIdx > dividerIdx) {
        dividerScrolledPastRef.current = true;
      }
    },
    [dividerAnchorId, dataWithDivider],
  );
  // FlashList v2 captures `viewabilityConfigCallbackPairs` at mount —
  // "Changing viewabilityConfig on the fly is not supported." So we wrap
  // the handler in a stable ref-backed forwarder and pass a frozen pairs
  // array. The inner closure still updates with deps (dividerAnchorId,
  // dataWithDivider), but the prop identity FlashList sees is stable.
  const handlerRef = useRef(handleViewableItemsChanged);
  useEffect(() => {
    handlerRef.current = handleViewableItemsChanged;
  }, [handleViewableItemsChanged]);
  const stableViewabilityHandler = useCallback(
    (info: { viewableItems: ViewToken[] }) => handlerRef.current(info),
    [],
  );
  const viewabilityCallbackPairs = useRef([
    {
      viewabilityConfig,
      onViewableItemsChanged: stableViewabilityHandler,
    },
  ]);

  // On unmount, mark the issue's timeline as "viewed up to now" if the
  // user has either (a) scrolled past the divider or (b) had no divider
  // because everything was already older than their previous visit.
  // Otherwise leave the snapshot alone so a next visit preserves the
  // "where I was" line.
  const markViewed = useLastViewedStore((s) => s.markViewed);
  useEffect(() => {
    const issueId = issue.id;
    return () => {
      if (!dividerAnchorId || dividerScrolledPastRef.current) {
        markViewed(issueId);
      }
    };
    // We intentionally bind the cleanup to the issueId-snapshot only —
    // re-running on `dividerAnchorId` changes would lose the original
    // anchor's "scrolled past" state if WS extended the timeline mid-read.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [issue.id]);

  const ListHeader = (
    <View>
      <IssueHeaderCard issue={issue} />
      <IssueDescription issueId={issue.id} description={issue.description} />
      <IssueReactionRow issue={issue} />
      <View className="px-4 pt-4 pb-2 border-t border-border">
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          Activity
        </Text>
      </View>
      {timelineLoading && (!entries || entries.length === 0) ? (
        <View className="py-6 items-center">
          <ActivityIndicator />
        </View>
      ) : null}
    </View>
  );

  // When a fresh inbox deep-link arrives AND timeline data has loaded, force
  // a FlashList remount so `getInitialScrollIndex()` re-runs with
  // `startRenderingFromBottom: true` and lands on the last data item. If we
  // mounted before data arrived, FlashList's one-shot initial-scroll
  // (useRecyclerViewController.applyInitialScrollIndex, gated on
  // `initialScrollCompletedRef`) would have already declared completion at
  // length 0 and never re-fire. Switching the key when data goes empty →
  // non-empty under a live highlight is the cheapest way to re-arm it.
  const hasData = dataWithDivider.length > 0;
  const flashListKey =
    highlightCommentId && hasData
      ? `hl-${highlightNonce ?? "0"}`
      : "list";

  return (
    <View className="flex-1">
      {/* Outer Pressable owns the "tap anywhere outside the selected
          comment to exit text-selection mode" gesture. Disabled when
          no comment is selected → layout-only wrapper, every tap passes
          through to cells / chips / reactions. Active state captures any
          tap that didn't fire an inner Pressable — selecting CommentBody
          renders without its own Pressable wrapper (see comment-card.tsx
          `if (isSelecting) return body;`), so taps on the selected
          comment dismiss too, matching iOS Notes / iMessage. Scroll
          gestures are unaffected. */}
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
        // Drag-to-dismiss keyboard — when the user scrolls the timeline
        // while the composer keyboard is up, the keyboard slides down
        // interactively (iMessage / WhatsApp / Slack idiom). Pairs with the
        // composer's `onBlur` → auto-collapse to pill: scroll dismisses
        // keyboard → TextInput blurs → composer collapses if empty.
        keyboardDismissMode="on-drag"
        // Tap-on-row inside the list (long-press a comment, tap a
        // reaction) should still register even when the keyboard is up.
        keyboardShouldPersistTaps="handled"
        // FlashList v2 MVCP. `startRenderingFromBottom` only applies when a
        // deep-link is active — a normal issue-open lands at the top so the
        // user sees the header (title, description, status) first. After
        // initial paint the (always-on) MVCP keeps visible content stable
        // when upper rows resize via async markdown / WS events; we do NOT
        // set `autoscrollToBottomThreshold` because timeline uses an
        // explicit "↓ N new" chip instead of silently following appends.
        maintainVisibleContentPosition={{
          startRenderingFromBottom: !!highlightCommentId,
        }}
        // "Activity" is a section heading, not a sibling row — it should
        // hug the first entry the way iOS Settings / Linear sections do.
        // 4 px is just enough breathing room without making the heading
        // float above the list. (12 px = row-to-row gap, wrong here.)
        ListHeaderComponentStyle={{ marginBottom: 4 }}
        ItemSeparatorComponent={RowSeparator}
        renderItem={({ item }) => {
          if (item.entry.id === DIVIDER_ID) {
            return <UnreadDivider />;
          }
          return item.entry.type === "comment" ? (
            <CommentCard
              entry={item.entry}
              replies={item.replies}
              issueId={issue.id}
              issueIdentifier={issue.identifier}
              highlightedCommentId={highlightedId}
            />
          ) : (
            <ActivityRow entry={item.entry} />
          );
        }}
        onScroll={handleScroll}
        // Any user-initiated scroll exits comment text-selection mode —
        // matches iMessage's behavior where scrolling implicitly commits /
        // dismisses the selection caret. Hooks both drag-start and the
        // momentum kick after a flick so a fast scroll can't escape.
        onScrollBeginDrag={() =>
          useCommentSelectStore.getState().clear()
        }
        onMomentumScrollBegin={() =>
          useCommentSelectStore.getState().clear()
        }
        viewabilityConfigCallbackPairs={viewabilityCallbackPairs.current}
        refreshControl={
          <RefreshControl refreshing={refreshing} onRefresh={onRefresh} />
        }
        contentContainerStyle={{ paddingBottom: 16 }}
      />
      </Pressable>
      {newCount > 0 ? (
        <NewCommentChip count={newCount} onPress={onJumpToNew} />
      ) : null}
    </View>
  );
}

/**
 * 12 px vertical gap between every timeline row. FlashList ignores
 * `gap-*` on `contentContainer`, so the spacing is supplied via this
 * separator component — same pattern as chat-message-list.tsx.
 */
function RowSeparator() {
  return <View style={{ height: 12 }} />;
}

/**
 * Horizontal rule + "New" pill spanning the row width. Drawn between the
 * last entry the user had seen on their previous visit and the first one
 * they haven't. Mirrors Slack / iMessage / Things' "unread divider"
 * idiom — a passive visual mark, not interactive (no tap-to-dismiss; it
 * disappears the next time the user scrolls past and unmounts the screen).
 */
function UnreadDivider() {
  return (
    <View className="flex-row items-center gap-2 px-4">
      <View className="flex-1 h-px bg-destructive/40" />
      <Text className="text-[10px] uppercase tracking-wider font-medium text-destructive">
        New
      </Text>
      <View className="flex-1 h-px bg-destructive/40" />
    </View>
  );
}

/**
 * Floating "↓ N new" chip pinned above the composer area. Surfaces WS
 * arrivals the user can't currently see because they're scrolled up.
 * Tap → smooth scrollToEnd + reset counter. Reaching the bottom by hand
 * also clears it (see handleScroll above).
 *
 * Positioned absolute bottom-center inside the parent <View flex-1> wrap;
 * doesn't overlap content because the timeline's `contentContainer`
 * already has its own bottom padding for breathing room above the
 * composer hand-off.
 */
function NewCommentChip({
  count,
  onPress,
}: {
  count: number;
  onPress: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const fg = THEME[colorScheme].primaryForeground;
  return (
    <Pressable
      onPress={onPress}
      className="absolute bottom-3 self-center px-3.5 py-1.5 rounded-full bg-primary active:opacity-80 flex-row items-center gap-1.5"
      accessibilityRole="button"
      accessibilityLabel={`Jump to ${count} new ${count === 1 ? "message" : "messages"}`}
      style={{
        // shadow comes from system, not Tailwind — keeps the chip readable
        // against either light or dark timeline content beneath.
        shadowColor: "#000",
        shadowOffset: { width: 0, height: 2 },
        shadowOpacity: 0.18,
        shadowRadius: 6,
        elevation: 4,
      }}
    >
      <Ionicons name="arrow-down" size={14} color={fg} />
      <Text className="text-xs font-semibold text-primary-foreground">
        {count} new
      </Text>
    </Pressable>
  );
}
