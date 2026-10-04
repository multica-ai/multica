/**
 * Long-press menu for a chat message bubble. HarmonyOS port of
 * apps/mobile/components/chat/message-long-press.tsx.
 *
 * Platform replacement: iOS fired `ActionSheetIOS.showActionSheetWithOptions`
 * straight from a hook (`useChatMessageLongPress`) and exposed `isPressed`
 * for the caller's highlight ring. RNOH has no action-sheet bridge, so the
 * same item set (Copy · Select Text · Cancel) renders in the shared
 * <BottomSheet> with more-menu row styling (components/nav/more-menu.tsx).
 *
 * The sheet is a CONTROLLED component mounted at the screen level (not
 * inside a list cell — an absolutely-positioned overlay must cover the
 * whole screen, and FlashList cells clip). The highlight signal becomes a
 * `message.id === menuMessageId` comparison at the call site instead of
 * local hook state.
 *
 * Item set (v1, conditional — identical to iOS):
 *   Copy · Select Text · Cancel
 *
 * Clipboard + haptics go through the platform TurboModule seams
 * (lib/clipboard.ts, lib/haptics.ts) with the same expo-shaped calls; both
 * degrade to no-ops when the native side is absent. "Select Text" parks the
 * message id in `data/chat-select-store.ts` exactly like iOS — the bubble
 * then drops its long-press wrapper and renders selectable text.
 */
import { StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { ChatMessage } from "@multica/core/types";
import * as Clipboard from "@/lib/clipboard";
import * as Haptics from "@/lib/haptics";
import { useThemeColors } from "@/lib/use-theme-colors";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { Icon } from "@/components/ui/icon";
import { Text } from "@/components/ui/text";
import { useChatSelectStore } from "@/data/chat-select-store";

interface Props {
  /** Message whose actions are on screen. Null = closed. */
  message: ChatMessage | null;
  visible: boolean;
  onClose: () => void;
}

export function ChatMessageLongPressMenu({ message, visible, onClose }: Props) {
  const c = useThemeColors();
  const hasContent = !!message?.content;

  const handleCopy = async () => {
    onClose();
    if (message?.content) {
      await Clipboard.setStringAsync(message.content);
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    }
  };

  const handleSelect = () => {
    onClose();
    if (message) {
      useChatSelectStore.getState().setSelecting(message.id);
    }
  };

  return (
    <BottomSheet visible={visible} onClose={onClose} avoidKeyboard={false}>
      <View style={styles.card}>
        {hasContent ? (
          <>
            <MenuRow
              icon="copy-outline"
              label="Copy"
              onPress={() => void handleCopy()}
            />
            <MenuRow
              icon="text-outline"
              label="Select Text"
              onPress={handleSelect}
            />
            <View style={[styles.separator, { backgroundColor: c.border }]} />
          </>
        ) : null}
        <MenuRow label="Cancel" bold onPress={onClose} />
      </View>
    </BottomSheet>
  );
}

function MenuRow({
  icon,
  label,
  bold,
  onPress,
}: {
  icon?: string;
  label: string;
  bold?: boolean;
  onPress: () => void;
}) {
  const c = useThemeColors();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      style={({ pressed }) => [
        styles.row,
        { backgroundColor: pressed ? c.secondary : "transparent" },
      ]}
      onPress={onPress}
    >
      {icon ? <Icon name={icon} size={18} color={c.foreground} /> : null}
      <Text
        style={[styles.rowLabel, { color: c.foreground }, bold && styles.bold]}
      >
        {label}
      </Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  // more-menu card rhythm: 8pt padding, 36pt rows, hairline separators
  card: { paddingTop: 4, paddingHorizontal: 8, paddingBottom: 4 },
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    height: 40,
    borderRadius: 8,
    paddingHorizontal: 4,
  },
  rowLabel: { fontSize: 15 },
  bold: { fontWeight: "600" },
  separator: { height: 1, marginVertical: 4 },
});
