import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import { findTheme } from "./installed-themes";
import { THEME_ID_PATTERN, themeCss } from "./theme-package";
import { DEFAULT_THEME_ID, findBuiltInTheme } from "./built-in-themes";
import {
  themeOptionAttributes,
  themeOptionVariables,
  type Theme,
  type ThemeOptionValue,
  type ThemeOptionValues,
} from "./themes";

export * from "./themes";
export * from "./built-in-themes";
export * from "./theme-css";
export * from "./theme-package";
export * from "./installed-themes";
export * from "./anchors";
export * from "./theme-variables";
export * from "./theme-archive";

/**
 * Appearance sits on top of light/dark mode: a theme (see ./themes) with its
 * options, an accent colour, type for the interface and for content, density
 * and two accessibility switches. All of it is saved on this device.
 * `AppearanceSync` in @multica/views writes it onto `<html>`: global
 * variables as inline style, the theme's CSS and the adjustments on top of
 * it as two generated stylesheets.
 */

export const UI_FONTS = ["inter", "system", "serif", "mono"] as const;
export type UiFont = (typeof UI_FONTS)[number];

/** Built-in CJK faces; the families per locale live in appearance.css. */
export const CJK_FONTS = ["sans", "serif", "kai"] as const;
export type CjkFont = (typeof CJK_FONTS)[number];

/** A family installed on this device, as `local:<family name>`. */
export type LocalFont = `local:${string}`;
export type CjkFontChoice = CjkFont | LocalFont;

/** Content settings can follow the interface instead of naming a value. */
export const FOLLOW_UI = "ui";
export type ContentFont = typeof FOLLOW_UI | UiFont;
export type ContentCjkFont = typeof FOLLOW_UI | CjkFontChoice;

export const CODE_FONTS = ["geist", "jetbrains", "system"] as const;
export type CodeFont = (typeof CODE_FONTS)[number];

export const FONT_SIZES = [12, 13, 14, 15, 16] as const;
export type FontSize = (typeof FONT_SIZES)[number];
export const DEFAULT_FONT_SIZE: FontSize = 14;

export const CONTENT_SIZES = [13, 14, 15, 16, 17, 18] as const;
export type ContentSize = typeof FOLLOW_UI | (typeof CONTENT_SIZES)[number];

export const CONTENT_LEADINGS = ["compact", "default", "relaxed"] as const;
export type ContentLeading = (typeof CONTENT_LEADINGS)[number];

export const DENSITIES = ["compact", "default", "comfortable"] as const;
export type Density = (typeof DENSITIES)[number];

/** `theme` keeps the theme's own brand colour. */
export const ACCENTS = ["theme", "blue", "violet", "green", "orange", "pink", "graphite"] as const;
export type Accent = (typeof ACCENTS)[number];

/**
 * An accented active item: --brand at this share over what sits behind it.
 * Sidebar items also take --brand mixed toward --foreground as their text, so
 * it stays legible on that tint; rail items keep the frame's own text.
 */
export const ACCENT_ACTIVE_TINT = 12;
export const ACCENT_ACTIVE_TEXT = 70;

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
  /** Option values per theme id, so switching themes keeps each one's choices. */
  themeOptions: Readonly<Record<string, ThemeOptionValues>>;
  accent: Accent;
  uiFont: UiFont;
  uiCjkFont: CjkFontChoice;
  fontSize: FontSize;
  contentFont: ContentFont;
  contentCjkFont: ContentCjkFont;
  contentSize: ContentSize;
  contentLeading: ContentLeading;
  codeFont: CodeFont;
  density: Density;
  reduceMotion: boolean;
  highContrast: boolean;
}

export const DEFAULT_APPEARANCE: AppearancePreferences = {
  theme: DEFAULT_THEME_ID,
  themeOptions: {},
  accent: "theme",
  uiFont: "inter",
  uiCjkFont: "sans",
  fontSize: DEFAULT_FONT_SIZE,
  contentFont: FOLLOW_UI,
  contentCjkFont: FOLLOW_UI,
  contentSize: FOLLOW_UI,
  contentLeading: "default",
  codeFont: "geist",
  density: "default",
  reduceMotion: false,
  highContrast: false,
};

