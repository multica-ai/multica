export { SCOPES, type HotkeyScope } from "./scopes";
export {
  useHotkeyRegistry,
  type HotkeyCommand,
} from "./registry";
export { useHotkey, useHotkeySequence } from "./use-hotkey";
export type {
  UseScopedHotkeyOptions,
  UseScopedSequenceOptions,
} from "./use-hotkey";
export { formatKeys, formatKeysForPlatform } from "./format";
