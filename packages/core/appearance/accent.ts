import { contrastRatio, parseColor, resolveColor } from "./theme-contrast";
import {
  themeModeVariables,
  themeOptionVariables,
  type Theme,
  type ThemeMode,
  type ThemeOptionValues,
  type ThemedColor,
} from "./themes";

/**
 * An accent replaces the theme's `--brand` for one workspace, so its windows
 * can be told apart at a glance. The label on brand fills follows it.
 */

/** Hex per mode, light ones dark enough for links on white and the reverse. */
export const ACCENT_PRESETS = [
  { id: "blue", color: { light: "#2563eb", dark: "#93c5fd" } },
  { id: "violet", color: { light: "#7c3aed", dark: "#c4b5fd" } },
  { id: "pink", color: { light: "#be185d", dark: "#f9a8d4" } },
  { id: "red", color: { light: "#b91c1c", dark: "#fca5a5" } },
  { id: "orange", color: { light: "#c2410c", dark: "#fdba74" } },
  { id: "green", color: { light: "#15803d", dark: "#4ade80" } },
  { id: "teal", color: { light: "#0f766e", dark: "#2dd4bf" } },
  { id: "slate", color: { light: "#475569", dark: "#cbd5e1" } },
] as const satisfies readonly { id: string; color: ThemedColor }[];

export type AccentPresetId = (typeof ACCENT_PRESETS)[number]["id"];

/** The app's `--brand-foreground` and its dark ink, as tokens.css sets them. */
const LIGHT_LABEL = "oklch(0.985 0 0)";
const DARK_LABEL = "oklch(0.21 0.006 285.885)";
const AA_TEXT = 4.5;

/** The label for a brand fill: light while it reads at AA, else whichever reads better. */
export function accentForeground(color: string): string {
  const fill = parseColor(color);
  if (!fill) return LIGHT_LABEL;
  const light = contrastRatio(parseColor(LIGHT_LABEL)!, fill);
  if (light >= AA_TEXT) return LIGHT_LABEL;
  return contrastRatio(parseColor(DARK_LABEL)!, fill) > light ? DARK_LABEL : LIGHT_LABEL;
}

/** The variables an accent sets, per mode. */
export function accentVariables(accent: ThemedColor): Record<ThemeMode, Record<string, string>> {
  const forMode = (color: string) => ({ "--brand": color, "--brand-foreground": accentForeground(color) });
  return { light: forMode(accent.light), dark: forMode(accent.dark) };
}

/** The surfaces links are read on, as packages/ui/styles/tokens.css declares them. */
export const READING_SURFACE_DEFAULTS: Record<ThemeMode, Readonly<Record<string, string>>> = {
  light: {
    "--page-canvas": "oklch(0.988087 0 0)",
    "--surface": "oklch(1 0 0)",
    "--surface-raised": "oklch(1 0 0)",
    "--background": "var(--page-canvas)",
  },
  dark: {
    "--page-canvas": "oklch(0.18 0.005 285.823)",
    "--surface": "oklch(0.21 0.006 285.885)",
    "--surface-raised": "oklch(0.235 0.007 285.885)",
    "--background": "var(--page-canvas)",
  },
};

export interface AccentContrast {
  mode: ThemeMode;
  /** `--brand` as text on a surface, or the label on a brand fill. */
  foreground: "--brand" | "--brand-foreground";
  background: string;
  ratio: number;
  required: number;
}

/**
 * The accent measured the way `pnpm theme:check` measures `--brand`, over
 * the theme each mode shows: as text on the reading surfaces, and under its
 * label. A surface the theme mixes or leaves unreadable is skipped.
 */
export function accentContrast(
  accent: ThemedColor,
  themeFor: (mode: ThemeMode) => Theme,
  themeOptions: Readonly<Record<string, ThemeOptionValues>>,
): AccentContrast[] {
  const out: AccentContrast[] = [];
  const accentVars = accentVariables(accent);
  for (const mode of ["light", "dark"] as const) {
    const theme = themeFor(mode);
    const own = Object.entries(themeModeVariables(theme.css)[mode]).map(([name, value]) => [`--${name}`, value] as const);
    const palette = new Map<string, string>([
      ...Object.entries(READING_SURFACE_DEFAULTS[mode]),
      ...own,
      ...Object.entries(themeOptionVariables(theme, themeOptions[theme.id])[mode]),
      ...Object.entries(accentVars[mode]),
    ]);
    const measure = (foreground: AccentContrast["foreground"], background: string) => {
      const fg = resolveColor(palette, foreground);
      const bg = resolveColor(palette, background);
      if (!("color" in fg) || !("color" in bg) || bg.color.a < 1) return;
      const ratio = Math.round(contrastRatio(fg.color, bg.color) * 100) / 100;
      out.push({ mode, foreground, background, ratio, required: AA_TEXT });
    };
    for (const surface of Object.keys(READING_SURFACE_DEFAULTS[mode])) measure("--brand", surface);
    measure("--brand-foreground", "--brand");
  }
  return out;
}
