import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { arrayMove } from "@dnd-kit/sortable";
import { createPersistStorage, defaultStorage } from "@multica/core/platform";
import { createSafeId } from "@multica/core/utils";
import { isReservedSlug } from "@multica/core/paths";
import type { DataRouter } from "react-router-dom";
import { createTabRouter } from "../routes";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface Tab {
  id: string;
  /** Every tab path is workspace-scoped: `/{workspaceSlug}/{route}/...`. */
  path: string;
  title: string;
  icon: string;
  router: DataRouter;
  historyIndex: number;
  historyLength: number;
}

export interface WorkspaceTabGroup {
  tabs: Tab[];
  /** Must be a valid tab.id in `tabs`; the empty-tabs state is transient only. */
  activeTabId: string;
}

interface TabStore {
  /**
   * The workspace currently visible in the TabBar / TabContent. Null in three
   * cases:
   *   - Fresh install, before any workspace exists or is selected.
   *   - Logged-out state (reset() wipes it).
   *   - Every workspace the user had access to got deleted / revoked.
   * When null, TabContent renders nothing and the WindowOverlay takes over.
   */
  activeWorkspaceSlug: string | null;

  /**
   * Tab groups keyed by workspace slug. Each slug maps to an independent
   * (tabs, activeTabId) pair; switching workspaces swaps the visible set
   * without affecting any other group. Cross-workspace tab leakage — the
   * bug that drove this refactor — is impossible by construction because
   * there is no global tab array anymore.
   */
  byWorkspace: Record<string, WorkspaceTabGroup>;

  /**
   * Switch to a workspace.
   *   - If the group doesn't exist yet, create it with a single default tab.
   *   - If `openPath` is given, find a tab with that exact path and activate
   *     it; otherwise add a new tab and activate it.
   *   - If `openPath` is omitted, restore the group's last active tab
   *     (VSCode / Slack behavior — workspaces resume where you left off).
   */
  switchWorkspace: (slug: string, openPath?: string) => void;
  /** Open-or-activate (dedupes by path) a tab in the active workspace. */
  openTab: (path: string, title: string, icon: string) => string;
  /** Always creates a new tab (no dedupe) in the active workspace. */
  addTab: (path: string, title: string, icon: string) => string;
  /**
   * Close a tab. Finds it across all workspaces (callers like the X button
   * only know the tab id, not the owning workspace). If this is the last
   * tab in its workspace, reseed a default tab so the invariant
   * "every live workspace has at least one tab" holds.
   */
  closeTab: (tabId: string) => void;
  /**
   * Activate a tab. Finds it across all workspaces. Sets both the owning
   * workspace as active and that group's activeTabId; needed for any code
   * path that "jumps" to a tab belonging to a non-active workspace.
   */
  setActiveTab: (tabId: string) => void;
  /** Patch metadata of a tab (router-sync, title-sync). Finds across groups. */
  updateTab: (tabId: string, patch: Partial<Pick<Tab, "path" | "title" | "icon">>) => void;
  /** Patch history tracking of a tab. Finds across groups. */
  updateTabHistory: (tabId: string, historyIndex: number, historyLength: number) => void;
  /** Reorder within the active workspace's group only. */
  moveTab: (fromIndex: number, toIndex: number) => void;
  /**
   * After the workspace list arrives/changes (login, realtime delete), drop
   * any tab group whose slug is no longer in `validSlugs`, and repoint
   * `activeWorkspaceSlug` if it pointed at one of the dropped groups.
   */
  validateWorkspaceSlugs: (validSlugs: Set<string>) => void;
  /**
   * Wipe everything. Called from logout so the next user doesn't inherit
   * the prior user's tabs. Zustand persist only writes to localStorage;
   * clearing the storage key alone would leave this live store intact
   * until app restart.
   */
  reset: () => void;
}

// ---------------------------------------------------------------------------
// Route → icon mapping (title comes from document.title, not from here)
// ---------------------------------------------------------------------------

const ROUTE_ICONS: Record<string, string> = {
  inbox: "Inbox",
  "my-issues": "CircleUser",
  issues: "ListTodo",
  projects: "FolderKanban",
  memory: "BookOpen",
  autopilots: "ListTodo",
  agents: "Bot",
  runtimes: "Monitor",
  skills: "BookOpenText",
  settings: "Settings",
};

/**
 * Resolve a route icon from a pathname.
 *
 * Tab paths are always workspace-scoped: `/{slug}/{route}/...`, so the route
 * segment lives at index 1. Pre-workspace flows (create, invite) are rendered
 * by the window overlay, never as tabs.
 *
 * Title is NOT determined here — it comes from document.title.
 */
