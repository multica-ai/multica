/**
 * Cross-platform action sheet.
 *
 * iOS keeps `ActionSheetIOS` (waterfall step 1 in apps/mobile/CLAUDE.md).
 * Android has no native equivalent that accepts more than three buttons, so
 * `ActionSheetHost` presents a Material-style bottom sheet. Call sites share
 * this function so ActionSheetIOS is never invoked on Android.
 */

import { ActionSheetIOS, Platform } from "react-native";
import type { ActionSheetOptions } from "./action-sheet-options";

export type { ActionSheetOptions } from "./action-sheet-options";
export { parseActionSheet } from "./action-sheet-options";
export type { ActionSheetRow, ParsedActionSheet } from "./action-sheet-options";

type Presenter = (
  options: ActionSheetOptions,
  callback: (buttonIndex: number) => void,
) => void;

let androidPresenter: Presenter | null = null;

export function registerAndroidActionSheet(presenter: Presenter): () => void {
  androidPresenter = presenter;
  return () => {
    if (androidPresenter === presenter) androidPresenter = null;
  };
}

export function showActionSheetWithOptions(
  options: ActionSheetOptions,
  callback: (buttonIndex: number) => void,
): void {
  if (Platform.OS === "ios") {
    ActionSheetIOS.showActionSheetWithOptions(options, callback);
    return;
  }
  if (androidPresenter) {
    androidPresenter(options, callback);
    return;
  }
  callback(options.cancelButtonIndex ?? -1);
}
