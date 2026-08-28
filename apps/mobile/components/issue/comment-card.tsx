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
import { useCallback, useEffect, Fragment, useMemo, useState } from "react";
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
import { useT } from "@/lib/use-t";
import { ReactionBar } from "./reaction-bar";
import { useCommentLongPress } from "./comment-context-menu";
import { ActionSheetModal } from "@/components/ui/action-sheet";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useCommentFocusStore } from "@/data/stores/comment-focus-store";
import { commentSummary } from "@/lib/comment-summary";

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
  /** RUYI-28 controlled expansion. When true the root renders expanded
   *  regardless of its default collapsed state (deep-link target, focus
   *  intent from the comments modal, or just-published). The expansion
   *  set lives in the per-issue session store because FlashList recycles
   *  rows — per-row useState resets when the row scrolls out of view. */
  forceExpanded?: boolean;
  /** Same channel the composer uses (RUYI-28): fired with the SERVER
   *  comment id when a Retry of THIS row's failed optimistic comment
   *  succeeds, so the owning root expands exactly like a composer
   *  publish. Optional — the just-published auto-expand is a nicety, not
   *  required for correctness. */
  onCommentPublished?: (commentId: string) => void;
}

export function CommentCard({
  entry,
  replies = [],
  issueId,
  issueIdentifier,
  highlightedCommentId,
  forceExpanded = false,
  onCommentPublished,
}: Props) {
  // Resolved threads default to a single-line bar; tap expands in place for
  // the current session. Unmount (scroll out of viewport) resets — same
  // behavior as iOS Mail's "tap to expand a thread" pattern. Replies cannot
  // themselves be resolved (server enforces root-only), so the resolved flag
  // on the root is the single source of truth for this card.
  const resolved = !!entry.resolved_at;
  const [expanded, setExpanded] = useState(false);
  // RUYI-28 root collapse: normal (unresolved) roots default to a compact
  // single-line summary bar; the store-controlled set (session-scoped per
  // issue) overrides. The SAME store gate now also applies to resolved
  // roots: a directory focus intent (or deep link / just-published) that
  // targets a resolved thread must land on the FULL thread, not a bar the
  // user has to tap again. Manual collapse (either path) removes the id
  // from the store, so the user's collapse choice still wins afterwards.
  const collapseStore = useCommentFocusStore((s) => s.collapseRoot);
  const expandStore = useCommentFocusStore((s) => s.expandRoot);
  const storeExpanded = useCommentFocusStore((s) =>
    s.expandedRoots[issueId]?.has(entry.id),
  );
  const isRootExpanded = forceExpanded || storeExpanded || expanded;
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
  // and force the user to tap again. RUYI-28 extends the same rule to
  // NORMAL roots, which now also default to collapsed. The expansion is
  // ALSO written to the per-issue store so it survives FlashList recycling
  // (a deep-linked row that scrolls out of view mid-read must not
  // re-collapse when it remounts).
  useEffect(() => {
    if (!highlightedCommentId) return;
    if (
      highlightedCommentId === entry.id ||
      replies.some((r) => r.id === highlightedCommentId)
    ) {
      setExpanded(true);
      expandStore(issueId, entry.id);
    }
  }, [highlightedCommentId, entry.id, replies, issueId, expandStore]);

  // ── RUYI-28 collapsed root (normal, unresolved) ────────────────────────
  // Default collapsed; expansion is session-scoped per issue. The bar shows
  // a 120-cp / 2-line summary (see lib/comment-summary.ts). Deep-link /
  // focus-intent / just-published paths come in via `forceExpanded`.
  if (!resolved && !isRootExpanded) {
    return (
      <CollapsedRootBar
        entry={entry}
        replies={replies}
        onExpand={() => {
          // Write BOTH the local state (this render) and the per-issue
          // session store (survives FlashList cell recycling — a per-row
          // useState alone would re-collapse when the row scrolls out of
          // the recycle window and remounts).
          setExpanded(true);
          expandStore(issueId, entry.id);
        }}
      />
    );
  }

  if (resolved && !isRootExpanded) {
    return (
      <ResolvedThreadBar
        entry={entry}
        replies={replies}
        onExpand={() => {
          setExpanded(true);
          expandStore(issueId, entry.id);
        }}
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
              onCollapse={() => {
                setExpanded(false);
                // Clear the store gate too — a directory/deep-link focus
                // wrote this root into the session expansion set, and a
                // manual collapse must win over it (same contract as the
                // unresolved CollapseRow below).
                collapseStore(issueId, entry.id);
              }}
            />
          ) : (
            // RUYI-28: expanded normal root carries a subtle collapse
            // affordance so the collapsed-by-default state is reachable
            // without scrolling away. Same self-contained-Pressable shape
            // as ResolvedIndicator's collapse link.
            <CollapseRow
              onCollapse={() => {
                setExpanded(false);
                collapseStore(issueId, entry.id);
              }}
            />
          )}
          <CommentBody
            entry={entry}
            issueId={issueId}
            issueIdentifier={issueIdentifier}
            onPressChange={handlePressChange}
            onCommentPublished={onCommentPublished}
          />
          {replies.map((reply) => (
            <View key={reply.id} className="border-t border-border/60 pt-3">
              <CommentBody
                entry={reply}
                issueId={issueId}
                issueIdentifier={issueIdentifier}
                onPressChange={handlePressChange}
                onCommentPublished={onCommentPublished}
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
  const { t } = useT("issues");
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
        accessibilityLabel={t(
          "mobile.comment.resolved_bar_a11y",
          "Resolved thread by {{authors}}, {{count}} messages. Tap to expand.",
          { count: total, authors: authorsLabel },
        )}
      >
        <Ionicons name="checkmark-circle" size={18} color={mutedFg} />
        <Text
          className="flex-1 text-sm text-muted-foreground"
          numberOfLines={1}
        >
          {/* 单复数交给 i18next 的 count 规则，不再手写三元：中日韩没有
              语法数，硬拼 `1 message` / `2 messages` 翻不出来。 */}
          {t(
            "mobile.comment.resolved_bar",
            "Resolved · {{count}} messages by {{authors}}",
            { count: total, authors: authorsLabel },
          )}
        </Text>
        <Ionicons name="chevron-down" size={14} color={mutedFg} />
      </Pressable>
    </View>
  );
}

/**
 * Compact collapsed bar for a NORMAL (unresolved) root comment (RUYI-28).
 * Author + time + 120-cp / 2-line summary + reply count. Tap anywhere to
 * expand for this page session.
 *
 * Deliberately mirrors the shape of <ResolvedThreadBar> above (same
 * `bg-surface-1` bubble rhythm) so collapsed rows — resolved or not —
 * read as the same "row" idiom while scrolling.
 */
function CollapsedRootBar({
  entry,
  replies,
  onExpand,
}: {
  entry: TimelineEntry;
  replies: TimelineEntry[];
  onExpand: () => void;
}) {
  const { getName } = useActorLookup();
  const { t } = useT("issues");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const name = getName(
    entry.actor_type as "member" | "agent" | null | undefined,
    entry.actor_id,
  );
  const summary = commentSummary(entry.content);
  const total = 1 + replies.length;

  return (
    <View className="px-4">
      <Pressable
        onPress={onExpand}
        className="px-4 py-3 rounded-2xl bg-surface-1 gap-1 active:opacity-70"
        accessibilityRole="button"
        accessibilityLabel={t(
          "mobile.comment.root_collapsed_a11y",
          "Comment by {{authors}}, {{count}} messages. Tap to expand.",
          { authors: name, count: total },
        )}
      >
        <View className="flex-row items-center gap-2">
          <ActorAvatar
            type={entry.actor_type as "member" | "agent"}
            id={entry.actor_id}
            size={18}
          />
          <Text
            className="text-sm font-medium text-foreground flex-1"
            numberOfLines={1}
          >
            {name}
          </Text>
          <Text className="text-xs text-muted-foreground">
            · {timeAgo(entry.created_at)}
          </Text>
          <Ionicons name="chevron-down" size={14} color={mutedFg} />
        </View>
        {summary ? (
          <Text className="text-sm text-muted-foreground" numberOfLines={2}>
            {summary}
          </Text>
        ) : null}
        {replies.length > 0 ? (
          <Text className="text-xs text-muted-foreground">
            {/* Plurals go through i18next's count rules (zh/ja/ko only
                carry the _other branch). */}
            {t("mobile.comment.reply_count", "{{count}} replies", {
              count: replies.length,
            })}
          </Text>
        ) : null}
      </Pressable>
    </View>
  );
}

/**
 * Minimal collapse affordance at the top of an expanded NORMAL root
 * (RUYI-28). Right-aligned "Collapse" text at the same scale as
 * ResolvedIndicator's collapse link — no icon, no chrome; the summary bar
 * below (after collapse) already carries the identity row.
 */
function CollapseRow({ onCollapse }: { onCollapse: () => void }) {
  const { t } = useT("issues");
  return (
    <Pressable
      onPress={onCollapse}
      className="self-end active:opacity-60"
      hitSlop={6}
      accessibilityRole="button"
      accessibilityLabel={t(
        "mobile.comment.collapse_root_a11y",
        "Collapse comment",
      )}
    >
      <Text className="text-xs text-muted-foreground">
        {t("mobile.comment.collapse", "Collapse")}
      </Text>
    </Pressable>
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
  const { t } = useT("issues");
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
      accessibilityLabel={t(
        "mobile.comment.collapse_a11y",
        "Collapse resolved thread",
      )}
    >
      <Ionicons name="checkmark-circle" size={14} color={mutedFg} />
      <Text className="text-xs text-muted-foreground flex-1" numberOfLines={1}>
        {/* 名字嵌在句中要保留独立字号/字重，不能整句插值，所以 label 与
            名字分成两段。中日韩「解决者 / 解決者 / 해결한 사람」放在名字
            前同样成立，语序不需要倒置。 */}
        {t("mobile.comment.resolved_by", "Resolved by")}{" "}
        <Text className="text-xs text-foreground font-medium">
          {resolverName}
        </Text>
        {entry.resolved_at ? ` · ${timeAgo(entry.resolved_at)}` : ""}
      </Text>
      <Text className="text-xs text-muted-foreground">
        {t("mobile.comment.collapse", "Collapse")}
      </Text>
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
  onCommentPublished,
}: {
  entry: TimelineEntry;
  issueId: string;
  issueIdentifier: string | undefined;
  onPressChange?: (entryId: string, pressed: boolean) => void;
  onCommentPublished?: (commentId: string) => void;
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
  const { t } = useT("issues");
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
    createComment.mutate(
      {
        content: failed.content,
        parentId: failed.parentId,
        attachmentIds: failed.attachmentIds,
      },
      // RUYI-28: a successful Retry must expand the owning root exactly
      // like a composer publish — same precise channel, no last-row
      // guessing. `onPublished` is the composer's existing plumbing; the
      // server id arrives only on success (guarded the same way the
      // composer's mutateAsync path guards `created?.id`).
      { onSuccess: (created) => created?.id && onCommentPublished?.(created.id) },
    );
  }, [
    failed,
    qc,
    wsId,
    issueId,
    entry.id,
    createComment,
    onCommentPublished,
  ]);

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
          {edited ? t("mobile.comment.edited_suffix", " · (edited)") : ""}
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
    <Fragment>
      <Pressable onLongPress={longPress.onLongPress} delayLongPress={500}>
        {body}
      </Pressable>
      <ActionSheetModal {...longPress.modalProps} />
    </Fragment>
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
  const { t } = useT("issues");
  const { colorScheme } = useColorScheme();
  const destructive = THEME[colorScheme].destructive;
  return (
    <View className="flex-row items-center gap-2 mt-0.5">
      <Ionicons name="alert-circle" size={14} color={destructive} />
      <Text
        className="flex-1 text-xs text-destructive"
        numberOfLines={1}
      >
        {/* `error` 来自服务端/网络层，本身可能是英文；这里只兜底空值。
            服务端消息的本地化不在本批次范围内。 */}
        {error || t("mobile.comment.send_failed", "Couldn't send")}
      </Text>
      <Pressable
        onPress={onRetry}
        hitSlop={6}
        accessibilityRole="button"
        accessibilityLabel={t(
          "mobile.comment.retry_a11y",
          "Retry sending comment",
        )}
      >
        <Text className="text-xs text-primary font-medium">
          {t("mobile.comment.retry", "Retry")}
        </Text>
      </Pressable>
      <Pressable
        onPress={onDiscard}
        hitSlop={6}
        accessibilityRole="button"
        accessibilityLabel={t(
          "mobile.comment.discard_a11y",
          "Discard failed comment",
        )}
      >
        <Text className="text-xs text-muted-foreground font-medium">
          {t("mobile.comment.discard", "Discard")}
        </Text>
      </Pressable>
    </View>
  );
}
