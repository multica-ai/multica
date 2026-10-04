/**
 * Chat composer — thin wrapper around the shared `<MessageComposer>` with
 * chat-specific wiring. HarmonyOS port of
 * apps/mobile/components/chat/chat-composer.tsx:
 *
 *   - **Controlled text**: parent (chat-screen.tsx) owns the draft via
 *     `useChatDraftsStore` so switching sessions rehydrates the right
 *     draft. Pass `value` + `onChangeText` through.
 *   - **Stop button**: while an agent task is running for the active
 *     session, `sending` flips true and we replace the Send button slot
 *     with a Stop affordance (filled foreground bg + stop glyph). Tap →
 *     `onStop()` cancels the in-flight task.
 *   - **Mention picker mode=chat**: chat is user ↔ single agent so
 *     @member / @agent / @squad / @all are noise + would notify the
 *     wrong people. iOS pushed the picker route with `?mode=chat`; the
 *     ported composer takes `mentionPickerMode="chat"` (issues only).
 *   - **No reply target**: chat is a flat conversation; passes no
 *     reply chip.
 *   - **No upload context**: chat attachments are session-scoped; the
 *     server back-fills `chat_message_id` on each row when the message
 *     persists (server-side).
 *   - **Parent owns keyboard**: chat-screen wraps in KeyboardAvoidingView,
 *     so `manageKeyboard={false}` prevents the composer from
 *     double-stacking its own keyboard handling.
 *
 * Platform deltas: `process.env.EXPO_OS` gating is gone — the stop haptic
 * goes through the ported expo-shaped seam unconditionally (silent no-op
 * when the TurboModule is absent); `pillIcon` uses `chatbubble-outline`
 * because `chatbubble-ellipses-outline` is not in the bundled glyph set
 * (scripts/generate-icons.mjs); the StopButton's 120ms reanimated fade is
 * dropped (reanimated is not wired into this app's Babel pipeline).
 */
import { useCallback } from "react";
import { StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { MessageComposer } from "@/components/composer/message-composer";
import * as Haptics from "@/lib/haptics";
import { useThemeColors } from "@/lib/use-theme-colors";

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
  /** Queued tasks remain busy, but do not expose Stop without draft restore. */
  allowStop?: boolean;
  /** Hard-disable typing + send. Used when there's no usable agent in the
   *  workspace or the session is archived (legacy). */
  disabled?: boolean;
  /** When `disabled`, replaces the pill label with the reason. */
  disabledReason?: string;
}

export function ChatComposer({
  value,
  onChangeText,
  onSend,
  onStop,
  sending,
  allowStop = true,
  disabled = false,
  disabledReason,
}: Props) {
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
    void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    onStop();
  }, [onStop]);

  return (
    <MessageComposer
      value={value}
      onChangeText={onChangeText}
      onSubmit={onSubmit}
      mentionPickerMode="chat"
      placeholder={sending ? "Agent is working…" : "Message…"}
      pillLabel={
        sending
          ? "Agent is working…"
          : disabled
            ? (disabledReason ?? "Chat unavailable")
            : "Message…"
      }
      pillIcon="chatbubble-outline"
      disabled={disabled}
      disabledReason={disabledReason}
      isSending={sending}
      renderStop={
        allowStop ? () => <StopButton onPress={handleStop} /> : undefined
      }
      manageKeyboard={false}
    />
  );
}

function StopButton({ onPress }: { onPress: () => void }) {
  const c = useThemeColors();
  return (
    <Pressable
      onPress={onPress}
      style={({ pressed }) => [
        styles.stopButton,
        { backgroundColor: c.foreground },
        pressed && styles.stopPressed,
      ]}
      hitSlop={12}
      accessibilityRole="button"
      accessibilityLabel="Stop agent"
    >
      <View
        style={{
          width: 10,
          height: 10,
          backgroundColor: c.background,
          borderRadius: 1.5,
        }}
      />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  // h-8 w-8 items-center justify-center rounded-full bg-foreground active:opacity-80
  stopButton: {
    width: 32,
    height: 32,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 999,
  },
  stopPressed: { opacity: 0.8 },
});
