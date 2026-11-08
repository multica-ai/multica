import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

/**
 * Appearance is two independent choices on top of light/dark mode: a style,
 * which repaints the whole UI (borders, shadows, radius), and a frame colour,
 * which tints only the app shell around the content card. Both are painted by
 * `packages/ui/styles/appearance.css`, keyed on attributes of `<html>`.
 */
export const APPEARANCE_STYLES = ["default", "pop"] as const;
export type AppearanceStyle = (typeof APPEARANCE_STYLES)[number];

export const APPEARANCE_FRAMES = ["neutral", "sand", "sage", "mist", "lilac", "rose"] as const;
export type AppearanceFrame = (typeof APPEARANCE_FRAMES)[number];

/** Pop paints its own fixed black frame, so a frame colour has no effect there. */
export function styleHasFixedFrame(style: AppearanceStyle): boolean {
  return style === "pop";
}

export const APPEARANCE_STORAGE_KEY = "multica_appearance";

export const APPEARANCE_STYLE_ATTR = "data-style";
export const APPEARANCE_FRAME_ATTR = "data-frame";

const isStyle = (v: unknown): v is AppearanceStyle =>
  (APPEARANCE_STYLES as readonly unknown[]).includes(v);
const isFrame = (v: unknown): v is AppearanceFrame =>
  (APPEARANCE_FRAMES as readonly unknown[]).includes(v);

/**
 * The `<html>` attribute values for a choice. Defaults map to `null` (remove
 * the attribute) so the base tokens apply untouched.
 */
export function appearanceAttributes(state: {
  style: AppearanceStyle;
  frame: AppearanceFrame;
}): Record<typeof APPEARANCE_STYLE_ATTR | typeof APPEARANCE_FRAME_ATTR, string | null> {
  return {
    [APPEARANCE_STYLE_ATTR]: state.style === "default" ? null : state.style,
    [APPEARANCE_FRAME_ATTR]: state.frame === "neutral" ? null : state.frame,
  };
}

interface AppearanceState {
  style: AppearanceStyle;
  frame: AppearanceFrame;
  setStyle: (style: AppearanceStyle) => void;
  setFrame: (frame: AppearanceFrame) => void;
}

// Saved on this device, like the light/dark theme next to it in Preferences.
export const useAppearanceStore = create<AppearanceState>()(
  persist(
    (set) => ({
      style: "default",
      frame: "neutral",
      setStyle: (style) => set({ style }),
      setFrame: (frame) => set({ frame }),
    }),
    {
      name: APPEARANCE_STORAGE_KEY,
      storage: createJSONStorage(() => defaultStorage),
      partialize: ({ style, frame }) => ({ style, frame }),
      merge: (persisted, current) => {
        const p = (persisted && typeof persisted === "object" ? persisted : {}) as Record<string, unknown>;
        return {
          ...current,
          style: isStyle(p.style) ? p.style : "default",
          frame: isFrame(p.frame) ? p.frame : "neutral",
        };
      },
    },
  ),
);
