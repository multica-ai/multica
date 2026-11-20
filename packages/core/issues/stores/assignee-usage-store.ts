"use client";

import { useCallback, useSyncExternalStore } from "react";
import { defaultStorage } from "../../platform/storage";
import type { IssueAssigneeType } from "../../types";
import type { StorageAdapter } from "../../types/storage";

// How often this user picks each assignee, on this device only.
//
// Stored as one key per signed-in user (`multica_assignee_usage:<userId>`),
// holding that user's buckets keyed by workspace id. Scoping the key by user
// keeps two people who share a browser from reading or adding to each other's
// counts; keying buckets by workspace id (not slug) survives renames and lets
// the picker write before the workspace layout has set the current slug (see
// recent-issues-store for that race).
//
// Not a zustand persist store on purpose: persist writes its whole in-memory
// snapshot back, so two tabs that each loaded the history would overwrite
// each other's picks. Here every pick re-reads the stored value, adds one, and
// writes it back, and other tabs pick the change up through the adapter's
// `subscribe` (the `storage` event on web and desktop).

export const ASSIGNEE_USAGE_STORAGE_PREFIX = "multica_assignee_usage";

// The Recent section only shows the top three, but counts must survive for
// everyone the user still assigns to, otherwise a newcomer could never climb
// past an established entry. So the cap is generous and evicts the entry used
// least recently rather than the one with the lowest count.
export const MAX_ENTRIES_PER_WORKSPACE = 100;
const MAX_WORKSPACES = 50;

export interface AssigneeUsageEntry {
  type: IssueAssigneeType;
  id: string;
  count: number;
  lastUsedAt: number;
}

type UsageByWorkspace = Record<string, AssigneeUsageEntry[]>;

const EMPTY_ENTRIES: AssigneeUsageEntry[] = [];
const EMPTY_BUCKETS: UsageByWorkspace = {};
const ASSIGNEE_TYPES: readonly string[] = ["member", "agent", "squad"];

/** Most-used first; the more recent pick wins a tie. */
export function compareAssigneeUsage(a: AssigneeUsageEntry, b: AssigneeUsageEntry): number {
  return b.count - a.count || b.lastUsedAt - a.lastUsedAt;
}

export function assigneeUsageStorageKey(userId: string): string {
  return `${ASSIGNEE_USAGE_STORAGE_PREFIX}:${userId}`;
}

function isEntry(value: unknown): value is AssigneeUsageEntry {
  if (!value || typeof value !== "object") return false;
  const e = value as Record<string, unknown>;
  return (
    typeof e.type === "string" &&
    ASSIGNEE_TYPES.includes(e.type) &&
    typeof e.id === "string" &&
    typeof e.count === "number" &&
    Number.isFinite(e.count) &&
    e.count > 0 &&
    typeof e.lastUsedAt === "number" &&
    Number.isFinite(e.lastUsedAt)
  );
}

function parse(raw: string | null): UsageByWorkspace {
  if (!raw) return EMPTY_BUCKETS;
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return EMPTY_BUCKETS;
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) return EMPTY_BUCKETS;
  const result: UsageByWorkspace = {};
  for (const [wsId, entries] of Object.entries(data as Record<string, unknown>)) {
    if (!Array.isArray(entries)) continue;
    const valid = entries.filter(isEntry);
    if (valid.length > 0) result[wsId] = valid;
  }
  return result;
}

// --- In-memory mirror -------------------------------------------------------

// userId → that user's buckets as last read from (or written to) storage.
const cache = new Map<string, UsageByWorkspace>();
const listeners = new Set<() => void>();
// Users whose last save failed: storage is behind memory for them, so the
// next change builds on memory instead of the stale stored copy.
const unsaved = new Set<string>();
let storageAdapter: StorageAdapter = defaultStorage;

function emit() {
  for (const listener of listeners) listener();
}

/** Read straight from storage; a failing or unavailable storage reads as empty. */
function readStored(userId: string): UsageByWorkspace | null {
  try {
    return parse(storageAdapter.getItem(assigneeUsageStorageKey(userId)));
  } catch {
    return null;
  }
}

function getBuckets(userId: string): UsageByWorkspace {
  let buckets = cache.get(userId);
  if (!buckets) {
    buckets = readStored(userId) ?? EMPTY_BUCKETS;
    cache.set(userId, buckets);
  }
  return buckets;
}

/**
 * Apply `update` to the freshest copy of the user's buckets and save it.
 *
 * Re-reading storage first is what keeps concurrent tabs from losing each
 * other's picks. Saving is best-effort: when storage is full or unavailable
 * the change still applies in memory for this session, and nothing throws —
 * a Recent section that does not persist must never block an assignment.
 */
