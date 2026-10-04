/**
 * HarmonyOS port of apps/mobile/components/issue/comment-card.tsx.
 * Comment timeline row. Rounded surface bubble containing the parent comment
 * plus, when applicable, every descendant reply stacked inline. The bubble
 * boundary itself is the thread indicator — no "↪ Replying to" header, no
 * recursive indentation.
 *
 * Mobile flat-list rule (apps/mobile/CLAUDE.md): same comments as web,
 * different layout — web shows recursive tree, mobile shows one bubble per
 * thread. Counts agree (no comment is dropped or duplicated).
 *
 * Interaction: long-press inside a bubble opens the comment action menu
 * (BottomSheet — the ActionSheetIOS stand-in; see comment-context-menu.tsx).
 * While the sheet is on screen the targeted bubble's border highlights.
 *
 * Resolved threads render in a collapsed single-line bar by default — tap
 * expands the bar in place; when expanded the resolved indicator stays at
 * the top of the body.
 *
 * Platform substitution: the iOS highlight overlays animate with Reanimated
 * (withSequence/withDelay/withTiming); Reanimated is not wired into this
 * app's Babel pipeline, so the identical 700ms-in / 1800ms-hold / 700ms-out
 * sequence runs on RN's core Animated (same call shape as pulse-dot.tsx).
 */
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Animated, Easing, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { Reaction, TimelineEntry } from "@multica/core/types";
import {
  commentLandingTarget,
  isDeletedComment,
} from "@multica/core/issues/comment-deletion";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { Icon } from "@/components/ui/icon";
import { useActorLookup } from "@/data/use-actor-name";
import { timeAgo } from "@/lib/time-ago";
import { Markdown } from "@/lib/markdown/markdown";
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
import { withAlpha, type ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { ReactionBar } from "./reaction-bar";
import { CommentActionsSheet, useCommentMenu } from "./comment-context-menu";

interface Props {
  entry: TimelineEntry;
  /** Flattened descendant replies. Rendered inline below the parent inside
   *  the same bubble, separated by a hairline divider. */
  replies?: TimelineEntry[];
  /** Plumbed through so each CommentBody can wire its reaction toggle to
   *  the correct issue's mutation key. */
  issueId: string;
  /** Human-readable identifier (e.g. `MUL-123`) used to build the shareable
   *  web URL for the menu's "Copy Link" item. Optional — that item
   *  hides when missing. */
  issueIdentifier: string | undefined;
  /** Inbox deep-link flash target. When this matches the root entry id we
   *  flash the outer bubble (ring + bg). When it matches a reply id we
   *  flash that reply's wrapper (bg only). Mirrors web's distinction. */
  highlightedCommentId?: string | null;
}

export function CommentCard({
  entry,
  replies = [],
  issueId,
  issueIdentifier,
  highlightedCommentId,
}: Props) {
  const c = useThemeColors();
  // Resolved threads default to a single-line bar; tap expands in place for
  // the current session. Unmount (scroll out of viewport) resets. Replies
  // cannot themselves be resolved (server enforces root-only), so the
  // resolved flag on the root is the single source of truth for this card.
  const resolved = !!entry.resolved_at;
  const [expanded, setExpanded] = useState(false);
  // Highlight ring while the long-press action menu is on screen — child
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

  const visibleReplies = replies.filter((reply) => !isDeletedComment(reply));
  // A deleted reply renders nothing, so a notification pointing at one has no
  // row to flash. Flash the comment just above where it was instead — the
  // expansion above still keys off the original id, which is in this thread
  // either way.
  const highlightId = highlightedCommentId
    ? commentLandingTarget(highlightedCommentId, entry.id, replies)
    : highlightedCommentId;

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
    <View style={styles.outerPad}>
      <View style={styles.outerRadius}>
        {/* Bubble uses `surface-1` (L 98%) — extremely subtle elevation above
         *  the page. Resolved-and-expanded path dims the bubble to 70% so the
         *  "this is settled" signal persists even while reading the body. */}
        <View
          style={[
            styles.bubble,
            { backgroundColor: c.surface1 },
            resolved && styles.bubbleResolved,
            isHighlighted
              ? { borderColor: withAlpha(c.primary, 0.3) }
              : null,
            isSelectingHere
              ? {
                  backgroundColor: withAlpha(c.primary, 0.05),
                  borderColor: withAlpha(c.primary, 0.3),
                }
              : null,
          ]}
        >
          {resolved ? (
            <ResolvedIndicator entry={entry} onCollapse={() => setExpanded(false)} />
          ) : null}
          <CommentBody
            entry={entry}
            issueId={issueId}
            issueIdentifier={issueIdentifier}
            onPressChange={handlePressChange}
          />
          {/* A deleted reply renders nothing: its row is kept only so its own
           *  replies keep a direct parent (#8296), and this list is flat, so
           *  they already render in its place. A deleted ROOT still renders
           *  its placeholder — it heads the thread. */}
          {visibleReplies.map((reply) => (
            <View
              key={reply.id}
              style={[styles.replyWrap, { borderTopColor: withAlpha(c.border, 0.6) }]}
            >
              <CommentBody
                entry={reply}
                issueId={issueId}
                issueIdentifier={issueIdentifier}
                onPressChange={handlePressChange}
              />
              <ReplyHighlightOverlay active={highlightId === reply.id} colors={c} />
            </View>
          ))}
        </View>
        <RootHighlightOverlay active={highlightId === entry.id} colors={c} />
      </View>
    </View>
  );
}

