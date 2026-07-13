import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { arrayMove } from "@dnd-kit/sortable";
import { createPersistStorage, defaultStorage } from "@multica/core/platform";
import { createSafeId } from "@multica/core/utils";
import { isReservedSlug } from "@multica/core/paths";
import type { HistorySnapshot } from "@/platform/history-mirror";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/**
 * A tab's serializable navigation session: the full memory-router history
 * stack (`entries` = `pathname+search+hash` strings) plus the current index.
 * The live router/mirror that produces this lives in the tab runtime registry
 * (platform/tab-runtime), not in the store.
 */
export type TabSession = HistorySnapshot;

/**
 * Per-tab scroll offsets for the tab's current page, keyed by scroll-root name.
 * Tagged with the `path` they were captured on so a stale page's offset isn't
 * restored after intra-tab navigation. Ephemeral — survives tab switches (the
 * store outlives the unmounted subtree) but is not persisted across restart.
 */
export interface TabScroll {
  path: string;
  offsets: Record<string, number>;
}

export interface Tab {
  id: string;
  /** Every tab path is workspace-scoped: `/{workspaceSlug}/{route}/...`. */
  path: string;
  title: string;
  icon: string;
  /** Serializable history session; the live router lives in the registry. */
  session: TabSession;
  /** Ephemeral scroll offsets for the current page; restored on tab switch. */
  scroll?: TabScroll;
  /**
   * Bumped by `resetTabRuntime` (tab reload) to force the tab subtree to
   * remount on a fresh runtime. Ephemeral — never persisted.
   */
  generation: number;
  /**
   * Pinned tabs render at the left of the tab bar as icon-only, suppress the
   * X close button, and turn any `navigation.push()` originating in them into
   * an `openInNewTab()` so they stay parked on their original path. Pinning
   * is invariant-preserving: pinned tabs always come before unpinned tabs in
   * a workspace's `tabs` array; `togglePin` / `moveTab` enforce this.
   */
  pinned: boolean;
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
  /** Close every other unpinned tab in the target tab's workspace. */
  closeOtherTabs: (tabId: string) => void;
  /**
   * Activate a tab. Finds it across all workspaces. Sets both the owning
   * workspace as active and that group's activeTabId; needed for any code
   * path that "jumps" to a tab belonging to a non-active workspace.
   */
  setActiveTab: (tabId: string) => void;
  /** Patch metadata of a tab (title-sync). Finds across groups. */
  updateTab: (tabId: string, patch: Partial<Pick<Tab, "path" | "title" | "icon">>) => void;
  /**
   * Sync a tab's live runtime state (current path/icon + history session) back
   * into the store. Called by the tab runtime sync on every committed
   * navigation. Finds across groups; no-ops when nothing changed.
   */
  syncTabRuntime: (
    tabId: string,
    patch: { path: string; icon: string; session: TabSession },
  ) => void;
  /**
   * Reset a tab's session to a single entry at its current path and bump its
   * generation, forcing the tab subtree to remount on a fresh runtime — the
   * crash-recovery path (registry.reloadActive drives this).
   */
  resetTabRuntime: (tabId: string) => void;
  /**
   * Persist a tab's scroll offsets. Called when the tab subtree unmounts
   * (switch away / close) so switching back can restore them. Finds across
   * groups; no-ops for a tab that no longer exists.
   */
  updateTabScroll: (tabId: string, scroll: TabScroll) => void;
  /**
   * Close the active tab. The always-safe escape from a route-level crash:
   * unlike reloading the tab (recreates the same crashing path) or navigating
   * to a "safe" route (which may itself be the route that crashed), closing
   * removes the tab entirely (the registry then disposes its router) and falls
   * back to a sibling tab (or a reseeded default if it was the last tab).
   */
  closeActiveTab: () => void;
  /**
   * Reorder within the active workspace's group only. Clamped so a tab can
   * never cross the pinned / unpinned boundary — a drag that would move a
   * pinned tab into the unpinned zone (or vice versa) is dropped at the
   * boundary instead. This keeps the "pinned tabs first" invariant without
   * requiring callers to know about it.
   */
  moveTab: (fromIndex: number, toIndex: number) => void;
  /**
   * Flip a tab's pinned state. Pinning moves it to the end of the pinned
   * zone; unpinning moves it to the start of the unpinned zone. Both
   * preserve the "pinned tabs before unpinned tabs" invariant.
   */
  togglePin: (tabId: string) => void;
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
    session: { entries: [path], index: 0 },
    generation: 0,
    pinned: false,
  };
}

