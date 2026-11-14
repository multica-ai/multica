import { ActionSheetIOS, Alert, Platform } from "react-native";

export interface SheetOption<T extends string> {
  value: T;
  label: string;
  destructive?: boolean;
}

/** Native action sheet; resolves with the chosen value, or null when cancelled. */
export function pick<T extends string>(title: string | undefined, options: readonly SheetOption<T>[], cancel: string): Promise<T | null> {
  return new Promise((resolve) => {
    if (Platform.OS !== "ios") {
      Alert.alert(title ?? "", undefined, [
        ...options.map((option) => ({ text: option.label, style: option.destructive ? ("destructive" as const) : undefined, onPress: () => resolve(option.value) })),
        { text: cancel, style: "cancel" as const, onPress: () => resolve(null) },
      ]);
      return;
    }
    const destructive = options.findIndex((option) => option.destructive);
    ActionSheetIOS.showActionSheetWithOptions(
      {
        title,
        options: [...options.map((option) => option.label), cancel],
        cancelButtonIndex: options.length,
        destructiveButtonIndex: destructive >= 0 ? destructive : undefined,
      },
      (index) => resolve(options[index]?.value ?? null),
    );
  });
}

/** A one-line text prompt; resolves with the text, or null when cancelled. iOS only. */
export function promptText(title: string, initial: string, save: string, cancel: string): Promise<string | null> {
  return new Promise((resolve) => {
    if (Platform.OS !== "ios") {
      resolve(null);
      return;
    }
    Alert.prompt(
      title,
      undefined,
      [
        { text: cancel, style: "cancel", onPress: () => resolve(null) },
        { text: save, onPress: (text?: string) => resolve(text ?? "") },
      ],
      "plain-text",
      initial,
    );
  });
}

/** A yes/no question before something that cannot be undone. */
export function confirmDestructive(title: string, message: string | undefined, action: string, cancel: string): Promise<boolean> {
  return new Promise((resolve) => {
    Alert.alert(title, message, [
      { text: cancel, style: "cancel", onPress: () => resolve(false) },
      { text: action, style: "destructive", onPress: () => resolve(true) },
    ]);
  });
}
