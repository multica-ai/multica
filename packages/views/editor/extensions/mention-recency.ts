// Tracks the last time the current user mentioned a given target (member /
// agent / issue / "all"), per workspace, in browser storage. Used to rank the
// mention suggestion dropdown so recently-mentioned targets surface first.
//
// Data is per-device by design — the goal is "make the next mention faster",
// not a cross-device profile. If localStorage is unavailable (SSR, sandboxed
// environments) every accessor degrades to a no-op so callers can use it
// unconditionally.

import type { MentionItem } from "./mention-suggestion";

type RecencyMap = Record<string, number>;

const STORAGE_PREFIX = "multica:mention-recency:";
const MAX_ENTRIES = 200;

function storageKey(workspaceId: string): string {
  return `${STORAGE_PREFIX}${workspaceId}`;
}

function getStorage(): Storage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function readRecencyMap(workspaceId: string): RecencyMap {
  const storage = getStorage();
  if (!storage) return {};
  const raw = storage.getItem(storageKey(workspaceId));
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object") return parsed as RecencyMap;
  } catch {
    // Corrupt entry — drop it on the next write rather than throwing.
  }
  return {};
}

function writeRecencyMap(workspaceId: string, map: RecencyMap): void {
  const storage = getStorage();
  if (!storage) return;
  try {
    storage.setItem(storageKey(workspaceId), JSON.stringify(map));
  } catch {
    // Quota exceeded or storage disabled — silently skip.
  }
}

function recencyKey(item: Pick<MentionItem, "type" | "id">): string {
  return `${item.type}:${item.id}`;
}

export function recordMentionUsage(
  workspaceId: string,
  item: Pick<MentionItem, "type" | "id">,
): void {
  if (!workspaceId) return;
  const map = readRecencyMap(workspaceId);
  map[recencyKey(item)] = Date.now();

  // Lazy prune: keep the map bounded so it doesn't grow forever as members
  // and agents come and go.
  const entries = Object.entries(map);
  if (entries.length > MAX_ENTRIES) {
    entries.sort(([, ta], [, tb]) => tb - ta);
    const trimmed: RecencyMap = {};
    for (const [key, ts] of entries.slice(0, MAX_ENTRIES)) {
      trimmed[key] = ts;
    }
    writeRecencyMap(workspaceId, trimmed);
    return;
  }

  writeRecencyMap(workspaceId, map);
}

export function getRecencyMap(workspaceId: string): RecencyMap {
  if (!workspaceId) return {};
  return readRecencyMap(workspaceId);
}

// Sorts user-type mention items (member/agent) by recency DESC, with an
// alphabetical name fallback for items the user has never mentioned. Used to
// merge the previously-separate member and agent buckets into a single list.
export function sortUserItemsByRecency(
  items: MentionItem[],
  recency: RecencyMap,
): MentionItem[] {
  return [...items].sort((a, b) => {
    const ra = recency[recencyKey(a)] ?? 0;
    const rb = recency[recencyKey(b)] ?? 0;
    if (ra !== rb) return rb - ra;
    return a.label.localeCompare(b.label);
  });
}