/**
 * Compact "thread is resolved" bar — substitutes the full card when a
 * resolved root is collapsed (default state). Tap anywhere to expand.
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
  const c = useThemeColors();
  const { getName } = useActorLookup();

  // Unique participant set across root + replies, preserving chronological
  // order of first appearance. Up to two authors are named; the rest are
  // rolled into "+N more" so the bar stays a single line on a narrow phone.
  const authorsLabel = useMemo(() => {
    const MAX_NAMED = 2;
    const seen = new Set<string>();
    const ordered: { type: string | null; id: string | null; name?: string }[] =
      [];
    for (const e of [entry, ...replies]) {
      // A deleted comment names no author (mirrors web's useAuthorsLabel).
      if (isDeletedComment(e)) continue;
      const key = `${e.actor_type}:${e.actor_id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      ordered.push({ type: e.actor_type, id: e.actor_id, name: e.actor_name });
    }
    const named = ordered
      .slice(0, MAX_NAMED)
      .map((a) =>
        a.name ||
        getName(a.type as "member" | "agent" | null | undefined, a.id),
      )
      .join(", ");
    const remaining = ordered.length - MAX_NAMED;
    return remaining > 0 ? `${named} +${remaining}` : named;
  }, [entry, replies, getName]);

  // Deleted replies render nothing when the thread expands, so the folded
  // count must not promise them either.
  const total = 1 + replies.filter((reply) => !isDeletedComment(reply)).length;

  return (
    <View style={styles.outerPad}>
      <Pressable
        onPress={onExpand}
        accessibilityRole="button"
        accessibilityLabel={`Resolved thread by ${authorsLabel}, ${total} ${total === 1 ? "message" : "messages"}. Tap to expand.`}
        style={({ pressed }) => [
          styles.resolvedBar,
          { backgroundColor: c.surface1 },
          pressed ? { opacity: 0.7 } : null,
        ]}
      >
        <Icon name="checkmark-circle" size={18} color={c.mutedForeground} />
        <Text
          style={[styles.resolvedBarLabel, { color: c.mutedForeground }]}
          numberOfLines={1}
        >
          Resolved · {total} {total === 1 ? "message" : "messages"} by{" "}
          {authorsLabel}
        </Text>
        <Icon name="chevron-down" size={14} color={c.mutedForeground} />
      </Pressable>
    </View>
  );
}

/**
 * Resolved indicator row that sits at the top of an expanded resolved
 * thread. Carries the "who resolved + when" attribution and a collapse
 * affordance. Tap collapses the thread back to the bar without firing the
 * CommentBody long-press menu (the row is a self-contained Pressable).
 */
