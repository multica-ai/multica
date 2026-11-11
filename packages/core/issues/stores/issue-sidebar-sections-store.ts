"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/** The issue sidebar's sections, in their default order. */
export const ISSUE_SIDEBAR_SECTIONS = [
  "properties",
  "quick_actions",
  "wakeups",
  "plugins",
  "parent",
  "duplicates",
  "pull_requests",
  "environments",
  "deliverables",
  "apps",
  "execution_log",
  "details",
  "metadata",
] as const;

export type IssueSidebarSection = (typeof ISSUE_SIDEBAR_SECTIONS)[number];

/** Sections every issue shows. They can move, but not be hidden. */
export const ISSUE_SIDEBAR_ALWAYS_SHOWN: readonly IssueSidebarSection[] = ["properties"];

const SECTION_IDS = new Set<string>(ISSUE_SIDEBAR_SECTIONS);
const isSection = (id: unknown): id is IssueSidebarSection => typeof id === "string" && SECTION_IDS.has(id);

/**
 * The sections in the user's order. Unknown and repeated ids are dropped; a
 * section missing from the saved order (one added since it was saved) is
 * placed right after the section it follows by default.
 */
export function resolveIssueSidebarOrder(saved: readonly unknown[] | null): IssueSidebarSection[] {
  if (!saved) return [...ISSUE_SIDEBAR_SECTIONS];
  const order = [...new Set(saved)].filter(isSection);
  ISSUE_SIDEBAR_SECTIONS.forEach((id, i) => {
    if (order.includes(id)) return;
    const previous = i === 0 ? -1 : order.indexOf(ISSUE_SIDEBAR_SECTIONS[i - 1]!);
    order.splice(previous + 1, 0, id);
  });
  return order;
}

/** Known sections the user hid, without repeats or the always-shown ones. */
export function resolveIssueSidebarHidden(saved: readonly unknown[] | null): IssueSidebarSection[] {
  if (!saved) return [];
  return [...new Set(saved)].filter(
    (id): id is IssueSidebarSection => isSection(id) && !ISSUE_SIDEBAR_ALWAYS_SHOWN.includes(id),
  );
}

interface IssueSidebarSectionsState {
  /** `null` until the user reorders, so the default order can still evolve. */
  order: IssueSidebarSection[] | null;
  hidden: IssueSidebarSection[];
  setOrder: (order: readonly IssueSidebarSection[]) => void;
  setHidden: (section: IssueSidebarSection, hidden: boolean) => void;
  reset: () => void;
}

/** This device's issue sidebar sections, shared by every issue and workspace. */
export const useIssueSidebarSectionsStore = create<IssueSidebarSectionsState>()(
  persist(
    (set, get) => ({
      order: null,
      hidden: [],
      setOrder: (next) => {
        const order = resolveIssueSidebarOrder(next);
        const isDefault = order.every((id, i) => id === ISSUE_SIDEBAR_SECTIONS[i]);
        set({ order: isDefault ? null : order });
      },
      setHidden: (section, hidden) => {
        const current = get().hidden.filter((id) => id !== section);
        set({ hidden: resolveIssueSidebarHidden(hidden ? [...current, section] : current) });
      },
      reset: () => set({ order: null, hidden: [] }),
    }),
    {
      name: "multica_issue_sidebar_sections",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ order: state.order, hidden: state.hidden }),
      merge: (persisted, current) => {
        const saved = persisted as { order?: unknown; hidden?: unknown } | undefined;
        const order = Array.isArray(saved?.order) ? resolveIssueSidebarOrder(saved.order) : null;
        const isDefault = order?.every((id, i) => id === ISSUE_SIDEBAR_SECTIONS[i]) ?? true;
        return {
          ...current,
          order: isDefault ? null : order,
          hidden: Array.isArray(saved?.hidden) ? resolveIssueSidebarHidden(saved.hidden) : [],
        };
      },
    },
  ),
);

export const selectIssueSidebarOrder = (state: IssueSidebarSectionsState): readonly IssueSidebarSection[] =>
  state.order ?? ISSUE_SIDEBAR_SECTIONS;

export const selectIssueSidebarCustomized = (state: IssueSidebarSectionsState) =>
  state.order !== null || state.hidden.length > 0;