export const APPEARANCE_STORAGE_KEY = "multica_appearance";
export const APPEARANCE_THEME_ATTR = "data-theme";

const oneOf =
  <T,>(values: readonly T[]) =>
  (v: unknown): v is T =>
    (values as readonly unknown[]).includes(v);

const LOCAL_FAMILY = /^[\p{L}\p{N} ._-]{1,64}$/u;

/** `local:<family>` with a family name that is safe to quote in CSS. */
export function isLocalFont(value: unknown): value is LocalFont {
  return (
    typeof value === "string" &&
    value.startsWith("local:") &&
    LOCAL_FAMILY.test(value.slice(6)) &&
    value.slice(6).trim() === value.slice(6)
  );
}

export function localFontFamily(choice: LocalFont): string {
  return choice.slice(6);
}

const isCjkChoice = (v: unknown): v is CjkFontChoice => oneOf(CJK_FONTS)(v) || isLocalFont(v);
const OPTION_KEY = /^[a-z][a-z0-9-]{0,47}$/;

function sanitizeOptionValue(v: unknown): ThemeOptionValue | undefined {
  if (typeof v === "boolean") return v;
  if (typeof v === "number") return Number.isFinite(v) ? v : undefined;
  if (typeof v === "string") return v.length <= 300 ? v : undefined;
  if (!v || typeof v !== "object" || Array.isArray(v)) return undefined;
  const color: { light?: string; dark?: string } = {};
  for (const mode of ["light", "dark"] as const) {
    const value = (v as Record<string, unknown>)[mode];
    if (typeof value === "string" && value.length <= 16) color[mode] = value;
  }
  return Object.keys(color).length > 0 ? color : undefined;
}

function sanitizeThemeOptions(value: unknown): Record<string, ThemeOptionValues> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const out: Record<string, ThemeOptionValues> = {};
  for (const [themeId, options] of Object.entries(value).slice(0, 40)) {
    if (!THEME_ID_PATTERN.test(themeId) || !options || typeof options !== "object") continue;
    const clean: Record<string, ThemeOptionValue> = {};
    for (const [key, v] of Object.entries(options).slice(0, 24)) {
      const value = sanitizeOptionValue(v);
      if (OPTION_KEY.test(key) && value !== undefined) clean[key] = value;
    }
    if (Object.keys(clean).length > 0) out[themeId] = clean;
  }
  return out;
}

/**
 * Reads whatever is stored, field by field, falling back to the default for
 * anything missing or unknown. The theme id is kept even when no such theme
 * is installed yet (installed themes load from their own storage); an
 * unknown id renders as the default theme. Older versions stored `style`
 * instead of `theme` and `cjkFont` instead of `uiCjkFont`.
 */
export function sanitizeAppearance(stored: unknown): AppearancePreferences {
  const p = (stored && typeof stored === "object" ? stored : {}) as Record<string, unknown>;
  const pick = <K extends keyof AppearancePreferences>(
    key: K,
    valid: (v: unknown) => v is AppearancePreferences[K],
    value: unknown = p[key],
  ): AppearancePreferences[K] => (valid(value) ? value : DEFAULT_APPEARANCE[key]);
  const isFollow = (v: unknown): v is typeof FOLLOW_UI => v === FOLLOW_UI;
  return {
    theme: pick(
      "theme",
      (v): v is string => typeof v === "string" && THEME_ID_PATTERN.test(v),
      p.theme ?? p.style,
    ),
    themeOptions: sanitizeThemeOptions(p.themeOptions),
    accent: pick("accent", oneOf(ACCENTS)),
    uiFont: pick("uiFont", oneOf(UI_FONTS)),
    uiCjkFont: pick("uiCjkFont", isCjkChoice, p.uiCjkFont ?? p.cjkFont),
    fontSize: pick("fontSize", oneOf(FONT_SIZES)),
    contentFont: pick("contentFont", (v): v is ContentFont => isFollow(v) || oneOf(UI_FONTS)(v)),
    contentCjkFont: pick("contentCjkFont", (v): v is ContentCjkFont => isFollow(v) || isCjkChoice(v)),
    contentSize: pick("contentSize", (v): v is ContentSize => isFollow(v) || oneOf(CONTENT_SIZES)(v)),
    contentLeading: pick("contentLeading", oneOf(CONTENT_LEADINGS)),
    codeFont: pick("codeFont", oneOf(CODE_FONTS)),
    density: pick("density", oneOf(DENSITIES)),
    reduceMotion: pick("reduceMotion", (v): v is boolean => typeof v === "boolean"),
    highContrast: pick("highContrast", (v): v is boolean => typeof v === "boolean"),
  };
}