export function resolveRouteIcon(pathname: string): string {
  const segments = pathname.split("/").filter(Boolean);
  return ROUTE_ICONS[segments[1] ?? ""] ?? "ListTodo";
}

/** Extract the leading workspace slug from a path, or null if the path
 *  isn't workspace-scoped (global path, root, or empty). */
function extractWorkspaceSlug(path: string): string | null {
  const first = path.split("/").filter(Boolean)[0] ?? "";
  if (!first) return null;
  if (isReservedSlug(first)) return null;
  return first;
}

// ---------------------------------------------------------------------------
// Path sanitization (defensive)
// ---------------------------------------------------------------------------

/**
 * Defensive: catch paths that don't belong in the tab store.
 *
 * Two kinds of rejects:
 *  1. **Transition paths** (`/workspaces/new`, `/invite/...`). These are
 *     pre-workspace flows rendered by the window overlay on desktop, not
 *     tab routes. The navigation adapter normally intercepts these before
 *     they reach the store; this guard catches older persisted state.
 *  2. **Malformed workspace-scoped paths** like a stray `/issues/abc` that
 *     was constructed without the workspace prefix. The router would
 *     interpret `issues` as a workspace slug → NoAccessPage.
 *
 * Returns null for rejects (caller decides how to recover — usually by
 * dropping the tab or substituting a default). Unlike the prior design,
 * there is no root "/" sentinel — tabs are always scoped.
 */
export function sanitizeTabPath(path: string): string | null {
  const firstSegment = path.split("/").filter(Boolean)[0] ?? "";
  if (!firstSegment) return null;
  if (isReservedSlug(firstSegment)) {
    // Don't log for known transition paths — these are legitimate inputs
    // at the interception boundary (older persisted state or stale callers).
    const isTransition = path === "/workspaces/new" || path.startsWith("/invite/");
    if (!isTransition) {
      // eslint-disable-next-line no-console
      console.warn(
        `[tab-store] tab path "${path}" starts with reserved slug "${firstSegment}" — ` +
          `caller likely forgot the workspace prefix. Dropping.`,
      );
    }
    return null;
  }
  return path;
}

// ---------------------------------------------------------------------------
// Tab factory
// ---------------------------------------------------------------------------

function createId(): string {
  return createSafeId();
}

function makeTab(path: string, title: string, icon: string): Tab {
  return {
    id: createId(),
    path,
    title,
    icon,
    router: createTabRouter(path),
    historyIndex: 0,
    historyLength: 1,
  };
}

/** Default entry point for a workspace — its issues list. */
function defaultPathFor(slug: string): string {
  return `/${slug}/issues`;
}

function defaultTabFor(slug: string): Tab {
  const path = defaultPathFor(slug);
  return makeTab(path, "Issues", resolveRouteIcon(path));
}

// ---------------------------------------------------------------------------
// Group helpers
// ---------------------------------------------------------------------------

