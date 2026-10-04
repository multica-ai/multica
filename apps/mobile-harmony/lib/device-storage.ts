/**
 * Key-value storage for the HarmonyOS client.
 *
 * Durable path: the MulticaSecureStorage TurboModule (Asset Store Kit,
 * encrypted at rest — see harmony/entry/src/main/ets/multica/). When the
 * module is absent (debug HAP built before the ArkTS landed, or a Metro
 * reload against an older install), reads/writes degrade to an in-memory
 * session-only Map so the app still works for the session; the iOS client's
 * Keychain semantics (expo-secure-store) are otherwise preserved.
 *
 * The write-serialization semantics in data/secure-storage.ts transfer
 * unchanged because the API mirrors the async SecureStore call shapes.
 */
import { secureStorage } from "@/lib/native-modules";

const memoryStore = new Map<string, string>();

export async function getItemAsync(key: string): Promise<string | null> {
  if (secureStorage) {
    try {
      return await secureStorage.get(key);
    } catch {
      return null;
    }
  }
  return memoryStore.get(key) ?? null;
}

export async function setItemAsync(key: string, value: string): Promise<void> {
  if (secureStorage) {
    try {
      const ok = await secureStorage.set(key, value);
      if (ok) return;
    } catch {
      // Fall through to the session-only store below.
    }
  }
  memoryStore.set(key, value);
}

export async function deleteItemAsync(key: string): Promise<void> {
  if (secureStorage) {
    try {
      await secureStorage.remove(key);
    } catch {
      // Treat as removed.
    }
  }
  memoryStore.delete(key);
}