/** Structural equality for two history sessions (no-op guard for sync). */
function sameSession(a: TabSession, b: TabSession): boolean {
  return (
    a.index === b.index &&
    a.entries.length === b.entries.length &&
    a.entries.every((entry, i) => entry === b.entries[i])
  );
}

/** Validate a persisted session, falling back to a fresh single-entry one. */
function sanitizeSession(
  session: TabSession | undefined,
  fallbackPath: string,
): TabSession {
  if (
    session &&
    Array.isArray(session.entries) &&
    session.entries.length > 0 &&
    session.entries.every((entry) => typeof entry === "string") &&
    typeof session.index === "number"
  ) {
    const index = Math.max(0, Math.min(session.index, session.entries.length - 1));
    return { entries: session.entries, index };
  }
  return { entries: [fallbackPath], index: 0 };
}

/** Index of the first unpinned tab in a group (== pinned count). */
function pinnedBoundary(tabs: Tab[]): number {
  let i = 0;
  while (i < tabs.length && tabs[i].pinned) i++;
  return i;
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

function buildCloseOtherTabsResult(
  byWorkspace: Record<string, WorkspaceTabGroup>,
  tabId: string,
): {
  nextByWorkspace: Record<string, WorkspaceTabGroup>;
  closingTabs: Tab[];
} | null {
  const hit = findTabLocation(byWorkspace, tabId);
  if (!hit) return null;
  const { slug, group } = hit;
  const closingTabs = group.tabs.filter(
    (tab) => !tab.pinned && tab.id !== tabId,
  );
  if (closingTabs.length === 0) return null;

  const closingIds = new Set(closingTabs.map((tab) => tab.id));
  const nextTabs = group.tabs.filter((tab) => !closingIds.has(tab.id));
  const nextActiveTabId = closingIds.has(group.activeTabId)
    ? tabId
    : group.activeTabId;

  return {
    nextByWorkspace: {
      ...byWorkspace,
      [slug]: { tabs: nextTabs, activeTabId: nextActiveTabId },
    },
    closingTabs,
  };
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

      closeOtherTabs(tabId) {
        const { byWorkspace } = get();
        const result = buildCloseOtherTabsResult(byWorkspace, tabId);
        if (!result) return;
        set({ byWorkspace: result.nextByWorkspace });
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
        if (
          next.path === current.path &&
          next.title === current.title &&
          next.icon === current.icon
        ) {
          return;
        }
        const nextTabs = [...group.tabs];
        nextTabs[index] = next;
        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
          },
        });
      },

      syncTabRuntime(tabId, patch) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;
        const current = group.tabs[index];
        if (
          current.path === patch.path &&
          current.icon === patch.icon &&
          sameSession(current.session, patch.session)
        ) {
          return;
        }
        const next: Tab = {
          ...current,
          path: patch.path,
          icon: patch.icon,
          session: patch.session,
        };
        const nextTabs = [...group.tabs];
        nextTabs[index] = next;
        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
          },
        });
      },

      resetTabRuntime(tabId) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;
        const current = group.tabs[index];
        const next: Tab = {
          ...current,
          session: { entries: [current.path], index: 0 },
          scroll: undefined,
          generation: current.generation + 1,
        };
        const nextTabs = [...group.tabs];
        nextTabs[index] = next;
        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
          },
        });
      },

      updateTabScroll(tabId, scroll) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;
        const current = group.tabs[index];
        const next: Tab = { ...current, scroll };
        const nextTabs = [...group.tabs];
        nextTabs[index] = next;
        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
          },
        });
      },

      closeActiveTab() {
        const { activeWorkspaceSlug, byWorkspace, closeTab } = get();
        if (!activeWorkspaceSlug) return;
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return;
        closeTab(group.activeTabId);
      },

      moveTab(fromIndex, toIndex) {
        if (fromIndex === toIndex) return;
        const { activeWorkspaceSlug, byWorkspace } = get();
        if (!activeWorkspaceSlug) return;
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return;
        if (fromIndex < 0 || fromIndex >= group.tabs.length) return;

        // Clamp the drop position to within the source tab's group (pinned vs
        // unpinned) so the "pinned tabs first" invariant survives drag-reorder.
        // Pinned zone is [0, boundary); unpinned zone is [boundary, length).
        const boundary = pinnedBoundary(group.tabs);
        const source = group.tabs[fromIndex];
        let clampedTo: number;
        if (source.pinned) {
          // boundary is exclusive upper bound for pinned-zone indices.
          clampedTo = Math.max(0, Math.min(toIndex, boundary - 1));
        } else {
          clampedTo = Math.max(boundary, Math.min(toIndex, group.tabs.length - 1));
        }
        if (clampedTo === fromIndex) return;
        set({
          byWorkspace: {
            ...byWorkspace,
            [activeWorkspaceSlug]: {
              ...group,
              tabs: arrayMove(group.tabs, fromIndex, clampedTo),
            },
          },
        });
      },

      togglePin(tabId) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;
        const current = group.tabs[index];
        const nextTab: Tab = { ...current, pinned: !current.pinned };

        // Remove from current position, then insert at the new zone boundary:
        //   pinning   → end of pinned zone (just before first unpinned tab)
        //   unpinning → start of unpinned zone (right after last pinned tab)
        const withoutCurrent = [
          ...group.tabs.slice(0, index),
          ...group.tabs.slice(index + 1),
        ];
        const newBoundary = pinnedBoundary(withoutCurrent);
        const insertAt = newBoundary;
        const nextTabs = [
          ...withoutCurrent.slice(0, insertAt),
          nextTab,
          ...withoutCurrent.slice(insertAt),
        ];

        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
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
          }
        }

        let nextActive = activeWorkspaceSlug;
        if (nextActive && !validSlugs.has(nextActive)) {
          nextActive = Object.keys(nextByWorkspace)[0] ?? null;
          changed = true;
        }

        if (!nextActive) {
          nextActive = Object.keys(nextByWorkspace)[0] ?? null;
          if (nextActive) changed = true;
        }

        if (!nextActive) {
          const fallbackSlug = validSlugs.values().next().value;
          if (fallbackSlug) {
            const fresh = defaultTabFor(fallbackSlug);
            nextByWorkspace[fallbackSlug] = {
              tabs: [fresh],
              activeTabId: fresh.id,
            };
            nextActive = fallbackSlug;
            changed = true;
          }
        }

        if (!changed) return;
        set({ byWorkspace: nextByWorkspace, activeWorkspaceSlug: nextActive });
      },

      reset() {
        // Runtime disposal is the registry's job — it reacts to byWorkspace
        // going empty and disposes every orphaned router/mirror.
        set({ activeWorkspaceSlug: null, byWorkspace: {} });
      },
    }),
    {
      name: "multica_tabs",
      version: 4,
      storage: createJSONStorage(() => createPersistStorage(defaultStorage)),
      migrate: (persistedState, version) => {
        // v1 → v2: flat `tabs` array → per-workspace grouping.
        // Tabs whose path isn't workspace-scoped (root `/`, login, etc.)
        // are dropped — they have no workspace to belong to, and the new
        // model's invariant is "every tab lives in a workspace group".
        let state = persistedState;
        if (version < 2 && state && typeof state === "object") {
          state = migrateV1ToV2(state as Partial<V1Persisted>);
        }
        // v2 → v3: introduce `Tab.pinned`. Existing tabs default to
        // unpinned; pin ordering invariant trivially holds (no pinned tabs).
        if (version < 3 && state && typeof state === "object") {
          state = migrateV2ToV3(state as V2Persisted);
        }
        // v3 → v4: introduce the per-tab history `session`, seeded from the
        // tab's last path. Router/history were never persisted, so there is
        // nothing else to carry over.
        if (version < 4 && state && typeof state === "object") {
          state = migrateV3ToV4(state as V3Persisted);
        }
        return state as V4Persisted;
      },
      partialize: (state) => ({
        activeWorkspaceSlug: state.activeWorkspaceSlug,
        byWorkspace: Object.fromEntries(
          Object.entries(state.byWorkspace).map(([slug, group]) => [
            slug,
            {
              activeTabId: group.activeTabId,
              // Persist the session; `generation` and `scroll` are ephemeral.
              tabs: group.tabs.map(
                ({ generation: _generation, scroll: _scroll, ...rest }) => rest,
              ),
            },
          ]),
        ),
      }),
      merge: (persistedState, currentState) => {
        const persisted = persistedState as Partial<V4Persisted> | undefined;
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
              session: sanitizeSession(pTab.session, clean),
              generation: 0,
              pinned: pTab.pinned === true,
            });
          }
          if (tabs.length === 0) continue;
          // Enforce the "pinned first" invariant on rehydration in case a
          // user (or a buggy older write) persisted the pinned tabs out of
          // order. Stable sort preserves intra-group order.
          tabs.sort((a, b) => (a.pinned === b.pinned ? 0 : a.pinned ? -1 : 1));
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

