import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

export const MOBILE_FONT_SIZES = ["small", "default", "large"] as const;
export const MOBILE_CONTENT_WIDTHS = ["standard", "full"] as const;

export type MobileFontSize = (typeof MOBILE_FONT_SIZES)[number];
export type MobileContentWidth = (typeof MOBILE_CONTENT_WIDTHS)[number];

export const DEFAULT_MOBILE_FONT_SIZE: MobileFontSize = "default";
export const DEFAULT_MOBILE_CONTENT_WIDTH: MobileContentWidth = "standard";

export function parseMobileFontSize(value: unknown): MobileFontSize {
  return (MOBILE_FONT_SIZES as readonly unknown[]).includes(value)
    ? (value as MobileFontSize)
    : DEFAULT_MOBILE_FONT_SIZE;
}

export function parseMobileContentWidth(value: unknown): MobileContentWidth {
  return (MOBILE_CONTENT_WIDTHS as readonly unknown[]).includes(value)
    ? (value as MobileContentWidth)
    : DEFAULT_MOBILE_CONTENT_WIDTH;
}

interface MobileAppearanceState {
  fontSize: MobileFontSize;
  contentWidth: MobileContentWidth;
  setFontSize: (fontSize: MobileFontSize) => void;
  setContentWidth: (contentWidth: MobileContentWidth) => void;
}

// Device-local, like theme and the other personal preferences: saved in this
// browser/PWA across workspaces and never sent to the backend. Both values
// only take effect below the `sm` breakpoint (see tokens.css).
export const useMobileAppearanceStore = create<MobileAppearanceState>()(
  persist(
    (set) => ({
      fontSize: DEFAULT_MOBILE_FONT_SIZE,
      contentWidth: DEFAULT_MOBILE_CONTENT_WIDTH,
      setFontSize: (fontSize) => set({ fontSize: parseMobileFontSize(fontSize) }),
      setContentWidth: (contentWidth) =>
        set({ contentWidth: parseMobileContentWidth(contentWidth) }),
    }),
    {
      name: "multica_mobile_appearance",
      storage: createJSONStorage(() => defaultStorage),
      partialize: ({ fontSize, contentWidth }) => ({ fontSize, contentWidth }),
      merge: (persisted, current) => {
        const saved =
          persisted && typeof persisted === "object"
            ? (persisted as Record<string, unknown>)
            : {};
        return {
          ...current,
          fontSize: parseMobileFontSize(saved.fontSize),
          contentWidth: parseMobileContentWidth(saved.contentWidth),
        };
      },
    },
  ),
);
