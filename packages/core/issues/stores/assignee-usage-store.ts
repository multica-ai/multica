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
// Several tabs share the stored value, so this store never writes back a
// snapshot. Each pick becomes a pending increment; increments are added to
// the stored value inside the adapter's atomic `update` (a Web Lock on web
// and desktop) and dropped from the pending set only once that write lands.
// What callers see is always "stored value + pending increments", so a write
// from another tab or a failed save never loses a count.

export const ASSIGNEE_USAGE_STORAGE_PREFIX = "multica_assignee_usage";

// The Recent section shows the top three, but every assignee keeps their full
// count so the ranking stays true. This cap is only a size backstop: when a
// bucket overflows, the entry with the lowest count goes (oldest first among
// equals), and never the one just picked — so neither the leader nor a
// newcomer can be pushed out.
export const MAX_ENTRIES_PER_WORKSPACE = 300;
const MAX_WORKSPACES = 50;

export interface AssigneeUsageEntry {
  type: IssueAssigneeType;
  id: string;
  count: number;
  lastUsedAt: number;
}

type AssigneeRef = { type: IssueAssigneeType; id: string };
type UsageByWorkspace = Record<string, AssigneeUsageEntry[]>;
/** wsId → `${type}:${id}` → increment not yet saved. */
type PendingIncrements = Map<string, Map<string, AssigneeUsageEntry>>;

const EMPTY_ENTRIES: AssigneeUsageEntry[] = [];
const ASSIGNEE_TYPES: readonly string[] = ["member", "agent", "squad"];

/** Most-used first; the more recent pick wins a tie. */
export function compareAssigneeUsage(a: AssigneeUsageEntry, b: AssigneeUsageEntry): number {
  return b.count - a.count || b.lastUsedAt - a.lastUsedAt;
}

export function assigneeUsageStorageKey(userId: string): string {
  return `${ASSIGNEE_USAGE_STORAGE_PREFIX}:${userId}`;
}

const entryKey = (e: AssigneeRef) => `${e.type}:${e.id}`;

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
  if (!raw) return {};
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return {};
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) return {};
  const result: UsageByWorkspace = {};
  for (const [wsId, entries] of Object.entries(data as Record<string, unknown>)) {
    if (!Array.isArray(entries)) continue;
    const valid = entries.filter(isEntry);
    if (valid.length > 0) result[wsId] = valid;
  }
  return result;
}

/** Drop the lowest-count entries (oldest first) beyond the cap, sparing `keep`. */
function capBucket(bucket: AssigneeUsageEntry[], keep: Set<string>): AssigneeUsageEntry[] {
  if (bucket.length <= MAX_ENTRIES_PER_WORKSPACE) return bucket;
  const evictable = bucket
    .filter((e) => !keep.has(entryKey(e)))
    .sort((a, b) => a.count - b.count || a.lastUsedAt - b.lastUsedAt);
  const evict = new Set(evictable.slice(0, bucket.length - MAX_ENTRIES_PER_WORKSPACE));
  return bucket.filter((e) => !evict.has(e));
}

/** Add `pending` increments on top of `base`, then apply the size caps. */
function applyIncrements(base: UsageByWorkspace, pending: PendingIncrements | undefined): UsageByWorkspace {
  if (!pending || pending.size === 0) return base;
  const next: UsageByWorkspace = { ...base };
  for (const [wsId, increments] of pending) {
    const byKey = new Map((next[wsId] ?? []).map((e) => [entryKey(e), e]));
    for (const [key, inc] of increments) {
      const existing = byKey.get(key);
      byKey.set(key, {
        type: inc.type,
        id: inc.id,
        count: (existing?.count ?? 0) + inc.count,
        lastUsedAt: Math.max(existing?.lastUsedAt ?? 0, inc.lastUsedAt),
      });
    }
    next[wsId] = capBucket([...byKey.values()], new Set(increments.keys()));
  }

  // Backstop for when pruning never runs: evict the workspaces whose last
  // assignment is the oldest.
  const ids = Object.keys(next);
  if (ids.length > MAX_WORKSPACES) {
    const latestUse = (id: string) =>
      next[id]?.reduce((max, e) => Math.max(max, e.lastUsedAt), 0) ?? 0;
    const evict = ids
      .filter((id) => !pending.has(id))
      .sort((a, b) => latestUse(a) - latestUse(b))
      .slice(0, ids.length - MAX_WORKSPACES);
    for (const id of evict) delete next[id];
  }
  return next;
}

function clonePending(pending: PendingIncrements): PendingIncrements {
  return new Map([...pending].map(([wsId, incs]) => [wsId, new Map(incs)]));
}

/** Remove the increments `saved` from `pending`, keeping any added since. */
function subtractSaved(pending: PendingIncrements, saved: PendingIncrements) {
  for (const [wsId, incs] of saved) {
    const current = pending.get(wsId);
    if (!current) continue;
    for (const [key, inc] of incs) {
      const left = current.get(key);
      if (!left) continue;
      if (left.count <= inc.count) current.delete(key);
      else current.set(key, { ...left, count: left.count - inc.count });
    }
    if (current.size === 0) pending.delete(wsId);
  }
}

// --- State ------------------------------------------------------------------

/** userId → stored value + pending increments, as last computed. */
const cache = new Map<string, UsageByWorkspace>();
/** userId → picks not yet saved. */
const pendingByUser = new Map<string, PendingIncrements>();
/** userId → the save currently running, so saves for one user never overlap. */
const saving = new Map<string, Promise<void>>();
const listeners = new Set<() => void>();
let storageAdapter: StorageAdapter = defaultStorage;

