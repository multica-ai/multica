"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

export const ISSUE_SIDEBAR_TABS = ["overview", "delivery", "runs"] as const;
export type IssueSidebarTab = (typeof ISSUE_SIDEBAR_TABS)[number];

/** One stack of tabs in the issue sidebar; groups are laid out top to bottom. */
export interface IssueSidebarGroup {
  id: string;
  tabs: IssueSidebarTab[];
  active: IssueSidebarTab;
}

export type IssueSidebarLayout = IssueSidebarGroup[];

/** Where a dragged tab is released. */
export type IssueSidebarDropTarget =
  | { type: "tab"; tab: IssueSidebarTab }
  | { type: "strip"; group: string }
  | { type: "zone"; group: string; edge: "above" | "below" };

export const DEFAULT_ISSUE_SIDEBAR_LAYOUT: IssueSidebarLayout = [
  { id: "g1", tabs: [...ISSUE_SIDEBAR_TABS], active: "overview" },
];

const TAB_IDS = new Set<string>(ISSUE_SIDEBAR_TABS);
const isTab = (value: unknown): value is IssueSidebarTab => typeof value === "string" && TAB_IDS.has(value);

/**
 * Drops empty groups, keeps every tab exactly once and every group's active
 * tab one of its own. Groups may sit empty mid-drag; anything committed or
 * read back from storage goes through here.
 */
export function normalizeIssueSidebarLayout(layout: IssueSidebarLayout): IssueSidebarLayout {
  const seenTabs = new Set<IssueSidebarTab>();
  const seenIds = new Set<string>();
  const groups: IssueSidebarLayout = [];
  for (const group of layout) {
    const tabs = group.tabs.filter((tab) => {
      if (seenTabs.has(tab)) return false;
      seenTabs.add(tab);
      return true;
    });
    if (tabs.length === 0) continue;
    const id = seenIds.has(group.id) ? nextGroupId([...seenIds]) : group.id;
    seenIds.add(id);
    groups.push({ id, tabs, active: tabs.includes(group.active) ? group.active : tabs[0]! });
  }
  if (groups.length === 0) return DEFAULT_ISSUE_SIDEBAR_LAYOUT;
  // A tab added since the layout was saved joins the first group.
  const missing = ISSUE_SIDEBAR_TABS.filter((tab) => !seenTabs.has(tab));
  if (missing.length > 0) groups[0] = { ...groups[0]!, tabs: [...groups[0]!.tabs, ...missing] };
  return groups;
}

/** Reads a stored layout, discarding anything it does not recognise. */
export function resolveIssueSidebarLayout(saved: unknown): IssueSidebarLayout {
  if (!Array.isArray(saved)) return DEFAULT_ISSUE_SIDEBAR_LAYOUT;
  const groups: IssueSidebarLayout = [];
  saved.forEach((raw, i) => {
    if (!raw || typeof raw !== "object") return;
    const { id, tabs, active } = raw as Record<string, unknown>;
    const valid = Array.isArray(tabs) ? tabs.filter(isTab) : [];
    groups.push({
      id: typeof id === "string" && id !== "" ? id : `g${i + 1}`,
      tabs: valid,
      active: isTab(active) ? active : valid[0] ?? "overview",
    });
  });
  return normalizeIssueSidebarLayout(groups);
}

export function isDefaultIssueSidebarLayout(layout: IssueSidebarLayout): boolean {
  return JSON.stringify(layout) === JSON.stringify(DEFAULT_ISSUE_SIDEBAR_LAYOUT);
}

export function groupOfTab(layout: IssueSidebarLayout, tab: IssueSidebarTab): IssueSidebarGroup | undefined {
  return layout.find((group) => group.tabs.includes(tab));
}

function nextGroupId(ids: readonly string[]): string {
  let n = 1;
  while (ids.includes(`g${n}`)) n += 1;
  return `g${n}`;
}

function withoutTab(layout: IssueSidebarLayout, tab: IssueSidebarTab): IssueSidebarLayout {
  return layout.map((group) => (group.tabs.includes(tab) ? { ...group, tabs: group.tabs.filter((t) => t !== tab) } : group));
}

/** Makes `tab` the visible tab of the group holding it. */
export function selectIssueSidebarTab(layout: IssueSidebarLayout, tab: IssueSidebarTab): IssueSidebarLayout {
  return layout.map((group) => (group.tabs.includes(tab) ? { ...group, active: tab } : group));
}

/**
 * Moves `tab` into `groupId` at `index` (clamped). Leaves an emptied group in
 * place so a drag in progress keeps its drop targets; normalize to drop it.
 */
