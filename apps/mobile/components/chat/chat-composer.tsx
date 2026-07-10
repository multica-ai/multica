/**
 * Chat composer — thin wrapper around the shared `<MessageComposer>` with
 * chat-specific wiring:
 *
 *   - **Controlled text**: parent (chat.tsx) owns the draft via
 *     `useChatDraftsStore` so switching sessions rehydrates the right
 *     draft. Pass `value` + `onChangeText` through.
 *   - **Stop button**: while an agent task is running for the active
 *     session, `sending` flips true and we replace the Send button slot
 *     with a Stop affordance (filled foreground bg + stop glyph). Tap →
 *     `onStop()` cancels the in-flight task.
 *   - **Mention picker mode=chat**: chat is user ↔ single agent so
 *     @member / @agent / @squad / @all are noise + would notify the
 *     wrong people. Picker route honors `?mode=chat` and surfaces only
 *     Issues (useful for "reference this ticket for context").
 *   - **No reply target**: chat is a flat conversation; passes no
 *     reply chip.
 *   - **No upload context**: chat attachments are session-scoped; the
 *     server back-fills `chat_message_id` on each row when the message
 *     persists (server-side). `MessageComposer` calls `api.uploadFile`
 *     without `{ issueId, commentId }`.
 *   - **Composer owns keyboard + bottom inset**: `chat/[id]` and
 *     `chat/new` are pushed screens with no tab bar underneath (unlike
 *     the old single-screen tab root), so the composer's own
 *     `KeyboardStickyView` + safe-area handling (`manageKeyboard`'s
 *     default `true`) is what keeps it above the keyboard and clear of
 *     the bottom gesture bar — `ChatConversationView` no longer wraps
 *     in its own `KeyboardAvoidingView`.
 *   - **Always expanded**: chat always shows the full input (never the
 *     collapsed pill) — `alwaysExpanded` skips the tap-to-expand step
 *     the comment composer uses, so tapping the input focuses it
 *     directly.
 *
 * Previously a hand-written 400-LOC twin of inline-comment-composer.tsx;
 * now ~50 LOC plus the StopButton subcomponent.
 */
import { useCallback } from "react";
import { Pressable, View } from "react-native";
import Animated, { FadeIn, FadeOut } from "react-native-reanimated";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { useTranslation } from "react-i18next";
import { MessageComposer } from "@/components/composer/message-composer";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

interface Props {
  /** Current draft text (controlled). Empty string = no draft. */
  value: string;
  /** Fired on every keystroke. The caller writes to the drafts store. */
  onChangeText: (next: string) => void;
  /** Send the serialised markdown content + the completed attachments'
   *  server ids. Caller resets the input by setting `value=""` after a
   *  successful send. */
  onSend: (content: string, attachmentIds: string[]) => Promise<void> | void;
  /** Cancel the in-flight agent task. Only callable while `sending===true`. */
  onStop: () => void;
  /** True while an agent task is running for the active session. The
   *  composer swaps Send for Stop. */
  sending: boolean;
  /** Hard-disable typing + send. Used when there's no usable agent in the
   *  workspace or the session is archived (legacy). */
  disabled?: boolean;
  /** When `disabled`, replaces the pill label with the reason. */
  disabledReason?: string;
}

const IS_IOS = process.env.EXPO_OS === "ios";

export function ChatComposer({
  value,
  onChangeText,
  onSend,
  onStop,
  sending,
  disabled = false,
  disabledReason,
}: Props) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { t } = useTranslation("chat");

  const onSubmit = useCallback(
    async ({
      content,
      attachmentIds,
    }: {
      content: string;
      attachmentIds: string[];
    }) => {
      // `onSend` may be sync or async; await is safe in both cases. If it
      // throws, MessageComposer's catch restores text + chips.
      await onSend(content, attachmentIds);
    },
    [onSend],
  );

  const handleStop = useCallback(() => {
    if (IS_IOS) {
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    }
    onStop();
  }, [onStop]);

  return (
    <MessageComposer
      value={value}
      onChangeText={onChangeText}
      onSubmit={onSubmit}
      mentionPickerPath={{
        pathname: "/[workspace]/mention-picker",
        params: { workspace: wsSlug ?? "", mode: "chat" },
      }}
      placeholder={
        sending ? t("composer.agent_working") : t("composer.message_placeholder")
      }
      pillLabel={
        sending
          ? t("composer.agent_working")
          : disabled
            ? (disabledReason ?? t("composer.chat_unavailable"))
            : t("composer.message_placeholder")
      }
      pillIcon="chatbubble-ellipses-outline"
      disabled={disabled}
      disabledReason={disabledReason}
      isSending={sending}
      renderStop={() => <StopButton onPress={handleStop} />}
      alwaysExpanded
    />
  );
}

function StopButton({ onPress }: { onPress: () => void }) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const { t } = useTranslation("chat");
  return (
    <Animated.View
      key="stop"
      entering={FadeIn.duration(120)}
      exiting={FadeOut.duration(120)}
    >
      <Pressable
        onPress={onPress}
        className="h-8 w-8 items-center justify-center rounded-full bg-foreground active:opacity-80"
        hitSlop={12}
        accessibilityRole="button"
        accessibilityLabel={t("composer.stop_agent_label")}
      >
        <View
          style={{
            width: 10,
            height: 10,
            backgroundColor: theme.background,
            borderRadius: 1.5,
          }}
        />
      </Pressable>
    </Animated.View>
  );
}
