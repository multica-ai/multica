/**
 * Wraps NativeWind's useColorScheme with persistence in expo-secure-store.
 *
 * RNR's template uses NativeWind's hook directly (no persistence) — we extend
 * it so the user's Settings → Appearance choice survives app restarts.
 *
 * - `colorScheme` — the resolved scheme ('light' | 'dark'). Tracks either the
 *   saved preference or the OS appearance when preference is 'system'.
 * - `preference` — what the user explicitly picked ('light' | 'dark' | 'system').
 *   'system' is the default for a fresh install.
 * - `setPreference(p)` — switches the scheme and persists in one step.
 *
 * On first mount we async-read the saved preference; before the read
 * completes NativeWind's default behaviour applies (follow OS). This means
 * a kill-and-relaunch on a user who picked 'dark' on a light OS may briefly
 * flash light before the saved preference applies. Acceptable for now —
 * the alternative is a synchronous storage backend, which secure-store isn't.
 */
import { useColorScheme as useNativewindColorScheme } from "nativewind";
import { useEffect, useState } from "react";
import * as SecureStore from "expo-secure-store";

const STORAGE_KEY = "theme-preference";

export type ThemePreference = "light" | "dark" | "system";

export function useColorScheme() {
  const { colorScheme, setColorScheme: applyScheme } =
    useNativewindColorScheme();
  const [preference, setPreferenceState] = useState<ThemePreference>("system");

  useEffect(() => {
    let cancelled = false;
    SecureStore.getItemAsync(STORAGE_KEY)
      .then((saved) => {
        if (cancelled) return;
        if (saved === "light" || saved === "dark" || saved === "system") {
          setPreferenceState(saved);
          applyScheme(saved);
        }
      })
      .catch(() => {
        // Read failures are non-fatal; keep default 'system'.
      });
    return () => {
      cancelled = true;
    };
  }, [applyScheme]);

  const setPreference = (p: ThemePreference) => {
    setPreferenceState(p);
    applyScheme(p);
    void SecureStore.setItemAsync(STORAGE_KEY, p);
  };

  return {
    colorScheme: colorScheme ?? "light",
    preference,
    setPreference,
    isDarkColorScheme: colorScheme === "dark",
  };
}
