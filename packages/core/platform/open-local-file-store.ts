import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "./storage";

/**
 * Device-local default app for opening a file from a run Diff.
 *
 * Stored on this machine only (like theme), never synced. `openerId` is a
 * known-app id from the desktop allowlist; `null` means "pick the first
 * installed of Cursor → VS Code → file manager".
 */
interface OpenLocalFileStore {
  openerId: string | null;
  setOpenerId: (openerId: string) => void;
}

export const useOpenLocalFileStore = create<OpenLocalFileStore>()(
  persist(
    (set) => ({
      openerId: null,
      setOpenerId: (openerId) => set({ openerId }),
    }),
    {
      name: "multica_open_local_file",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