function ResolvedIndicator({
  entry,
  onCollapse,
}: {
  entry: TimelineEntry;
  onCollapse: () => void;
}) {
  const c = useThemeColors();
  const { getName } = useActorLookup();
  const resolverName = getName(
    entry.resolved_by_type as "member" | "agent" | null | undefined,
    entry.resolved_by_id,
  );

  return (
    <Pressable
      onPress={onCollapse}
      accessibilityRole="button"
      accessibilityLabel="Collapse resolved thread"
      style={({ pressed }) => [
        styles.resolvedIndicator,
        pressed ? { opacity: 0.6 } : null,
      ]}
    >
      <Icon name="checkmark-circle" size={14} color={c.mutedForeground} />
      <Text
        style={[styles.resolvedLabel, { color: c.mutedForeground }]}
        numberOfLines={1}
      >
        Resolved by{" "}
        <Text style={[styles.resolvedName, { color: c.foreground }]}>
          {resolverName}
        </Text>
        {entry.resolved_at ? ` · ${timeAgo(entry.resolved_at)}` : ""}
      </Text>
      <Text style={[styles.collapseLabel, { color: c.mutedForeground }]}>
        Collapse
      </Text>
    </Pressable>
  );
}

/** Fade-in → hold → fade-out, shared by both highlight overlays. */
function useHighlightProgress(active: boolean): Animated.Value {
  const progress = useRef(new Animated.Value(0)).current;
  useEffect(() => {
    if (!active) return;
    // 700ms fade-in → 1800ms hold → 700ms fade-out. Matches the iOS
    // Reanimated sequence (and web's duration-700 + setTimeout(2500)).
    Animated.sequence([
      Animated.timing(progress, {
        toValue: 1,
        duration: 700,
        easing: Easing.inOut(Easing.quad),
        useNativeDriver: true,
      }),
      Animated.delay(1800),
      Animated.timing(progress, {
        toValue: 0,
        duration: 700,
        easing: Easing.inOut(Easing.quad),
        useNativeDriver: true,
      }),
    ]).start();
  }, [active, progress]);
  return progress;
}

/**
 * Animated highlight overlay for a root comment bubble. Absolute-positioned
 * over the bubble, no pointer capture (long-press still works through it).
 * Border + background wash — equivalent to web's `ring-2 ring-brand/50
 * bg-brand/5`. Only opacity animates; borderColor / backgroundColor stay
 * constant.
 */
function RootHighlightOverlay({
  active,
  colors,
}: {
  active: boolean;
  colors: ThemeColors;
}) {
  const progress = useHighlightProgress(active);
  return (
    <Animated.View
      pointerEvents="none"
      style={[
        styles.rootOverlay,
        {
          opacity: progress,
          borderColor: withAlpha(colors.brand, 0.5),
          backgroundColor: withAlpha(colors.brand, 0.05),
        },
      ]}
    />
  );
}

/**
 * Animated wash overlay for a reply row. Same timing as root, but no
 * border — mirrors web's reply branch which applies only `bg-brand/5`.
 */
