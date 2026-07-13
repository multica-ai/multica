/**
 * Long-press handler for a chat message bubble. Exposes `onLongPress`
 * (drives a native iOS ActionSheetIOS) and `isPressed` (drives the
 * caller's highlight ring while the sheet is on screen).
 *
 * iOS-native first per apps/mobile/CLAUDE.md §UI components → waterfall
 * step 1: `ActionSheetIOS.showActionSheetWithOptions`. Zero custom
 * layout, zero animation, zero overflow math, zero new deps.
 *
 * Item set (v1, conditional):
 *   Copy · Select Text · Cancel
 *
 * Mirrors `useCommentLongPress` in `components/issue/comment-context-
 * menu.tsx` — kept as a sibling rather than a shared primitive because
 * we have only 2 callers (chat + comments). Below the "3 callers + no
 * native alternative" threshold in apps/mobile/CLAUDE.md.
 */
import { useCallback, useState } from "react";
import { ActionSheetIOS } from "react-native";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import type { ChatMessage } from "@multica/core/types";
import { useChatSelectStore } from "@/data/chat-select-store";

export function useChatMessageLongPress(
  message: ChatMessage,
): { onLongPress: () => void; isPressed: boolean } {
  const [isPressed, setIsPressed] = useState(false);

  const onLongPress = useCallback(() => {
    const hasContent = !!message.content;

    Haptics.selectionAsync().catch(() => {});
    setIsPressed(true);

    type Action =
      | { kind: "copy" }
      | { kind: "select" }
      | { kind: "cancel" };

    const options: string[] = [];
    const actions: Action[] = [];
    const push = (label: string, action: Action) => {
      options.push(label);
      actions.push(action);
    };

    if (hasContent) {
      push("Copy", { kind: "copy" });
      push("Select Text", { kind: "select" });
    }
    push("Cancel", { kind: "cancel" });

    const cancelButtonIndex = options.length - 1;

    ActionSheetIOS.showActionSheetWithOptions(
      { options, cancelButtonIndex },
      (i) => {
        setIsPressed(false);
        const action = actions[i];
        if (!action || action.kind === "cancel") return;

        switch (action.kind) {
          case "copy":
            if (message.content) {
              Clipboard.setStringAsync(message.content);
              Haptics.notificationAsync(
                Haptics.NotificationFeedbackType.Success,
              ).catch(() => {});
            }
            return;
          case "select":
            useChatSelectStore.getState().setSelecting(message.id);
            return;
        }
      },
    );
  }, [message]);

  return { onLongPress, isPressed };
}
