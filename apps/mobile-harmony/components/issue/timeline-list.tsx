/**
 * HarmonyOS port of apps/mobile/components/issue/timeline-list.tsx — the
 * scrolling timeline. ASC chronological — oldest at top, newest near the
 * bottom (above the composer). Pull-to-refresh refetches issue + timeline.
 *
 * Backend returns the full timeline in one shot (server-side pagination was
 * dropped in #2322). Pipeline:
 *   1. coalesceTimeline → merge consecutive identical activities
 *   2. buildTimelineRows → bundle each reply chain into its parent row
 *
 * Platform substitutions (documented deviations from the iOS file):
 *   - FlashList v2 → FlatList. FlashList is not on the RNOH matrix; the
 *     cell-recycling and native MVCP benefits don't carry over, so the
 *     FlashList remount/deep-link landing trick is replaced by a plain
 *     `scrollToEnd` once data lands under a live highlight (estimate-free,
 *     good enough for the one-shot flat list here).
 *   - `maintainVisibleContentPosition` is dropped: on FlatList it is
 *     iOS/Android-native-only and unverified on RNOH; the "↓ N new" chip
 *     covers the append-while-scrolled-up case instead.
 *   - ImageSequenceProvider (swipeable lightbox sequence) is dropped — the
 *     ported MarkdownImage opens its own per-image fullscreen Modal.
 */
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, FlatList, RefreshControl, StyleSheet, View, type NativeScrollEvent, type NativeSyntheticEvent, type ViewToken } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { Issue, TimelineEntry } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Icon } from "@/components/ui/icon";
import { IssueHeaderCard } from "./issue-header-card";
import { IssueDescription } from "./issue-description";
import { IssueReactionRow } from "./issue-reaction-row";
import { ActivityRow } from "./activity-row";
import { CommentCard } from "./comment-card";
import { useLastViewedStore } from "@/data/stores/last-viewed-store";
import { coalesceTimeline } from "@/lib/timeline-coalesce";
import { buildTimelineRows, type TimelineRow } from "@/lib/timeline-thread";
import { useWorkspaceStore } from "@/data/workspace-store";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { useCommentSelectStore } from "@/data/comment-select-store";
import type { IssuePickerField } from "./attribute-row";

interface Props {
  issue: Issue;
  entries: TimelineEntry[] | undefined;
  timelineLoading: boolean;
  refreshing: boolean;
  onRefresh: () => void;
  /** Opens an attribute picker sheet for this issue (status, priority, …). */
  onOpenPicker: (field: IssuePickerField) => void;
  /** Opens the issue's runs surface. */
  onOpenRuns: (issueId: string) => void;
  /** Inbox deep-link target. Root comment id OR reply id — replies live
   *  inline inside their parent's CommentCard, so a reply target highlights
   *  the matching child. */
  highlightCommentId?: string;
  /** Per-tap nonce. Re-tapping the same inbox row produces the same
   *  `highlightCommentId` but a fresh nonce, which re-triggers the
   *  scroll-and-flash effect (without this, identical props short-circuit). */
  highlightNonce?: string;
}

/** How long the flash stays "claimed" before we let a new highlight take
 *  over. The fade-out itself is driven by the Animated sequence inside
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
  onOpenPicker,
  onOpenRuns,
  highlightCommentId,
  highlightNonce,
}: Props) {
  const c = useThemeColors();
  // Top-level selection subscription gates the outer "tap-outside-to-dismiss"
  // Pressable below. When null, the Pressable stays disabled and every tap
  // passes through to comment cards / chip rows / reactions normally.
  const selectingId = useCommentSelectStore((s) => s.selectingId);

  // Server already returns ASC oldest-first. Pipeline: coalesce → bundle
  // replies under their parent.
  const data = useMemo<TimelineRow[]>(() => {
    if (!entries) return [];
    return buildTimelineRows(coalesceTimeline(entries));
  }, [entries]);

  const listRef = useRef<FlatList<TimelineRow>>(null);
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

  // Deep-link highlight: set the flash target, hold for HIGHLIGHT_HOLD_MS,
  // and land at the bottom of the list once data exists (replies live in
  // parent rows, so bottom is the best estimate without height predictors).
  useEffect(() => {
    if (!highlightCommentId || data.length === 0) return;
    const stamp = `${highlightCommentId}:${highlightNonce ?? ""}`;
    if (lastStampRef.current === stamp) return;
    lastStampRef.current = stamp;

    setHighlightedId(highlightCommentId);
    // Wait a tick so the FlatList has laid out before scrolling.
    requestAnimationFrame(() => {
      listRef.current?.scrollToEnd({ animated: false });
    });

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
  // FlatList captures viewability callbacks unstably otherwise — wrap the
  // handler in a ref-backed forwarder so the callback identity is stable
  // while the closure stays fresh.
  const handlerRef = useRef(handleViewableItemsChanged);
  useEffect(() => {
    handlerRef.current = handleViewableItemsChanged;
  }, [handleViewableItemsChanged]);
  const stableViewabilityHandler = useCallback(
    (info: { viewableItems: ViewToken[] }) => handlerRef.current(info),
    [],
  );
  const viewabilityConfig = useMemo(
    () => ({ itemVisiblePercentThreshold: 1 }),
    [],
  );

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
    // Bind the cleanup to the issueId snapshot only — re-running on
    // `dividerAnchorId` changes would lose the original anchor's
    // "scrolled past" state if WS extended the timeline mid-read.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [issue.id]);

  const ListHeader = (
    <View>
      <IssueHeaderCard
        issue={issue}
        onOpenPicker={onOpenPicker}
        onOpenRuns={onOpenRuns}
      />
      <IssueDescription issueId={issue.id} description={issue.description} />
      <IssueReactionRow issue={issue} />
      <View style={[styles.activityHeading, { borderTopColor: c.border }]}>
        <Text
          style={[styles.activityHeadingText, { color: c.mutedForeground }]}
        >
          Activity
        </Text>
      </View>
      {timelineLoading && (!entries || entries.length === 0) ? (
        <View style={styles.loadingRow}>
          <ActivityIndicator />
        </View>
      ) : null}
    </View>
  );

  return (
    <View style={styles.fill}>
      {/* Outer Pressable owns the "tap anywhere outside the selected
          comment to exit text-selection mode" gesture. Disabled when
          no comment is selected → layout-only wrapper, every tap passes
          through to cells / chips / reactions normally. */}
      <Pressable
        onPress={
          selectingId
            ? () => useCommentSelectStore.getState().clear()
            : undefined
        }
        disabled={!selectingId}
        style={styles.fill}
      >
        <FlatList
          ref={listRef}
          data={dataWithDivider}
          keyExtractor={(row) => row.entry.id}
          ListHeaderComponent={ListHeader}
          // Drag-to-dismiss keyboard — scrolling the timeline with the
          // composer keyboard up slides the keyboard down (iMessage /
          // WhatsApp / Slack idiom). Pairs with the composer's `onBlur`
          // → auto-collapse to pill.
          keyboardDismissMode="on-drag"
          // Tap-on-row inside the list (long-press a comment, tap a
          // reaction) should still register even when the keyboard is up.
          keyboardShouldPersistTaps="handled"
          // "Activity" is a section heading, not a sibling row — it should
          // hug the first entry the way iOS Settings / Linear sections do.
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
          viewabilityConfig={viewabilityConfig}
          onViewableItemsChanged={stableViewabilityHandler}
          refreshControl={
            <RefreshControl refreshing={refreshing} onRefresh={onRefresh} />
          }
          contentContainerStyle={styles.content}
        />
      </Pressable>
      {newCount > 0 ? (
        <NewCommentChip count={newCount} onPress={onJumpToNew} />
      ) : null}
    </View>
  );
}

