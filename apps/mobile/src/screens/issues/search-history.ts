export type SearchHistoryStorage = {
  getItem(key: string): string | null;
  removeItem(key: string): void;
  setItem(key: string, value: string): void;
};

export const SEARCH_HISTORY_LIMIT = 10;

const SEARCH_HISTORY_KEY_PREFIX = "multica.mobile.searchHistory";

export function getSearchHistoryStorageKey(workspaceId: string): string {
  return `${SEARCH_HISTORY_KEY_PREFIX}.${workspaceId}`;
}

export function readSearchHistory(
  storage: Pick<SearchHistoryStorage, "getItem">,
  workspaceId: string,
): string[] {
  const raw = storage.getItem(getSearchHistoryStorageKey(workspaceId));
  if (!raw) return [];

  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return normalizeHistoryItems(parsed);
  } catch {
    return [];
  }
}

export function addSearchHistoryItem(
  storage: Pick<SearchHistoryStorage, "getItem" | "setItem">,
  workspaceId: string,
  query: string,
): string[] {
  const value = normalizeQuery(query);
  const existing = readSearchHistory(storage, workspaceId);
  if (!value) return existing;

  const next = [value, ...existing.filter((item) => item !== value)].slice(0, SEARCH_HISTORY_LIMIT);
  storage.setItem(getSearchHistoryStorageKey(workspaceId), JSON.stringify(next));
  return next;
}

export function removeSearchHistoryItem(
  storage: Pick<SearchHistoryStorage, "getItem" | "setItem">,
  workspaceId: string,
  query: string,
): string[] {
  const value = normalizeQuery(query);
  const next = readSearchHistory(storage, workspaceId).filter((item) => item !== value);
  storage.setItem(getSearchHistoryStorageKey(workspaceId), JSON.stringify(next));
  return next;
}

export function clearSearchHistory(
  storage: Pick<SearchHistoryStorage, "removeItem">,
  workspaceId: string,
): string[] {
  storage.removeItem(getSearchHistoryStorageKey(workspaceId));
  return [];
}

function normalizeHistoryItems(items: unknown[]): string[] {
  const seen = new Set<string>();
  const normalized: string[] = [];

  for (const item of items) {
    if (typeof item !== "string") continue;
    const value = normalizeQuery(item);
    if (!value || seen.has(value)) continue;
    seen.add(value);
    normalized.push(value);
    if (normalized.length >= SEARCH_HISTORY_LIMIT) break;
  }

  return normalized;
}

function normalizeQuery(query: string): string {
  return query.trim().replace(/\s+/g, " ");
}
