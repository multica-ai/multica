import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

interface CompanyFloorState {
  /** The floor the company was last showing in this workspace; null until one is picked. */
  lastFloorId: string | null;
  setLastFloorId: (floorId: string | null) => void;
}

/**
 * The last company floor viewed, remembered per workspace as a client
 * preference. It is only a pointer: a floor that no longer exists is ignored
 * by the view, which falls back to the first floor.
 */
export const useCompanyFloorStore = create<CompanyFloorState>()(
  persist(
    (set) => ({
      lastFloorId: null,
      setLastFloorId: (floorId) => set({ lastFloorId: floorId }),
    }),
    {
      name: "multica_company_floor",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useCompanyFloorStore.persist.rehydrate());
