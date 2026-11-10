import { create } from "zustand";
import { DEFAULT_THEME_ID } from "./built-in-themes";

/**
 * Safe mode ignores the chosen theme for the rest of the browser session, so
 * a theme that hides or breaks the interface can always be undone. Entered
 * with `?theme=default` in the URL or by holding Shift while the app loads.
 */

const SAFE_MODE_KEY = "multica_theme_safe_mode";

function readSafeMode(): boolean {
  if (typeof window === "undefined") return false;
  try {
    if (new URLSearchParams(window.location.search).get("theme") === DEFAULT_THEME_ID) {
      window.sessionStorage.setItem(SAFE_MODE_KEY, "1");
    }
    return window.sessionStorage.getItem(SAFE_MODE_KEY) === "1";
  } catch {
    return false;
  }
}

interface SafeModeState {
  active: boolean;
  enter: () => void;
  exit: () => void;
}

export const useThemeSafeMode = create<SafeModeState>()((set) => ({
  active: readSafeMode(),
  enter: () => {
    try {
      window.sessionStorage.setItem(SAFE_MODE_KEY, "1");
    } catch {
      // Session storage can be unavailable; safe mode then lasts until reload.
    }
    set({ active: true });
  },
  exit: () => {
    try {
      window.sessionStorage.removeItem(SAFE_MODE_KEY);
    } catch {
      // Nothing stored to clear.
    }
    set({ active: false });
  },
}));
