/**
 * Per-backend session helpers for Desktop multi-server support.
 *
 * Auth tokens and tab layout are namespaced by API host so switching
 * backends can restore the previous session without re-login.
 */

export function serverKeyFromApiUrl(apiUrl: string): string {
  try {
    return new URL(apiUrl).host.toLowerCase();
  } catch {
    return apiUrl.trim().toLowerCase();
  }
}

export function tokenStorageKey(apiUrl: string): string {
  return `multica_token@${serverKeyFromApiUrl(apiUrl)}`;
}

export function tabsStorageKey(apiUrl: string): string {
  return `multica_tabs@${serverKeyFromApiUrl(apiUrl)}`;
}

/** Keys that should be dual-written / restored when switching servers. */
export const ACTIVE_TOKEN_KEY = "multica_token";
export const ACTIVE_TABS_KEY = "multica_tabs";

const TOKEN_NAMESPACE_PREFIX = "multica_token@";

/**
 * Before leaving a server, mirror the live session keys into the
 * host-scoped namespace so the next switch-back can restore them.
 */
export function snapshotServerSession(apiUrl: string, storage: Storage = localStorage): void {
  const token = storage.getItem(ACTIVE_TOKEN_KEY);
  if (token) {
    storage.setItem(tokenStorageKey(apiUrl), token);
  } else {
    storage.removeItem(tokenStorageKey(apiUrl));
  }

  const tabs = storage.getItem(ACTIVE_TABS_KEY);
  if (tabs) {
    storage.setItem(tabsStorageKey(apiUrl), tabs);
  } else {
    storage.removeItem(tabsStorageKey(apiUrl));
  }
}

function hasAnyNamespacedToken(storage: Storage): boolean {
  for (let i = 0; i < storage.length; i++) {
    const key = storage.key(i);
    if (key?.startsWith(TOKEN_NAMESPACE_PREFIX)) return true;
  }
  return false;
}

/**
 * On boot / after switch, restore the host-scoped session into the live
 * keys that CoreProvider and Zustand persist expect.
 *
 * Migration (first run after upgrade): if no namespaced tokens exist yet
 * and a legacy unscoped `multica_token` is present, claim it for this
 * server. On subsequent switches we never re-claim the live key — that
 * would leak server A's token onto server B.
 */
export function restoreServerSession(apiUrl: string, storage: Storage = localStorage): void {
  const scopedKey = tokenStorageKey(apiUrl);
  const namespacedToken = storage.getItem(scopedKey);

  if (namespacedToken) {
    storage.setItem(ACTIVE_TOKEN_KEY, namespacedToken);
  } else if (!hasAnyNamespacedToken(storage)) {
    const legacyToken = storage.getItem(ACTIVE_TOKEN_KEY);
    if (legacyToken) {
      storage.setItem(scopedKey, legacyToken);
      // keep ACTIVE_TOKEN_KEY as-is
    } else {
      storage.removeItem(ACTIVE_TOKEN_KEY);
    }
  } else {
    // Target server has no saved session; force login on this backend.
    storage.removeItem(ACTIVE_TOKEN_KEY);
  }

  const namespacedTabs = storage.getItem(tabsStorageKey(apiUrl));
  if (namespacedTabs) {
    storage.setItem(ACTIVE_TABS_KEY, namespacedTabs);
  } else {
    // Do not migrate unscoped tabs across servers — workspace slugs collide.
    storage.removeItem(ACTIVE_TABS_KEY);
  }
}

/**
 * Storage adapter that dual-writes auth tokens to the host-scoped key
 * while keeping the legacy `multica_token` slot for code that still
 * reads it directly (daemon sync, older helpers).
 */
export function createServerScopedTokenStorage(
  apiUrl: string,
  storage: Storage = localStorage,
): {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
  removeItem: (key: string) => void;
} {
  const scopedTokenKey = tokenStorageKey(apiUrl);
  return {
    getItem(key) {
      if (key === ACTIVE_TOKEN_KEY) {
        return storage.getItem(scopedTokenKey) ?? storage.getItem(ACTIVE_TOKEN_KEY);
      }
      return storage.getItem(key);
    },
    setItem(key, value) {
      if (key === ACTIVE_TOKEN_KEY) {
        storage.setItem(scopedTokenKey, value);
        storage.setItem(ACTIVE_TOKEN_KEY, value);
        return;
      }
      storage.setItem(key, value);
    },
    removeItem(key) {
      if (key === ACTIVE_TOKEN_KEY) {
        storage.removeItem(scopedTokenKey);
        storage.removeItem(ACTIVE_TOKEN_KEY);
        return;
      }
      storage.removeItem(key);
    },
  };
}
