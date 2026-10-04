/**
 * Accessors for the app's own ArkTS TurboModules (see
 * harmony/entry/src/main/ets/multica/). Each getter returns null when the
 * native side is missing — e.g. a debug HAP rebuilt before the ArkTS change
 * landed, or a Metro reload against an older install — so callers must
 * degrade gracefully instead of crashing.
 */
import type { TurboModule } from "react-native";
import { TurboModuleRegistry } from "react-native";

interface SecureStorageSpec extends TurboModule {
  set(key: string, value: string): Promise<boolean>;
  get(key: string): Promise<string | null>;
  remove(key: string): Promise<boolean>;
}

interface ClipboardSpec extends TurboModule {
  setString(text: string): Promise<boolean>;
  getString(): Promise<string>;
}

interface HapticsSpec extends TurboModule {
  impactLight(): Promise<boolean>;
  impactMedium(): Promise<boolean>;
  notificationSuccess(): Promise<boolean>;
  notificationError(): Promise<boolean>;
}

interface MediaPickerSpec extends TurboModule {
  /** Resolves a `file://` URI of the picked image copied into the HAP
   *  cache, or null on user cancel / failure. */
  pickImage(): Promise<string | null>;
  /** Same contract for an arbitrary document. */
  pickDocument(): Promise<string | null>;
}

export const secureStorage = TurboModuleRegistry.get<SecureStorageSpec>(
  "MulticaSecureStorage",
);
export const clipboard = TurboModuleRegistry.get<ClipboardSpec>(
  "MulticaClipboard",
);
export const haptics = TurboModuleRegistry.get<HapticsSpec>("MulticaHaptics");
export const mediaPicker = TurboModuleRegistry.get<MediaPickerSpec>(
  "MulticaMediaPicker",
);
