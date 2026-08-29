/**
 * Android fallback for `showActionSheetWithOptions`. Mount once at the app
 * root. iOS renders nothing — ActionSheetIOS owns that path.
 */
import { useEffect, useState } from "react";
import { Modal, Platform, Pressable, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Text } from "@/components/ui/text";
import {
  parseActionSheet,
  registerAndroidActionSheet,
  type ActionSheetOptions,
} from "@/lib/action-sheet";

export function ActionSheetHost() {
  const insets = useSafeAreaInsets();
  const [state, setState] = useState<{
    options: ActionSheetOptions;
    callback: (buttonIndex: number) => void;
  } | null>(null);

  useEffect(() => {
    if (Platform.OS === "ios") return;
    return registerAndroidActionSheet((options, callback) => {
      setState({ options, callback });
    });
  }, []);

  if (Platform.OS === "ios" || !state) return null;

  const parsed = parseActionSheet(state.options);
  const dismiss = (index: number) => {
    const cb = state.callback;
    setState(null);
    cb(index);
  };
  const cancelIndex = parsed.cancel?.index ?? -1;

  return (
    <Modal
      transparent
      animationType="fade"
      visible
      onRequestClose={() => dismiss(cancelIndex)}
    >
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Dismiss"
        className="flex-1 justify-end bg-black/40"
        onPress={() => dismiss(cancelIndex)}
      >
        <Pressable
          className="px-3"
          style={{ paddingBottom: Math.max(insets.bottom, 12) }}
          onPress={(e) => e.stopPropagation()}
        >
          <View className="overflow-hidden rounded-xl bg-popover">
            {parsed.title ? (
              <Text className="px-4 pt-3 pb-1 text-center text-sm text-muted-foreground">
                {parsed.title}
              </Text>
            ) : null}
            {parsed.message ? (
              <Text className="px-4 pb-2 text-center text-sm text-muted-foreground">
                {parsed.message}
              </Text>
            ) : null}
            {parsed.actions.map((row, i) => (
              <Pressable
                key={row.index}
                disabled={row.role === "disabled"}
                onPress={() => dismiss(row.index)}
                className={
                  i === 0 && !parsed.title && !parsed.message
                    ? "min-h-12 items-center justify-center px-4 py-3 active:bg-secondary"
                    : "min-h-12 items-center justify-center border-t border-border px-4 py-3 active:bg-secondary"
                }
              >
                <Text
                  className={
                    row.role === "destructive"
                      ? "text-base text-destructive"
                      : row.role === "disabled"
                        ? "text-base text-muted-foreground"
                        : "text-base text-foreground"
                  }
                >
                  {row.label}
                </Text>
              </Pressable>
            ))}
          </View>
          {parsed.cancel ? (
            <Pressable
              onPress={() => dismiss(parsed.cancel!.index)}
              className="mt-2 min-h-12 items-center justify-center rounded-xl bg-popover px-4 py-3 active:bg-secondary"
            >
              <Text className="text-base font-semibold text-foreground">
                {parsed.cancel.label}
              </Text>
            </Pressable>
          ) : null}
        </Pressable>
      </Pressable>
    </Modal>
  );
}
