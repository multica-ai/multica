"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * The issue sidebar's cards, top to bottom, and the sections each one holds.
 * The last group is not a card: it is the small print under them.
 */
export const ISSUE_SIDEBAR_GROUPS = [
  ["properties"],
  ["pull_requests", "deliverables", "environments"],
  ["parent", "sub_issues", "duplicates"],
  ["execution_log"],
  ["quick_actions", "wakeups", "plugins", "apps"],
  ["details", "metadata"],
] as const;

export type IssueSidebarSection = (typeof ISSUE_SIDEBAR_GROUPS)[number][number];

/** Every section, in the order the sidebar shows them. */
export const ISSUE_SIDEBAR_SECTIONS: readonly IssueSidebarSection[] = ISSUE_SIDEBAR_GROUPS.flat();

/** Sections every issue shows. */
export const ISSUE_SIDEBAR_ALWAYS_SHOWN: readonly IssueSidebarSection[] = ["properties"];

const SECTION_IDS = new Set<string>(ISSUE_SIDEBAR_SECTIONS);
const isSection = (id: unknown): id is IssueSidebarSection => typeof id === "string" && SECTION_IDS.has(id);

/** Known sections the user hid, without repeats or the always-shown ones. */
export function resolveIssueSidebarHidden(saved: readonly unknown[] | null): IssueSidebarSection[] {
  if (!saved) return [];
  return [...new Set(saved)].filter(
    (id): id is IssueSidebarSection => isSection(id) && !ISSUE_SIDEBAR_ALWAYS_SHOWN.includes(id),
  );
}

interface IssueSidebarSectionsState {
  hidden: IssueSidebarSection[];
  setHidden: (section: IssueSidebarSection, hidden: boolean) => void;
  reset: () => void;
}

/**
 * This device's hidden issue sidebar sections, shared by every issue and
 * workspace. Saves from before the sidebar became cards also carry an
 * `order`; the cards fix the order, so it is dropped on read.
 */
export const useIssueSidebarSectionsStore = create<IssueSidebarSectionsState>()(
  persist(
    (set, get) => ({
      hidden: [],
      setHidden: (section, hidden) => {
        const current = get().hidden.filter((id) => id !== section);
        set({ hidden: resolveIssueSidebarHidden(hidden ? [...current, section] : current) });
      },
      reset: () => set({ hidden: [] }),
    }),
    {
      name: "multica_issue_sidebar_sections",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ hidden: state.hidden }),
      merge: (persisted, current) => {
        const saved = persisted as { hidden?: unknown } | undefined;
        return {
          ...current,
          hidden: Array.isArray(saved?.hidden) ? resolveIssueSidebarHidden(saved.hidden) : [],
        };
      },
    },
  ),
);

export const selectIssueSidebarCustomized = (state: IssueSidebarSectionsState) => state.hidden.length > 0;
