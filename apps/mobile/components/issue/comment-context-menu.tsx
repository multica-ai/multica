/**
 * Long-press handler for a comment bubble. Exposes `onLongPress` (drives a
 * native iOS ActionSheetIOS) and `isPressed` (drives the caller's highlight
 * ring while the sheet is on screen).
 *
 * iOS-native first per apps/mobile/CLAUDE.md §UI components → waterfall step
 * 1: `ActionSheetIOS.showActionSheetWithOptions`. Zero custom layout, zero
 * animation, zero overflow math, zero new deps.
 *
 * Item set (conditional, mirrors web's comment context menu):
 *   Reply (stub) · React… (opens nested sheet) · Copy · Select Text ·
 *   Copy Link · Resolve/Unresolve Thread (root only) · Delete (own only) ·
 *   Cancel
 *
 * The nested React… sheet (5 quick emojis + More reactions… + Cancel) is
 * fired from INSIDE the outer sheet's completion callback rather than
 * inline, because iOS will refuse to present a second ActionSheet while the
 * first is still dismissing — the callback runs after dismissal completes.
 */
import { useCallback, useState } from "react";
import { ActionSheetIOS, Alert } from "react-native";
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
import { i18n, useT } from "@/lib/i18n";

const QUICK_ROW_SIZE = 5;

export function useCommentLongPress(
  entry: TimelineEntry,
  issueId: string,
  issueIdentifier: string | undefined,
): { onLongPress: () => void; isPressed: boolean } {
  const [isPressed, setIsPressed] = useState(false);
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
    const reactions = (entry.reactions ?? []) as Reaction[];

    Haptics.selectionAsync().catch(() => {});
    setIsPressed(true);

    type Action =
      | { kind: "reply" }
      | { kind: "react" }
      | { kind: "copy" }
      | { kind: "select" }
      | { kind: "copyLink" }
      | { kind: "resolve" }
      | { kind: "delete" }
      | { kind: "cancel" };

    const options: string[] = [];
    const actions: Action[] = [];
    const push = (label: string, action: Action) => {
      options.push(label);
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
    push(t("common:actions.cancel"), { kind: "cancel" });

    const cancelButtonIndex = options.length - 1;
    const destructiveButtonIndex = isOwn
      ? actions.findIndex((a) => a.kind === "delete")
      : undefined;

    ActionSheetIOS.showActionSheetWithOptions(
      {
        options,
        cancelButtonIndex,
        ...(destructiveButtonIndex !== undefined &&
        destructiveButtonIndex >= 0
          ? { destructiveButtonIndex }
          : {}),
      },
      (i) => {
        setIsPressed(false);
        const action = actions[i];
        if (!action || action.kind === "cancel") return;

        switch (action.kind) {
          case "reply": {
            // Set the reply target — the InlineCommentComposer subscribes
            // to this store, auto-expands, and threads the next submit
            // under entry.id via useCreateComment's `parentId`.
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
          case "react":
            // Present the nested React sheet from inside this completion
            // callback — see file header for why.
            presentReactSheet({
              entry,
              reactions,
              userId,
              wsSlug,
              issueId,
              toggle: (emoji, existing) =>
                toggleReaction.mutate({
                  commentId: entry.id,
                  emoji,
                  existing,
                }),
            });
            return;
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
            if (!canCopyLink) return;
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
              // Promise kept replies only when the server declares it (#8296);
              // older servers delete the replies too.
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
      },
    );
  }, [
    entry,
    issueId,
    issueIdentifier,
    userId,
    wsSlug,
    toggleReaction,
    deleteComment,
    resolveComment,
    getName,
    keepReplies,
    t,
  ]);

  return { onLongPress, isPressed };
}

function presentReactSheet(args: {
  entry: TimelineEntry;
  reactions: Reaction[];
  userId: string | undefined;
  wsSlug: string | null;
  issueId: string;
  toggle: (emoji: string, existing: Reaction | undefined) => void;
}) {
  const { entry, reactions, userId, wsSlug, issueId, toggle } = args;
  const emojis = QUICK_EMOJIS.slice(0, QUICK_ROW_SIZE);
  const t = i18n.t.bind(i18n);
  const options = [
    ...emojis,
    t("issues:comments.more_reactions"),
    t("common:actions.cancel"),
  ];
  const cancelButtonIndex = options.length - 1;

  ActionSheetIOS.showActionSheetWithOptions(
    { options, cancelButtonIndex },
    (i) => {
      if (i === cancelButtonIndex) return;
      if (i === emojis.length) {
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
      const emoji = emojis[i];
      if (!emoji) return;
      const existing = reactions.find(
        (r) =>
          r.emoji === emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggle(emoji, existing);
    },
  );
}
