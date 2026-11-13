import { useSyncExternalStore } from "react";
import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import { getCurrentWsId, subscribeToCurrentSlug } from "../platform/workspace-storage";
import { findTheme } from "./installed-themes";
import { THEME_ID_PATTERN, themeCss } from "./theme-package";
import { DEFAULT_THEME_ID, findBuiltInTheme } from "./built-in-themes";
import { isLocalFont, localFontFamily, type LocalFont } from "./local-fonts";
import { accentVariables } from "./accent";
import {
  HEX_COLOR,
  themeOptionAttributes,
  themeOptionVariables,
  type Theme,
  type ThemeMode,
  type ThemeOptionValue,
  type ThemeOptionValues,
  type ThemedColor,
} from "./themes";

export * from "./themes";
export * from "./accent";
export * from "./built-in-themes";
export * from "./theme-css";
export * from "./theme-package";
export * from "./installed-themes";
export * from "./anchors";
export * from "./theme-variables";
export * from "./local-fonts";
export * from "./theme-archive";
export * from "./theme-export";
export * from "./safe-mode";

/**
 * Appearance sits on top of light/dark mode: a theme per mode (see ./themes)
 * with its options, type for the interface and for content, density and two
 * accessibility switches, and per workspace an accent or other themes. All
 * of it is saved on this device.
 * `AppearanceSync` in @multica/views writes it onto `<html>`: global
 * variables as inline style, the theme's CSS and the adjustments on top of
 * it as two generated stylesheets.
 */

export const UI_FONTS = ["inter", "system", "serif", "mono"] as const;
export type UiFont = (typeof UI_FONTS)[number];

/** Built-in CJK faces; the families per locale live in appearance.css. */
export const CJK_FONTS = ["sans", "serif", "kai"] as const;
export type CjkFont = (typeof CJK_FONTS)[number];

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

export const CONTENT_SIZES = [12, 13, 14, 15, 16, 17, 18] as const;
export type ContentSize = typeof FOLLOW_UI | (typeof CONTENT_SIZES)[number];

export const CONTENT_LEADINGS = ["compact", "default", "relaxed"] as const;
export type ContentLeading = (typeof CONTENT_LEADINGS)[number];

export const DENSITIES = ["compact", "default", "comfortable"] as const;
export type Density = (typeof DENSITIES)[number];

/** A theme id per mode; the same id in both unless the person split them. */
export interface ThemePair {
  lightTheme: string;
  darkTheme: string;
}

/** What one workspace shows instead of the global choice. Every field is optional. */
export interface WorkspaceAppearance extends Partial<ThemePair> {
  /** Replaces the theme's `--brand`, per mode. */
  accent?: ThemedColor;
}

export interface AppearancePreferences extends ThemePair {
  /** Option values per theme id, so switching themes keeps each one's choices. */
  themeOptions: Readonly<Record<string, ThemeOptionValues>>;
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
  /** Overrides by workspace id. */
  workspaces: Readonly<Record<string, WorkspaceAppearance>>;
}

export const DEFAULT_APPEARANCE: AppearancePreferences = {
  lightTheme: DEFAULT_THEME_ID,
  darkTheme: DEFAULT_THEME_ID,
  themeOptions: {},
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
  workspaces: {},
};

export const APPEARANCE_STORAGE_KEY = "multica_appearance";
export const APPEARANCE_THEME_ATTR = "data-theme";

const oneOf =
  <T,>(values: readonly T[]) =>
  (v: unknown): v is T =>
    (values as readonly unknown[]).includes(v);

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

const isThemeId = (v: unknown): v is string => typeof v === "string" && THEME_ID_PATTERN.test(v);
const isHex = (v: unknown): v is string => typeof v === "string" && HEX_COLOR.test(v);
const WORKSPACE_KEY = /^[\w-]{1,64}$/;

/** A pair where one side may be missing takes the other for it; neither side valid is no pair. */
function sanitizePair<T>(light: unknown, dark: unknown, valid: (v: unknown) => v is T): { light: T; dark: T } | undefined {
  const l = valid(light) ? light : undefined;
  const d = valid(dark) ? dark : undefined;
  if (l === undefined && d === undefined) return undefined;
  return { light: (l ?? d)!, dark: (d ?? l)! };
}

