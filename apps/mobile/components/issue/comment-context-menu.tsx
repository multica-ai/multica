/**
 * Long-press handler for a comment bubble. Opens a shared action menu on both
 * iOS and Android while keeping the caller's pressed highlight in sync.
 *
 * Item set (conditional, mirrors web's comment context menu):
 *   Reply (stub) · React… (opens nested menu) · Copy · Select Text ·
 *   Copy Link · Resolve/Unresolve Thread (root only) · Delete (own only)
 *
 * The nested React… menu (5 quick emojis + More reactions…) opens after the
 * first modal dismisses so two native modals are never presented together.
 */
import { useCallback, useState } from "react";
import { Alert } from "react-native";
import { router } from "expo-router";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { useQuery } from "@tanstack/react-query";
import type { Reaction, TimelineEntry } from "@multica/core/types";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { useActorLookup } from "@/data/use-actor-name";
import {
  commentDeleteKeepsReplies,
  useDeleteComment,
  useResolveComment,
  useToggleCommentReaction,
} from "@/data/mutations/issues";
import { appConfigOptions } from "@/data/queries/billing";
import { QUICK_EMOJIS } from "@/lib/quick-emojis";
import { useT } from "@/lib/i18n";
import type { ActionMenuOption } from "@/components/ui/action-menu-modal";

type CommentAction =
  | { kind: "reply" }
  | { kind: "react" }
  | { kind: "copy" }
  | { kind: "select" }
  | { kind: "copyLink" }
  | { kind: "resolve" }
  | { kind: "delete" };

const QUICK_ROW_SIZE = 5;

