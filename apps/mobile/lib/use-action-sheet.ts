import { useCallback } from "react";
import { useActionSheet as useExpoActionSheet, type ActionSheetOptions } from "@expo/react-native-action-sheet";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { Platform } from "react-native";

/** Native iOS menus and a scrollable Android sheet with system Back support. */
export function useActionSheet() {
  const { showActionSheetWithOptions: show } = useExpoActionSheet();
  const { colorScheme } = useColorScheme();
  const { bottom } = useSafeAreaInsets();
  const theme = THEME[colorScheme];

  const showActionSheetWithOptions = useCallback(
    (options: ActionSheetOptions, onSelect: (index: number) => void | Promise<void>) => {
      show(
        {
          tintColor: theme.foreground,
          destructiveColor: theme.destructive,
          containerStyle: { backgroundColor: theme.popover, paddingBottom: bottom },
          userInterfaceStyle: colorScheme,
          // A native Modal also places Android menus above native-stack modals.
          useModal: true,
          autoFocus: true,
          ...options,
        },
        (index) => {
          const selected = index ?? options.cancelButtonIndex ?? -1;
          // The Android provider calls us before its hidden state commits.
          // A follow-on sheet opened in that batch would be lost (React 19).
          if (Platform.OS === "android") setTimeout(() => { void onSelect(selected); }, 0);
          else void onSelect(selected);
        },
      );
    },
    [show, theme, bottom, colorScheme],
  );

  return { showActionSheetWithOptions };
}
