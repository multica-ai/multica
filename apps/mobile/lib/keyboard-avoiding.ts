/**
 * KeyboardAvoidingView `enabled` policy.
 *
 * Full-screen pages: Android `softwareKeyboardLayoutMode: "resize"` already
 * shrinks the window, so extra padding would double-offset the layout.
 * Modal / formSheet pages: Android often does not resize the window, so
 * padding must stay on.
 */

export type KeyboardSurface = "fullScreen" | "modal";

export function keyboardAvoidingEnabled(
  surface: KeyboardSurface,
  os: string,
): boolean {
  return surface === "modal" || os === "ios";
}
