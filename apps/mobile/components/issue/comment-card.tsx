/**
 * Comment timeline row. Rounded gray bubble containing the parent comment
 * plus, when applicable, every descendant reply stacked inline. The bubble
 * boundary itself is the thread indicator — no "↪ Replying to" header, no
 * recursive indentation. This matches the user's design call: "放在一个 card
 * 内部就行了 / no need for the Replying to label".
 *
 * Mobile flat-list rule (apps/mobile/CLAUDE.md): same comments as web,
 * different layout — web shows recursive tree, mobile shows one bubble per
 * thread. Counts agree (no comment is dropped or duplicated).
 *
 * Interaction: long-press inside a bubble fires a native iOS
 * `ActionSheetIOS` with the comment's actions (Reply, React…, Copy,
 * Select Text, Copy Link, Resolve, Delete). While the sheet is on screen
 * the targeted bubble's border highlights. See `useCommentLongPress` in
 * `./comment-context-menu.tsx`.
 *
 * Resolved threads render in a collapsed `<ResolvedThreadBar>` by default —
 * mirrors the same state language web uses (`packages/views/issues/
 * components/resolved-thread-bar.tsx`), but the visual is a single-line
 * tap-to-expand bar at iOS section-row scale. Tap expands the bar in place;
 * when expanded the resolved indicator stays at the top of the body so the
 * user keeps the "this thread is resolved" signal even while reading.
 */
import { useCallback, useEffect, useMemo, useState } from "react";
import { Pressable, View } from "react-native";
import Animated, {
  useAnimatedStyle,
  useSharedValue,
  withDelay,
  withSequence,
  withTiming,
} from "react-native-reanimated";
import { Ionicons } from "@expo/vector-icons";
import type { Reaction, TimelineEntry } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { useActorLookup } from "@/data/use-actor-name";
import { timeAgo } from "@/lib/time-ago";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Markdown } from "@/lib/markdown";
import { CommentAttachmentList } from "@/components/issue/comment-attachment-list";
import {
  discardFailedComment,
  useCreateComment,
  useToggleCommentReaction,
} from "@/data/mutations/issues";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { issueAttachmentsOptions } from "@/data/queries/issues";
import { useFailedCommentsStore } from "@/data/stores/failed-comments-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { ReactionBar } from "./reaction-bar";
import { useCommentLongPress } from "./comment-context-menu";
import { useCommentSelectStore } from "@/data/comment-select-store";

interface Props {
  entry: TimelineEntry;
  /** Flattened descendant replies. Rendered inline below the parent inside
   *  the same bubble, separated by a hairline divider. */
  replies?: TimelineEntry[];
  /** Plumbed through so each CommentBody can wire its reaction toggle to
   *  the correct issue's mutation key. */
  issueId: string;
  /** Human-readable identifier (e.g. `MUL-123`) used to build the shareable
   *  web URL for the long-press "Copy Link" item. Optional — that item
   *  hides when missing. */
  issueIdentifier: string | undefined;
  /** Inbox deep-link flash target. When this matches the root entry id we
   *  flash the outer bubble (ring + bg). When it matches a reply id we
   *  flash that reply's wrapper (bg only). Mirrors web's distinction at
   *  packages/views/issues/components/comment-card.tsx:498-682. */
  highlightedCommentId?: string | null;
}