function findTabLocation(
  byWorkspace: Record<string, WorkspaceTabGroup>,
  tabId: string,
): { slug: string; group: WorkspaceTabGroup; index: number } | null {
  for (const slug of Object.keys(byWorkspace)) {
    const group = byWorkspace[slug];
    const index = group.tabs.findIndex((t) => t.id === tabId);
    if (index >= 0) return { slug, group, index };
  }
  return null;
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

export const useTabStore = create<TabStore>()(
  persist(
    (set, get) => ({
      activeWorkspaceSlug: null,
      byWorkspace: {},

      switchWorkspace(slug, openPath) {
        // Defensive no-op if slug is empty/invalid — callers like the
        // NavigationAdapter's path-parser should already have filtered
        // these, but belt-and-braces keeps garbage out of the store.
        if (!slug) return;
        const { byWorkspace } = get();
        const existing = byWorkspace[slug];

        // Decide the desired active path for this workspace.
        const desiredPath = openPath ?? (existing ? null : defaultPathFor(slug));

        if (!existing) {
          // First time entering this workspace — create the group.
          const seedPath =
            desiredPath && sanitizeTabPath(desiredPath) === desiredPath
              ? desiredPath
              : defaultPathFor(slug);
          const tab = makeTab(seedPath, "Issues", resolveRouteIcon(seedPath));
          set({
            activeWorkspaceSlug: slug,
            byWorkspace: {
              ...byWorkspace,
              [slug]: { tabs: [tab], activeTabId: tab.id },
            },
          });
          return;
        }

        // Workspace already has tabs. Either dedupe into an existing tab or
        // add a new one (when openPath was supplied and no tab matches it).
        if (desiredPath) {
          const clean = sanitizeTabPath(desiredPath);
          if (clean) {
            const match = existing.tabs.find((t) => t.path === clean);
            if (match) {
              set({
                activeWorkspaceSlug: slug,
                byWorkspace: {
                  ...byWorkspace,
                  [slug]: { ...existing, activeTabId: match.id },
                },
              });
              return;
            }
            const tab = makeTab(clean, "Issues", resolveRouteIcon(clean));
            set({
              activeWorkspaceSlug: slug,
              byWorkspace: {
                ...byWorkspace,
                [slug]: {
                  tabs: [...existing.tabs, tab],
                  activeTabId: tab.id,
                },
              },
            });
            return;
          }
        }

        // No openPath (or openPath was rejected) — just restore the group.
        set({ activeWorkspaceSlug: slug });
      },

      openTab(path, title, icon) {
        const { activeWorkspaceSlug, byWorkspace } = get();
        const clean = sanitizeTabPath(path);
        if (!activeWorkspaceSlug || !clean) return "";
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return "";

        const existing = group.tabs.find((t) => t.path === clean);
        if (existing) {
          set({
            byWorkspace: {
              ...byWorkspace,
              [activeWorkspaceSlug]: { ...group, activeTabId: existing.id },
            },
          });
          return existing.id;
        }

        const tab = makeTab(clean, title, icon);
        set({
          byWorkspace: {
            ...byWorkspace,
            [activeWorkspaceSlug]: {
              tabs: [...group.tabs, tab],
              activeTabId: group.activeTabId,
            },
          },
        });
        return tab.id;
      },

      addTab(path, title, icon) {
        const { activeWorkspaceSlug, byWorkspace } = get();
        const clean = sanitizeTabPath(path);
        if (!activeWorkspaceSlug || !clean) return "";
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return "";

        const tab = makeTab(clean, title, icon);
        set({
          byWorkspace: {
            ...byWorkspace,
            [activeWorkspaceSlug]: {
              tabs: [...group.tabs, tab],
              activeTabId: group.activeTabId,
            },
          },
        });
        return tab.id;
      },

      closeTab(tabId) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;

        const closing = group.tabs[index];
        closing.router.dispose();

        if (group.tabs.length === 1) {
          // Last tab in this workspace — reseed a default so the workspace
          // always has at least one tab. Closing a workspace as an explicit
          // action is a separate concern (Leave/Delete in Settings).
          const fresh = defaultTabFor(slug);
          set({
            byWorkspace: {
              ...byWorkspace,
              [slug]: { tabs: [fresh], activeTabId: fresh.id },
            },
          });
          return;
        }

        const nextTabs = group.tabs.filter((t) => t.id !== tabId);
        const nextActiveTabId =
          group.activeTabId === tabId
            ? nextTabs[Math.min(index, nextTabs.length - 1)].id
            : group.activeTabId;

        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { tabs: nextTabs, activeTabId: nextActiveTabId },
          },
        });
      },

      setActiveTab(tabId) {
        const { byWorkspace, activeWorkspaceSlug } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group } = hit;
        if (slug === activeWorkspaceSlug && group.activeTabId === tabId) return;
        set({
          activeWorkspaceSlug: slug,
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, activeTabId: tabId },
          },
        });
      },

      updateTab(tabId, patch) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;
        const current = group.tabs[index];
        const next: Tab = { ...current, ...patch };
        const nextTabs = [...group.tabs];
        nextTabs[index] = next;
        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
          },
        });
      },

      updateTabHistory(tabId, historyIndex, historyLength) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;
        const current = group.tabs[index];
        const next: Tab = { ...current, historyIndex, historyLength };
        const nextTabs = [...group.tabs];
        nextTabs[index] = next;
        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
          },
        });
      },

      moveTab(fromIndex, toIndex) {
        if (fromIndex === toIndex) return;
        const { activeWorkspaceSlug, byWorkspace } = get();
        if (!activeWorkspaceSlug) return;
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return;
        set({
          byWorkspace: {
            ...byWorkspace,
            [activeWorkspaceSlug]: {
              ...group,
              tabs: arrayMove(group.tabs, fromIndex, toIndex),
            },
          },
        });
      },

      validateWorkspaceSlugs(validSlugs) {
        const { activeWorkspaceSlug, byWorkspace } = get();
        let changed = false;
        const nextByWorkspace: Record<string, WorkspaceTabGroup> = {};
        for (const slug of Object.keys(byWorkspace)) {
          if (validSlugs.has(slug)) {
            nextByWorkspace[slug] = byWorkspace[slug];
          } else {
            changed = true;
            for (const t of byWorkspace[slug].tabs) t.router.dispose();
          }
        }

        let nextActive = activeWorkspaceSlug;
        if (nextActive && !validSlugs.has(nextActive)) {
          nextActive = Object.keys(nextByWorkspace)[0] ?? null;
          changed = true;
        }

        if (!changed) return;
        set({ byWorkspace: nextByWorkspace, activeWorkspaceSlug: nextActive });
      },

      reset() {
        const { byWorkspace } = get();
        for (const slug of Object.keys(byWorkspace)) {
          for (const t of byWorkspace[slug].tabs) t.router.dispose();
        }
        set({ activeWorkspaceSlug: null, byWorkspace: {} });
      },
    }),
    {
      name: "multica_tabs",
      version: 2,
      storage: createJSONStorage(() => createPersistStorage(defaultStorage)),
      migrate: (persistedState, version) => {
        // v1 → v2: flat `tabs` array → per-workspace grouping.
        // Tabs whose path isn't workspace-scoped (root `/`, login, etc.)
        // are dropped — they have no workspace to belong to, and the new
        // model's invariant is "every tab lives in a workspace group".
        if (version < 2 && persistedState && typeof persistedState === "object") {
          return migrateV1ToV2(persistedState as Partial<V1Persisted>);
        }
        return persistedState as V2Persisted;
      },
      partialize: (state) => ({
        activeWorkspaceSlug: state.activeWorkspaceSlug,
        byWorkspace: Object.fromEntries(
          Object.entries(state.byWorkspace).map(([slug, group]) => [
            slug,
            {
              activeTabId: group.activeTabId,
              tabs: group.tabs.map(
                ({ router: _router, historyIndex: _hi, historyLength: _hl, ...rest }) =>
                  rest,
              ),
            },
          ]),
        ),
      }),
      merge: (persistedState, currentState) => {
        const persisted = persistedState as Partial<V2Persisted> | undefined;
        if (!persisted?.byWorkspace) return currentState;

        const byWorkspace: Record<string, WorkspaceTabGroup> = {};
        for (const [slug, pGroup] of Object.entries(persisted.byWorkspace)) {
          const tabs: Tab[] = [];
          for (const pTab of pGroup.tabs) {
            const clean = sanitizeTabPath(pTab.path);
            // Persisted path may have come from a stale version or a
            // manual edit. Drop rather than rewrite so we never silently
            // put users on a path that doesn't match the group's slug.
            if (!clean || extractWorkspaceSlug(clean) !== slug) {
              // eslint-disable-next-line no-console
              console.warn(
                `[tab-store] dropping persisted tab "${pTab.path}" from ` +
                  `group "${slug}" — path/slug mismatch`,
              );
              continue;
            }
            tabs.push({
              id: pTab.id,
              path: clean,
              title: pTab.title,
              icon: pTab.icon,
              router: createTabRouter(clean),
              historyIndex: 0,
              historyLength: 1,
            });
          }
          if (tabs.length === 0) continue;
          const activeTabId = tabs.some((t) => t.id === pGroup.activeTabId)
            ? pGroup.activeTabId
            : tabs[0].id;
          byWorkspace[slug] = { tabs, activeTabId };
        }

        const activeWorkspaceSlug =
          persisted.activeWorkspaceSlug && byWorkspace[persisted.activeWorkspaceSlug]
            ? persisted.activeWorkspaceSlug
            : (Object.keys(byWorkspace)[0] ?? null);

        return { ...currentState, byWorkspace, activeWorkspaceSlug };
      },
    },
  ),
);

