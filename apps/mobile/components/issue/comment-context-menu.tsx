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
import { useCallback, useRef, useState } from "react";
import { ActionSheetIOS, Alert } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import type { Reaction, TimelineEntry } from "@multica/core/types";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import {
  ensureCommentBranchRequest,
  shouldRetainCommentBranchRequest,
  type CommentBranchRequestState,
} from "@multica/core/issues/comment-branch";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { useActorLookup } from "@/data/use-actor-name";
import {
  useDeleteComment,
  useBranchComment,
  useResolveComment,
  useToggleCommentReaction,
} from "@/data/mutations/issues";
import { QUICK_EMOJIS } from "@/lib/quick-emojis";
import { agentListOptions } from "@/data/queries/agents";
import { memberListOptions } from "@/data/queries/members";
import { ApiError } from "@/data/api";
import { isAgentRuntimeBound } from "@/lib/is-agent-runtime-bound";
import { randomUUID } from "@/lib/uuid";

const QUICK_ROW_SIZE = 5;

export function useCommentLongPress(
  entry: TimelineEntry,
  issueId: string,
  issueIdentifier: string | undefined,
  branchEnabled = false,
): { onLongPress: () => void; isPressed: boolean } {
  const [isPressed, setIsPressed] = useState(false);
  const branchRequestRef = useRef<CommentBranchRequestState | null>(null);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const userId = useAuthStore((s) => s.user?.id);
  const toggleReaction = useToggleCommentReaction(issueId);
  const deleteComment = useDeleteComment(issueId);
  const resolveComment = useResolveComment(issueId);
  const branchComment = useBranchComment(issueId);
  const { getName } = useActorLookup();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));

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
      | { kind: "branch" }
      | { kind: "resolve" }
      | { kind: "delete" }
      | { kind: "cancel" };

    const options: string[] = [];
    const actions: Action[] = [];
    const push = (label: string, action: Action) => {
      options.push(label);
      actions.push(action);
    };

    push("Reply", { kind: "reply" });
    push("React…", { kind: "react" });
    if (hasContent) {
      push("Copy", { kind: "copy" });
      push("Select Text", { kind: "select" });
    }
    if (canCopyLink) push("Copy Link", { kind: "copyLink" });
    if (branchEnabled && !entry.id.startsWith("optimistic-")) {
      push("Start independent run here", { kind: "branch" });
    }
    if (isRoot) {
      push(resolved ? "Unresolve Thread" : "Resolve Thread", {
        kind: "resolve",
      });
    }
    if (isOwn) push("Delete", { kind: "delete" });
    push("Cancel", { kind: "cancel" });

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
          case "branch": {
            const memberRole =
              members.find((member) => member.user_id === userId)?.role ?? null;
            const selectableAgents = agents
              .filter(
                (agent) =>
                  !agent.archived_at &&
                  isAgentRuntimeBound(agent) &&
                  canAssignAgentToIssue(agent, {
                    userId: userId ?? null,
                    role: memberRole,
                  }).allowed,
              )
              .sort((a, b) => a.name.localeCompare(b.name));
            const branchOptions = [
              "Automatic",
              ...selectableAgents.map((agent) => agent.name),
              "Cancel",
            ];
            const cancelIndex = branchOptions.length - 1;
            ActionSheetIOS.showActionSheetWithOptions(
              {
                title: "Start independent run",
                message:
                  "The run receives the issue snapshot and only this comment's ancestor path.",
                options: branchOptions,
                cancelButtonIndex: cancelIndex,
              },
              (agentIndex) => {
                if (agentIndex === cancelIndex) {
                  branchRequestRef.current = null;
                  return;
                }
                const selectedAgent =
                  agentIndex === 0 ? undefined : selectableAgents[agentIndex - 1];
                Alert.alert(
                  "Start independent run?",
                  selectedAgent
                    ? `Run with ${selectedAgent.name}.`
                    : "Use the source task agent, the issue's assigned agent, or its squad leader.",
                  [
                    {
                      text: "Cancel",
                      style: "cancel",
                      onPress: () => {
                        branchRequestRef.current = null;
                      },
                    },
                    {
                      text: "Start",
                      onPress: () => {
                        const request = ensureCommentBranchRequest(
                          branchRequestRef.current,
                          {
                            commentId: entry.id,
                            agentId: selectedAgent?.id,
                            contentBase: entry.content ?? "",
                          },
                          randomUUID,
                        );
                        branchRequestRef.current = request;
                        branchComment.mutate(
                          {
                            commentId: entry.id,
                            agentId: selectedAgent?.id,
                            contentBase: entry.content ?? "",
                            requestId: request.requestId,
                          },
                          {
                            onSuccess: () => {
                              if (
                                branchRequestRef.current?.requestId ===
                                request.requestId
                              ) {
                                branchRequestRef.current = null;
                              }
                              Haptics.notificationAsync(
                                Haptics.NotificationFeedbackType.Success,
                              ).catch(() => {});
                            },
                            onError: (error) => {
                              if (
                                !shouldRetainCommentBranchRequest(
                                  error instanceof ApiError ? error.status : undefined,
                                ) &&
                                branchRequestRef.current?.requestId === request.requestId
                              ) {
                                branchRequestRef.current = null;
                              }
                              Alert.alert(
                                "Could not start run",
                                error.message || "Try again.",
                              );
                            },
                          },
                        );
                      },
                    },
                  ],
                );
              },
            );
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
              "Delete comment?",
              "This comment will be permanently deleted. Replies in the thread will also be removed. This cannot be undone.",
              [
                { text: "Cancel", style: "cancel" },
                {
                  text: "Delete",
                  style: "destructive",
                  onPress: () =>
                    deleteComment.mutate(entry.id, {
                      onError: (error) =>
                        Alert.alert(
                          "Could not delete comment",
                          error.message || "Try again.",
                        ),
                    }),
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
    branchComment,
    branchEnabled,
    agents,
    members,
    getName,
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
  const options = [...emojis, "More reactions…", "Cancel"];
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