function sanitizeWorkspaces(value: unknown): Record<string, WorkspaceAppearance> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const out: Record<string, WorkspaceAppearance> = {};
  for (const [workspaceId, entry] of Object.entries(value).slice(0, 100)) {
    if (!WORKSPACE_KEY.test(workspaceId) || !entry || typeof entry !== "object") continue;
    const e = entry as Record<string, unknown>;
    const themes = sanitizePair(e.lightTheme, e.darkTheme, isThemeId);
    const accent = e.accent && typeof e.accent === "object" ? (e.accent as Record<string, unknown>) : {};
    const color = sanitizePair(accent.light, accent.dark, isHex);
    const clean: WorkspaceAppearance = {
      ...(themes ? { lightTheme: themes.light, darkTheme: themes.dark } : {}),
      ...(color ? { accent: color } : {}),
    };
    if (Object.keys(clean).length > 0) out[workspaceId] = clean;
  }
  return out;
}

/**
 * Reads whatever is stored, field by field, falling back to the default for
 * anything missing or unknown. Theme ids are kept even when no such theme is
 * installed yet (installed themes load from their own storage); an unknown
 * id renders as the default theme. Older versions stored one theme for both
 * modes as `theme`, before that as `style`, and `cjkFont` instead of
 * `uiCjkFont`.
 */
export function sanitizeAppearance(stored: unknown): AppearancePreferences {
  const p = (stored && typeof stored === "object" ? stored : {}) as Record<string, unknown>;
  const pick = <K extends keyof AppearancePreferences>(
    key: K,
    valid: (v: unknown) => v is AppearancePreferences[K],
    value: unknown = p[key],
  ): AppearancePreferences[K] => (valid(value) ? value : DEFAULT_APPEARANCE[key]);
  const isFollow = (v: unknown): v is typeof FOLLOW_UI => v === FOLLOW_UI;
  const legacy = p.theme ?? p.style;
  const themes = sanitizePair(p.lightTheme, p.darkTheme, isThemeId) ??
    sanitizePair(legacy, legacy, isThemeId) ?? { light: DEFAULT_THEME_ID, dark: DEFAULT_THEME_ID };
  return {
    lightTheme: themes.light,
    darkTheme: themes.dark,
    themeOptions: sanitizeThemeOptions(p.themeOptions),
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
    workspaces: sanitizeWorkspaces(p.workspaces),
  };
}

