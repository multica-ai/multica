"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type QuickCreateField = "project" | "priority" | "due_date";
export type ManualCreateField =
  | "status"
  | "priority"
  | "assignee"
  | "labels"
  | "project"
  | "due_date"
  | "start_date";

// Canonical field order — the settings tab renders rows in this order and
// setters normalize persisted arrays against it, so a toggle sequence never
// produces two different persisted encodings of the same selection.
export const QUICK_CREATE_FIELDS: QuickCreateField[] = ["project", "priority", "due_date"];
export const MANUAL_CREATE_FIELDS: ManualCreateField[] = [
  "status",
  "priority",
  "assignee",
  "labels",
  "project",
  "due_date",
  "start_date",
];

export const DEFAULT_QUICK_CREATE_FIELDS: QuickCreateField[] = ["project"];
// Mirrors the manual dialog's historical toolbar: these five always rendered,
// while due/start date lived behind the ⋯ overflow.
export const DEFAULT_MANUAL_CREATE_FIELDS: ManualCreateField[] = [
  "status",
  "priority",
  "assignee",
  "labels",
  "project",
];

// Which optional fields each create-issue mode keeps on its toolbar. Owned by
// Settings → Issue and read by both create dialogs; a field toggled off here
// stays reachable from the dialog's ⋯ overflow and always re-surfaces while it
// holds a value. Per-workspace via the workspace-aware storage (projects and
// custom properties differ per workspace), per-user for free from
// localStorage being browser-profile-local — same scoping as quick-create's
// actor/project memory.
interface IssueCreateSettingsState {
  quickCreateFields: QuickCreateField[];
  setQuickCreateFieldVisible: (field: QuickCreateField, visible: boolean) => void;
  manualCreateFields: ManualCreateField[];
  setManualCreateFieldVisible: (field: ManualCreateField, visible: boolean) => void;
}

function toggle<F extends string>(all: F[], current: F[], field: F, visible: boolean): F[] {
  return all.filter((f) => (f === field ? visible : current.includes(f)));
}

export const useIssueCreateSettingsStore = create<IssueCreateSettingsState>()(
  persist(
    (set) => ({
      quickCreateFields: DEFAULT_QUICK_CREATE_FIELDS,
      setQuickCreateFieldVisible: (field, visible) =>
        set((s) => ({
          quickCreateFields: toggle(QUICK_CREATE_FIELDS, s.quickCreateFields, field, visible),
        })),
      manualCreateFields: DEFAULT_MANUAL_CREATE_FIELDS,
      setManualCreateFieldVisible: (field, visible) =>
        set((s) => ({
          manualCreateFields: toggle(MANUAL_CREATE_FIELDS, s.manualCreateFields, field, visible),
        })),
    }),
    {
      name: "multica_issue_create_settings",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      merge: (persistedState, currentState) => {
        const persisted = (persistedState ?? {}) as Partial<IssueCreateSettingsState>;
        return {
          ...currentState,
          ...persisted,
          quickCreateFields: persisted.quickCreateFields ?? DEFAULT_QUICK_CREATE_FIELDS,
          manualCreateFields: persisted.manualCreateFields ?? DEFAULT_MANUAL_CREATE_FIELDS,
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useIssueCreateSettingsStore.persist.rehydrate());