function ReplyHighlightOverlay({
  active,
  colors,
}: {
  active: boolean;
  colors: ThemeColors;
}) {
  const progress = useHighlightProgress(active);
  return (
    <Animated.View
      pointerEvents="none"
      style={[
        styles.replyOverlay,
        { opacity: progress, backgroundColor: withAlpha(colors.brand, 0.05) },
      ]}
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
  const c = useThemeColors();
  // When this comment is the active selection target, drop the long-press
  // wrapper AND make the markdown selectable — so the next long-press
  // routes to the platform text-selection affordance instead of our menu.
  // Selection mode is exited via scrolling the timeline or unmounting the
  // issue screen.
  const isSelecting = useCommentSelectStore(
    (s) => s.selectingId === entry.id,
  );
  const { getName } = useActorLookup();
  const userId = useAuthStore((s) => s.user?.id);
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const toggle = useToggleCommentReaction(issueId);
  const qc = useQueryClient();
  const createComment = useCreateComment(issueId);
  const menu = useCommentMenu();
  // Failed-comment state for THIS entry — undefined when the entry is a
  // normal server-backed comment OR an in-flight optimistic.
  const failed = useFailedCommentsStore((s) => s.failed[entry.id]);
  // Same query as IssueDescription — TanStack dedupes so this fires once
  // per issue regardless of how many comments need to resolve attachments.
  const { data: attachments } = useQuery(
    issueAttachmentsOptions(wsId, issueId),
  );

  const name =
    entry.actor_name ||
    getName(
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

  // Highlight the bubble shell while the long-press menu is on screen.
  useEffect(() => {
    if (isSelecting) return;
    onPressChange?.(entry.id, menu.menuVisible);
  }, [menu.menuVisible, entry.id, isSelecting, onPressChange]);

  if (isDeletedComment(entry)) {
    // Only a deleted thread ROOT reaches this — the card filters deleted
    // replies out. The root keeps a placeholder because its replies hang off
    // it and the thread would otherwise have no head.
    return (
      <Text style={[styles.deletedText, { color: c.mutedForeground }]}>
        This comment was deleted
      </Text>
    );
  }

  const body = (
    <View style={styles.body}>
      <View style={styles.headerRow}>
        <ActorAvatar
          type={entry.actor_type as "member" | "agent"}
          id={entry.actor_id}
          name={entry.actor_name}
          avatarUrl={entry.actor_avatar_url}
          size={24}
        />
        <Text style={[styles.authorName, { color: c.foreground }]}>{name}</Text>
        <Text style={[styles.metaText, { color: c.mutedForeground }]}>
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

  // When selecting, the long-press wrapper is gone and markdown is
  // selectable — the next long-press fires the platform selection UI.
  if (isSelecting) return body;

  return (
    <View>
      <Pressable onLongPress={menu.openMenu} delayLongPress={500}>
        {body}
      </Pressable>
      <CommentActionsSheet
        visible={menu.menuVisible}
        onClose={menu.closeMenu}
        entry={entry}
        issueId={issueId}
        issueIdentifier={issueIdentifier}
      />
    </View>
  );
}

/**
 * Inline retry strip shown beneath a failed optimistic comment body. Sits
 * where ReactionBar normally lives — same vertical rhythm, but the slot
 * carries the error message + Retry/Discard buttons. Single source of the
 * error surface (no parallel toast).
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
  const c = useThemeColors();
  return (
    <View style={styles.failedRow}>
      <Icon name="alert-circle" size={14} color={c.destructive} />
      <Text style={[styles.failedError, { color: c.destructive }]} numberOfLines={1}>
        {error || "Couldn't send"}
      </Text>
      <Pressable
        onPress={onRetry}
        hitSlop={6}
        accessibilityRole="button"
        accessibilityLabel="Retry sending comment"
      >
        <Text style={[styles.retryLabel, { color: c.primary }]}>Retry</Text>
      </Pressable>
      <Pressable
        onPress={onDiscard}
        hitSlop={6}
        accessibilityRole="button"
        accessibilityLabel="Discard failed comment"
      >
        <Text style={[styles.discardLabel, { color: c.mutedForeground }]}>
          Discard
        </Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  // px-4 (outer card padding)
  outerPad: { paddingHorizontal: 16 },
  outerRadius: { borderRadius: 12 },
  // rounded-xl px-4 py-3 gap-3 border-2 border-transparent
  bubble: {
    borderRadius: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
    gap: 12,
    borderWidth: 2,
    borderColor: "transparent",
  },
  bubbleResolved: { opacity: 0.7 },
  // border-t border-border/60 pt-3
  replyWrap: {
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: 12,
  },
  // absolute inset-0 rounded-xl border-2
  rootOverlay: {
    ...StyleSheet.absoluteFillObject,
    borderRadius: 12,
    borderWidth: 2,
  },
  replyOverlay: { ...StyleSheet.absoluteFillObject },
  // flex-row items-center gap-2.5 px-4 py-3 rounded-xl
  resolvedBar: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 16,
    paddingVertical: 12,
    borderRadius: 12,
  },
  // text-sm
  resolvedBarLabel: { flex: 1, fontSize: 14 },
  // flex-row items-center gap-2
  resolvedIndicator: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  // text-xs flex-1
  resolvedLabel: { flex: 1, fontSize: 12 },
  // text-xs font-medium
  resolvedName: { fontSize: 12, fontWeight: "500" },
  // text-xs
  collapseLabel: { fontSize: 12 },
  // gap-2
  body: { gap: 8 },
  // flex-row items-center gap-2
  headerRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  // text-sm font-medium
  authorName: { fontSize: 14, fontWeight: "500" },
  // text-xs
  metaText: { fontSize: 12 },
  // text-sm italic
  deletedText: { fontSize: 14, fontStyle: "italic" },
  // flex-row items-center gap-2 mt-0.5
  failedRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    marginTop: 2,
  },
  // text-xs flex-1
  failedError: { flex: 1, fontSize: 12 },
  // text-xs font-medium
  retryLabel: { fontSize: 12, fontWeight: "500" },
  discardLabel: { fontSize: 12, fontWeight: "500" },
});
