import { findBuiltInTheme } from "./built-in-themes";
import { readZip, type ArchiveFile } from "./theme-archive";
import { isSafeThemeAsset, sanitizeThemeCss, themeAssetPath } from "./theme-css";
import {
  HEX_COLOR,
  THEME_API_VERSION,
  isSafeFontValue,
  isSafeThemeValue,
  type LocalizedText,
  type Theme,
  type ThemeOption,
} from "./themes";

/**
 * Reads a theme package: `manifest.json`, `theme.css` and the fonts and
 * images the CSS references, from a folder or a .zip someone picked, or from
 * storage. Everything in it is untrusted, so the result is rebuilt field by
 * field from what passes validation; unknown manifest fields are dropped.
 */

export const THEME_PACKAGE_MAX_BYTES = 1024 * 1024;
const MAX_ASSETS = 24;
const MAX_OPTIONS = 24;
const MAX_CHOICES = 8;

export const THEME_ID_PATTERN = /^[a-z0-9][a-z0-9-]{0,39}$/;
const OPTION_ID = /^[a-z][a-z0-9-]{0,47}$/;
const CHOICE_VALUE = /^[a-z0-9][a-z0-9-]{0,31}$/;
const UNIT = /^(?:[a-z]{1,4}|%)?$/;
const LOCALE_KEY = /^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$/;

export type ThemePackageResult = { ok: true; theme: Theme } | { ok: false; errors: string[] };

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

function option(value: unknown, errors: string[]): ThemeOption | null {
  if (!isRecord(value)) return null;
  const id = typeof value.id === "string" && OPTION_ID.test(value.id) ? value.id : null;
  const label = localized(value.label, 60);
  if (!id || !label) {
    errors.push(`Option ${JSON.stringify(value.id)} needs an id (a-z, 0-9, -) and a label`);
    return null;
  }
  const description = localized(value.description, 200);
  const base = { id, label, description };
  if (value.type === "heading") return { ...base, type: "heading" };
  if (value.type === "color") {
    const def = value.default;
    const light = typeof def === "string" ? def : isRecord(def) ? def.light : undefined;
    const dark = typeof def === "string" ? def : isRecord(def) ? def.dark : undefined;
    if (typeof light !== "string" || typeof dark !== "string" || !HEX_COLOR.test(light) || !HEX_COLOR.test(dark)) {
      errors.push(`Option "${id}" needs a hex default, or one for light and one for dark`);
      return null;
    }
    return { ...base, type: "color", default: { light, dark } };
  }
  if (value.type === "number" || value.type === "slider") {
    const num = (v: unknown) => (typeof v === "number" && Number.isFinite(v) ? v : undefined);
    const min = num(value.min);
    const max = num(value.max);
    const step = num(value.step);
    const def = num(value.default);
    const unit = typeof value.unit === "string" && UNIT.test(value.unit) ? value.unit : undefined;
    if (
      def === undefined ||
      (value.unit !== undefined && unit === undefined) ||
      (step !== undefined && step <= 0) ||
      (min !== undefined && max !== undefined && min >= max) ||
      (value.type === "slider" && (min === undefined || max === undefined))
    ) {
      errors.push(`Option "${id}" needs a numeric default${value.type === "slider" ? ", min and max" : ""} and a unit of letters or %`);
      return null;
    }
    return { ...base, type: value.type, default: def, min, max, step, unit };
  }
  if (value.type === "text" || value.type === "font") {
    const valid = value.type === "font" ? isSafeFontValue(value.default) : isSafeThemeValue(value.default);
    if (!valid) {
      errors.push(`Option "${id}" needs a default ${value.type === "font" ? "font stack" : "CSS value"}`);
      return null;
    }
    return { ...base, type: value.type, default: value.default as string };
  }
  if (value.type === "variable-select" && Array.isArray(value.choices)) {
    const choices = value.choices
      .slice(0, MAX_CHOICES)
      .filter(isRecord)
      .flatMap((choice) => {
        const l = localized(choice.label, 40);
        return isSafeThemeValue(choice.value) && l ? [{ value: choice.value, label: l }] : [];
      });
    if (choices.length < 2) {
      errors.push(`Option "${id}" needs at least two valid choices`);
      return null;
    }
    const preferred = typeof value.default === "string" ? value.default : choices[0]!.value;
    return {
      ...base,
      type: "variable-select",
      default: choices.some((c) => c.value === preferred) ? preferred : choices[0]!.value,
      choices,
    };
  }
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

export interface ThemePackageInput {
  /** The parsed manifest.json. */
  manifest: unknown;
  /** theme.css */
  css: unknown;
  assets?: unknown;
}

export function parseTheme({ manifest, css, assets: rawAssets }: ThemePackageInput): ThemePackageResult {
  const errors: string[] = [];
  if (!isRecord(manifest)) return { ok: false, errors: ["manifest.json must be a JSON object"] };
  if (typeof css !== "string") return { ok: false, errors: ["theme.css is missing"] };
  if (manifest.apiVersion !== THEME_API_VERSION) {
    errors.push(`apiVersion must be ${THEME_API_VERSION}`);
  }
  const id = typeof manifest.id === "string" && THEME_ID_PATTERN.test(manifest.id) ? manifest.id : null;
  if (!id) errors.push("id must be 1–40 characters of a-z, 0-9 and -");
  else if (findBuiltInTheme(id)) errors.push(`"${id}" is a built-in theme id`);
  const name = shortText(manifest.name, 40);
  if (!name) errors.push("name is required (at most 40 characters)");

  const assets: Record<string, string> = {};
  let size = css.length;
  if (rawAssets !== undefined) {
    if (!isRecord(rawAssets) || Object.keys(rawAssets).length > MAX_ASSETS) {
      errors.push(`A theme can ship at most ${MAX_ASSETS} fonts and images`);
    } else {
      for (const [path, url] of Object.entries(rawAssets)) {
        const clean = themeAssetPath(path);
        if (!clean || !isSafeThemeAsset(url)) {
          errors.push(`${JSON.stringify(path)} must be a font or image inside the theme folder`);
        } else {
          assets[clean] = url;
          size += url.length;
        }
      }
    }
  }
  if (size > THEME_PACKAGE_MAX_BYTES) {
    return { ok: false, errors: [`A theme can be at most ${THEME_PACKAGE_MAX_BYTES / 1024} KB`] };
  }

  const checked = sanitizeThemeCss(css, assets);
  if (!checked.ok) errors.push(...checked.errors.map((e) => `theme.css line ${e.line}: ${e.message}`));

  const options: ThemeOption[] = [];
  if (manifest.options !== undefined) {
    if (!Array.isArray(manifest.options) || manifest.options.length > MAX_OPTIONS) {
      errors.push(`options must be a list of at most ${MAX_OPTIONS}`);
    } else {
      for (const raw of manifest.options) {
        const parsed = option(raw, errors);
        if (parsed && !options.some((o) => o.id === parsed.id)) options.push(parsed);
      }
    }
  }

  if (errors.length > 0 || !id || !name) return { ok: false, errors };
  return {
    ok: true,
    theme: {
      apiVersion: THEME_API_VERSION,
      id,
      name,
      author: shortText(manifest.author, 60),
      version: shortText(manifest.version, 20),
      description: shortText(manifest.description, 200),
      options: options.length > 0 ? options : undefined,
      css,
      assets: Object.keys(assets).length > 0 ? assets : undefined,
    },
  };
}

/** A theme as it is stored: the manifest fields with `css` and `assets` alongside. */
export function parseStoredTheme(stored: unknown): ThemePackageResult {
  const record = isRecord(stored) ? stored : {};
  return parseTheme({ manifest: record, css: record.css, assets: record.assets });
}

const ASSET_TYPES: Record<string, string> = {
  woff2: "font/woff2",
  woff: "font/woff",
  ttf: "font/ttf",
  otf: "font/otf",
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  webp: "image/webp",
  gif: "image/gif",
  avif: "image/avif",
  svg: "image/svg+xml",
};

function base64(data: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < data.length; i += 0x8000) {
    binary += String.fromCharCode(...data.subarray(i, i + 0x8000));
  }
  return btoa(binary);
}

