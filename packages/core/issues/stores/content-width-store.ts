import { useEffect } from "react";
import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";
import type { StorageAdapter } from "../../types/storage";

export type ContentWidth = "standard" | "full";

interface ContentWidthState {
  contentWidth: ContentWidth;
  setContentWidth: (contentWidth: ContentWidth) => void;
}

const contentWidthStorage: StorageAdapter = {
  ...defaultStorage,
  getItem: (key) => {
    const value = defaultStorage.getItem(key);
    if (value === null) return null;
    try {
      JSON.parse(value);
      return value;
    } catch {
      return null;
    }
  },
};

export const useContentWidthStore = create<ContentWidthState>()(
  persist(
    (set) => ({
      contentWidth: "standard",
      setContentWidth: (contentWidth) => set({ contentWidth }),
    }),
    {
      name: "multica_content_width",
      storage: createJSONStorage(() => contentWidthStorage),
      skipHydration: true,
      partialize: ({ contentWidth }) => ({ contentWidth }),
      merge: (persisted, current) => ({
        ...current,
        contentWidth:
          persisted && typeof persisted === "object" &&
          "contentWidth" in persisted && persisted.contentWidth === "full"
            ? "full"
            : "standard",
      }),
    },
  ),
);

let hydrationStarted = false;

/** Hydrate after the first client render so SSR and the initial client frame agree. */
export function useContentWidth(): ContentWidth {
  const contentWidth = useContentWidthStore((state) => state.contentWidth);
  useEffect(() => {
    if (hydrationStarted) return;
    hydrationStarted = true;
    void useContentWidthStore.persist.rehydrate();
  }, []);
  return contentWidth;
}
