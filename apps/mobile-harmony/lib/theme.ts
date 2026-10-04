/**
 * HarmonyOS port of the iOS app's theme tokens (apps/mobile/lib/theme.ts,
 * which mirrors apps/mobile/global.css). Keep the values in sync.
 *
 * `THEME` feeds explicit StyleSheet styles — the slice does not use
 * NativeWind className styling (see README roadmap for why).
 */
export type ThemeColors = (typeof THEME)["light"];

/**
 * Apply an alpha channel to a THEME color. THEME tokens use the modern
 * space-separated `hsl(H S% L%)` syntax and are opaque, so components that
 * need a translucent theme shade (the tailwind `bg-x/nn` forms: bg-brand/15,
 * bg-destructive/10, text-muted-foreground/40, …) convert through this
 * helper — React Native has no color-mix/alpha modifier at style time.
 * Non-hsl inputs (e.g. #rrggbb from server data) are returned unchanged.
 */
export function withAlpha(color: string, alpha: number): string {
  // Accepts both "hsl(H S% L%)" (token source of truth) and the normalized
  // comma form produced by normalizeHsl below.
  const match =
    /^hsl\(\s*([\d.]+)(?:deg)?[\s,]+([\d.]+)%[\s,]+([\d.]+)%\s*\)$/.exec(
      color.trim(),
    );
  if (!match) return color;
  return `hsla(${match[1]}, ${match[2]}%, ${match[3]}%, ${alpha})`;
}

/**
 * RNOH's color pipeline silently DROPS the modern space-separated
 * "hsl(H S% L%)" syntax (renders as transparent — surfaces and tinted text
 * vanish), so every token is normalized to the comma form the platform
 * accepts. Applied once over THEME below; keep token literals in the
 * global.css syntax for diffability.
 */
function normalizeHsl(color: string): string {
  const match =
    /^hsl\(\s*([\d.]+)(deg)?\s+([\d.]+)%\s+([\d.]+)%\s*\)$/.exec(
      color.trim(),
    );
  if (!match) return color;
  return `hsl(${match[1]}, ${match[3]}%, ${match[4]}%)`;
}
const RAW_THEME = {
  light: {
    background: "hsl(0 0% 100%)",
    foreground: "hsl(0 0% 3.9%)",
    card: "hsl(0 0% 100%)",
    cardForeground: "hsl(0 0% 3.9%)",
    popover: "hsl(0 0% 100%)",
    popoverForeground: "hsl(0 0% 3.9%)",
    primary: "hsl(0 0% 9%)",
    primaryForeground: "hsl(0 0% 98%)",
    secondary: "hsl(0 0% 96.1%)",
    secondaryForeground: "hsl(0 0% 9%)",
    muted: "hsl(0 0% 96.1%)",
    mutedForeground: "hsl(0 0% 45.1%)",
    accent: "hsl(0 0% 96.1%)",
    accentForeground: "hsl(0 0% 9%)",
    destructive: "hsl(0 84.2% 60.2%)",
    destructiveForeground: "hsl(0 0% 98%)",
    border: "hsl(0 0% 84%)",
    input: "hsl(0 0% 84%)",
    ring: "hsl(0 0% 63%)",
    radius: "0.625rem",
    chart1: "hsl(12 76% 61%)",
    chart2: "hsl(173 58% 39%)",
    chart3: "hsl(197 37% 24%)",
    chart4: "hsl(43 74% 66%)",
    chart5: "hsl(27 87% 67%)",

    // Multica custom
    brand: "hsl(225 71% 58%)",
    brandForeground: "hsl(0 0% 98%)",
    success: "hsl(142 71% 45%)",
    warning: "hsl(48 89% 47%)",
    info: "hsl(217 91% 60%)",
    priority: "hsl(25 95% 53%)",
    codeSurface: "hsl(240 4% 92%)",
    // Surface elevation tiers — see global.css for the full scale.
    surface1: "hsl(0 0% 98%)",
    surface2: "hsl(0 0% 90%)",
  },
  dark: {
    background: "hsl(0 0% 3.9%)",
    foreground: "hsl(0 0% 98%)",
    card: "hsl(0 0% 3.9%)",
    cardForeground: "hsl(0 0% 98%)",
    popover: "hsl(0 0% 3.9%)",
    popoverForeground: "hsl(0 0% 98%)",
    primary: "hsl(0 0% 98%)",
    primaryForeground: "hsl(0 0% 9%)",
    secondary: "hsl(0 0% 14.9%)",
    secondaryForeground: "hsl(0 0% 98%)",
    muted: "hsl(0 0% 14.9%)",
    mutedForeground: "hsl(0 0% 63.9%)",
    accent: "hsl(0 0% 14.9%)",
    accentForeground: "hsl(0 0% 98%)",
    destructive: "hsl(0 70.9% 59.4%)",
    destructiveForeground: "hsl(0 0% 98%)",
    border: "hsl(0 0% 25%)",
    input: "hsl(0 0% 25%)",
    ring: "hsl(300 0% 45%)",
    radius: "0.625rem",
    chart1: "hsl(220 70% 50%)",
    chart2: "hsl(160 60% 45%)",
    chart3: "hsl(30 80% 55%)",
    chart4: "hsl(280 65% 60%)",
    chart5: "hsl(340 75% 55%)",

    // Multica custom — dark mirrors light until demand
    brand: "hsl(225 71% 58%)",
    brandForeground: "hsl(0 0% 98%)",
    success: "hsl(142 71% 45%)",
    warning: "hsl(48 89% 47%)",
    info: "hsl(217 91% 60%)",
    priority: "hsl(25 95% 53%)",
    // code-surface is the ONE exception that needs a real dark value —
    // see global.css for rationale. Keep this in sync with .dark:root.
    codeSurface: "hsl(240 4% 18%)",
    // Dark elevation tiers — lightness INCREASES with elevation. See global.css.
    surface1: "hsl(0 0% 8%)",
    surface2: "hsl(0 0% 19%)",
  },
};

export const THEME = Object.fromEntries(
  Object.entries(RAW_THEME).map(([mode, tokens]) => [
    mode,
    Object.fromEntries(
      Object.entries(tokens).map(([token, value]) => [token, normalizeHsl(value)]),
    ),
  ]),
) as typeof RAW_THEME;


