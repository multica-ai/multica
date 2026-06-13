import {
  ActionSheetIOS,
  Alert,
  Platform,
  type AlertButton,
} from "react-native";

type ActionSheetOptions = Parameters<
  typeof ActionSheetIOS.showActionSheetWithOptions
>[0];
type ActionSheetCallback = Parameters<
  typeof ActionSheetIOS.showActionSheetWithOptions
>[1];

type AndroidAlertConfig = {
  buttonIndices: number[];
  nextStart?: number;
  options: ActionSheetOptions;
  callback: ActionSheetCallback;
  actionIndices: number[];
};

export function showActionSheetWithOptions(
  options: ActionSheetOptions,
  callback: ActionSheetCallback,
) {
  if (Platform.OS === "ios") {
    ActionSheetIOS.showActionSheetWithOptions(options, callback);
    return;
  }

  presentAndroidActionSheet(options, callback);
}

function presentAndroidActionSheet(
  options: ActionSheetOptions,
  callback: ActionSheetCallback,
) {
  if (options.options.length === 0) return;

  const cancelButtonIndex = options.cancelButtonIndex;
  const actionIndices = options.options
    .map((_, index) => index)
    .filter((index) => index !== cancelButtonIndex);

  if (options.options.length <= 3) {
    presentAndroidAlert({
      buttonIndices: options.options.map((_, index) => index),
      options,
      callback,
      actionIndices,
    });
    return;
  }

  if (cancelButtonIndex !== undefined && actionIndices.length <= 3) {
    presentAndroidAlert({
      buttonIndices: actionIndices,
      options,
      callback,
      actionIndices,
    });
    return;
  }

  presentAndroidPage({
    start: 0,
    options,
    callback,
    actionIndices,
  });
}

function presentAndroidPage(args: {
  start: number;
  options: ActionSheetOptions;
  callback: ActionSheetCallback;
  actionIndices: number[];
}) {
  const { start, options, callback, actionIndices } = args;
  const remaining = actionIndices.length - start;
  const visibleCount = remaining > 3 ? 2 : Math.min(remaining, 3);
  const nextStart = start + visibleCount;

  presentAndroidAlert({
    buttonIndices: actionIndices.slice(start, nextStart),
    nextStart: nextStart < actionIndices.length ? nextStart : undefined,
    options,
    callback,
    actionIndices,
  });
}

function presentAndroidAlert({
  buttonIndices,
  nextStart,
  options,
  callback,
  actionIndices,
}: AndroidAlertConfig) {
  let handled = false;

  const buttons: AlertButton[] = buttonIndices.map((buttonIndex) => ({
    text: options.options[buttonIndex],
    style: getButtonStyle(options, buttonIndex),
    onPress: () => {
      handled = true;
      setTimeout(() => callback(buttonIndex), 0);
    },
  }));

  if (nextStart !== undefined) {
    buttons.push({
      text: "More…",
      onPress: () => {
        handled = true;
        setTimeout(
          () =>
            presentAndroidPage({
              start: nextStart,
              options,
              callback,
              actionIndices,
            }),
          0,
        );
      },
    });
  }

  Alert.alert(options.title ?? "", options.message, buttons, {
    cancelable: true,
    onDismiss: () => {
      if (!handled && options.cancelButtonIndex !== undefined) {
        setTimeout(() => callback(options.cancelButtonIndex as number), 0);
      }
    },
  });
}

function getButtonStyle(
  options: ActionSheetOptions,
  buttonIndex: number,
): AlertButton["style"] {
  if (buttonIndex === options.cancelButtonIndex) {
    return "cancel";
  }

  const destructive = options.destructiveButtonIndex;
  if (Array.isArray(destructive)) {
    return destructive.includes(buttonIndex) ? "destructive" : "default";
  }

  return destructive === buttonIndex ? "destructive" : "default";
}
