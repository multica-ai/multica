"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * Whether run surfaces show how full the model's context window is.
 *
 * Off by default: the figure answers a question most readers never ask, and
 * most runtimes report nothing anyway, so it earns its place only for someone
 * who turns it on.
 *
 * `showOnIssueRow` is the second level, and only means anything while
 * `enabled` is true: an issue shows the figure both on the run's row in the
 * timeline (outer) and inside the run (the transcript dialog, the expanded
 * step list). Someone who wants it only when they open a run turns the outer
 * one off without losing the rest.
 */
export interface ContextUsageSettings {
  enabled: boolean;
  showOnIssueRow: boolean;
}

export const DEFAULT_CONTEXT_USAGE_SETTINGS: ContextUsageSettings = {
  enabled: false,
  showOnIssueRow: true,
};

interface ContextUsageSettingsState extends ContextUsageSettings {
  set: (patch: Partial<ContextUsageSettings>) => void;
}

export const useContextUsageSettingsStore = create<ContextUsageSettingsState>()(
  persist(
    (set) => ({
      ...DEFAULT_CONTEXT_USAGE_SETTINGS,
      set: (patch) => set(patch),
    }),
    {
      name: "multica_context_usage",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({
        enabled: state.enabled,
        showOnIssueRow: state.showOnIssueRow,
      }),
      merge: (persisted, current) => {
        const saved = (persisted ?? {}) as Partial<ContextUsageSettings>;
        return {
          ...current,
          enabled:
            typeof saved.enabled === "boolean"
              ? saved.enabled
              : DEFAULT_CONTEXT_USAGE_SETTINGS.enabled,
          showOnIssueRow:
            typeof saved.showOnIssueRow === "boolean"
              ? saved.showOnIssueRow
              : DEFAULT_CONTEXT_USAGE_SETTINGS.showOnIssueRow,
        };
      },
    },
  ),
);