export function isDefaultAppearance(prefs: AppearancePreferences): boolean {
  return (Object.keys(DEFAULT_APPEARANCE) as (keyof AppearancePreferences)[]).every((key) =>
    key === "themeOptions"
      ? Object.keys(prefs.themeOptions).length === 0
      : prefs[key] === DEFAULT_APPEARANCE[key],
  );
}

const UI_FONT_FAMILIES: Record<UiFont, string> = {
  inter: "var(--font-ui-inter)",
  system: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial',
  serif: "var(--font-ui-serif)",
  mono: "var(--font-ui-mono)",
};
const CJK_FONT_FAMILIES: Record<CjkFont, string> = {
  sans: "var(--font-cjk-sans)",
  serif: "var(--font-cjk-serif)",
  kai: "var(--font-cjk-kai)",
};

/** The CSS stack for a CJK choice; a local family falls back to the built-in sans chain. */
export function cjkFontStack(choice: CjkFontChoice): string {
  return isLocalFont(choice)
    ? `"${localFontFamily(choice)}", var(--font-cjk-sans)`
    : CJK_FONT_FAMILIES[choice];
}

export function uiFontStack(font: UiFont): string {
  return UI_FONT_FAMILIES[font];
}

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
const LEADINGS: Record<ContentLeading, [string, string] | null> = {
  compact: ["1.5", "1.65"],
  default: null,
  relaxed: ["1.8", "1.95"],
};

/**
 * Mode-independent variables for `<html>`'s inline style. `null` means
 * "remove", so the theme's value or the stylesheet default applies. The font
 * variables feed the `--font-sans` stack in each app's globals.css and
 * `--font-content` in appearance.css.
 */
export function appearanceRootVariables(
  prefs: AppearancePreferences,
): Record<string, string | null> {
  const leading = LEADINGS[prefs.contentLeading];
  return {
    "--font-ui": prefs.uiFont === "inter" ? null : UI_FONT_FAMILIES[prefs.uiFont],
    "--font-cjk": prefs.uiCjkFont === "sans" ? null : cjkFontStack(prefs.uiCjkFont),
    "--font-content-latin":
      prefs.contentFont === FOLLOW_UI ? null : UI_FONT_FAMILIES[prefs.contentFont],
    "--font-content-cjk":
      prefs.contentCjkFont === FOLLOW_UI ? null : cjkFontStack(prefs.contentCjkFont),
    "--font-mono": CODE_FONT_FAMILIES[prefs.codeFont],
    "--font-scale": prefs.fontSize === DEFAULT_FONT_SIZE ? null : String(prefs.fontSize / 14),
    "--content-scale": prefs.contentSize === FOLLOW_UI ? null : String(prefs.contentSize / 14),
    "--content-leading": leading?.[0] ?? null,
    "--content-leading-cjk": leading?.[1] ?? null,
    "--issue-row-height": ROW_HEIGHTS[prefs.density],
  };
}

/* ── Safe mode ──────────────────────────────────────────────────────────────
 * Ignores the chosen theme for the rest of the browser session, so a theme
 * that hides or breaks the interface can always be undone. Entered with
 * `?theme=default` in the URL or by holding Shift while the app loads. */

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