export function CommentCard({
  entry,
  replies = [],
  issueId,
  issueIdentifier,
  highlightedCommentId,
}: Props) {
  // Resolved threads default to a single-line bar; tap expands in place for
  // the current session. Unmount (scroll out of viewport) resets — same
  // behavior as iOS Mail's "tap to expand a thread" pattern. Replies cannot
  // themselves be resolved (server enforces root-only), so the resolved flag
  // on the root is the single source of truth for this card.
  const resolved = !!entry.resolved_at;
  const [expanded, setExpanded] = useState(false);
  // Highlight ring while a long-press action sheet is on screen — child
  // CommentBody flips this via onPressChange so the outer bubble shell can
  // visually bind the sheet to the targeted entry.
  const [pressedEntryId, setPressedEntryId] = useState<string | null>(null);
  const handlePressChange = useCallback(
    (entryId: string, pressed: boolean) => {
      setPressedEntryId((cur) => {
        if (pressed) return entryId;
        return cur === entryId ? null : cur;
      });
    },
    [],
  );
  const isHighlighted =
    pressedEntryId === entry.id ||
    replies.some((r) => r.id === pressedEntryId);
  // Translucent primary-tinted background while ANY body inside this card
  // is in text-selection mode. Subtle visual cue that replaces the prior
  // Done pill — exit is via scroll / tab switch / selecting another body.
  const selectingId = useCommentSelectStore((s) => s.selectingId);
  const isSelectingHere =
    selectingId === entry.id || replies.some((r) => r.id === selectingId);

  // Inbox deep-link target inside a resolved thread expands automatically —
  // otherwise tapping a notification would just reveal a bar with no content
  // and force the user to tap again.
  useEffect(() => {
    if (!resolved || !highlightedCommentId) return;
    if (
      highlightedCommentId === entry.id ||
      replies.some((r) => r.id === highlightedCommentId)
    ) {
      setExpanded(true);
    }
  }, [resolved, highlightedCommentId, entry.id, replies]);

  if (resolved && !expanded) {
    return (
      <ResolvedThreadBar
        entry={entry}
        replies={replies}
        onExpand={() => setExpanded(true)}
      />
    );
  }

  return (
    <View className="px-4">
      <View className="rounded-2xl">
        {/* Bubble uses `surface-1` (L 98%) — extremely subtle elevation
         *  above the page, visible mostly through the rounded edge rather
         *  than the fill (iOS settings cell feel; see Refactoring UI #4
         *  "cards subtle from page"). Internal markdown elements (table
         *  headers / code blocks via markdown-style.ts) use `surface-2`
         *  (L 90%), 8% darker than the bubble — well over the 5%
         *  perceptibility threshold so the inner box is clearly framed.
         *  Border (L 84%) adds 6% on top for the outline. See global.css
         *  for the full 5-tier elevation scale.
         *
         *  Resolved-and-expanded path dims the bubble to 70% so the
         *  "this is settled" signal persists even while reading the
         *  body — mirrors web's muted resolved card visual. */}
        <View
          className={cn(
            "bg-surface-1 rounded-2xl px-4 py-3 gap-3 border-2 border-transparent transition-colors",
            resolved && "opacity-70",
            isHighlighted && "border-primary/30",
            isSelectingHere && "bg-primary/5 border-primary/30",
          )}
        >
          {resolved ? (
            <ResolvedIndicator
              entry={entry}
              onCollapse={() => setExpanded(false)}
            />
          ) : null}
          <CommentBody
            entry={entry}
            issueId={issueId}
            issueIdentifier={issueIdentifier}
            onPressChange={handlePressChange}
          />
          {replies.map((reply) => (
            <View key={reply.id} className="border-t border-border/60 pt-3">
              <CommentBody
                entry={reply}
                issueId={issueId}
                issueIdentifier={issueIdentifier}
                onPressChange={handlePressChange}
              />
              <ReplyHighlightOverlay
                active={highlightedCommentId === reply.id}
              />
            </View>
          ))}
        </View>
        <RootHighlightOverlay active={highlightedCommentId === entry.id} />
      </View>
    </View>
  );
}

/**
 * Compact "thread is resolved" bar — substitutes the full card when a
 * resolved root is collapsed (default state). Tap anywhere to expand.
 *
 * Mirrors web's `<ResolvedThreadBar>` (`packages/views/issues/components/
 * resolved-thread-bar.tsx`): checkmark + N participant authors + reply
 * count + chevron. On mobile we drop the dedicated <Card> chrome and use
 * the same `bg-surface-1` bubble so the resolved bar reads as the same
 * "row" rhythm as the full card it stands in for.
 */
