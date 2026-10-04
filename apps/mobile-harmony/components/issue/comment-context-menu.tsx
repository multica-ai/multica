/**
 * HarmonyOS port of apps/mobile/components/issue/comment-context-menu.tsx —
 * long-press menu for a comment bubble.
 *
 * iOS-native first on the iOS side: ActionSheetIOS with the comment's
 * actions. This platform has no ActionSheetIOS; the established stand-in
 * (see project-detail-screen.tsx) is a `BottomSheet` menu whose rows follow
 * components/nav/more-menu.tsx styling. Same item set, same conditionals,
 * same confirm flows.
 *
 * Item set (conditional, mirrors web's comment context menu):
 *   Reply · React… · Copy · Select Text · Copy Link ·
 *   Resolve/Unresolve Thread (root only) · Delete (own only) · Cancel
 *
 * The iOS "React…" row presented a nested ActionSheet from inside the
 * dismissal callback; here it flips to the EmojiPickerSheet (BottomSheet)
 * mounted as a sibling so the two sheet transitions crossfade.
 */
import React, { useCallback, useState } from "react";
import { Alert, Modal, StyleSheet, Text as RNText, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { TimelineEntry } from "@multica/core/types";
import { Icon } from "@/components/ui/icon";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { useActorLookup } from "@/data/use-actor-name";
import {
  commentDeleteKeepsReplies,
  useDeleteComment,
  useResolveComment,
} from "@/data/mutations/issues";
import { appConfigOptions } from "@/data/queries/billing";
import { setStringAsync as clipboardSetStringAsync } from "@/lib/clipboard";
import * as Haptics from "@/lib/haptics";
import { useThemeColors } from "@/lib/use-theme-colors";
import { EmojiPickerSheet } from "./emoji-picker-sheet";

interface MenuTrigger {
  /** True while the action sheet is on screen — drives the caller's
   *  highlight ring (the iOS isPressed contract). */
  menuVisible: boolean;
  /** Fires the haptic tick and presents the menu. */
  openMenu: () => void;
  closeMenu: () => void;
}

export function useCommentMenu(): MenuTrigger {
  const [menuVisible, setMenuVisible] = useState(false);
  const openMenu = useCallback(() => {
    Haptics.selectionAsync().catch(() => {});
    setMenuVisible(true);
  }, []);
  const closeMenu = useCallback(() => setMenuVisible(false), []);
  return { menuVisible, openMenu, closeMenu };
}

interface SheetProps {
  visible: boolean;
  onClose: () => void;
  entry: TimelineEntry;
  issueId: string;
  issueIdentifier: string | undefined;
}

export function CommentActionsSheet({
  visible,
  onClose,
  entry,
  issueId,
  issueIdentifier,
}: SheetProps) {
  const c = useThemeColors();
  const userId = useAuthStore((s) => s.user?.id);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const deleteComment = useDeleteComment(issueId);
  const resolveComment = useResolveComment(issueId);
  const { getName } = useActorLookup();
  // Same config cache useDeleteComment reads when it runs, so the copy and
  // the delete route agree.
  const { data: keepReplies = false } = useQuery({
    ...appConfigOptions(),
    select: commentDeleteKeepsReplies,
  });
  // The "React…" destination. Flips when the row is tapped so the menu
  // sheet hands off to the emoji sheet (iOS presented it from inside the
  // ActionSheetIOS completion callback — same sequencing).
  const [emojiOpen, setEmojiOpen] = useState(false);

  const isOwn = entry.actor_type === "member" && entry.actor_id === userId;
  const isRoot = !entry.parent_id;
  const resolved = !!entry.resolved_at;
  const hasContent = !!entry.content;
  // EXPO_PUBLIC_WEB_URL on iOS; the harmony bundle inlines MULTICA_WEB_URL
  // instead (babel transform — see AGENTS.md env variants).
  const webUrl = process.env.MULTICA_WEB_URL;
  const canCopyLink = !!(webUrl && wsSlug && issueIdentifier);

  const onReply = () => {
    onClose();
    // Set the reply target — the InlineCommentComposer subscribes to this
    // store, auto-expands, and threads the next submit under entry.id via
    // useCreateComment's `parentId`.
    const actorName =
      entry.actor_name ||
      getName(
        entry.actor_type as "member" | "agent" | null | undefined,
        entry.actor_id,
      );
    useReplyTargetStore.getState().setTarget({
      commentId: entry.id,
      actorName: actorName || "comment",
      preview: entry.content ?? "",
    });
  };

  const onReact = () => {
    onClose();
    setEmojiOpen(true);
  };

  const onCopy = () => {
    onClose();
    if (entry.content) {
      void clipboardSetStringAsync(entry.content);
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(
        () => {},
      );
    }
  };

  const onSelectText = () => {
    onClose();
    useCommentSelectStore.getState().setSelecting(entry.id);
  };

  const onCopyLink = () => {
    onClose();
    if (!canCopyLink) return;
    const url = `${webUrl}/${wsSlug}/issue/${issueIdentifier}#comment-${entry.id}`;
    void clipboardSetStringAsync(url);
    Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(
      () => {},
    );
  };

  const onResolve = () => {
    onClose();
    resolveComment.mutate({
      commentId: entry.id,
      resolved: !entry.resolved_at,
    });
  };

  const onDelete = () => {
    onClose();
    Alert.alert(
      "Delete comment?",
      // Promise kept replies only when the server declares it (#8296);
      // older servers delete the replies too.
      keepReplies
        ? "This comment will be permanently deleted. Any replies to it stay in the thread. This cannot be undone."
        : "This comment will be permanently deleted. Replies in the thread will also be removed. This cannot be undone.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () => deleteComment.mutate(entry.id),
        },
      ],
    );
  };

  return (
    // Modal host: this sheet mounts inside FlatList rows, where BottomSheet's
    // absoluteFill would anchor to the row's own bounds. Modal hoists the
    // content to the window level — the same platform escape hatch the
    // markdown fullscreen viewer uses (lib/markdown/markdown-image.tsx).
    <Modal
      transparent
      visible={visible || emojiOpen}
      animationType="fade"
      statusBarTranslucent
      onRequestClose={() => {
        // Only fires when no inner BottomSheet BackHandler consumed the
        // event; close whichever layer is open.
        if (emojiOpen) setEmojiOpen(false);
        else onClose();
      }}
    >
      <View style={StyleSheet.absoluteFill}>
        <BottomSheet visible={visible} onClose={onClose}>
          <View style={styles.menu}>
            <MenuRow
              label="Reply"
              icon="return-up-back"
              onPress={onReply}
            />
            <MenuRow label="React…" icon="happy-outline" onPress={onReact} />
            {hasContent ? (
              <MenuRow label="Copy" icon="copy-outline" onPress={onCopy} />
            ) : null}
            {hasContent ? (
              <MenuRow
                label="Select Text"
                icon="text-outline"
                onPress={onSelectText}
              />
            ) : null}
            {canCopyLink ? (
              <MenuRow label="Copy Link" icon="link-outline" onPress={onCopyLink} />
            ) : null}
            {isRoot ? (
              <MenuRow
                label={resolved ? "Unresolve Thread" : "Resolve Thread"}
                icon={resolved ? "close-circle-outline" : "checkmark-circle-outline"}
                onPress={onResolve}
              />
            ) : null}
            {isOwn ? (
              <MenuRow
                label="Delete"
                icon="trash-outline"
                destructive
                onPress={onDelete}
              />
            ) : null}
            <View style={[styles.separator, { backgroundColor: c.border }]} />
            <MenuRow label="Cancel" onPress={onClose} />
          </View>
        </BottomSheet>
        {/* Stacks above the menu sheet (later sibling = later overlay). The
            menu is already closed when this opens — same hand-off sequencing
            as the iOS nested ActionSheet. */}
        <EmojiPickerSheet
          issueId={issueId}
          commentId={entry.id}
          visible={emojiOpen}
          onClose={() => setEmojiOpen(false)}
        />
      </View>
    </Modal>
  );
}

/**
 * Action-sheet row. Same content order as the iOS ActionSheetIOS options;
 * destructive rows render in the destructive token color.
 */
function MenuRow({
  label,
  icon,
  destructive,
  onPress,
}: {
  label: string;
  icon?: string;
  destructive?: boolean;
  onPress: () => void;
}) {
  const c = useThemeColors();
  const color = destructive ? c.destructive : c.foreground;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      style={({ pressed }) => [
        styles.menuRow,
        pressed ? { backgroundColor: c.secondary } : null,
      ]}
      onPress={onPress}
    >
      {icon ? <Icon name={icon} size={18} color={color} /> : null}
      <RNText style={[styles.menuLabel, { color }]}>{label}</RNText>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  menu: { paddingHorizontal: 8, paddingBottom: 8 },
  menuRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    height: 44,
    borderRadius: 8,
    paddingHorizontal: 12,
  },
  menuLabel: { fontSize: 16 },
  separator: { height: 1, marginVertical: 6 },
});
