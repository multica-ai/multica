/**
 * Long-press handler for a comment bubble. Exposes `onLongPress` (drives a
 * cross-platform action sheet) and `isPressed` (drives the caller's
 * highlight ring while the sheet is on screen).
 *
 * Uses `@expo/react-native-action-sheet` per apps/mobile/CLAUDE.md
 * §Tech-stack baseline — native-styled sheet on iOS, Material bottom
 * drawer on Android. Zero custom layout, zero animation, zero overflow
 * math.
 *
 * Item set (conditional, mirrors web's comment context menu):
 *   Reply (stub) · React… (opens nested sheet) · Copy · Select Text ·
 *   Copy Link · Resolve/Unresolve Thread (root only) · Delete (own only) ·
 *   Cancel
 *
 * The nested React… sheet (5 quick emojis + More reactions… + Cancel) is
 * fired from INSIDE the outer sheet's completion callback rather than
 * inline, because a second sheet cannot be presented while the first is
 * still dismissing — the callback runs after dismissal completes.
 */
import { useCallback, useState } from "react";
import { Alert } from "react-native";
import { useActionSheet } from "@expo/react-native-action-sheet";
import { router } from "expo-router";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import type { Reaction, TimelineEntry } from "@multica/core/types";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { useActorLookup } from "@/data/use-actor-name";
import {
  useDeleteComment,
  useResolveComment,
  useToggleCommentReaction,
} from "@/data/mutations/issues";
import { QUICK_EMOJIS } from "@/lib/quick-emojis";

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
  const { showActionSheetWithOptions } = useActionSheet();
  const { t } = useTranslation("issues");
  const { t: tCommon } = useTranslation("common");

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

    push(t("comment.menu.reply"), { kind: "reply" });
    push(t("comment.menu.react"), { kind: "react" });
    if (hasContent) {
      push(t("comment.menu.copy"), { kind: "copy" });
      push(t("comment.menu.select_text"), { kind: "select" });
    }
    if (canCopyLink) push(t("comment.menu.copy_link"), { kind: "copyLink" });
    if (isRoot) {
      push(
        resolved
          ? t("comment.menu.unresolve_thread")
          : t("comment.menu.resolve_thread"),
        { kind: "resolve" },
      );
    }
    if (isOwn) push(t("comment.menu.delete"), { kind: "delete" });
    push(tCommon("cancel"), { kind: "cancel" });

    const cancelButtonIndex = options.length - 1;
    const destructiveButtonIndex = isOwn
      ? actions.findIndex((a) => a.kind === "delete")
      : undefined;

    showActionSheetWithOptions(
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
        if (i === undefined) return;
        const action = actions[i];
        if (!action || action.kind === "cancel") return;

        switch (action.kind) {
          case "reply": {
            // Set the reply target — the InlineCommentComposer subscribes
            // to this store, auto-expands, and threads the next submit
            // under entry.id via useCreateComment's `parentId`.
            const actorName = getName(
              entry.actor_type as "member" | "agent" | null | undefined,
              entry.actor_id,
            );
            useReplyTargetStore.getState().setTarget({
              commentId: entry.id,
              actorName: actorName || "comment",
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
              showActionSheetWithOptions,
              t,
              tCommon,
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
              t("comment.menu.delete_confirm.title"),
              t("comment.menu.delete_confirm.message"),
              [
                {
                  text: t("comment.menu.delete_confirm.cancel"),
                  style: "cancel",
                },
                {
                  text: t("comment.menu.delete_confirm.confirm"),
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
    showActionSheetWithOptions,
    t,
    tCommon,
  ]);

  return { onLongPress, isPressed };
}

function presentReactSheet(args: {
  entry: TimelineEntry;
  reactions: Reaction[];
  userId: string | undefined;
  wsSlug: string | null;
  issueId: string;
  showActionSheetWithOptions: ReturnType<
    typeof useActionSheet
  >["showActionSheetWithOptions"];
  // `useTranslation` cannot be called from this function — it's a plain
  // helper invoked from inside the outer sheet's completion callback, not
  // a hook. The caller (`useCommentLongPress`, a real hook) calls
  // `useTranslation` at its top level and passes `t` / `tCommon` through.
  t: TFunction;
  tCommon: TFunction;
  toggle: (emoji: string, existing: Reaction | undefined) => void;
}) {
  const {
    entry,
    reactions,
    userId,
    wsSlug,
    issueId,
    showActionSheetWithOptions,
    t,
    tCommon,
    toggle,
  } = args;
  const emojis = QUICK_EMOJIS.slice(0, QUICK_ROW_SIZE);
  const options = [...emojis, t("comment.reaction.more_reactions"), tCommon("cancel")];
  const cancelButtonIndex = options.length - 1;

  showActionSheetWithOptions(
    { options, cancelButtonIndex },
    (i) => {
      if (i === undefined || i === cancelButtonIndex) return;
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
