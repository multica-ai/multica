/**
 * A theme is a folder: `manifest.json` names it and declares its options,
 * `theme.css` overrides the public variables (./theme-variables) under
 * `.theme-light` and `.theme-dark` and may restyle the public anchors
 * (./anchors), and any fonts or images the CSS references sit next to it.
 *
 * Built-in themes (./built-in-themes) are written as token tables so the
 * contrast test can read them, and compiled to the same CSS an outside theme
 * ships. Everything a theme ships goes through `sanitizeThemeCss` (./theme-css).
 * docs/theming.md is the public reference; keep it in step with this file.
 */

/** Bumped only for changes that would break existing themes. */
export const THEME_API_VERSION = 1;

/** Text a theme shows on the Appearance page: one string, or one per locale. */
export type LocalizedText = string | Readonly<Record<string, string>>;

/**
 * Options a theme declares in its manifest, rendered on the Appearance page
 * (the types follow Obsidian's Style Settings). `toggle` and `select` become
 * `data-theme-option-<id>` on <html>; the others set the variable `--<id>`
 * for both modes, or per mode for `color`.
 */
interface ThemeOptionBase {
  id: string;
  label: LocalizedText;
  description?: LocalizedText;
}

/** Starts a group of options. */
export interface ThemeHeadingOption extends ThemeOptionBase {
  type: "heading";
}

/** On: the attribute is present with the value `on`. Off: it is absent. */
export interface ThemeToggleOption extends ThemeOptionBase {
  type: "toggle";
  default: boolean;
}

/** The attribute carries the chosen value. */
export interface ThemeSelectOption extends ThemeOptionBase {
  type: "select";
  default: string;
  choices: readonly { value: string; label: LocalizedText; swatch?: string }[];
}

/** A hex colour per mode. */
export interface ThemeColorOption extends ThemeOptionBase {
  type: "color";
  default: ThemedColor;
}

/** `slider` needs `min` and `max`; the value is written with `unit` appended. */
export interface ThemeNumberOption extends ThemeOptionBase {
  type: "number" | "slider";
  default: number;
  min?: number;
  max?: number;
  step?: number;
  unit?: string;
}

/** Free text: a CSS value for `text`, a font stack for `font`. */
export interface ThemeTextOption extends ThemeOptionBase {
  type: "text" | "font";
  default: string;
}

/** A choice between CSS values. */
export interface ThemeVariableSelectOption extends ThemeOptionBase {
  type: "variable-select";
  default: string;
  choices: readonly { value: string; label: LocalizedText }[];
}

export type ThemeOption =
  | ThemeHeadingOption
  | ThemeToggleOption
  | ThemeSelectOption
  | ThemeColorOption
  | ThemeNumberOption
  | ThemeTextOption
  | ThemeVariableSelectOption;

export interface ThemedColor {
  light: string;
  dark: string;
}

/** manifest.json */
export interface ThemeManifest {
  apiVersion: typeof THEME_API_VERSION;
  id: string;
  name: string;
  /** Who made it; built-in themes name the product they follow. */
  author?: string;
  version?: string;
  description?: string;
  options?: readonly ThemeOption[];
}

/** A theme ready to apply: its manifest, theme.css and the files the CSS references. */
export interface Theme extends ThemeManifest {
  css: string;
  /** Relative path to a base64 `data:` URL. */
  assets?: Readonly<Record<string, string>>;
}

/** Variable name (without `--`) to value. */
export type ThemeTokens = Readonly<Record<string, string>>;

export interface BuiltInTheme extends Theme {
  tokens: { light: ThemeTokens; dark: ThemeTokens };
  /** Tokens for what sits on the frame: the rail, desktop toolbar and tab bar. */
  frame?: { light?: ThemeTokens; dark?: ThemeTokens };
}

const FRAME_ANCHORS = ':is([data-mc="app-rail"], [data-mc="window-toolbar"], [data-mc="tab-bar"])';

function declarations(tokens: ThemeTokens | undefined): string {
  return Object.entries(tokens ?? {})
    .map(([name, value]) => `--${name}: ${value};`)
    .join(" ");
}

/** The CSS a token table stands for. */
export function themeTokensCss(theme: Pick<BuiltInTheme, "tokens" | "frame">): string {
  const blocks: string[] = [];
  const add = (selector: string, body: string) => {
    if (body) blocks.push(`${selector} { ${body} }`);
  };
  add(".theme-light", declarations(theme.tokens.light));
  add(".theme-dark", declarations(theme.tokens.dark));
  add(`.theme-light ${FRAME_ANCHORS}`, declarations(theme.frame?.light));
  add(`.theme-dark ${FRAME_ANCHORS}`, declarations(theme.frame?.dark));
  return blocks.join("\n");
}