// ---------------------------------------------------------------------------
// Persisted shapes (for migration)
// ---------------------------------------------------------------------------

interface V1Tab {
  id: string;
  path: string;
  title: string;
  icon: string;
}

interface V1Persisted {
  tabs: V1Tab[];
  activeTabId: string;
}

interface V2PersistedTab {
  id: string;
  path: string;
  title: string;
  icon: string;
}

interface V2PersistedGroup {
  tabs: V2PersistedTab[];
  activeTabId: string;
}

interface V2Persisted {
  activeWorkspaceSlug: string | null;
  byWorkspace: Record<string, V2PersistedGroup>;
}

export function migrateV1ToV2(v1: Partial<V1Persisted>): V2Persisted {
  const byWorkspace: Record<string, V2PersistedGroup> = {};
  const oldTabs = v1.tabs ?? [];
  for (const tab of oldTabs) {
    const slug = extractWorkspaceSlug(tab.path);
    if (!slug) continue; // drop root / global-path tabs
    if (!byWorkspace[slug]) byWorkspace[slug] = { tabs: [], activeTabId: "" };
    byWorkspace[slug].tabs.push({
      id: tab.id,
      path: tab.path,
      title: tab.title,
      icon: tab.icon,
    });
  }

  // Each group needs a valid activeTabId. Prefer the one from v1 if it
  // landed in this group; otherwise fall back to the first tab.
  for (const slug of Object.keys(byWorkspace)) {
    const group = byWorkspace[slug];
    const hasOldActive = group.tabs.some((t) => t.id === v1.activeTabId);
    group.activeTabId = hasOldActive
      ? (v1.activeTabId as string)
      : group.tabs[0].id;
  }

  // Active workspace: whichever group inherited the v1 activeTab, falling
  // back to the first group we created (arbitrary but deterministic given
  // Object.keys iteration order on string keys).
  let activeWorkspaceSlug: string | null = null;
  for (const slug of Object.keys(byWorkspace)) {
    if (byWorkspace[slug].activeTabId === v1.activeTabId) {
      activeWorkspaceSlug = slug;
      break;
    }
  }
  if (!activeWorkspaceSlug) {
    activeWorkspaceSlug = Object.keys(byWorkspace)[0] ?? null;
  }

  return { activeWorkspaceSlug, byWorkspace };
}

