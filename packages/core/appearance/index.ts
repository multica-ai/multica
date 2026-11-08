import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import { DEFAULT_THEME_ID, findTheme, themeStylesheet, themeTraits } from "./themes";

export * from "./themes";

/**
 * Appearance sits on top of light/dark mode: a theme (see ./themes), an
 * accent colour, type, density and two accessibility switches. All of it is
 * saved on this device. `AppearanceSync` in @multica/views writes it onto
 * `<html>`: global variables as inline style, mode-dependent tokens as one
 * generated stylesheet.
 */

export const UI_FONTS = ["inter", "system", "serif", "mono"] as const;
export type UiFont = (typeof UI_FONTS)[number];

/** How CJK characters are drawn; the families per locale live in appearance.css. */
export const CJK_FONTS = ["sans", "serif", "kai"] as const;
export type CjkFont = (typeof CJK_FONTS)[number];

export const CODE_FONTS = ["geist", "jetbrains", "system"] as const;
export type CodeFont = (typeof CODE_FONTS)[number];

export const FONT_SIZES = [12, 13, 14, 15, 16] as const;
export type FontSize = (typeof FONT_SIZES)[number];
export const DEFAULT_FONT_SIZE: FontSize = 14;

export const DENSITIES = ["compact", "default", "comfortable"] as const;
export type Density = (typeof DENSITIES)[number];

/** `theme` keeps the theme's own brand colour. */
export const ACCENTS = ["theme", "blue", "violet", "green", "orange", "pink", "graphite"] as const;
export type Accent = (typeof ACCENTS)[number];

/** Light and dark values of --brand per accent, and the text that sits on it. */
export const ACCENT_COLORS: Record<
  Exclude<Accent, "theme">,
  { light: string; dark: string; darkForeground?: string }
> = {
  blue: { light: "oklch(0.53 0.19 256.4)", dark: "oklch(0.75 0.13 255)" },
  violet: { light: "oklch(0.52 0.2 292)", dark: "oklch(0.75 0.13 292)" },
  green: { light: "oklch(0.5 0.11 160)", dark: "oklch(0.75 0.13 160)" },
  orange: { light: "oklch(0.55 0.16 45)", dark: "oklch(0.76 0.13 52)" },
  pink: { light: "oklch(0.54 0.2 355)", dark: "oklch(0.76 0.13 355)" },
  graphite: {
    light: "oklch(0.32 0.01 286)",
    dark: "oklch(0.86 0.01 286)",
    darkForeground: "oklch(0.21 0.006 285.885)",
  },
};

export interface AppearancePreferences {
  theme: string;
  accent: Accent;
  uiFont: UiFont;
  cjkFont: CjkFont;
  codeFont: CodeFont;
  fontSize: FontSize;
  density: Density;
  reduceMotion: boolean;
  highContrast: boolean;
}

export const DEFAULT_APPEARANCE: AppearancePreferences = {
  theme: DEFAULT_THEME_ID,
  accent: "theme",
  uiFont: "inter",
  cjkFont: "sans",
  codeFont: "geist",
  fontSize: DEFAULT_FONT_SIZE,
  density: "default",
  reduceMotion: false,
  highContrast: false,
};

export const APPEARANCE_STORAGE_KEY = "multica_appearance";
export const APPEARANCE_THEME_ATTR = "data-theme";
export const APPEARANCE_TRAITS_ATTR = "data-theme-traits";

const oneOf =
  <T,>(values: readonly T[]) =>
  (v: unknown): v is T =>
    (values as readonly unknown[]).includes(v);

/**
 * Reads whatever is stored, field by field, falling back to the default for
 * anything missing or unknown. The first version stored only `style`, whose
 * values are theme ids now.
 */
export function sanitizeAppearance(stored: unknown): AppearancePreferences {
  const p = (stored && typeof stored === "object" ? stored : {}) as Record<string, unknown>;
  const theme = typeof p.theme === "string" ? p.theme : p.style;
  const pick = <K extends keyof AppearancePreferences>(
    key: K,
    valid: (v: unknown) => v is AppearancePreferences[K],
    value: unknown = p[key],
  ): AppearancePreferences[K] => (valid(value) ? value : DEFAULT_APPEARANCE[key]);
  return {
    theme: pick("theme", (v): v is string => typeof v === "string" && !!findTheme(v), theme),
    accent: pick("accent", oneOf(ACCENTS)),
    uiFont: pick("uiFont", oneOf(UI_FONTS)),
    cjkFont: pick("cjkFont", oneOf(CJK_FONTS)),
    codeFont: pick("codeFont", oneOf(CODE_FONTS)),
    fontSize: pick("fontSize", oneOf(FONT_SIZES)),
    density: pick("density", oneOf(DENSITIES)),
    reduceMotion: pick("reduceMotion", (v): v is boolean => typeof v === "boolean"),
    highContrast: pick("highContrast", (v): v is boolean => typeof v === "boolean"),
  };
}

