import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { arrayMove } from "@dnd-kit/sortable";
import { createPersistStorage, defaultStorage } from "@multica/core/platform";
import { createSafeId } from "@multica/core/utils";
import type { DataRouter } from "react-router-dom";
import { createTabRouter } from "../routes";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface Tab {
  id: string;
  path: string;
  title: string;
  icon: string;
  router: DataRouter;
  historyIndex: number;
  historyLength: number;
}

interface TabStore {
  tabs: Tab[];
  activeTabId: string;

  /** Open a background tab. Deduplicates by path. Returns the tab id. */
  openTab: (path: string, title: string, icon: string) => string;
  /** Always create a new tab (no dedup). Returns the tab id. */
  addTab: (path: string, title: string, icon: string) => string;
  /** Close a tab. Disposes router. */
  closeTab: (tabId: string) => void;
  /** Switch to a tab by id. */
  setActiveTab: (tabId: string) => void;
  /** Update a tab's metadata (path, title, icon — partial). */
  updateTab: (tabId: string, patch: Partial<Pick<Tab, "path" | "title" | "icon">>) => void;
  /** Update a tab's history tracking. */
  updateTabHistory: (tabId: string, historyIndex: number, historyLength: number) => void;
  /** Reorder tabs by moving one from fromIndex to toIndex. Preserves router/history. */
  moveTab: (fromIndex: number, toIndex: number) => void;
}

// ---------------------------------------------------------------------------
// Route → icon mapping (title comes from document.title, not from here)
// ---------------------------------------------------------------------------

const ROUTE_ICONS: Record<string, string> = {
  "/inbox": "Inbox",
  "/my-issues": "CircleUser",
  "/issues": "ListTodo",
  "/projects": "FolderKanban",
  "/agents": "Bot",
  "/runtimes": "Monitor",
  "/skills": "BookOpenText",
  "/settings": "Settings",
};

/** Resolve a route icon. Title is NOT determined here — it comes from document.title. */
export function resolveRouteIcon(pathname: string): string {
  return ROUTE_ICONS[pathname]
    ?? (pathname.startsWith("/issues/") ? "ListTodo" : undefined)
    ?? (pathname.startsWith("/projects/") ? "FolderKanban" : undefined)
    ?? "ListTodo";
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

const DEFAULT_PATH = "/issues";

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

const initialTab = makeTab(DEFAULT_PATH, "Issues", resolveRouteIcon(DEFAULT_PATH));

export const useTabStore = create<TabStore>()(
  persist(
    (set, get) => ({
  tabs: [initialTab],
  activeTabId: initialTab.id,

  openTab(path, title, icon) {
    const { tabs } = get();
    const existing = tabs.find((t) => t.path === path);
    if (existing) return existing.id;

    const tab = makeTab(path, title, icon);
    set({ tabs: [...tabs, tab] });
    return tab.id;
  },

  addTab(path, title, icon) {
    const tab = makeTab(path, title, icon);
    set((s) => ({ tabs: [...s.tabs, tab] }));
    return tab.id;
  },

  closeTab(tabId) {
    const { tabs, activeTabId } = get();

    const closingTab = tabs.find((t) => t.id === tabId);

    // Never close the last tab — replace with default
    if (tabs.length === 1) {
      closingTab?.router.dispose();
      const fresh = makeTab(DEFAULT_PATH, "Issues", resolveRouteIcon(DEFAULT_PATH));
      set({ tabs: [fresh], activeTabId: fresh.id });
      return;
    }

    const idx = tabs.findIndex((t) => t.id === tabId);
    if (idx === -1) return;

    closingTab?.router.dispose();
    const next = tabs.filter((t) => t.id !== tabId);

    if (tabId === activeTabId) {
      const newActive = next[Math.min(idx, next.length - 1)];
      set({ tabs: next, activeTabId: newActive.id });
    } else {
      set({ tabs: next });
    }
  },

  setActiveTab(tabId) {
    set({ activeTabId: tabId });
  },

  updateTab(tabId, patch) {
    set((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === tabId ? { ...t, ...patch } : t,
      ),
    }));
  },

  updateTabHistory(tabId, historyIndex, historyLength) {
    set((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === tabId ? { ...t, historyIndex, historyLength } : t,
      ),
    }));
  },

  moveTab(fromIndex, toIndex) {
    if (fromIndex === toIndex) return;
    set((s) => ({ tabs: arrayMove(s.tabs, fromIndex, toIndex) }));
  },
    }),
    {
      name: "multica_tabs",
      version: 1,
      storage: createJSONStorage(() => createPersistStorage(defaultStorage)),
      partialize: (state) => ({
        tabs: state.tabs.map(
          ({ router, historyIndex, historyLength, ...rest }) => rest,
        ),
        activeTabId: state.activeTabId,
      }),
      merge: (persistedState, currentState) => {
        const persisted = persistedState as
          | Pick<TabStore, "tabs" | "activeTabId">
          | undefined;
        if (!persisted?.tabs?.length) return currentState;

        const tabs: Tab[] = persisted.tabs.map((tab) => ({
          ...tab,
          router: createTabRouter(tab.path),
          historyIndex: 0,
          historyLength: 1,
        }));

        // Validate activeTabId — fall back to first tab if stale
        const activeTabId = tabs.some((t) => t.id === persisted.activeTabId)
          ? persisted.activeTabId
          : tabs[0].id;

        return { ...currentState, tabs, activeTabId };
      },
    },
  ),
);
