import type { StorageAdapter } from "@multica/core/types";

export interface KeyValueStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface EnumerableKeyValueStore extends KeyValueStore {
  keys(): string[];
}

/** Keys that hold credentials; they live in the Keychain, everything else in the app's database. */
export const SECURE_KEYS: ReadonlySet<string> = new Set(["multica_token"]);

// Lives in the app database, which iOS deletes on uninstall. The Keychain
// survives it, so a missing marker means any credential there belongs to a
// previous installation.
const INSTALL_MARKER_KEY = "multica_mobile_installed";

export function createMobileStorage(
  general: EnumerableKeyValueStore,
  secure: KeyValueStore,
): Required<StorageAdapter> {
  const storeFor = (key: string) => (SECURE_KEYS.has(key) ? secure : general);
  return {
    getItem: (key) => storeFor(key).getItem(key),
    setItem: (key, value) => storeFor(key).setItem(key, value),
    removeItem: (key) => storeFor(key).removeItem(key),
    keys: () => [
      ...general.keys().filter((key) => key !== INSTALL_MARKER_KEY),
      ...[...SECURE_KEYS].filter((key) => secure.getItem(key) !== null),
    ],
  };
}

export interface NativeSecureStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  deleteItemAsync(key: string): Promise<void>;
}

/**
 * The Keychain reads and writes synchronously but only deletes asynchronously.
 * Reads go through what this session last wrote, so a signed-out token is gone
 * at once rather than when the deletion lands.
 */
export function createSecureStore(native: NativeSecureStore): KeyValueStore {
  const written = new Map<string, string | null>();
  return {
    getItem: (key) => (written.has(key) ? (written.get(key) ?? null) : native.getItem(key)),
    setItem: (key, value) => {
      native.setItem(key, value);
      written.set(key, value);
    },
    removeItem: (key) => {
      written.set(key, null);
      void native.deleteItemAsync(key);
    },
  };
}

export function forgetCredentialsFromPreviousInstall(
  general: KeyValueStore,
  secure: KeyValueStore,
): void {
  if (general.getItem(INSTALL_MARKER_KEY) !== null) return;
  for (const key of SECURE_KEYS) secure.removeItem(key);
  general.setItem(INSTALL_MARKER_KEY, "1");
}
