import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import * as SecureStore from "expo-secure-store";

export interface SideLayout { tab: "files" | "web" | "side"; url?: string; sideId?: string }
interface AIViewPreferences {
  layouts: Record<string, SideLayout>;
  setLayout(key: string, patch: Partial<SideLayout>): void;
}

export const useAIViewPreferences = create(persist<AIViewPreferences, [], [], Pick<AIViewPreferences, "layouts">>((set) => ({
  layouts: {},
  setLayout: (key, patch) => set((state) => ({ layouts: { ...state.layouts, [key]: { ...(state.layouts[key] ?? { tab: "files" }), ...patch } } })),
}), {
  name: "mobile.ai-session-preferences",
  partialize: (state) => ({ layouts: state.layouts }),
  merge: (persisted, current) => ({ ...current, layouts: (persisted as Partial<AIViewPreferences> | undefined)?.layouts ?? {} }),
  storage: createJSONStorage(() => ({ getItem: SecureStore.getItemAsync, setItem: (key, value) => SecureStore.setItemAsync(key, value), removeItem: SecureStore.deleteItemAsync })),
}));