export function isDefaultAppearance(prefs: AppearancePreferences): boolean {
  return (Object.keys(DEFAULT_APPEARANCE) as (keyof AppearancePreferences)[]).every(
    (key) => prefs[key] === DEFAULT_APPEARANCE[key],
  );
}

const UI_FONT_FAMILIES: Record<UiFont, string | null> = {
  inter: null,
  system: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial',
  serif: "var(--font-ui-serif)",
  mono: "var(--font-ui-mono)",
};
const CJK_FONT_FAMILIES: Record<CjkFont, string | null> = {
  sans: null,
  serif: "var(--font-cjk-serif)",
  kai: "var(--font-cjk-kai)",
};
const CODE_FONT_FAMILIES: Record<CodeFont, string | null> = {
  geist: null,
  jetbrains: "var(--font-code-jetbrains), var(--font-ui-mono), ui-monospace, monospace",
  system: 'ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
};
const ROW_HEIGHTS: Record<Density, string | null> = {
  compact: "32px",
  default: null,
  comfortable: "42px",
};

/**
 * Mode-independent variables for `<html>`'s inline style. `null` means
 * "remove", so the stylesheet default applies. The font variables are read by
 * the `--font-sans` stack in each app's globals.css.
 */
export function appearanceRootVariables(
  prefs: AppearancePreferences,
): Record<string, string | null> {
  return {
    "--font-ui": UI_FONT_FAMILIES[prefs.uiFont],
    "--font-cjk": CJK_FONT_FAMILIES[prefs.cjkFont],
    "--font-mono": CODE_FONT_FAMILIES[prefs.codeFont],
    "--font-scale": prefs.fontSize === DEFAULT_FONT_SIZE ? null : String(prefs.fontSize / 14),
    "--issue-row-height": ROW_HEIGHTS[prefs.density],
  };
}

const BOTH_MODES = ":root:not(.dark), :root.dark";

/**
 * Everything mode-dependent, in cascade order: the theme, then the accent
 * and contrast adjustments on top of whichever theme is active. Every block
 * uses the theme blocks' specificity, so order decides.
 */
export function appearanceStylesheet(prefs: AppearancePreferences): string {
  const theme = findTheme(prefs.theme);
  const parts: string[] = theme ? [themeStylesheet(theme)] : [];
  if (prefs.accent !== "theme") {
    const accent = ACCENT_COLORS[prefs.accent];
    parts.push(
      `:root:not(.dark) { --brand: ${accent.light}; --brand-foreground: oklch(0.985 0 0); }`,
      `:root.dark { --brand: ${accent.dark}; --brand-foreground: ${accent.darkForeground ?? "oklch(0.985 0 0)"}; }`,
    );
  }
  if (prefs.highContrast) {
    const toward = (share: number) =>
      `color-mix(in oklab, var(--foreground) ${share}%, var(--page-canvas))`;
    parts.push(
      `${BOTH_MODES} { --muted-foreground: ${toward(80)}; --faint-foreground: ${toward(62)}; --border: ${toward(22)}; --surface-border: ${toward(30)}; --input: ${toward(40)}; }`,
    );
  }
  if (prefs.reduceMotion) {
    parts.push(
      "*, *::before, *::after { animation-duration: 0.01ms !important; animation-iteration-count: 1 !important; transition-duration: 0.01ms !important; scroll-behavior: auto !important; }",
    );
  }
  return parts.filter(Boolean).join("\n");
}

/** `data-theme` / `data-theme-traits` values; `null` removes the attribute. */
export function appearanceThemeAttributes(prefs: AppearancePreferences): {
  theme: string | null;
  traits: string | null;
} {
  const theme = findTheme(prefs.theme);
  if (!theme || theme.id === DEFAULT_THEME_ID) return { theme: null, traits: null };
  return { theme: theme.id, traits: themeTraits(theme) };
}

interface AppearanceState extends AppearancePreferences {
  set: (patch: Partial<AppearancePreferences>) => void;
  reset: () => void;
}

export const useAppearanceStore = create<AppearanceState>()(
  persist(
    (set) => ({
      ...DEFAULT_APPEARANCE,
      set: (patch) => set(patch),
      reset: () => set(DEFAULT_APPEARANCE),
    }),
    {
      name: APPEARANCE_STORAGE_KEY,
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => sanitizeAppearance(state),
      merge: (persisted, current) => ({ ...current, ...sanitizeAppearance(persisted) }),
    },
  ),
);

/** The saved preferences without the store's actions. */
export function selectAppearance(state: AppearanceState): AppearancePreferences {
  return {
    theme: state.theme,
    accent: state.accent,
    uiFont: state.uiFont,
    cjkFont: state.cjkFont,
    codeFont: state.codeFont,
    fontSize: state.fontSize,
    density: state.density,
    reduceMotion: state.reduceMotion,
    highContrast: state.highContrast,
  };
}
