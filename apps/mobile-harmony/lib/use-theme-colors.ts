import { useResolvedColorScheme } from "@/lib/use-color-scheme";

import { THEME, type ThemeColors } from "@/lib/theme";

/**
 * THEME colors for the active scheme. The scheme combines the OS appearance
 * with the user's Settings → Appearance override (lib/use-color-scheme.ts),
 * so a 'dark' preference flips every useThemeColors() consumer app-wide —
 * the harmony counterpart of NativeWind's color-scheme override on iOS.
 */
export function useThemeColors(): ThemeColors {
  const scheme = useResolvedColorScheme();
  return THEME[scheme];
}

export { THEME };
