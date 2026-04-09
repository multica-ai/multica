import { create } from "zustand";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface Tab {
  id: string;
  path: string;
  title: string;
  icon: string;
}

interface TabStore {
  tabs: Tab[];
  activeTabId: string;

  /** Open a background tab. Deduplicates by path. Returns the tab id. */
  openTab: (path: string, title: string, icon: string) => string;
  /** Always create a new tab (no dedup). Returns the tab id. */
  addTab: (path: string, title: string, icon: string) => string;
  /** Close a tab. Returns the path to navigate to if active tab changed, or null. */
  closeTab: (tabId: string) => string | null;
  /** Switch to a tab by id. */
  setActiveTab: (tabId: string) => void;
  /** Update the active tab's metadata. */
  updateActiveTab: (path: string, title: string, icon: string) => void;
}

// ---------------------------------------------------------------------------
// Route → icon mapping (title comes from document.title, not from here)
// ---------------------------------------------------------------------------

const ROUTE_ICONS: Record<string, string> = {
  "/inbox": "Inbox",
  "/my-issues": "CircleUser",
  "/issues": "ListTodo",
  "/agents": "Bot",
  "/runtimes": "Monitor",
  "/skills": "BookOpenText",
  "/settings": "Settings",
};

/** Resolve a route icon. Title is NOT determined here — it comes from document.title. */
export function resolveRouteIcon(pathname: string): string {
  return ROUTE_ICONS[pathname] ?? (pathname.startsWith("/issues/") ? "ListTodo" : "ListTodo");
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

const DEFAULT_PATH = "/issues";

function createId(): string {
  return crypto.randomUUID();
}

const initialTab: Tab = {
  id: createId(),
  path: DEFAULT_PATH,
  title: "Issues",
  icon: resolveRouteIcon(DEFAULT_PATH),
};

export const useTabStore = create<TabStore>((set, get) => ({
  tabs: [initialTab],
  activeTabId: initialTab.id,

  openTab(path, title, icon) {
    const { tabs } = get();
    const existing = tabs.find((t) => t.path === path);
    if (existing) return existing.id;

    const tab: Tab = { id: createId(), path, title, icon };
    set({ tabs: [...tabs, tab] });
    return tab.id;
  },

  addTab(path, title, icon) {
    const tab: Tab = { id: createId(), path, title, icon };
    set((s) => ({ tabs: [...s.tabs, tab] }));
    return tab.id;
  },

  closeTab(tabId) {
    const { tabs, activeTabId } = get();

    // Never close the last tab — replace with default
    if (tabs.length === 1) {
      const fresh: Tab = { id: createId(), path: DEFAULT_PATH, title: "Issues", icon: resolveRouteIcon(DEFAULT_PATH) };
      set({ tabs: [fresh], activeTabId: fresh.id });
      return fresh.path;
    }

    const idx = tabs.findIndex((t) => t.id === tabId);
    if (idx === -1) return null;

    const next = tabs.filter((t) => t.id !== tabId);

    if (tabId === activeTabId) {
      const newActive = next[Math.min(idx, next.length - 1)];
      set({ tabs: next, activeTabId: newActive.id });
      return newActive.path;
    }

    set({ tabs: next });
    return null;
  },

  setActiveTab(tabId) {
    set({ activeTabId: tabId });
  },

  updateActiveTab(path, title, icon) {
    const { tabs, activeTabId } = get();
    set({
      tabs: tabs.map((t) =>
        t.id === activeTabId ? { ...t, path, title, icon } : t,
      ),
    });
  },
}));