const IGNORED = /(^|\/)(\.|__MACOSX\/)|(^|\/)(README|LICENSE)[^/]*$/i;

/**
 * Builds a theme from its files: a picked folder, several picked files, or
 * the contents of a .zip. The files may sit inside one top-level folder.
 */
export function themeFromFiles(files: readonly ArchiveFile[]): ThemePackageResult {
  const kept = files.filter((f) => !IGNORED.test(f.path));
  const manifestFile = kept.find((f) => f.path === "manifest.json" || f.path.endsWith("/manifest.json"));
  if (!manifestFile) return { ok: false, errors: ["manifest.json is missing"] };
  const root = manifestFile.path.slice(0, -"manifest.json".length);
  const decoder = new TextDecoder();
  let manifest: unknown;
  try {
    manifest = JSON.parse(decoder.decode(manifestFile.data));
  } catch {
    return { ok: false, errors: ["manifest.json is not valid JSON"] };
  }
  const cssFile = kept.find((f) => f.path === `${root}theme.css`);
  const assets: Record<string, string> = {};
  for (const file of kept) {
    if (!file.path.startsWith(root) || file === manifestFile || file === cssFile) continue;
    const type = ASSET_TYPES[file.path.split(".").pop()?.toLowerCase() ?? ""];
    if (type) assets[file.path.slice(root.length)] = `data:${type};base64,${base64(file.data)}`;
  }
  return parseTheme({ manifest, css: cssFile ? decoder.decode(cssFile.data) : undefined, assets });
}

/** Reads what someone picked: one .zip, or the files of a theme folder. */
export async function readThemePackage(files: readonly File[]): Promise<ThemePackageResult> {
  const total = files.reduce((sum, f) => sum + f.size, 0);
  if (total > THEME_PACKAGE_MAX_BYTES) {
    return { ok: false, errors: [`A theme can be at most ${THEME_PACKAGE_MAX_BYTES / 1024} KB`] };
  }
  const bytes = async (file: File) => new Uint8Array(await file.arrayBuffer());
  if (files.length === 1 && /\.zip$/i.test(files[0]!.name)) {
    const archive = await readZip(await bytes(files[0]!), THEME_PACKAGE_MAX_BYTES);
    return archive.ok ? themeFromFiles(archive.files) : { ok: false, errors: [archive.error] };
  }
  const entries = await Promise.all(
    files.map(async (file) => ({ path: file.webkitRelativePath || file.name, data: await bytes(file) })),
  );
  return themeFromFiles(entries);
}

const compiledCss = new WeakMap<Theme, string>();

/**
 * The theme's CSS ready to inject: checked again and with asset references
 * resolved. Empty when it no longer passes.
 */
export function themeCss(theme: Theme): string {
  if (!theme.css) return "";
  const cached = compiledCss.get(theme);
  if (cached !== undefined) return cached;
  const result = sanitizeThemeCss(theme.css, theme.assets);
  const css = result.ok ? result.css : "";
  compiledCss.set(theme, css);
  return css;
}