export function isDefaultAppearance(prefs: AppearancePreferences): boolean {
  return (Object.keys(DEFAULT_APPEARANCE) as (keyof AppearancePreferences)[]).every((key) =>
    key === "themeOptions" || key === "workspaces"
      ? Object.keys(prefs[key]).length === 0
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

/** Where a theme is shown: the mode drawn, and the workspace of the window. */
export interface ThemeTarget {
  mode: ThemeMode;
  workspaceId?: string | null;
}

type ThemeChoices = Pick<AppearancePreferences, "lightTheme" | "darkTheme" | "workspaces">;

const themeForMode = (pair: Partial<ThemePair> | undefined, mode: ThemeMode) =>
  mode === "dark" ? pair?.darkTheme : pair?.lightTheme;

/**
 * The theme to render: the workspace's theme for the mode, else the global
 * theme for the mode, else Default. A theme that is not installed is passed
 * over. Safe mode always renders Default.
 */
export function activeTheme(prefs: ThemeChoices, target: ThemeTarget, safeMode = false): Theme {
  const fallback = findBuiltInTheme(DEFAULT_THEME_ID)!;
  if (safeMode) return fallback;
  const workspace = target.workspaceId ? prefs.workspaces[target.workspaceId] : undefined;
  for (const id of [themeForMode(workspace, target.mode), themeForMode(prefs, target.mode)]) {
    const theme = id ? findTheme(id) : undefined;
    if (theme) return theme;
  }
  return fallback;
}

/** The accent a workspace replaces the theme's brand colour with, if any. */
export function activeAccent(
  prefs: Pick<AppearancePreferences, "workspaces">,
  workspaceId: string | null | undefined,
): ThemedColor | undefined {
  return workspaceId ? prefs.workspaces[workspaceId]?.accent : undefined;
}

/** `pair` showing `id` in `mode`, or in both modes while they share a theme. */
function pairWithTheme(pair: ThemePair, id: string, mode: ThemeMode): ThemePair {
  if (pair.lightTheme === pair.darkTheme) return { lightTheme: id, darkTheme: id };
  return mode === "dark" ? { ...pair, darkTheme: id } : { ...pair, lightTheme: id };
}

const withoutUndefined = <T extends object>(value: T): T =>
  Object.fromEntries(Object.entries(value).filter(([, v]) => v !== undefined)) as T;

/**
 * This window's workspace, which per-workspace appearance follows. It reads
 * the platform mirror rather than the route, because Desktop mounts
 * AppearanceSync above its workspace routes.
 */
export function useAppearanceWorkspaceId(): string | null {
  return useSyncExternalStore(subscribeToCurrentSlug, getCurrentWsId, () => null);
}

const BOTH_MODES = ":root:not(.dark), :root.dark";

const declare = (variables: Record<string, string>) =>
  Object.entries(variables)
    .map(([name, value]) => `${name}: ${value};`)
    .join(" ");

/**
 * What the person chose on top of the theme: its option values, the
 * workspace's accent, contrast and motion. Written after the theme's CSS,
 * with selectors that outrank `.theme-light` and `.theme-dark`, so they hold
 * whichever theme is active.
 */
export function appearanceStylesheet(theme: Theme, prefs: AppearancePreferences, accent?: ThemedColor): string {
  const parts: string[] = [];
  const options = themeOptionVariables(theme, prefs.themeOptions[theme.id]);
  if (Object.keys(options.light).length > 0) {
    parts.push(`:root.theme-light { ${declare(options.light)} }`, `:root.theme-dark { ${declare(options.dark)} }`);
  }
  if (accent) {
    const variables = accentVariables(accent);
    parts.push(`:root.theme-light { ${declare(variables.light)} }`, `:root.theme-dark { ${declare(variables.dark)} }`);
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
  /**
   * Shows `id` where `target` points: in the workspace's themes when it has
   * its own, else in the global ones; in both modes while they share a theme.
   */
  chooseTheme: (id: string, target: ThemeTarget) => void;
  /** Merges into a workspace's overrides; `undefined` clears a field and `null` all of them. */
  setWorkspaceAppearance: (workspaceId: string, patch: WorkspaceAppearance | null) => void;
  /** Stops using a theme being removed: Default globally, the other mode's theme in a workspace. */
  forgetTheme: (id: string) => void;
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
      chooseTheme: (id, { mode, workspaceId }) =>
        set((state) => {
          const workspace = workspaceId ? state.workspaces[workspaceId] : undefined;
          if (workspaceId && workspace?.lightTheme && workspace.darkTheme) {
            const pair = pairWithTheme({ lightTheme: workspace.lightTheme, darkTheme: workspace.darkTheme }, id, mode);
            return { workspaces: { ...state.workspaces, [workspaceId]: { ...workspace, ...pair } } };
          }
          return pairWithTheme(state, id, mode);
        }),
      setWorkspaceAppearance: (workspaceId, patch) =>
        set((state) => {
          const { [workspaceId]: current, ...rest } = state.workspaces;
          const next = patch ? withoutUndefined({ ...current, ...patch }) : {};
          return { workspaces: Object.keys(next).length > 0 ? { ...rest, [workspaceId]: next } : rest };
        }),
      forgetTheme: (id) =>
        set((state) => {
          const workspaces: Record<string, WorkspaceAppearance> = {};
          for (const [workspaceId, { lightTheme, darkTheme, ...rest }] of Object.entries(state.workspaces)) {
            const pair = sanitizePair(lightTheme === id ? null : lightTheme, darkTheme === id ? null : darkTheme, isThemeId);
            const next = pair ? { ...rest, lightTheme: pair.light, darkTheme: pair.dark } : rest;
            if (Object.keys(next).length > 0) workspaces[workspaceId] = next;
          }
          return {
            lightTheme: state.lightTheme === id ? DEFAULT_THEME_ID : state.lightTheme,
            darkTheme: state.darkTheme === id ? DEFAULT_THEME_ID : state.darkTheme,
            workspaces,
          };
        }),
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
    lightTheme: state.lightTheme,
    darkTheme: state.darkTheme,
    themeOptions: state.themeOptions,
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
    workspaces: state.workspaces,
  };
}

const SYSTEM_REDUCED_MOTION = "(prefers-reduced-motion: reduce)";

function systemReducedMotion(): boolean {
  return typeof window !== "undefined" && window.matchMedia?.(SYSTEM_REDUCED_MOTION).matches === true;
}

function subscribeSystemReducedMotion(onChange: () => void): () => void {
  const query = typeof window !== "undefined" ? window.matchMedia?.(SYSTEM_REDUCED_MOTION) : undefined;
  query?.addEventListener?.("change", onChange);
  return () => query?.removeEventListener?.("change", onChange);
}

/**
 * Whether to skip motion: the Appearance switch or the system preference.
 * For animation driven from JS; CSS durations already follow both.
 */
export function useReducedMotion(): boolean {
  const reduceMotion = useAppearanceStore((state) => state.reduceMotion);
  const system = useSyncExternalStore(subscribeSystemReducedMotion, systemReducedMotion, () => false);
  return reduceMotion || system;
}

/** `useReducedMotion` read once, for code outside render. */
export function prefersReducedMotion(): boolean {
  return useAppearanceStore.getState().reduceMotion || systemReducedMotion();
}