interface V3PersistedTab {
  id: string;
  path: string;
  title: string;
  icon: string;
  pinned: boolean;
}

interface V3PersistedGroup {
  tabs: V3PersistedTab[];
  activeTabId: string;
}

interface V3Persisted {
  activeWorkspaceSlug: string | null;
  byWorkspace: Record<string, V3PersistedGroup>;
}

export function migrateV2ToV3(v2: V2Persisted): V3Persisted {
  const byWorkspace: Record<string, V3PersistedGroup> = {};
  for (const [slug, group] of Object.entries(v2.byWorkspace ?? {})) {
    byWorkspace[slug] = {
      activeTabId: group.activeTabId,
      tabs: group.tabs.map((t) => ({ ...t, pinned: false })),
    };
  }
  return {
    activeWorkspaceSlug: v2.activeWorkspaceSlug ?? null,
    byWorkspace,
  };
}

interface V4PersistedTab {
  id: string;
  path: string;
  title: string;
  icon: string;
  pinned: boolean;
  session: TabSession;
}

interface V4PersistedGroup {
  tabs: V4PersistedTab[];
  activeTabId: string;
}

interface V4Persisted {
  activeWorkspaceSlug: string | null;
  byWorkspace: Record<string, V4PersistedGroup>;
}

export function migrateV3ToV4(v3: V3Persisted): V4Persisted {
  const byWorkspace: Record<string, V4PersistedGroup> = {};
  for (const [slug, group] of Object.entries(v3.byWorkspace ?? {})) {
    byWorkspace[slug] = {
      activeTabId: group.activeTabId,
      tabs: group.tabs.map((t) => ({
        ...t,
        session: { entries: [t.path], index: 0 },
      })),
    };
  }
  return {
    activeWorkspaceSlug: v3.activeWorkspaceSlug ?? null,
    byWorkspace,
  };
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

/** Find a tab by id across all workspace groups. Pure non-hook helper. */
export function getTabById(s: TabStore, tabId: string): Tab | null {
  for (const slug of Object.keys(s.byWorkspace)) {
    const tab = s.byWorkspace[slug].tabs.find((t) => t.id === tabId);
    if (tab) return tab;
  }
  return null;
}

/**
 * The active workspace's tab group, or null when no workspace is active.
 *
 * Zustand compares selector returns with `Object.is`. Because `syncTabRuntime`
 * replaces the group object on every committed navigation (immutable update),
 * this selector returns a new reference on every navigation — that's fine for
 * TabBar which needs to observe tab-list changes, but don't use this selector
 * from components that only care about one primitive (use `useActiveTabHistory`
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
 * Back/forward availability for the active tab, derived from its history
 * session. Primitive selectors so consumers re-render only when the numbers
 * change (i.e. on real navigations), not on unrelated store updates. The
 * active tab's live router itself lives in the registry — see
 * `useActiveTabRouter` in platform/tab-runtime.
 */
export function useActiveTabHistory(): {
  canGoBack: boolean;
  canGoForward: boolean;
} {
  const canGoBack = useTabStore((s) => {
    const tab = getActiveTab(s);
    return tab ? tab.session.index > 0 : false;
  });
  const canGoForward = useTabStore((s) => {
    const tab = getActiveTab(s);
    return tab ? tab.session.index < tab.session.entries.length - 1 : false;
  });
  return { canGoBack, canGoForward };
}
