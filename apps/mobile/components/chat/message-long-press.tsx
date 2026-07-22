/**
 * Long-press handler for a chat message bubble. Exposes `onLongPress`
 * (drives a cross-platform action sheet) and `isPressed` (drives the
 * caller's highlight ring while the sheet is on screen).
 *
 * Uses `@expo/react-native-action-sheet` per apps/mobile/CLAUDE.md
 * §Tech-stack baseline — native-styled sheet on iOS, Material bottom
 * drawer on Android. Zero custom layout, zero animation, zero overflow
 * math.
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
import { useActionSheet } from "@expo/react-native-action-sheet";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { useTranslation } from "react-i18next";
import type { ChatMessage } from "@multica/core/types";
import { useChatSelectStore } from "@/data/chat-select-store";

export function useChatMessageLongPress(
  message: ChatMessage,
): { onLongPress: () => void; isPressed: boolean } {
  const [isPressed, setIsPressed] = useState(false);
  const { showActionSheetWithOptions } = useActionSheet();
  // Everything below runs inside this hook's own top-level scope (no
  // nested plain-function helper the way `presentReactSheet` works in
  // `components/issue/comment-context-menu.tsx`), so `t`/`tCommon` can be
  // called directly here and simply closed over by the `useCallback`
  // below — no need to thread them through as explicit parameters.
  const { t } = useTranslation("chat");
  const { t: tCommon } = useTranslation("common");

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
      push(t("long_press.copy"), { kind: "copy" });
      push(t("long_press.select_text"), { kind: "select" });
    }
    push(tCommon("cancel"), { kind: "cancel" });

    const cancelButtonIndex = options.length - 1;

    showActionSheetWithOptions(
      { options, cancelButtonIndex },
      (i) => {
        setIsPressed(false);
        if (i === undefined) return;
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
  }, [message, showActionSheetWithOptions, t, tCommon]);

  return { onLongPress, isPressed };
}
