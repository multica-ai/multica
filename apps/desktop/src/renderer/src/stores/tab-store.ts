import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { arrayMove } from "@dnd-kit/sortable";
import { createPersistStorage, defaultStorage } from "@multica/core/platform";
import { createSafeId } from "@multica/core/utils";
import { isReservedSlug } from "@multica/core/paths";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------
//
// MUL-4741 Phase 2: tabs are *sessions*, not routers. A TabSession is pure
// serializable state — URL, identity, virtual history, and a memento of
// restorable view state. The single app router is a projection of the active
// session, driven exclusively by the tab Coordinator (src/platform/
// tab-coordinator.ts) which subscribes to this store and reconciles the
// router to `activeSession.url` with a navigation token. Nothing in this
// file touches react-router.

export interface TabMemento {
  /**
   * Scroll offsets keyed `${routePathname}::${containerKey}` where the
   * container key is the `data-tab-scroll-root` attribute value ("main"
   * when bare). Scoping by route lets in-tab back/forward restore each
   * route's own offsets and stops same-named containers on different routes
   * from colliding (Linear keys its scroll memory the same way: route +
   * tab). `height` is the container's scrollHeight at capture time, kept
   * for diagnostics and future pre-sizing heuristics.
   */
  scroll: Record<string, { top: number; height: number }>;
}

export function emptyMemento(): TabMemento {
  return { scroll: {} };
}

export function scrollMementoKey(routeKey: string, containerKey: string): string {
  return `${routeKey}::${containerKey}`;
}

export interface TabSession {
  id: string;
  /**
   * Full tab URL (pathname + search + hash). Every tab URL is
   * workspace-scoped: `/{workspaceSlug}/{route}/...`.
   */
  url: string;
  /**
   * Dedup identity: the pathname alone. Search/hash (filters, anchors) are
   * view state and live in `url`/`memento`, NOT in the identity — opening
   * `/slug/issues?filter=a` while `/slug/issues?filter=b` exists focuses
   * the existing tab (RFC §8.2; deliberate semantic change from the old
   * exact-path dedupe).
   */
  resourceKey: string;
  title: string;
  icon: string;
  /**
   * Pinned tabs render at the left of the tab bar as icon-only, suppress the
   * X close button, and turn any `navigation.push()` originating in them into
   * an `openInNewTab()` so they stay parked on their original path. Pinning
   * is invariant-preserving: pinned tabs always come before unpinned tabs in
   * a workspace's `tabs` array; `togglePin` / `moveTab` enforce this.
   */
  pinned: boolean;
  /**
   * Virtual per-tab history. The single router never uses its own history
   * (the Coordinator always navigates with replace), so back/forward are
   * session operations: move `index` and project `stack[index]` to `url`.
   */
  history: { stack: string[]; index: number };
  memento: TabMemento;
}

/** Back-compat alias — external consumers still say `Tab`. */
export type Tab = TabSession;

export interface WorkspaceTabGroup {
  tabs: TabSession[];
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
   * without affecting any other group. Cross-workspace tab leakage is
   * impossible by construction because there is no global tab array.
   */
  byWorkspace: Record<string, WorkspaceTabGroup>;

  /**
   * Bumped by reloadActiveTab. The ActiveTabHost keys its subtree on
   * `${activeTabId}:${mountGeneration}` so a bump force-remounts the whole
   * page tree (RFC: reload is a remount, never router.revalidate()).
   * Deliberately NOT persisted.
   */
  mountGeneration: number;