const SAFE_VALUE = /^[\w\s#.,%()/+-]+$/;
const UNSAFE_FUNCTION = /\b(url|image|image-set|cross-fade|element|expression|attr|env|src)\s*\(/i;

/** A token value a theme may set: colours, lengths, shadows and var() references only. */
export function isSafeThemeValue(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.length <= 200 &&
    SAFE_VALUE.test(value) &&
    !UNSAFE_FUNCTION.test(value)
  );
}

const FONT_VAR = /var\(--[a-z0-9-]+\)/g;
const SAFE_FONT_VALUE = /^[\p{L}\p{N} "',._-]+$/u;

/** A font stack: family names, quoted or not, and var() references to other stacks. */
export function isSafeFontValue(value: unknown): value is string {
  if (typeof value !== "string" || value.length === 0 || value.length > 300) return false;
  const rest = value.replace(FONT_VAR, "x");
  return SAFE_FONT_VALUE.test(rest) && (rest.split('"').length - 1) % 2 === 0 && (rest.split("'").length - 1) % 2 === 0;
}

const MODE_SELECTOR = /^(?::root|html)?\.theme-(light|dark)$/;

/**
 * The variables a theme sets directly on `.theme-light` and `.theme-dark`,
 * for previews. Only values that would pass as a token are returned.
 */
export function themeModeVariables(css: string): { light: Record<string, string>; dark: Record<string, string> } {
  const out = { light: {} as Record<string, string>, dark: {} as Record<string, string> };
  const clean = css.replace(/\/\*[\s\S]*?\*\//g, "");
  for (const rule of clean.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const modes = rule[1]!
      .split(",")
      .map((selector) => MODE_SELECTOR.exec(selector.trim())?.[1] as "light" | "dark" | undefined)
      .filter((mode) => mode !== undefined);
    for (const declaration of rule[2]!.matchAll(/--([a-z0-9-]+)\s*:\s*([^;]+)/gi)) {
      const value = declaration[2]!.trim();
      if (!isSafeThemeValue(value)) continue;
      for (const mode of modes) out[mode][declaration[1]!] = value;
    }
  }
  return out;
}

export const THEME_OPTION_ATTR_PREFIX = "data-theme-option-";

export type ThemeOptionValue = string | boolean | number | Readonly<Partial<ThemedColor>>;
export type ThemeOptionValues = Readonly<Record<string, ThemeOptionValue>>;

export const HEX_COLOR = /^#(?:[0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/i;

/** The value an option has: the person's choice when it is still valid, else the default. */
export function themeOptionValue(option: ThemeOption, chosen: unknown): ThemeOptionValue {
  switch (option.type) {
    case "heading":
      return "";
    case "toggle":
      return typeof chosen === "boolean" ? chosen : option.default;
    case "select":
    case "variable-select":
      return typeof chosen === "string" && option.choices.some((c) => c.value === chosen) ? chosen : option.default;
    case "color": {
      const pick = (mode: keyof ThemedColor) => {
        const value = chosen && typeof chosen === "object" ? (chosen as Partial<ThemedColor>)[mode] : undefined;
        return typeof value === "string" && HEX_COLOR.test(value) ? value : option.default[mode];
      };
      return { light: pick("light"), dark: pick("dark") };
    }
    case "number":
    case "slider": {
      if (typeof chosen !== "number" || !Number.isFinite(chosen)) return option.default;
      return Math.min(option.max ?? Infinity, Math.max(option.min ?? -Infinity, chosen));
    }
    case "text":
      return isSafeThemeValue(chosen) ? chosen : option.default;
    case "font":
      return isSafeFontValue(chosen) ? chosen : option.default;
  }
}

/** The variables the theme's options set, per mode. */
export function themeOptionVariables(
  theme: ThemeManifest,
  chosen: ThemeOptionValues | undefined,
): { light: Record<string, string>; dark: Record<string, string> } {
  const out = { light: {} as Record<string, string>, dark: {} as Record<string, string> };
  for (const option of theme.options ?? []) {
    const name = `--${option.id}`;
    const value = themeOptionValue(option, chosen?.[option.id]);
    if (option.type === "color") {
      const color = value as ThemedColor;
      out.light[name] = color.light;
      out.dark[name] = color.dark;
    } else if (option.type === "number" || option.type === "slider") {
      out.light[name] = out.dark[name] = `${value}${option.unit ?? ""}`;
    } else if (option.type === "text" || option.type === "font" || option.type === "variable-select") {
      out.light[name] = out.dark[name] = value as string;
    }
  }
  return out;
}

/** `data-theme-option-*` attributes for <html>; `null` means absent. */
export function themeOptionAttributes(
  theme: ThemeManifest,
  chosen: ThemeOptionValues | undefined,
): Record<string, string | null> {
  const attributes: Record<string, string | null> = {};
  for (const option of theme.options ?? []) {
    if (option.type !== "toggle" && option.type !== "select") continue;
    const value = themeOptionValue(option, chosen?.[option.id]);
    attributes[`${THEME_OPTION_ATTR_PREFIX}${option.id}`] =
      value === true ? "on" : value === false ? null : String(value);
  }
  return attributes;
}

/** Picks the text for `locale`: exact match, then its language, then English, then any. */
export function localizedText(text: LocalizedText | undefined, locale: string): string {
  if (text === undefined) return "";
  if (typeof text === "string") return text;
  const language = locale.split("-")[0] ?? locale;
  return (
    text[locale] ??
    text[language] ??
    Object.entries(text).find(([key]) => key.startsWith(`${language}-`))?.[1] ??
    text.en ??
    Object.values(text)[0] ??
    ""
  );
}
