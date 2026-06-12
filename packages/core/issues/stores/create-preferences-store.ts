"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

export type IssueDuplicatePolicy = "confirm" | "allow";

interface IssueCreatePreferencesState {
  duplicatePolicy: IssueDuplicatePolicy;
  setDuplicatePolicy: (policy: IssueDuplicatePolicy) => void;
}

export const useIssueCreatePreferencesStore = create<IssueCreatePreferencesState>()(
  persist(
    (set) => ({
      duplicatePolicy: "confirm",
      setDuplicatePolicy: (policy) => set({ duplicatePolicy: policy }),
    }),
    {
      name: "multica_issue_create_preferences",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