function ResolvedThreadBar({
  entry,
  replies,
  onExpand,
}: {
  entry: TimelineEntry;
  replies: TimelineEntry[];
  onExpand: () => void;
}) {
  const { getName } = useActorLookup();
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  // Unique participant set across root + replies, preserving chronological
  // order of first appearance. Up to two authors are named; the rest are
  // rolled into "+N more" so the bar stays a single line on a narrow phone.
  const authorsLabel = useMemo(() => {
    const MAX_NAMED = 2;
    const seen = new Set<string>();
    const ordered: { type: string | null; id: string | null }[] = [];
    for (const e of [entry, ...replies]) {
      const key = `${e.actor_type}:${e.actor_id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      ordered.push({ type: e.actor_type, id: e.actor_id });
    }
    const named = ordered
      .slice(0, MAX_NAMED)
      .map((a) =>
        getName(a.type as "member" | "agent" | null | undefined, a.id),
      )
      .join(", ");
    const remaining = ordered.length - MAX_NAMED;
    return remaining > 0 ? `${named} +${remaining}` : named;
  }, [entry, replies, getName]);

  const total = 1 + replies.length;

  return (
    <View className="px-4">
      <Pressable
        onPress={onExpand}
        className="flex-row items-center gap-2.5 px-4 py-3 rounded-2xl bg-surface-1 active:opacity-70"
        accessibilityRole="button"
        accessibilityLabel={`Resolved thread by ${authorsLabel}, ${total} ${total === 1 ? "message" : "messages"}. Tap to expand.`}
      >
        <Ionicons name="checkmark-circle" size={18} color={mutedFg} />
        <Text
          className="flex-1 text-sm text-muted-foreground"
          numberOfLines={1}
        >
          Resolved · {total} {total === 1 ? "message" : "messages"} by{" "}
          {authorsLabel}
        </Text>
        <Ionicons name="chevron-down" size={14} color={mutedFg} />
      </Pressable>
    </View>
  );
}

/**
 * Resolved indicator row that sits at the top of an expanded resolved
 * thread. Carries the "who resolved + when" attribution and a collapse
 * affordance — equivalent to web's "Mark as resolved" header bar
 * (`packages/views/issues/components/comment-card.tsx:519-532`).
 *
 * Tap collapses the thread back to the bar without firing the
 * <CommentBody> long-press action sheet (the row is a self-contained
 * Pressable, sits above CommentBody in the bubble's gap-3 layout).
 */
function ResolvedIndicator({
  entry,
  onCollapse,
}: {
  entry: TimelineEntry;
  onCollapse: () => void;
}) {
  const { getName } = useActorLookup();
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const resolverName = getName(
    entry.resolved_by_type as "member" | "agent" | null | undefined,
    entry.resolved_by_id,
  );

  return (
    <Pressable
      onPress={onCollapse}
      className="flex-row items-center gap-2 active:opacity-60"
      accessibilityRole="button"
      accessibilityLabel="Collapse resolved thread"
    >
      <Ionicons name="checkmark-circle" size={14} color={mutedFg} />
      <Text className="text-xs text-muted-foreground flex-1" numberOfLines={1}>
        Resolved by{" "}
        <Text className="text-xs text-foreground font-medium">
          {resolverName}
        </Text>
        {entry.resolved_at ? ` · ${timeAgo(entry.resolved_at)}` : ""}
      </Text>
      <Text className="text-xs text-muted-foreground">Collapse</Text>
    </Pressable>
  );
}

/**
 * Animated highlight overlay for a root comment bubble. Sits absolute-
 * positioned over the parent <View className="rounded-2xl">, no pointer
 * capture (long-press still works through it). Border + background wash
 * — equivalent to web's `ring-2 ring-brand/50 bg-brand/5`.
 *
 * Reflow note: animating `borderWidth` would push children every frame,
 * so we keep it constant at 2 and animate `opacity` 0→1→0. Same trick
 * for the wash. Single shared value, one animated style.
 */
function RootHighlightOverlay({ active }: { active: boolean }) {
  const progress = useSharedValue(0);

  useEffect(() => {
    if (!active) return;
    // 700ms fade-in → 1800ms hold → 700ms fade-out. Matches web's
    // `transition-colors duration-700` + `setTimeout(2500)` timing.
    progress.value = withSequence(
      withTiming(1, { duration: 700 }),
      withDelay(1800, withTiming(0, { duration: 700 })),
    );
  }, [active, progress]);

  const style = useAnimatedStyle(() => ({ opacity: progress.value }));

  // Brand colour comes from the `brand` token; alpha via NativeWind `/50`
  // syntax mirrors web's `ring-brand/50 bg-brand/5`. Only opacity is
  // animated — the borderColor / backgroundColor stay constant, so
  // className is safe here (animating those channels via className isn't).
  return (
    <Animated.View
      pointerEvents="none"
      className="absolute inset-0 rounded-2xl border-2 border-brand/50 bg-brand/5"
      style={style}
    />
  );
}

/**
 * Animated wash overlay for a reply row. Same timing as root, but no
 * border — mirrors web's reply branch which applies only `bg-brand/5`
 * (packages/views/issues/components/comment-card.tsx:682).
 */
function ReplyHighlightOverlay({ active }: { active: boolean }) {
  const progress = useSharedValue(0);

  useEffect(() => {
    if (!active) return;
    progress.value = withSequence(
      withTiming(1, { duration: 700 }),
      withDelay(1800, withTiming(0, { duration: 700 })),
    );
  }, [active, progress]);

  const style = useAnimatedStyle(() => ({ opacity: progress.value }));

  return (
    <Animated.View
      pointerEvents="none"
      className="absolute inset-0 bg-brand/5"
      style={style}
    />
  );
}

function CommentBody({
  entry,
  issueId,
  issueIdentifier,
  onPressChange,
}: {
  entry: TimelineEntry;
  issueId: string;
  issueIdentifier: string | undefined;
  onPressChange?: (entryId: string, pressed: boolean) => void;
}) {
  // When this comment is the active selection target, drop the long-press
  // wrapper AND make the markdown selectable — so the next long-press
  // routes to UIKit's native text-selection magnifier instead of our
  // gesture handler. Selection mode is exited via the Done pill, scrolling
  // the timeline, or unmounting the issue screen.
  const isSelecting = useCommentSelectStore(
    (s) => s.selectingId === entry.id,
  );
  const { getName } = useActorLookup();
  const userId = useAuthStore((s) => s.user?.id);
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const toggle = useToggleCommentReaction(issueId);
  const qc = useQueryClient();
  const createComment = useCreateComment(issueId);
  // Failed-comment state for THIS entry — undefined when the entry is a
  // normal server-backed comment OR an in-flight optimistic. Only set when
  // the matching `useCreateComment` mutation errored and the entry was
  // intentionally left in the cache to surface inline retry.
  const failed = useFailedCommentsStore((s) => s.failed[entry.id]);
  // Same query as IssueDescription — TanStack dedupes so this fires once
  // per issue regardless of how many comments need to resolve attachments.
  const { data: attachments } = useQuery(
    issueAttachmentsOptions(wsId, issueId),
  );

  const name = getName(
    entry.actor_type as "member" | "agent" | null | undefined,
    entry.actor_id,
  );
  const edited =
    entry.updated_at &&
    entry.created_at &&
    entry.updated_at !== entry.created_at;

  // Reactions live on TimelineEntry.reactions (mirrored from Comment).
  // Pass through to the bar; toggle finds existing match by emoji + actor.
  const reactions: Reaction[] = (entry.reactions ?? []) as Reaction[];

  const onToggleReaction = useCallback(
    (emoji: string) => {
      const existing = reactions.find(
        (r) =>
          r.emoji === emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggle.mutate({ commentId: entry.id, emoji, existing });
    },
    [reactions, userId, toggle, entry.id],
  );

  const handleRetry = useCallback(() => {
    if (!failed || !wsId) return;
    // Remove the stale optimistic + failed marker BEFORE re-firing so the
    // mutation's own optimistic insert lands on a clean slate instead of
    // creating a duplicate row. The new attempt mints a fresh optimistic id.
    discardFailedComment(qc, wsId, issueId, entry.id);
    createComment.mutate({
      content: failed.content,
      parentId: failed.parentId,
      attachmentIds: failed.attachmentIds,
    });
  }, [failed, qc, wsId, issueId, entry.id, createComment]);

  const handleDiscard = useCallback(() => {
    if (!wsId) return;
    discardFailedComment(qc, wsId, issueId, entry.id);
  }, [qc, wsId, issueId, entry.id]);

  // Per-comment attachments render in two complementary places:
  //   - inline via the markdown renderer when the content references
  //     them with `![](url)` (typical for web/desktop comments authored
  //     in the rich editor)
  //   - via <CommentAttachmentList> below the body when they exist but
  //     aren't referenced in markdown (mobile-authored comments take this
  //     path — see inline-comment-composer.tsx for why mobile doesn't
  //     inline-insert).
  // Mirrors web's split: comment-card.tsx:124 `AttachmentList`.
  //
  // When NOT selecting: long-press fires the native ActionSheetIOS via
  // useCommentLongPress. Markdown is non-selectable so the long-press
  // gesture doesn't race UIKit's text selection.
  //
  // When selecting: long-press wrapper is gone, markdown is selectable.
  // The next long-press fires UIKit's native text-selection magnifier
  // + handles + Copy/Look Up callout. The outer bubble shell carries a
  // translucent primary-tint background as the mode cue (no Done pill).
  // Exit: scroll the timeline, leave the issue, or long-press another body.
  const longPress = useCommentLongPress(entry, issueId, issueIdentifier);

  useEffect(() => {
    if (isSelecting) return;
    onPressChange?.(entry.id, longPress.isPressed);
  }, [longPress.isPressed, entry.id, isSelecting, onPressChange]);

  const body = (
    <View className="gap-2">
      <View className="flex-row items-center gap-2">
        <ActorAvatar
          type={entry.actor_type as "member" | "agent"}
          id={entry.actor_id}
          size={24}
          showPresence
        />
        <Text className="text-sm font-medium text-foreground">{name}</Text>
        <Text className="text-xs text-muted-foreground">
          · {timeAgo(entry.created_at)}
          {edited ? " · (edited)" : ""}
        </Text>
      </View>
      {entry.content ? (
        <Markdown
          content={entry.content}
          attachments={attachments}
          selectable={isSelecting}
        />
      ) : null}
      <CommentAttachmentList
        attachments={entry.attachments}
        content={entry.content}
      />
      {failed ? (
        <FailedActions
          error={failed.error}
          onRetry={handleRetry}
          onDiscard={handleDiscard}
        />
      ) : (
        <ReactionBar
          reactions={reactions}
          currentUserId={userId}
          onToggle={onToggleReaction}
        />
      )}
    </View>
  );

  if (isSelecting) return body;

  return (
    <Pressable onLongPress={longPress.onLongPress} delayLongPress={500}>
      {body}
    </Pressable>
  );
}

/**
 * Inline retry strip shown beneath a failed optimistic comment body. Sits
 * where ReactionBar normally lives — same vertical rhythm, but the slot
 * carries the error message + Retry/Discard buttons. Single source of the
 * error surface (no parallel toast), so the user always lands on the row
 * they typed if they come back later.
 */
function FailedActions({
  error,
  onRetry,
  onDiscard,
}: {
  error: string;
  onRetry: () => void;
  onDiscard: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const destructive = THEME[colorScheme].destructive;
  return (
    <View className="flex-row items-center gap-2 mt-0.5">
      <Ionicons name="alert-circle" size={14} color={destructive} />
      <Text
        className="flex-1 text-xs text-destructive"
        numberOfLines={1}
      >
        {error || "Couldn't send"}
      </Text>
      <Pressable
        onPress={onRetry}
        hitSlop={6}
        accessibilityRole="button"
        accessibilityLabel="Retry sending comment"
      >
        <Text className="text-xs text-primary font-medium">Retry</Text>
      </Pressable>
      <Pressable
        onPress={onDiscard}
        hitSlop={6}
        accessibilityRole="button"
        accessibilityLabel="Discard failed comment"
      >
        <Text className="text-xs text-muted-foreground font-medium">
          Discard
        </Text>
      </Pressable>
    </View>
  );
}
