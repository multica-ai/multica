import { Modal, Pressable, Text, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import * as Haptics from "expo-haptics";

// Hard-coded set mirrors web's QuickEmojiPicker top row. Custom picker is M2.
const QUICK_EMOJIS = ["👍", "❤️", "😄", "🎉", "👀", "🚀", "🙏", "👌"];

interface Props {
  visible: boolean;
  onClose: () => void;
  onSelect: (emoji: string) => void;
}

// Bottom-sheet picker triggered by tapping a comment's `+` reaction button or
// "React" in the long-press menu. Lives outside @gorhom/bottom-sheet to avoid
// adding a dep for one trivial sheet — RN Modal + safe-area insets is enough.
export function EmojiQuickPicker({ visible, onClose, onSelect }: Props) {
  const insets = useSafeAreaInsets();

  const pick = (emoji: string) => {
    Haptics.selectionAsync().catch(() => {});
    onSelect(emoji);
    onClose();
  };

  return (
    <Modal
      visible={visible}
      animationType="fade"
      transparent
      onRequestClose={onClose}
    >
      <Pressable className="flex-1 bg-black/30" onPress={onClose}>
        <View className="flex-1" />
        <Pressable
          onPress={(e) => e.stopPropagation()}
          className="bg-background rounded-t-3xl pt-4 px-4"
          style={{ paddingBottom: Math.max(insets.bottom, 16) }}
        >
          <View className="self-center w-10 h-1 rounded-full bg-muted mb-4" />
          <View className="flex-row flex-wrap justify-around pb-2">
            {QUICK_EMOJIS.map((emoji) => (
              <Pressable
                key={emoji}
                onPress={() => pick(emoji)}
                className="rounded-full active:bg-muted items-center justify-center my-1"
                style={{ width: 56, height: 56 }}
              >
                <Text style={{ fontSize: 30 }}>{emoji}</Text>
              </Pressable>
            ))}
          </View>
        </Pressable>
      </Pressable>
    </Modal>
  );
}
