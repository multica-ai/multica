import { Modal, Pressable, ScrollView, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Text } from "@/components/ui/text";

export interface ActionMenuOption {
  id: string;
  label: string;
  destructive?: boolean;
}

/** Cross-platform action menu for header and long-press actions. */
export function ActionMenuModal({
  visible,
  title,
  options,
  cancelLabel,
  onSelect,
  onCancel,
}: {
  visible: boolean;
  title?: string;
  options: ActionMenuOption[];
  cancelLabel: string;
  onSelect: (optionId: string) => void;
  onCancel: () => void;
}) {
  const insets = useSafeAreaInsets();

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      statusBarTranslucent
      onRequestClose={onCancel}
    >
      <View
        className="flex-1 justify-end"
        style={{ paddingBottom: insets.bottom }}
      >
        <Pressable
          className="absolute inset-0 bg-black/40"
          onPress={onCancel}
          accessibilityRole="button"
          accessibilityLabel={cancelLabel}
        />
        <View
          accessibilityViewIsModal
          className="mx-3 mb-2 overflow-hidden rounded-2xl border border-border bg-popover"
        >
          {title ? (
            <View className="border-b border-border px-4 py-3">
              <Text className="text-center text-sm font-semibold text-muted-foreground">
                {title}
              </Text>
            </View>
          ) : null}
          <ScrollView style={{ maxHeight: "70%" }} bounces={false}>
            {options.map((option, index) => (
              <Pressable
                key={option.id}
                onPress={() => onSelect(option.id)}
                accessibilityRole="button"
                className={`min-h-12 justify-center px-4 py-3 active:bg-secondary ${index > 0 ? "border-t border-border" : ""}`}
              >
                <Text
                  className={`text-center text-base ${option.destructive ? "text-destructive" : "text-foreground"}`}
                >
                  {option.label}
                </Text>
              </Pressable>
            ))}
          </ScrollView>
        </View>
        <Pressable
          onPress={onCancel}
          accessibilityRole="button"
          className="mx-3 mb-2 min-h-12 items-center justify-center rounded-2xl bg-popover px-4 py-3"
        >
          <Text className="text-base font-semibold text-foreground">
            {cancelLabel}
          </Text>
        </Pressable>
      </View>
    </Modal>
  );
}