  /**
   * Switch to a workspace.
   *   - If the group doesn't exist yet, create it with a single default tab.
   *   - If `openPath` is given, find a tab with that resourceKey and activate
   *     it; otherwise add a new tab and activate it.
   *   - If `openPath` is omitted, restore the group's last active tab
   *     (VSCode / Slack behavior — workspaces resume where you left off).
   */
  switchWorkspace: (slug: string, openPath?: string) => void;
  /**
   * Open-or-activate (dedupes by resourceKey) a tab in the active workspace.
   * `activate` focuses the opened/existing tab in the SAME store write — a
   * separate setActiveTab call would give subscribers (and React) two full
   * passes for one user action.
   */
  openTab: (
    path: string,
    title: string,
    icon: string,
    opts?: { activate?: boolean },
  ) => string;
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
  /** Patch display metadata of a tab (title-sync). Finds across groups. */
  updateTab: (tabId: string, patch: Partial<Pick<TabSession, "title" | "icon">>) => void;
  /**
   * In-tab navigation: update the active session's url/resourceKey/icon and
   * its virtual history. This is the ONLY way a session's url changes; the
   * Coordinator reconciles the router afterwards.
   */
  navigateActiveSession: (url: string, opts?: { replace?: boolean }) => void;
  /** Session-driven back/forward (there is no router history to pop). */
  goBack: () => void;
  goForward: () => void;
  /**
   * Persist captured scroll offsets for one route of a tab (Coordinator, on
   * deactivate / before in-tab navigation). REPLACE semantics per route: all
   * of the route's previous entries are dropped and the given ones written —
   * so a container scrolled back to 0 (captured as "no entry") clears its
   * stale offset instead of resurrecting it on the next visit.
   */
  commitScrollMemento: (
    tabId: string,
    routeKey: string,
    entries: Record<string, { top: number; height: number }>,
  ) => void;
  /**
   * Force-remount the active tab at the same URL: bump mountGeneration.
   * Query invalidation for the current page's scope is handled by the
   * Coordinator (it watches this counter) so this action stays pure state.
   */
  reloadActiveTab: () => void;
  /**
   * Close the active tab. The always-safe escape from a route-level crash:
   * unlike reloadActiveTab (remounts the same crashing URL), closing
   * destroys the crashing session entirely and falls back to a sibling tab
   * (or a reseeded default if it was the last tab).
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
 * Tab URLs are always workspace-scoped: `/{slug}/{route}/...`, so the route
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
// URL helpers
// ---------------------------------------------------------------------------

/** Split a tab URL into pathname and the search+hash suffix. */
export function splitTabUrl(url: string): { pathname: string; suffix: string } {
  const searchIdx = url.indexOf("?");
  const hashIdx = url.indexOf("#");
  const cut =
    searchIdx === -1
      ? hashIdx
      : hashIdx === -1
        ? searchIdx
        : Math.min(searchIdx, hashIdx);
  if (cut === -1) return { pathname: url, suffix: "" };
  return { pathname: url.slice(0, cut), suffix: url.slice(cut) };
}

/** Dedup identity of a tab URL: its pathname (search/hash are view state). */
export function resourceKeyForUrl(url: string): string {
  return splitTabUrl(url).pathname;
}

/**
 * Defensive: catch URLs that don't belong in the tab store, and normalize
 * the ones that do.
 *
 * Rejects:
 *  1. **Transition paths** (`/workspaces/new`, `/invite/...`). These are
 *     pre-workspace flows rendered by the window overlay on desktop, not
 *     tab routes. The navigation adapter normally intercepts these before
 *     they reach the store; this guard catches older persisted state.
 *  2. **Malformed workspace-scoped paths** like a stray `/issues/abc` that
 *     was constructed without the workspace prefix. The router would
 *     interpret `issues` as a workspace slug → NoAccessPage.
 *
 * Normalizes: a bare `/{slug}` (no route segment) becomes `/{slug}/issues` —
 * the workspace's default surface. This replaces the old in-router
 * `<Navigate to="issues">` index redirect (MUL-4741 invariant 1: the router
 * never self-navigates; URLs are normalized before they become sessions).
 *
 * Returns null for rejects (caller decides how to recover — usually by
 * dropping the tab or substituting a default).
 */
export function sanitizeTabPath(path: string): string | null {
  const { pathname, suffix } = splitTabUrl(path);
  const segments = pathname.split("/").filter(Boolean);
  const firstSegment = segments[0] ?? "";
  if (!firstSegment) return null;
  if (isReservedSlug(firstSegment)) {
    // Don't log for known transition paths — these are legitimate inputs
    // at the interception boundary (older persisted state or stale callers).
    const isTransition = path === "/workspaces/new" || path.startsWith("/invite/");
    if (!isTransition) {
      console.warn(
        `[tab-store] tab path "${path}" starts with reserved slug "${firstSegment}" — ` +
          `caller likely forgot the workspace prefix. Dropping.`,
      );
    }
    return null;
  }
  if (segments.length === 1) {
    return `/${firstSegment}/issues${suffix}`;
  }
  return path;
}

// ---------------------------------------------------------------------------
// Session factory
// ---------------------------------------------------------------------------

function createId(): string {
  return createSafeId();
}

function makeSession(url: string, title: string, icon: string): TabSession {
  return {
    id: createId(),
    url,
    resourceKey: resourceKeyForUrl(url),
    title,
    icon,
    pinned: false,
    history: { stack: [url], index: 0 },
    memento: emptyMemento(),
  };
}

/** Index of the first unpinned tab in a group (== pinned count). */
function pinnedBoundary(tabs: TabSession[]): number {
  let i = 0;
  while (i < tabs.length && tabs[i].pinned) i++;
  return i;
}

/** Default entry point for a workspace — its issues list. */
function defaultPathFor(slug: string): string {
  return `/${slug}/issues`;
}

function defaultTabFor(slug: string): TabSession {
  const path = defaultPathFor(slug);
  return makeSession(path, "Issues", resolveRouteIcon(path));
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
): Record<string, WorkspaceTabGroup> | null {
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
    ...byWorkspace,
    [slug]: { tabs: nextTabs, activeTabId: nextActiveTabId },
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
      mountGeneration: 0,

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
          const cleanDesired = desiredPath ? sanitizeTabPath(desiredPath) : null;
          const seedPath = cleanDesired ?? defaultPathFor(slug);
          const tab = makeSession(seedPath, "Issues", resolveRouteIcon(seedPath));
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
            const key = resourceKeyForUrl(clean);
            const match = existing.tabs.find((t) => t.resourceKey === key);
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
            const tab = makeSession(clean, "Issues", resolveRouteIcon(clean));
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

      openTab(path, title, icon, opts) {
        const { activeWorkspaceSlug, byWorkspace } = get();
        const clean = sanitizeTabPath(path);
        if (!activeWorkspaceSlug || !clean) return "";
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return "";

        // Dedup by resourceKey: the existing tab keeps its own url (its
        // filters/anchor are its view state); we only focus it.
        const key = resourceKeyForUrl(clean);
        const existing = group.tabs.find((t) => t.resourceKey === key);
        if (existing) {
          set({
            byWorkspace: {
              ...byWorkspace,
              [activeWorkspaceSlug]: { ...group, activeTabId: existing.id },
            },
          });
          return existing.id;
        }

        const tab = makeSession(clean, title, icon);
        set({
          byWorkspace: {
            ...byWorkspace,
            [activeWorkspaceSlug]: {
              tabs: [...group.tabs, tab],
              activeTabId: opts?.activate === true ? tab.id : group.activeTabId,
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

        const tab = makeSession(clean, title, icon);
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
        const next = buildCloseOtherTabsResult(byWorkspace, tabId);
        if (!next) return;
        set({ byWorkspace: next });
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
        const next: TabSession = { ...current, ...patch };
        if (next.title === current.title && next.icon === current.icon) {
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

      navigateActiveSession(url, opts) {
        const { activeWorkspaceSlug, byWorkspace } = get();
        if (!activeWorkspaceSlug) return;
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return;
        const index = group.tabs.findIndex((t) => t.id === group.activeTabId);
        if (index < 0) return;
        const clean = sanitizeTabPath(url);
        if (!clean) return;
        // A session never navigates out of its workspace — cross-workspace
        // pushes go through switchWorkspace at the adapter layer.
        if (extractWorkspaceSlug(clean) !== activeWorkspaceSlug) return;

        const current = group.tabs[index];
        if (current.url === clean) return;

        const replace = opts?.replace === true;
        const stack = replace
          ? [
              ...current.history.stack.slice(0, current.history.index),
              clean,
              ...current.history.stack.slice(current.history.index + 1),
            ]
          : [...current.history.stack.slice(0, current.history.index + 1), clean];
        const historyIndex = replace
          ? current.history.index
          : current.history.index + 1;

        const next: TabSession = {
          ...current,
          url: clean,
          resourceKey: resourceKeyForUrl(clean),
          icon: resolveRouteIcon(splitTabUrl(clean).pathname),
          history: { stack, index: historyIndex },
        };
        const nextTabs = [...group.tabs];
        nextTabs[index] = next;
        set({
          byWorkspace: {
            ...byWorkspace,
            [activeWorkspaceSlug]: { ...group, tabs: nextTabs },
          },
        });
      },

      goBack() {
        stepHistory(get, set, -1);
      },

      goForward() {
        stepHistory(get, set, +1);
      },

      commitScrollMemento(tabId, routeKey, entries) {
        const { byWorkspace } = get();
        const hit = findTabLocation(byWorkspace, tabId);
        if (!hit) return;
        const { slug, group, index } = hit;
        const current = group.tabs[index];

        const prefix = `${routeKey}::`;
        const nextScroll: TabMemento["scroll"] = {};
        for (const [key, value] of Object.entries(current.memento.scroll)) {
          if (!key.startsWith(prefix)) nextScroll[key] = value;
        }
        for (const [containerKey, value] of Object.entries(entries)) {
          nextScroll[scrollMementoKey(routeKey, containerKey)] = value;
        }

        // Skip the write when nothing changed (common: an unscrolled route
        // captured as empty over an already-empty route scope) — avoids a
        // re-entrant store tick from inside the Coordinator's subscription.
        const prevKeys = Object.keys(current.memento.scroll);
        const nextKeys = Object.keys(nextScroll);
        const unchanged =
          prevKeys.length === nextKeys.length &&
          nextKeys.every((k) => {
            const prev = current.memento.scroll[k];
            const next = nextScroll[k];
            return prev !== undefined && prev.top === next.top && prev.height === next.height;
          });
        if (unchanged) return;

        const nextTabs = [...group.tabs];
        nextTabs[index] = { ...current, memento: { scroll: nextScroll } };
        set({
          byWorkspace: {
            ...byWorkspace,
            [slug]: { ...group, tabs: nextTabs },
          },
        });
      },

      reloadActiveTab() {
        const { activeWorkspaceSlug, byWorkspace, mountGeneration } = get();
        if (!activeWorkspaceSlug) return;
        const group = byWorkspace[activeWorkspaceSlug];
        if (!group) return;
        if (!group.tabs.some((t) => t.id === group.activeTabId)) return;
        set({ mountGeneration: mountGeneration + 1 });
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
        const nextTab: TabSession = { ...current, pinned: !current.pinned };

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
        set({ activeWorkspaceSlug: null, byWorkspace: {}, mountGeneration: 0 });
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
        // v3 → v4: Tab → TabSession (MUL-4741). One-time import of legacy
        // view-state: `path` becomes `url`, identity/history/memento are
        // derived. Routers are no longer part of the model.
        if (version < 4 && state && typeof state === "object") {
          state = migrateV3ToV4(state as V3Persisted);
        }
        return state as V4Persisted;
      },
      partialize: (state) => ({
        activeWorkspaceSlug: state.activeWorkspaceSlug,
        // mountGeneration is deliberately excluded: reload pressure must
        // never persist across restarts.
        byWorkspace: Object.fromEntries(
          Object.entries(state.byWorkspace).map(([slug, group]) => [
            slug,
            {
              activeTabId: group.activeTabId,
              tabs: group.tabs.map((t) => ({
                id: t.id,
                url: t.url,
                title: t.title,
                icon: t.icon,
                pinned: t.pinned,
                history: t.history,
                memento: t.memento,
              })),
            },
          ]),
        ),
      }),
      merge: (persistedState, currentState) => {
        const persisted = persistedState as Partial<V4Persisted> | undefined;
        if (!persisted?.byWorkspace) return currentState;

        const byWorkspace: Record<string, WorkspaceTabGroup> = {};
        for (const [slug, pGroup] of Object.entries(persisted.byWorkspace)) {
          const tabs: TabSession[] = [];
          for (const pTab of pGroup.tabs) {
            const clean = sanitizeTabPath(pTab.url);
            // Persisted url may have come from a stale version or a
            // manual edit. Drop rather than rewrite so we never silently
            // put users on a url that doesn't match the group's slug.
            if (!clean || extractWorkspaceSlug(clean) !== slug) {
              console.warn(
                `[tab-store] dropping persisted tab "${pTab.url}" from ` +
                  `group "${slug}" — url/slug mismatch`,
              );
              continue;
            }
            const stack =
              Array.isArray(pTab.history?.stack) && pTab.history.stack.length > 0
                ? pTab.history.stack
                : [clean];
            const index = Math.min(
              Math.max(pTab.history?.index ?? stack.length - 1, 0),
              stack.length - 1,
            );
            tabs.push({
              id: pTab.id,
              url: clean,
              resourceKey: resourceKeyForUrl(clean),
              title: pTab.title,
              icon: pTab.icon,
              pinned: pTab.pinned === true,
              history: { stack, index },
              memento:
                pTab.memento && typeof pTab.memento.scroll === "object"
                  ? pTab.memento
                  : emptyMemento(),
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

function stepHistory(
  get: () => TabStore,
  set: (partial: Partial<TabStore>) => void,
  delta: -1 | 1,
) {
  const { activeWorkspaceSlug, byWorkspace } = get();
  if (!activeWorkspaceSlug) return;
  const group = byWorkspace[activeWorkspaceSlug];
  if (!group) return;
  const index = group.tabs.findIndex((t) => t.id === group.activeTabId);
  if (index < 0) return;
  const current = group.tabs[index];
  const nextIndex = current.history.index + delta;
  if (nextIndex < 0 || nextIndex >= current.history.stack.length) return;
  const url = current.history.stack[nextIndex];
  const next: TabSession = {
    ...current,
    url,
    resourceKey: resourceKeyForUrl(url),
    icon: resolveRouteIcon(splitTabUrl(url).pathname),
    history: { ...current.history, index: nextIndex },
  };
  const nextTabs = [...group.tabs];
  nextTabs[index] = next;
  set({
    byWorkspace: {
      ...byWorkspace,
      [activeWorkspaceSlug]: { ...group, tabs: nextTabs },
    },
  });
}

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

interface V4PersistedTab {
  id: string;
  url: string;
  title: string;
  icon: string;
  pinned: boolean;
  history: { stack: string[]; index: number };
  memento: TabMemento;
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
        id: t.id,
        url: t.path,
        title: t.title,
        icon: t.icon,
        pinned: t.pinned,
        history: { stack: [t.path], index: 0 },
        memento: emptyMemento(),
      })),
    };
  }
  return {
    activeWorkspaceSlug: v3.activeWorkspaceSlug ?? null,
    byWorkspace,
  };
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
export function getActiveTab(s: TabStore): TabSession | null {
  if (!s.activeWorkspaceSlug) return null;
  const group = s.byWorkspace[s.activeWorkspaceSlug];
  if (!group) return null;
  return group.tabs.find((t) => t.id === group.activeTabId) ?? null;
}

/**
 * The active workspace's tab group, or null when no workspace is active.
 *
 * Zustand compares selector returns with `Object.is`. Because `updateTab` /
 * `navigateActiveSession` replace the group object on every change
 * (immutable update), this selector returns a new reference on those
 * events — that's fine for TabBar which needs to observe tab-list changes,
 * but don't use this selector from components that only care about one
 * primitive (use `useActiveTabIdentity` / `useActiveTabHistory` instead).
 */
export function useActiveGroup(): WorkspaceTabGroup | null {
  return useTabStore((s) =>
    s.activeWorkspaceSlug ? (s.byWorkspace[s.activeWorkspaceSlug] ?? null) : null,
  );
}

/**
 * Active tab id + active workspace slug as a compact pair. Both primitives
 * are stable across unrelated store updates, so consumers don't re-render.
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

/** The active session's url as a primitive (Coordinator, providers). */
export function useActiveTabUrl(): string | null {
  return useTabStore((s) => getActiveTab(s)?.url ?? null);
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
  const historyIndex = useTabStore(
    (s) => getActiveTab(s)?.history.index ?? 0,
  );
  const historyLength = useTabStore(
    (s) => getActiveTab(s)?.history.stack.length ?? 1,
  );
  return { historyIndex, historyLength };
}