export function useCommentLongPress(
  entry: TimelineEntry,
  issueId: string,
  issueIdentifier: string | undefined,
): {
  onLongPress: () => void;
  isPressed: boolean;
  menu: { options: ActionMenuOption[]; actions: CommentAction[] } | null;
  reactionMenu: { options: ActionMenuOption[]; reactions: Reaction[] } | null;
  onSelect: (actionId: string) => void;
  onSelectReaction: (reactionId: string) => void;
  onCancel: () => void;
} {
  const [isPressed, setIsPressed] = useState(false);
  const [pendingMenu, setPendingMenu] = useState<{
    options: ActionMenuOption[];
    actions: CommentAction[];
  } | null>(null);
  const [pendingReactionMenu, setPendingReactionMenu] = useState<{
    options: ActionMenuOption[];
    reactions: Reaction[];
  } | null>(null);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const userId = useAuthStore((s) => s.user?.id);
  const toggleReaction = useToggleCommentReaction(issueId);
  const deleteComment = useDeleteComment(issueId);
  const resolveComment = useResolveComment(issueId);
  const { getName } = useActorLookup();
  // Same config cache useDeleteComment reads when it runs, so the copy and
  // the delete route agree.
  const { data: keepReplies = false } = useQuery({
    ...appConfigOptions(),
    select: commentDeleteKeepsReplies,
  });
  const { t } = useT("issues");

  const onLongPress = useCallback(() => {
    const isOwn = entry.actor_type === "member" && entry.actor_id === userId;
    const isRoot = !entry.parent_id;
    const resolved = !!entry.resolved_at;
    const hasContent = !!entry.content;
    const webUrl = process.env.EXPO_PUBLIC_WEB_URL;
    const canCopyLink = !!(webUrl && wsSlug && issueIdentifier);
    Haptics.selectionAsync().catch(() => {});
    setIsPressed(true);

    const options: ActionMenuOption[] = [];
    const actions: CommentAction[] = [];
    const push = (label: string, action: CommentAction) => {
      options.push({
        id: action.kind,
        label,
        ...(action.kind === "delete" ? { destructive: true } : {}),
      });
      actions.push(action);
    };

    push(t("comments.reply"), { kind: "reply" });
    push(t("comments.react"), { kind: "react" });
    if (hasContent) {
      push(t("comments.copy"), { kind: "copy" });
      push(t("comments.select_text"), { kind: "select" });
    }
    if (canCopyLink) push(t("menu.copy_link"), { kind: "copyLink" });
    if (isRoot) {
      push(resolved ? t("comments.unresolve") : t("comments.resolve"), {
        kind: "resolve",
      });
    }
    if (isOwn) push(t("common:actions.delete"), { kind: "delete" });
    setPendingMenu({ options, actions });
  }, [
    entry,
    issueIdentifier,
    userId,
    wsSlug,
    t,
  ]);

  const onSelect = useCallback(
    (actionId: string) => {
      const action = pendingMenu?.actions.find(
        (item) => item.kind === actionId,
      );
      setPendingMenu(null);
      setIsPressed(false);
      if (!action) return;

      // Let the modal's fade-out finish before presenting another route/dialog.
      setTimeout(() => {
        switch (action.kind) {
          case "reply": {
            const actorName =
              entry.actor_name ||
              getName(
                entry.actor_type as "member" | "agent" | null | undefined,
                entry.actor_id,
              );
            useReplyTargetStore.getState().setTarget({
              commentId: entry.id,
              actorName: actorName || t("comments.fallback_actor"),
              preview: entry.content ?? "",
            });
            return;
          }
          case "react": {
            const emojis = QUICK_EMOJIS.slice(0, QUICK_ROW_SIZE);
            setPendingReactionMenu({
              options: [
                ...emojis.map((emoji, index) => ({
                  id: `emoji-${index}`,
                  label: emoji,
                })),
                { id: "more", label: t("comments.more_reactions") },
              ],
              reactions: (entry.reactions ?? []) as Reaction[],
            });
            return;
          }
          case "copy":
            if (entry.content) {
              Clipboard.setStringAsync(entry.content);
              Haptics.notificationAsync(
                Haptics.NotificationFeedbackType.Success,
              ).catch(() => {});
            }
            return;
          case "select":
            useCommentSelectStore.getState().setSelecting(entry.id);
            return;
          case "copyLink": {
            const webUrl = process.env.EXPO_PUBLIC_WEB_URL;
            if (!webUrl || !wsSlug || !issueIdentifier) return;
            const url = `${webUrl}/${wsSlug}/issue/${issueIdentifier}#comment-${entry.id}`;
            Clipboard.setStringAsync(url);
            Haptics.notificationAsync(
              Haptics.NotificationFeedbackType.Success,
            ).catch(() => {});
            return;
          }
          case "resolve":
            resolveComment.mutate({
              commentId: entry.id,
              resolved: !entry.resolved_at,
            });
            return;
          case "delete":
            Alert.alert(
              t("comments.delete_title"),
              keepReplies
                ? t("comments.delete_keep_replies")
                : t("comments.delete_with_replies"),
              [
                { text: t("common:actions.cancel"), style: "cancel" },
                {
                  text: t("common:actions.delete"),
                  style: "destructive",
                  onPress: () => deleteComment.mutate(entry.id),
                },
              ],
            );
            return;
        }
      }, 200);
    },
    [
      pendingMenu,
      entry,
      getName,
      t,
      wsSlug,
      issueIdentifier,
      resolveComment,
      keepReplies,
      deleteComment,
    ],
  );

  const onCancel = useCallback(() => {
    setPendingMenu(null);
    setPendingReactionMenu(null);
    setIsPressed(false);
  }, []);

  const onSelectReaction = useCallback(
    (reactionId: string) => {
      const menu = pendingReactionMenu;
      setPendingReactionMenu(null);
      if (!menu) return;
      setTimeout(() => {
        if (reactionId === "more") {
          if (!wsSlug) return;
          router.push({
            pathname:
              "/[workspace]/issue/[id]/comment/[commentId]/emoji-picker",
            params: {
              workspace: wsSlug,
              id: issueId,
              commentId: entry.id,
            },
          });
          return;
        }
        const index = Number(reactionId.replace("emoji-", ""));
        const emoji = QUICK_EMOJIS.slice(0, QUICK_ROW_SIZE)[index];
        if (!emoji) return;
        const existing = menu.reactions.find(
          (reaction) =>
            reaction.emoji === emoji &&
            reaction.actor_type === "member" &&
            reaction.actor_id === userId,
        );
        toggleReaction.mutate({ commentId: entry.id, emoji, existing });
      }, 200);
    },
    [pendingReactionMenu, wsSlug, issueId, entry.id, userId, toggleReaction],
  );

  return {
    onLongPress,
    isPressed,
    menu: pendingMenu,
    reactionMenu: pendingReactionMenu,
    onSelect,
    onSelectReaction,
    onCancel,
  };
}
