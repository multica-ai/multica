import type { StorageAdapter } from "../types/storage";

const INSTALL_ID_KEY = "multica_install_id";
const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

function randomUUID(): string {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return globalThis.crypto.randomUUID();
  }
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  bytes[6] = (bytes[6]! & 0x0f) | 0x40;
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0"));
  return `${hex.slice(0, 4).join("")}-${hex.slice(4, 6).join("")}-${hex.slice(6, 8).join("")}-${hex.slice(8, 10).join("")}-${hex.slice(10).join("")}`;
}

/** Returns the installation-scoped ID, creating it only when storage has no valid UUID. */
export function getOrCreateInstallId(
  storage: StorageAdapter,
  generate: () => string = randomUUID,
): string {
  const existing = storage.getItem(INSTALL_ID_KEY);
  if (existing && UUID_PATTERN.test(existing)) return existing.toLowerCase();

  const created = generate().toLowerCase();
  if (!UUID_PATTERN.test(created)) {
    throw new Error("install id generator returned an invalid UUID");
  }
  storage.setItem(INSTALL_ID_KEY, created);
  return created;
}
