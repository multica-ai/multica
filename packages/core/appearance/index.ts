import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

/**
 * Appearance is a style on top of light/dark mode. A style repaints the whole
 * UI (borders, shadows, radius); `packages/ui/styles/appearance.css` paints it,
 * keyed on an attribute of `<html>`.
 */
export const APPEARANCE_STYLES = ["default", "pop"] as const;
export type AppearanceStyle = (typeof APPEARANCE_STYLES)[number];

export const APPEARANCE_STORAGE_KEY = "multica_appearance";

export const APPEARANCE_STYLE_ATTR = "data-style";

const isStyle = (v: unknown): v is AppearanceStyle =>
  (APPEARANCE_STYLES as readonly unknown[]).includes(v);

/**
 * The `<html>` attribute value for a style. The default maps to `null`
 * (remove the attribute) so the base tokens apply untouched.
 */
export function appearanceStyleAttribute(style: AppearanceStyle): string | null {
  return style === "default" ? null : style;
}

interface AppearanceState {
  style: AppearanceStyle;
  setStyle: (style: AppearanceStyle) => void;
}

// Saved on this device, like the light/dark theme next to it.
export const useAppearanceStore = create<AppearanceState>()(
  persist(
    (set) => ({
      style: "default",
      setStyle: (style) => set({ style }),
    }),
    {
      name: APPEARANCE_STORAGE_KEY,
      storage: createJSONStorage(() => defaultStorage),
      partialize: ({ style }) => ({ style }),
      merge: (persisted, current) => {
        const p = (persisted && typeof persisted === "object" ? persisted : {}) as Record<string, unknown>;
        return { ...current, style: isStyle(p.style) ? p.style : "default" };
      },
    },
  ),
);
