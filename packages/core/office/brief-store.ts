import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

interface OfficeBriefState {
  /** The newest brief the viewer opened in this workspace; null until one is opened. */
  lastSeenBriefId: string | null;
  markBriefSeen: (briefId: string) => void;
}

/**
 * The last brief the viewer opened, remembered per workspace as a client
 * preference. It only drives the unread dot; the briefs themselves come from
 * the server.
 */
export const useOfficeBriefStore = create<OfficeBriefState>()(
  persist(
    (set) => ({
      lastSeenBriefId: null,
      markBriefSeen: (briefId) => set({ lastSeenBriefId: briefId }),
    }),
    {
      name: "multica_office_brief",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useOfficeBriefStore.persist.rehydrate());