// ---------------------------------------------------------------------------
// Selectors (convenience hooks)
// ---------------------------------------------------------------------------

/**
 * Pure non-hook helper — useful from event handlers / effects that already
 * need `.getState()`. For React subscriptions prefer the stable selectors
 * below.
 */
export function getActiveTab(s: TabStore): Tab | null {
  if (!s.activeWorkspaceSlug) return null;
  const group = s.byWorkspace[s.activeWorkspaceSlug];
  if (!group) return null;
  return group.tabs.find((t) => t.id === group.activeTabId) ?? null;
}

/**
 * The active workspace's tab group, or null when no workspace is active.
 *
 * Zustand compares selector returns with `Object.is`. Because `updateTab`
 * /  `updateTabHistory` replace the group object on every router tick
 * (immutable update), this selector returns a new reference on every
 * router event — that's fine for TabBar which needs to observe tab-list
 * changes, but don't use this selector from components that only care
 * about one primitive (use `useActiveTabHistory` / `useActiveTabRouter`
 * instead).
 */
export function useActiveGroup(): WorkspaceTabGroup | null {
  return useTabStore((s) =>
    s.activeWorkspaceSlug ? (s.byWorkspace[s.activeWorkspaceSlug] ?? null) : null,
  );
}

/**
 * Active tab id + active workspace slug as a compact pair. Both primitives
 * are stable across unrelated store updates — e.g. an inactive tab's
 * router tick doesn't churn these, so consumers don't re-render.
 *
 * Useful anywhere you'd previously have reached for `useActiveTab()` and
 * only needed the identity (for memoization, effect deps, ipc).
 */
export function useActiveTabIdentity(): { slug: string | null; tabId: string | null } {
  const slug = useTabStore((s) => s.activeWorkspaceSlug);
  const tabId = useTabStore((s) =>
    s.activeWorkspaceSlug
      ? (s.byWorkspace[s.activeWorkspaceSlug]?.activeTabId ?? null)
      : null,
  );
  return { slug, tabId };
}

/**
 * Active tab's router — a stable reference across tab updates, because
 * routers are created once per tab and never replaced by `updateTab`.
 * Subscribers only re-render when the active tab *changes*, not on
 * router events within the current tab.
 */
export function useActiveTabRouter(): DataRouter | null {
  return useTabStore((s) => getActiveTab(s)?.router ?? null);
}

/**
 * History tracking for the active tab as primitives. Subscribers re-render
 * only when the numeric index / length change (i.e. on actual navigations),
 * not on unrelated store updates.
 */
export function useActiveTabHistory(): {
  historyIndex: number;
  historyLength: number;
} {
  const historyIndex = useTabStore((s) => getActiveTab(s)?.historyIndex ?? 0);
  const historyLength = useTabStore((s) => getActiveTab(s)?.historyLength ?? 1);
  return { historyIndex, historyLength };
}
