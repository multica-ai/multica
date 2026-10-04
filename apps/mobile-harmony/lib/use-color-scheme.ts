/**
 * HarmonyOS port of apps/mobile/lib/use-color-scheme.ts — appearance
 * preference with persistence. The iOS version persists to expo-secure-store
 * and applies through NativeWind's setColorScheme; on this matrix NativeWind
 * is not wired, so the "apply" side is a tiny module-level override store
 * that `lib/use-theme-colors.ts` consults (and every useThemeColors()
 * consumer flips with it). Persistence goes through @/lib/device-storage,
 * the platform stand-in for expo-secure-store.
 *
 * - `colorScheme` — the resolved scheme ('light' | 'dark'). Tracks either the
 *   saved preference or the OS appearance when preference is 'system'.
 * - `preference` — what the user explicitly picked ('light' | 'dark' | 'system').
 *   'system' is the default for a fresh install.
 * - `setPreference(p)` — switches the scheme and persists in one step.
 *
 * On first mount we async-read the saved preference; before the read
 * completes the OS appearance applies. This means a kill-and-relaunch of a
 * user who picked 'dark' on a light OS may briefly flash light before the
 * saved preference applies — same accepted trade-off as the iOS hook.
 */
import { useEffect, useSyncExternalStore } from "react";
import { useColorScheme as useRNColorScheme } from "react-native";
import { getItemAsync, setItemAsync } from "@/lib/device-storage";

const STORAGE_KEY = "theme-preference";

export type ThemePreference = "light" | "dark" | "system";

export type ResolvedColorScheme = "light" | "dark";

let preference: ThemePreference = "system";
let hydrating: Promise<void> | null = null;
const listeners = new Set<() => void>();

function emit() {
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function getSnapshot(): ThemePreference {
  return preference;
}

/** Read the saved preference once per JS session. Multiple hook consumers
 *  share the single in-flight read; failures keep the 'system' default. */
function hydrate(): Promise<void> {
  if (!hydrating) {
    hydrating = getItemAsync(STORAGE_KEY)
      .then((saved) => {
        if (saved === "light" || saved === "dark" || saved === "system") {
          preference = saved;
          emit();
        }
      })
      .catch(() => {
        // Read failures are non-fatal; keep default 'system'.
      });
  }
  return hydrating;
}

/** Switch the scheme and persist — mirrors the iOS hook's setPreference. */
export function setThemePreference(p: ThemePreference) {
  preference = p;
  emit();
  void setItemAsync(STORAGE_KEY, p);
}

/** Resolves a preference against the OS scheme reported by RN. */
export function resolveColorScheme(
  pref: ThemePreference,
  osScheme: "light" | "dark" | "unspecified" | null | undefined,
): ResolvedColorScheme {
  if (pref === "system") return osScheme === "dark" ? "dark" : "light";
  return pref;
}

/**
 * The iOS hook's call shape (preference / setPreference / colorScheme /
 * isDarkColorScheme), so ported screens keep their structure.
 */
export function useColorScheme() {
  const osScheme = useRNColorScheme();
  const pref = useSyncExternalStore(subscribe, getSnapshot);

  useEffect(() => {
    void hydrate();
  }, []);

  const colorScheme = resolveColorScheme(pref, osScheme);
  return {
    colorScheme,
    preference: pref,
    setPreference: setThemePreference,
    isDarkColorScheme: colorScheme === "dark",
  };
}

/**
 * Just the resolved scheme, for components that only need which THEME table
 * to use (lib/use-theme-colors.ts). Subscribes to the same store and
 * triggers the shared one-time hydration.
 */
export function useResolvedColorScheme(): ResolvedColorScheme {
  const osScheme = useRNColorScheme();
  const pref = useSyncExternalStore(subscribe, getSnapshot);

  useEffect(() => {
    void hydrate();
  }, []);

  return resolveColorScheme(pref, osScheme);
}