export function moveIssueSidebarTab(
  layout: IssueSidebarLayout,
  tab: IssueSidebarTab,
  groupId: string,
  index: number,
): IssueSidebarLayout {
  if (!groupOfTab(layout, tab) || !layout.some((group) => group.id === groupId)) return layout;
  return withoutTab(layout, tab).map((group) => {
    if (group.id !== groupId) return group;
    // Within one group `index` is the slot the tab ends up in, as with arrayMove.
    const at = Math.max(0, Math.min(group.tabs.length, index));
    const tabs = [...group.tabs];
    tabs.splice(at, 0, tab);
    return { ...group, tabs };
  });
}

/** Moves `tab` into a new group directly above or below `groupId`. */
export function splitIssueSidebarTab(
  layout: IssueSidebarLayout,
  tab: IssueSidebarTab,
  groupId: string,
  edge: "above" | "below",
): IssueSidebarLayout {
  const source = groupOfTab(layout, tab);
  if (!source || !layout.some((group) => group.id === groupId)) return layout;
  if (source.id === groupId && source.tabs.length === 1) return layout;
  const created: IssueSidebarGroup = { id: nextGroupId(layout.map((group) => group.id)), tabs: [tab], active: tab };
  const next: IssueSidebarLayout = [];
  for (const group of withoutTab(layout, tab)) {
    if (group.id === groupId && edge === "above") next.push(created);
    next.push(group);
    if (group.id === groupId && edge === "below") next.push(created);
  }
  return next;
}

/** Puts every tab back into the first group, keeping the visible order. */
export function mergeIssueSidebarGroups(layout: IssueSidebarLayout): IssueSidebarLayout {
  const first = layout[0];
  if (!first || layout.length === 1) return layout;
  return [{ id: first.id, tabs: layout.flatMap((group) => group.tabs), active: first.active }];
}

/**
 * The layout after dropping `tab` on `target`, with the tab made visible
 * where it lands. Returns `layout` unchanged for a drop that moves nothing.
 */
export function dropIssueSidebarTab(
  layout: IssueSidebarLayout,
  tab: IssueSidebarTab,
  target: IssueSidebarDropTarget,
): IssueSidebarLayout {
  let next: IssueSidebarLayout;
  if (target.type === "zone") {
    next = splitIssueSidebarTab(layout, tab, target.group, target.edge);
  } else if (target.type === "strip") {
    const group = layout.find((g) => g.id === target.group);
    if (!group) return layout;
    next = group.tabs.includes(tab) ? layout : moveIssueSidebarTab(layout, tab, group.id, group.tabs.length);
  } else {
    const group = groupOfTab(layout, target.tab);
    if (!group || target.tab === tab) next = layout;
    else next = moveIssueSidebarTab(layout, tab, group.id, group.tabs.indexOf(target.tab));
  }
  if (next === layout) return layout;
  return selectIssueSidebarTab(next, tab);
}

interface IssueSidebarLayoutState {
  /** `null` while the user has not changed anything, so the default can evolve. */
  layout: IssueSidebarLayout | null;
  /**
   * The latest tab asked for through `selectTab`, as a fresh object per call.
   * Views that show fewer groups than the layout (the mobile sheet) follow
   * it, since the selection may land on a tab that was already active.
   */
  reveal: { tab: IssueSidebarTab } | null;
  setLayout: (layout: IssueSidebarLayout) => void;
  selectTab: (tab: IssueSidebarTab) => void;
  reset: () => void;
}

/** This device's issue sidebar layout, shared by every issue and workspace. */
export const useIssueSidebarLayoutStore = create<IssueSidebarLayoutState>()(
  persist(
    (set, get) => ({
      layout: null,
      reveal: null,
      setLayout: (next) => {
        const layout = normalizeIssueSidebarLayout(next);
        set({ layout: isDefaultIssueSidebarLayout(layout) ? null : layout });
      },
      selectTab: (tab) => {
        get().setLayout(selectIssueSidebarTab(get().layout ?? DEFAULT_ISSUE_SIDEBAR_LAYOUT, tab));
        set({ reveal: { tab } });
      },
      reset: () => set({ layout: null, reveal: null }),
    }),
    {
      name: "multica_issue_sidebar_layout",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ layout: state.layout }),
      merge: (persisted, current) => {
        const saved = (persisted as { layout?: unknown } | undefined)?.layout;
        if (!Array.isArray(saved)) return { ...current, layout: null };
        const layout = resolveIssueSidebarLayout(saved);
        return { ...current, layout: isDefaultIssueSidebarLayout(layout) ? null : layout };
      },
    },
  ),
);

export const selectIssueSidebarLayout = (state: IssueSidebarLayoutState): IssueSidebarLayout =>
  state.layout ?? DEFAULT_ISSUE_SIDEBAR_LAYOUT;
