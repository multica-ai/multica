import { isSafeThemeAsset, sanitizeThemeCss, themeAssetPath } from "./theme-css";
import {
  THEME_API_VERSION,
  THEME_TOKENS,
  findBuiltInTheme,
  isSafeThemeValue,
  isSafeTokenValue,
  type LocalizedText,
  type ThemeManifest,
  type ThemeOption,
  type ThemeTokens,
} from "./themes";

/**
 * Reads a theme package: a JSON object in the ThemeManifest shape, from a
 * file someone picked or from storage. Everything in it is untrusted, so the
 * result is rebuilt field by field from what passes validation; unknown
 * fields are dropped. Unknown token names are ignored rather than rejected,
 * so a theme written for a newer app still loads.
 */

export const THEME_PACKAGE_MAX_BYTES = 1024 * 1024;
const MAX_ASSETS = 24;
const MAX_OPTIONS = 12;
const MAX_CHOICES = 8;

export const THEME_ID_PATTERN = /^[a-z0-9][a-z0-9-]{0,39}$/;
const OPTION_ID = /^[a-z][a-z0-9-]{0,31}$/;
const CHOICE_VALUE = /^[a-z0-9][a-z0-9-]{0,31}$/;
const LOCALE_KEY = /^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$/;

export type ThemePackageResult =
  | { ok: true; theme: ThemeManifest }
  | { ok: false; errors: string[] };

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

function shortText(value: unknown, max: number): string | undefined {
  if (typeof value !== "string") return undefined;
  const text = value.trim();
  return text.length > 0 && text.length <= max ? text : undefined;
}

function localized(value: unknown, max: number): LocalizedText | undefined {
  if (typeof value === "string") return shortText(value, max);
  if (!isRecord(value)) return undefined;
  const out: Record<string, string> = {};
  for (const [key, text] of Object.entries(value)) {
    const clean = shortText(text, max);
    if (LOCALE_KEY.test(key) && clean) out[key] = clean;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function tokens(value: unknown): ThemeTokens {
  if (!isRecord(value)) return {};
  const out: ThemeTokens = {};
  for (const name of THEME_TOKENS) {
    if (isSafeTokenValue(name, value[name])) out[name] = value[name] as string;
  }
  return out;
}

function option(value: unknown, errors: string[]): ThemeOption | null {
  if (!isRecord(value)) return null;
  const id = typeof value.id === "string" && OPTION_ID.test(value.id) ? value.id : null;
  const label = localized(value.label, 60);
  if (!id || !label) {
    errors.push(`Option ${JSON.stringify(value.id)} needs an id (a-z, 0-9, -) and a label`);
    return null;
  }
  const description = localized(value.description, 200);
  if (value.type === "toggle") {
    return { id, type: "toggle", label, description, default: value.default === true };
  }
  if (value.type === "select" && Array.isArray(value.choices)) {
    const choices = value.choices
      .slice(0, MAX_CHOICES)
      .filter(isRecord)
      .flatMap((choice) => {
        const v = typeof choice.value === "string" && CHOICE_VALUE.test(choice.value) ? choice.value : null;
        const l = localized(choice.label, 40);
        if (!v || !l) return [];
        return [{ value: v, label: l, swatch: isSafeThemeValue(choice.swatch) ? choice.swatch : undefined }];
      });
    if (choices.length < 2) {
      errors.push(`Option "${id}" needs at least two valid choices`);
      return null;
    }
    const fallback = choices[0]!.value;
    const preferred = typeof value.default === "string" ? value.default : fallback;
    return {
      id,
      type: "select",
      label,
      description,
      default: choices.some((c) => c.value === preferred) ? preferred : fallback,
      choices,
    };
  }
  errors.push(`Option "${id}" has an unknown type`);
  return null;
}

export function parseThemePackage(input: unknown): ThemePackageResult {
  const errors: string[] = [];
  if (!isRecord(input)) return { ok: false, errors: ["A theme is a JSON object"] };
  if (JSON.stringify(input).length > THEME_PACKAGE_MAX_BYTES) {
    return { ok: false, errors: [`A theme can be at most ${THEME_PACKAGE_MAX_BYTES / 1024} KB`] };
  }
  if (input.apiVersion !== THEME_API_VERSION) {
    errors.push(`apiVersion must be ${THEME_API_VERSION}`);
  }
  const id = typeof input.id === "string" && THEME_ID_PATTERN.test(input.id) ? input.id : null;
  if (!id) errors.push("id must be 1–40 characters of a-z, 0-9 and -");
  else if (findBuiltInTheme(id)) errors.push(`"${id}" is a built-in theme id`);
  const name = shortText(input.name, 40);
  if (!name) errors.push("name is required (at most 40 characters)");
  if (!isRecord(input.tokens)) errors.push("tokens.light and tokens.dark are required");

  const assets: Record<string, string> = {};
  if (input.assets !== undefined) {
    if (!isRecord(input.assets) || Object.keys(input.assets).length > MAX_ASSETS) {
      errors.push(`assets must map at most ${MAX_ASSETS} relative paths to data: URLs`);
    } else {
      for (const [path, url] of Object.entries(input.assets)) {
        const clean = themeAssetPath(path);
        if (!clean || !isSafeThemeAsset(url)) {
          errors.push(`Asset ${JSON.stringify(path)} must be a relative path to a base64 font or image`);
        } else assets[clean] = url;
      }
    }
  }

  let css: string | undefined;
  if (input.css !== undefined) {
    if (typeof input.css !== "string") errors.push("css must be a string");
    else {
      const result = sanitizeThemeCss(input.css, assets);
      if (result.ok) css = input.css;
      else errors.push(...result.errors.map((e) => `css line ${e.line}: ${e.message}`));
    }
  }

  const options: ThemeOption[] = [];
  if (input.options !== undefined) {
    if (!Array.isArray(input.options) || input.options.length > MAX_OPTIONS) {
      errors.push(`options must be a list of at most ${MAX_OPTIONS}`);
    } else {
      for (const raw of input.options) {
        const parsed = option(raw, errors);
        if (parsed && !options.some((o) => o.id === parsed.id)) options.push(parsed);
      }
    }
  }

  if (errors.length > 0 || !id || !name) return { ok: false, errors };
  const rawTokens = input.tokens as Record<string, unknown>;
  const frame = isRecord(input.frame) ? input.frame : undefined;
  return {
    ok: true,
    theme: {
      apiVersion: THEME_API_VERSION,
      id,
      name,
      author: shortText(input.author, 60),
      version: shortText(input.version, 20),
      description: shortText(input.description, 200),
      tokens: { light: tokens(rawTokens.light), dark: tokens(rawTokens.dark) },
      frame: frame ? { light: tokens(frame.light), dark: tokens(frame.dark) } : undefined,
      options: options.length > 0 ? options : undefined,
      css,
      assets: Object.keys(assets).length > 0 ? assets : undefined,
    },
  };
}

const compiledCss = new WeakMap<ThemeManifest, string>();

/**
 * The theme's CSS ready to inject: checked again and with asset references
 * resolved. Empty when the theme has none or it no longer passes.
 */
export function themeCss(theme: ThemeManifest): string {
  if (!theme.css) return "";
  const cached = compiledCss.get(theme);
  if (cached !== undefined) return cached;
  const result = sanitizeThemeCss(theme.css, theme.assets);
  const css = result.ok ? result.css : "";
  compiledCss.set(theme, css);
  return css;
}
