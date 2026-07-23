import { beforeEach, describe, expect, it } from "vitest";
import {
  ACTIVE_TABS_KEY,
  ACTIVE_TOKEN_KEY,
  createServerScopedTokenStorage,
  restoreServerSession,
  serverKeyFromApiUrl,
  snapshotServerSession,
  tabsStorageKey,
  tokenStorageKey,
} from "./server-session";

function memoryStorage(initial: Record<string, string> = {}): Storage {
  const map = new Map(Object.entries(initial));
  return {
    get length() {
      return map.size;
    },
    clear() {
      map.clear();
    },
    getItem(key: string) {
      return map.has(key) ? (map.get(key) as string) : null;
    },
    key(index: number) {
      return [...map.keys()][index] ?? null;
    },
    removeItem(key: string) {
      map.delete(key);
    },
    setItem(key: string, value: string) {
      map.set(key, value);
    },
  };
}

describe("server-session", () => {
  let storage: Storage;

  beforeEach(() => {
    storage = memoryStorage();
  });

  it("derives a stable host key from apiUrl", () => {
    expect(serverKeyFromApiUrl("https://api.multica.ai/")).toBe("api.multica.ai");
    expect(serverKeyFromApiUrl("http://127.0.0.1:28443")).toBe("127.0.0.1:28443");
  });

  it("snapshots and restores tokens per server without cross-leak", () => {
    const cloud = "https://api.multica.ai";
    const personal = "http://127.0.0.1:28443";

    storage.setItem(ACTIVE_TOKEN_KEY, "cloud-token");
    storage.setItem(ACTIVE_TABS_KEY, JSON.stringify({ cloud: true }));
    snapshotServerSession(cloud, storage);

    storage.setItem(ACTIVE_TOKEN_KEY, "personal-token");
    storage.setItem(ACTIVE_TABS_KEY, JSON.stringify({ personal: true }));
    snapshotServerSession(personal, storage);

    restoreServerSession(cloud, storage);
    expect(storage.getItem(ACTIVE_TOKEN_KEY)).toBe("cloud-token");
    expect(storage.getItem(ACTIVE_TABS_KEY)).toBe(JSON.stringify({ cloud: true }));

    restoreServerSession(personal, storage);
    expect(storage.getItem(ACTIVE_TOKEN_KEY)).toBe("personal-token");
    expect(storage.getItem(ACTIVE_TABS_KEY)).toBe(JSON.stringify({ personal: true }));
  });

  it("migrates a legacy unscoped token onto the first server restore", () => {
    storage.setItem(ACTIVE_TOKEN_KEY, "legacy-token");
    restoreServerSession("https://api.multica.ai", storage);
    expect(storage.getItem(ACTIVE_TOKEN_KEY)).toBe("legacy-token");
    expect(storage.getItem(tokenStorageKey("https://api.multica.ai"))).toBe("legacy-token");
  });

  it("clears live token when switching to a server with no saved session", () => {
    const cloud = "https://api.multica.ai";
    const company = "https://company.example.com";

    storage.setItem(ACTIVE_TOKEN_KEY, "cloud-token");
    snapshotServerSession(cloud, storage);

    // Live key still holds cloud token until restore; company has no session.
    restoreServerSession(company, storage);
    expect(storage.getItem(ACTIVE_TOKEN_KEY)).toBeNull();
    expect(storage.getItem(tokenStorageKey(company))).toBeNull();
    // Cloud session remains available for switch-back.
    expect(storage.getItem(tokenStorageKey(cloud))).toBe("cloud-token");
  });

  it("does not migrate unscoped tabs across servers", () => {
    storage.setItem(ACTIVE_TABS_KEY, JSON.stringify({ only: "legacy" }));
    restoreServerSession("https://api.multica.ai", storage);
    expect(storage.getItem(ACTIVE_TABS_KEY)).toBeNull();
    expect(storage.getItem(tabsStorageKey("https://api.multica.ai"))).toBeNull();
  });

  it("dual-writes tokens through the scoped storage adapter", () => {
    const adapter = createServerScopedTokenStorage("https://api.multica.ai", storage);
    adapter.setItem(ACTIVE_TOKEN_KEY, "tok");
    expect(storage.getItem(ACTIVE_TOKEN_KEY)).toBe("tok");
    expect(storage.getItem(tokenStorageKey("https://api.multica.ai"))).toBe("tok");

    adapter.removeItem(ACTIVE_TOKEN_KEY);
    expect(storage.getItem(ACTIVE_TOKEN_KEY)).toBeNull();
    expect(storage.getItem(tokenStorageKey("https://api.multica.ai"))).toBeNull();
  });
});