function emit() {
  for (const listener of listeners) listener();
}

function readStored(userId: string): UsageByWorkspace {
  try {
    return parse(storageAdapter.getItem(assigneeUsageStorageKey(userId)));
  } catch {
    return {};
  }
}

/** Recompute what this tab shows from storage plus its unsaved picks. */
function refresh(userId: string) {
  cache.set(userId, applyIncrements(readStored(userId), pendingByUser.get(userId)));
}

function getBuckets(userId: string): UsageByWorkspace {
  if (!cache.has(userId)) refresh(userId);
  return cache.get(userId)!;
}

/** Atomic read-modify-write of one user's stored value. */
function updateStored(userId: string, updater: (current: UsageByWorkspace) => UsageByWorkspace | null) {
  const key = assigneeUsageStorageKey(userId);
  const run = (raw: string | null) => {
    const next = updater(parse(raw));
    return next === null ? null : JSON.stringify(next);
  };
  if (storageAdapter.update) return storageAdapter.update(key, run);
  return new Promise<void>((resolve) => {
    const next = run(storageAdapter.getItem(key));
    if (next !== null) storageAdapter.setItem(key, next);
    resolve();
  });
}

/**
 * Save this user's pending picks. Runs one save at a time per user; picks
 * made meanwhile are picked up by the next one. A failed save (quota exceeded,
 * storage disabled) keeps the picks pending for the next attempt and never
 * throws — a Recent section that does not persist must not block assigning.
 */
function save(userId: string): Promise<void> {
  const previous = saving.get(userId) ?? Promise.resolve();
  const next = previous.then(async () => {
    const pending = pendingByUser.get(userId);
    if (!pending || pending.size === 0) return;
    const batch = clonePending(pending);
    try {
      await updateStored(userId, (current) => applyIncrements(current, batch));
    } catch {
      return;
    }
    subtractSaved(pending, batch);
    if (pending.size === 0) pendingByUser.delete(userId);
    refresh(userId);
    emit();
  }).catch(() => {});
  saving.set(userId, next);
  void next.finally(() => {
    if (saving.get(userId) === next) saving.delete(userId);
  });
  return next;
}

// --- Cross-tab sync ---------------------------------------------------------

let unsubscribeStorage: (() => void) | null = null;

function onStoredKeyChanged(key: string | null) {
  if (key === null) {
    // Another tab cleared storage: recompute every user on next access.
    if (cache.size > 0) {
      cache.clear();
      emit();
    }
    return;
  }
  const prefix = `${ASSIGNEE_USAGE_STORAGE_PREFIX}:`;
  if (!key.startsWith(prefix)) return;
  const userId = key.slice(prefix.length);
  // Not cached yet means nobody has read it; the next read computes it.
  if (cache.has(userId)) refresh(userId);
  emit();
}

/** Listen for any change to the usage this tab sees, local or from another tab. */
export function subscribeAssigneeUsage(listener: () => void): () => void {
  if (!unsubscribeStorage) {
    // While nobody was listening, other tabs' writes went unseen. Recompute
    // on next read; React re-reads the snapshot right after subscribing.
    cache.clear();
    unsubscribeStorage = storageAdapter.subscribe?.(onStoredKeyChanged) ?? (() => {});
  }
  listeners.add(listener);
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

/**
 * Count one pick of `assignee` by `userId` in `wsId`. Shows up immediately;
 * the returned promise settles once the save has been attempted. Never
 * rejects.
 */
export function recordAssigneeUsage(userId: string, wsId: string, assignee: AssigneeRef): Promise<void> {
  let pending = pendingByUser.get(userId);
  if (!pending) {
    pending = new Map();
    pendingByUser.set(userId, pending);
  }
  let increments = pending.get(wsId);
  if (!increments) {
    increments = new Map();
    pending.set(wsId, increments);
  }
  const key = entryKey(assignee);
  increments.set(key, {
    type: assignee.type,
    id: assignee.id,
    count: (increments.get(key)?.count ?? 0) + 1,
    lastUsedAt: Date.now(),
  });
  refresh(userId);
  emit();
  return save(userId);
}

/**
 * Drop this user's buckets for workspaces they no longer belong to. Writes
 * only when something is actually removed, and does it atomically, so it can
 * never overwrite picks another tab has just saved.
 */
export function pruneAssigneeUsageWorkspaces(userId: string, activeWsIds: string[]): Promise<void> {
  const allow = new Set(activeWsIds);
  const pending = pendingByUser.get(userId);
  if (pending) for (const wsId of [...pending.keys()]) if (!allow.has(wsId)) pending.delete(wsId);

  return updateStored(userId, (current) => {
    const stale = Object.keys(current).filter((wsId) => !allow.has(wsId));
    if (stale.length === 0) return null;
    const next: UsageByWorkspace = {};
    for (const [wsId, entries] of Object.entries(current)) {
      if (allow.has(wsId)) next[wsId] = entries;
    }
    return next;
  })
    .catch(() => {})
    .then(() => {
      refresh(userId);
      emit();
    });
}

/** Test hook: forget all in-memory state and optionally swap the storage backend. */
export function resetAssigneeUsageForTests(adapter: StorageAdapter = defaultStorage): void {
  if (unsubscribeStorage) {
    unsubscribeStorage();
    unsubscribeStorage = listeners.size > 0 ? (adapter.subscribe?.(onStoredKeyChanged) ?? (() => {})) : null;
  }
  storageAdapter = adapter;
  cache.clear();
  pendingByUser.clear();
  saving.clear();
  emit();
}