/** The theme to render: the chosen one, or Default in safe mode or when it is not installed. */
export function activeTheme(prefs: Pick<AppearancePreferences, "theme">, safeMode = false): Theme {
  const fallback = findBuiltInTheme(DEFAULT_THEME_ID)!;
  if (safeMode) return fallback;
  return findTheme(prefs.theme) ?? fallback;
}

const BOTH_MODES = ":root:not(.dark), :root.dark";

const declare = (variables: Record<string, string>) =>
  Object.entries(variables)
    .map(([name, value]) => `${name}: ${value};`)
    .join(" ");

/**
 * What the person chose on top of the theme: its option values, accent and
 * contrast. Written after the theme's CSS, with selectors that outrank
 * `.theme-light` and `.theme-dark`, so they hold whichever theme is active.
 */
export function appearanceStylesheet(theme: Theme, prefs: AppearancePreferences): string {
  const parts: string[] = [];
  const options = themeOptionVariables(theme, prefs.themeOptions[theme.id]);
  if (Object.keys(options.light).length > 0) {
    parts.push(`:root.theme-light { ${declare(options.light)} }`, `:root.theme-dark { ${declare(options.dark)} }`);
  }
  if (prefs.accent !== "theme") {
    const accent = ACCENT_COLORS[prefs.accent];
    parts.push(
      `:root:not(.dark) { --brand: ${accent.light}; --brand-foreground: oklch(0.985 0 0); }`,
      `:root.dark { --brand: ${accent.dark}; --brand-foreground: ${accent.darkForeground ?? "oklch(0.985 0 0)"}; }`,
    );
    const tint = `color-mix(in srgb, var(--brand) ${ACCENT_ACTIVE_TINT}%, transparent)`;
    const text = `color-mix(in oklab, var(--brand) ${ACCENT_ACTIVE_TEXT}%, var(--foreground))`;
    parts.push(
      `${BOTH_MODES} { ${declare({
        "--nav-item-active-background": tint,
        "--nav-item-active-color": text,
        "--rail-item-active-background": tint,
        "--ring": "var(--brand)",
      })} }`,
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

/** The theme's own CSS, checked and with assets resolved. */
export function appearanceThemeCss(theme: Theme): string {
  return themeCss(theme);
}

/** Attributes for <html>: `data-theme` and the theme's options; `null` removes one. */
export function appearanceThemeAttributes(
  theme: Theme,
  prefs: AppearancePreferences,
): Record<string, string | null> {
  if (theme.id === DEFAULT_THEME_ID) return { [APPEARANCE_THEME_ATTR]: null };
  return {
    [APPEARANCE_THEME_ATTR]: theme.id,
    ...themeOptionAttributes(theme, prefs.themeOptions[theme.id]),
  };
}

interface AppearanceState extends AppearancePreferences {
  set: (patch: Partial<AppearancePreferences>) => void;
  setThemeOption: (themeId: string, optionId: string, value: ThemeOptionValue) => void;
  reset: () => void;
}

export const useAppearanceStore = create<AppearanceState>()(
  persist(
    (set) => ({
      ...DEFAULT_APPEARANCE,
      set: (patch) => set(patch),
      setThemeOption: (themeId, optionId, value) =>
        set((state) => ({
          themeOptions: {
            ...state.themeOptions,
            [themeId]: { ...state.themeOptions[themeId], [optionId]: value },
          },
        })),
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
    themeOptions: state.themeOptions,
    accent: state.accent,
    uiFont: state.uiFont,
    uiCjkFont: state.uiCjkFont,
    fontSize: state.fontSize,
    contentFont: state.contentFont,
    contentCjkFont: state.contentCjkFont,
    contentSize: state.contentSize,
    contentLeading: state.contentLeading,
    codeFont: state.codeFont,
    density: state.density,
    reduceMotion: state.reduceMotion,
    highContrast: state.highContrast,
  };
}