function mutate(userId: string, update: (current: UsageByWorkspace) => UsageByWorkspace | null) {
  const current = (!unsaved.has(userId) && readStored(userId)) || getBuckets(userId);
  const next = update(current);
  if (!next) {
    // Nothing to change. Still adopt what storage holds now so this tab
    // catches up, but write nothing.
    if (current !== cache.get(userId)) {
      cache.set(userId, current);
      emit();
    }
    return;
  }
  cache.set(userId, next);
  emit();
  try {
    storageAdapter.setItem(assigneeUsageStorageKey(userId), JSON.stringify(next));
    unsaved.delete(userId);
  } catch {
    // Quota exceeded, storage disabled, etc. Keep the in-memory count only.
    unsaved.add(userId);
  }
}

// --- Cross-tab sync ---------------------------------------------------------

let unsubscribeStorage: (() => void) | null = null;

function onStoredKeyChanged(key: string | null) {
  if (key === null) {
    // Another tab cleared storage: re-read every user on next access.
    if (cache.size > 0) {
      cache.clear();
      emit();
    }
    return;
  }
  const prefix = `${ASSIGNEE_USAGE_STORAGE_PREFIX}:`;
  if (!key.startsWith(prefix)) return;
  const userId = key.slice(prefix.length);
  if (!cache.has(userId)) return;
  cache.set(userId, readStored(userId) ?? EMPTY_BUCKETS);
  emit();
}

/** Listen for any change to the in-memory usage, local or from another tab. */
export function subscribeAssigneeUsage(listener: () => void): () => void {
  listeners.add(listener);
  if (!unsubscribeStorage) {
    unsubscribeStorage = storageAdapter.subscribe?.(onStoredKeyChanged) ?? (() => {});
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && unsubscribeStorage) {
      unsubscribeStorage();
      unsubscribeStorage = null;
    }
  };
}

// --- Public API -------------------------------------------------------------

/** This user's usage entries in this workspace, unsorted. */
export function getAssigneeUsage(userId: string | null, wsId: string | null): AssigneeUsageEntry[] {
  if (!userId || !wsId) return EMPTY_ENTRIES;
  return getBuckets(userId)[wsId] ?? EMPTY_ENTRIES;
}

/** Reactive {@link getAssigneeUsage}; updates on local picks and on picks in other tabs. */
export function useAssigneeUsage(userId: string | null, wsId: string | null): AssigneeUsageEntry[] {
  const getSnapshot = useCallback(() => getAssigneeUsage(userId, wsId), [userId, wsId]);
  return useSyncExternalStore(subscribeAssigneeUsage, getSnapshot, () => EMPTY_ENTRIES);
}

/** Count one pick of `assignee` by `userId` in `wsId`. Never throws. */
export function recordAssigneeUsage(
  userId: string,
  wsId: string,
  assignee: { type: IssueAssigneeType; id: string },
): void {
  const now = Date.now();
  mutate(userId, (current) => {
    const bucket = current[wsId] ?? EMPTY_ENTRIES;
    const existing = bucket.find((e) => e.type === assignee.type && e.id === assignee.id);
    const updated: AssigneeUsageEntry = {
      type: assignee.type,
      id: assignee.id,
      count: (existing?.count ?? 0) + 1,
      lastUsedAt: now,
    };
    // Newest first, so the cap drops the entries used least recently.
    const nextBucket = [updated, ...bucket.filter((e) => e !== existing)]
      .sort((a, b) => b.lastUsedAt - a.lastUsedAt)
      .slice(0, MAX_ENTRIES_PER_WORKSPACE);

    let next: UsageByWorkspace = { ...current, [wsId]: nextBucket };

    // Backstop for when pruning never runs: evict the workspace whose last
    // assignment is the oldest.
    const ids = Object.keys(next);
    if (ids.length > MAX_WORKSPACES) {
      const latestUse = (id: string) =>
        next[id]?.reduce((max, e) => Math.max(max, e.lastUsedAt), 0) ?? 0;
      const oldest = ids.reduce((a, b) => (latestUse(b) < latestUse(a) ? b : a));
      const { [oldest]: _, ...rest } = next;
      next = rest;
    }
    return next;
  });
}

/**
 * Drop this user's buckets for workspaces they no longer belong to. Writes
 * only when something was actually removed, so an idle tab running this on
 * every membership refresh cannot overwrite newer picks with a stale copy.
 */
export function pruneAssigneeUsageWorkspaces(userId: string, activeWsIds: string[]): void {
  const allow = new Set(activeWsIds);
  mutate(userId, (current) => {
    const stale = Object.keys(current).filter((wsId) => !allow.has(wsId));
    if (stale.length === 0) return null;
    const next: UsageByWorkspace = {};
    for (const [wsId, entries] of Object.entries(current)) {
      if (allow.has(wsId)) next[wsId] = entries;
    }
    return next;
  });
}

/** Test hook: forget the in-memory mirror and optionally swap the storage backend. */
export function resetAssigneeUsageForTests(adapter: StorageAdapter = defaultStorage): void {
  if (unsubscribeStorage) {
    unsubscribeStorage();
    unsubscribeStorage = listeners.size > 0 ? (adapter.subscribe?.(onStoredKeyChanged) ?? (() => {})) : null;
  }
  storageAdapter = adapter;
  cache.clear();
  unsaved.clear();
  emit();
}
