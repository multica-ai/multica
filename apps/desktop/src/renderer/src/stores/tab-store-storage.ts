import type { StateStorage } from "zustand/middleware";
import type { StorageAdapter } from "@multica/core/types";

type SyncStorage = Pick<StorageAdapter, "getItem" | "setItem" | "removeItem">;

interface PersistedEnvelope {
  state: { activeWorkspaceSlug?: unknown; byWorkspace: Record<string, unknown> };
  version?: number;
}

function parseEnvelope(raw: string | null): PersistedEnvelope | null {
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object") return null;
    const state = (parsed as { state?: unknown }).state;
    if (!state || typeof state !== "object") return null;
    const byWorkspace = (state as { byWorkspace?: unknown }).byWorkspace;
    if (!byWorkspace || typeof byWorkspace !== "object" || Array.isArray(byWorkspace)) {
      return null;
    }
    return parsed as PersistedEnvelope;
  } catch {
    return null;
  }
}

function snapshotGroups(byWorkspace: Record<string, unknown>): Map<string, string> {
  return new Map(
    Object.entries(byWorkspace).map(([slug, group]) => [slug, JSON.stringify(group)]),
  );
}

/**
 * Three-way merge of workspace tab groups. A group this window changed or
 * removed since `baseline` (its last write, or what it hydrated) is taken
 * from `ours`; every other group is taken from `stored`, which may hold
 * another window's newer copy.
 */
export function mergeTabGroups(
  ours: Record<string, unknown>,
  stored: Record<string, unknown>,
  baseline: ReadonlyMap<string, string>,
): Record<string, unknown> {
  const slugs = new Set([...Object.keys(ours), ...Object.keys(stored)]);
  const merged: [string, unknown][] = [];
  for (const slug of slugs) {
    const own = Object.hasOwn(ours, slug) ? ours[slug] : undefined;
    const ownSnapshot = own === undefined ? undefined : JSON.stringify(own);
    const group =
      ownSnapshot !== baseline.get(slug)
        ? own
        : Object.hasOwn(stored, slug)
          ? stored[slug]
          : undefined;
    if (group !== undefined) merged.push([slug, group]);
  }
  return Object.fromEntries(merged);
}

export interface WindowTabStorage extends StateStorage {
  /** Record the groups this window holds once hydration has finished. */
  setBaseline(byWorkspace: Record<string, unknown>): void;
  /** The persisted copy of one workspace's group, as last written by any window. */
  readStoredGroup(name: string, slug: string): unknown;
}

/**
 * Every main window persists its tabs under the same key, and each holds a
 * stale copy of the workspaces it is not showing. Writing whole snapshots
 * would let one window erase the tabs another just opened, so each write
 * merges per workspace group. An empty state (logout) is written as-is.
 */
export function createWindowTabStorage(inner: SyncStorage): WindowTabStorage {
  let baseline: Map<string, string> | null = null;

  return {
    getItem: (name) => inner.getItem(name),
    removeItem: (name) => inner.removeItem(name),
    setItem(name, value) {
      const ours = parseEnvelope(value);
      if (!ours) {
        inner.setItem(name, value);
        return;
      }
      const ownGroups = ours.state.byWorkspace;
      const isEmpty =
        ours.state.activeWorkspaceSlug == null && Object.keys(ownGroups).length === 0;
      const stored = baseline && !isEmpty ? parseEnvelope(inner.getItem(name)) : null;
      const next =
        baseline && stored && stored.version === ours.version
          ? JSON.stringify({
              ...ours,
              state: {
                ...ours.state,
                byWorkspace: mergeTabGroups(ownGroups, stored.state.byWorkspace, baseline),
              },
            })
          : value;
      baseline = snapshotGroups(ownGroups);
      inner.setItem(name, next);
    },
    setBaseline(byWorkspace) {
      baseline = snapshotGroups(byWorkspace);
    },
    readStoredGroup(name, slug) {
      const groups = parseEnvelope(inner.getItem(name))?.state.byWorkspace;
      return groups && Object.hasOwn(groups, slug) ? groups[slug] : undefined;
    },
  };
}