/**
 * 12 px vertical gap between every timeline row.
 */
function RowSeparator() {
  return <View style={styles.rowSeparator} />;
}

/**
 * Horizontal rule + "New" pill spanning the row width. Drawn between the
 * last entry the user had seen on their previous visit and the first one
 * they haven't. Mirrors Slack / iMessage / Things' "unread divider" idiom —
 * a passive visual mark, not interactive (it disappears the next time the
 * user scrolls past and unmounts the screen).
 */
function UnreadDivider() {
  const c = useThemeColors();
  const rule = { backgroundColor: withAlpha(c.destructive, 0.4) };
  return (
    <View style={styles.dividerRow}>
      <View style={[styles.dividerRule, rule]} />
      <Text style={[styles.dividerLabel, { color: c.destructive }]}>New</Text>
      <View style={[styles.dividerRule, rule]} />
    </View>
  );
}

/**
 * Floating "↓ N new" chip pinned above the composer area. Surfaces WS
 * arrivals the user can't currently see because they're scrolled up.
 * Tap → smooth scrollToEnd + reset counter. Reaching the bottom by hand
 * also clears it (see handleScroll above).
 */
function NewCommentChip({
  count,
  onPress,
}: {
  count: number;
  onPress: () => void;
}) {
  const c = useThemeColors();
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`Jump to ${count} new ${count === 1 ? "message" : "messages"}`}
      style={({ pressed }) => [
        styles.chip,
        { backgroundColor: c.primary },
        pressed ? { opacity: 0.8 } : null,
      ]}
    >
      <Icon name="arrow-down" size={14} color={c.primaryForeground} />
      <Text
        style={[styles.chipLabel, { color: c.primaryForeground }]}
      >
        {count} new
      </Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  // paddingBottom 16
  content: { paddingBottom: 16 },
  // height 12
  rowSeparator: { height: 12 },
  // px-4 pt-4 pb-2 border-t
  activityHeading: {
    paddingHorizontal: 16,
    paddingTop: 16,
    paddingBottom: 8,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  // text-xs uppercase tracking-wider font-medium
  activityHeadingText: {
    fontSize: 12,
    textTransform: "uppercase",
    letterSpacing: 0.8,
    fontWeight: "500",
  },
  // py-6 items-center
  loadingRow: { paddingVertical: 24, alignItems: "center" },
  // flex-row items-center gap-2 px-4
  dividerRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 16,
  },
  // flex-1 h-px bg-destructive/40
  dividerRule: { flex: 1, height: StyleSheet.hairlineWidth },
  // text-[10px] uppercase tracking-wider font-medium
  dividerLabel: {
    fontSize: 10,
    textTransform: "uppercase",
    letterSpacing: 0.8,
    fontWeight: "500",
  },
  // absolute bottom-3 self-center px-3.5 py-1.5 rounded-full flex-row items-center gap-1.5
  chip: {
    position: "absolute",
    bottom: 12,
    alignSelf: "center",
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    paddingHorizontal: 14,
    paddingVertical: 6,
    borderRadius: 999,
    shadowColor: "#000",
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.18,
    shadowRadius: 6,
    elevation: 4,
  },
  // text-xs font-semibold
  chipLabel: { fontSize: 12, fontWeight: "600" },
});
