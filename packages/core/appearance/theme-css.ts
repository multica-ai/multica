/**
 * Checks and rewrites the stylesheet a theme ships (`ThemeManifest.css`).
 *
 * A theme's CSS runs on every product page, so anything that makes the
 * browser fetch a URL could leak page content: attribute selectors can match
 * text one character at a time and request a different URL per match. Pages
 * have no CSP to stop that (and `img-src` must stay open for images in
 * comments), so this check is the only guard. It rejects every construct
 * that loads a resource, except `url()` pointing at a file the theme package
 * ships, which is rewritten to that file's `data:` URL.
 *
 * The scanner follows the CSS Syntax tokenizer closely enough to see what a
 * browser sees, including escapes (`u\72 l(`), comments and strings. Where it
 * differs, it errs towards finding more function names than a browser would,
 * which can only reject more.
 */

export const THEME_CSS_MAX_LENGTH = 512 * 1024;

export interface ThemeCssError {
  line: number;
  message: string;
}

export type ThemeCssResult = { ok: true; css: string } | { ok: false; errors: ThemeCssError[] };

const ASSET_DATA_URL =
  /^data:(?:font\/(?:woff2|woff|ttf|otf)|application\/font-woff2?|image\/(?:png|jpeg|webp|gif|avif|svg\+xml));base64,[A-Za-z0-9+/]+={0,2}$/;

/** A `data:` URL a theme may ship as an asset: fonts and images, base64. */
export function isSafeThemeAsset(value: unknown): value is string {
  return typeof value === "string" && ASSET_DATA_URL.test(value);
}

/** Normalises `./fonts/a.woff2` to `fonts/a.woff2`; null for anything outside the package. */
export function themeAssetPath(ref: string): string | null {
  const path = ref.trim().replace(/^\.\//, "");
  if (!path || path.startsWith("/") || path.includes("\\") || path.includes(":")) return null;
  if (path.split("/").some((part) => part === ".." || part === "." || part === "")) return null;
  return path;
}

const BLOCKED_FUNCTION = /^(?:-[a-z]+-)?(?:image-set|cross-fade|element|image)$|^(?:expression|src)$/;
const BLOCKED_IDENT = new Set(["behavior", "-ms-behavior", "-moz-binding", "url"]);
const BLOCKED_AT_RULE = new Set(["import"]);

const isNewline = (c: string) => c === "\n" || c === "\r" || c === "\f";
const isWhitespace = (c: string) => c === " " || c === "\t" || isNewline(c);
const isHex = (c: string) => /^[0-9a-fA-F]$/.test(c);
const isNameStart = (c: string | undefined) =>
  c !== undefined && (/^[A-Za-z_]$/.test(c) || c.charCodeAt(0) >= 0x80);
const isName = (c: string | undefined) =>
  c !== undefined && (isNameStart(c) || /^[0-9-]$/.test(c));

export function sanitizeThemeCss(
  css: string,
  assets: Readonly<Record<string, string>> = {},
): ThemeCssResult {
  if (css.length > THEME_CSS_MAX_LENGTH) {
    return { ok: false, errors: [{ line: 1, message: `CSS is larger than ${THEME_CSS_MAX_LENGTH / 1024} KB` }] };
  }

  const errors: ThemeCssError[] = [];
  const replacements: { start: number; end: number; text: string }[] = [];
  let i = 0;

  const lineAt = (index: number) => css.slice(0, index).split(/\r\n|[\r\n\f]/).length;
  const fail = (index: number, message: string) => errors.push({ line: lineAt(index), message });

  const validEscape = (at: number) => css[at] === "\\" && at + 1 < css.length && !isNewline(css[at + 1]!);

  /** Consumes the escape at `i` (pointing at the backslash) and returns the code point. */
  const consumeEscape = (): string => {
    i += 1;
    const c = css[i];
    if (c === undefined) return "\uFFFD";
    if (isHex(c)) {
      let hex = "";
      while (hex.length < 6 && i < css.length && isHex(css[i]!)) hex += css[i++];
      if (i < css.length && isWhitespace(css[i]!)) {
        i += css[i] === "\r" && css[i + 1] === "\n" ? 2 : 1;
      }
      const code = Number.parseInt(hex, 16);
      return code === 0 || code > 0x10ffff || (code >= 0xd800 && code <= 0xdfff)
        ? "\uFFFD"
        : String.fromCodePoint(code);
    }
    i += 1;
    return c;
  };

  const startsIdent = (at: number) => {
    const c = css[at];
    if (c === "-") return isNameStart(css[at + 1]) || css[at + 1] === "-" || validEscape(at + 1);
    if (c === "\\") return validEscape(at);
    return isNameStart(c);
  };

  const consumeName = (): string => {
    let name = "";
    while (i < css.length) {
      const c = css[i]!;
      if (isName(c)) {
        name += c;
        i += 1;
      } else if (validEscape(i)) {
        name += consumeEscape();
      } else break;
    }
    return name;
  };

  /** Reads a string starting at the quote at `i`; null when a newline ends it early. */
  const consumeString = (): string | null => {
    const quote = css[i];
    i += 1;
    let value = "";
    while (i < css.length) {
      const c = css[i]!;
      if (c === quote) {
        i += 1;
        return value;
      }
      if (isNewline(c)) return null;
      if (c === "\\") {
        if (i + 1 >= css.length) {
          i += 1;
          return value;
        }
        if (isNewline(css[i + 1]!)) {
          i += css[i + 1] === "\r" && css[i + 2] === "\n" ? 3 : 2;
          continue;
        }
        value += consumeEscape();
        continue;
      }
      value += c;
      i += 1;
    }
    return value;
  };

  const skipWhitespace = () => {
    while (i < css.length && isWhitespace(css[i]!)) i += 1;
  };

  /** `url(` has been consumed; `start` is where the function name began. */
  const consumeUrl = (start: number) => {
    skipWhitespace();
    let ref: string | null;
    if (css[i] === '"' || css[i] === "'") {
      ref = consumeString();
      skipWhitespace();
      if (ref === null || css[i] !== ")") {
        fail(start, "Malformed url()");
        return;
      }
    } else {
      ref = "";
      while (i < css.length && css[i] !== ")") {
        const c = css[i]!;
        if (isWhitespace(c)) {
          skipWhitespace();
          if (css[i] !== ")") ref = null;
          break;
        }
        if (c === '"' || c === "'" || c === "(" || c.charCodeAt(0) < 0x20 || c === "\u007f") {
          ref = null;
          break;
        }
        if (c === "\\") {
          if (!validEscape(i)) {
            ref = null;
            break;
          }
          ref += consumeEscape();
          continue;
        }
        ref += c;
        i += 1;
      }
      if (ref === null || css[i] !== ")") {
        fail(start, "Malformed url()");
        while (i < css.length && css[i] !== ")") i += 1;
        i += 1;
        return;
      }
    }
    i += 1;

    if (/^#[\w-]+$/.test(ref)) return;
    const path = themeAssetPath(ref);
    const asset = path ? assets[path] : undefined;
    if (!asset || !isSafeThemeAsset(asset)) {
      fail(start, `url(${JSON.stringify(ref)}) must point to a file in the theme's assets`);
      return;
    }
    replacements.push({ start, end: i, text: `url("${asset}")` });
  };

  while (i < css.length) {
    const c = css[i]!;
    if (c === "/" && css[i + 1] === "*") {
      const end = css.indexOf("*/", i + 2);
      i = end === -1 ? css.length : end + 2;
    } else if (c === '"' || c === "'") {
      const at = i;
      if (consumeString() === null) fail(at, "Unterminated string");
    } else if (c === "@" && startsIdent(i + 1)) {
      const at = i;
      i += 1;
      const name = consumeName().toLowerCase();
      if (BLOCKED_AT_RULE.has(name)) fail(at, `@${name} is not allowed`);
    } else if (startsIdent(i)) {
      const at = i;
      const name = consumeName();
      const lower = name.replace(/[A-Z]/g, (ch) => ch.toLowerCase());
      if (css[i] === "(") {
        i += 1;
        if (lower === "url") consumeUrl(at);
        else if (BLOCKED_FUNCTION.test(lower)) fail(at, `${lower}() is not allowed`);
      } else if (BLOCKED_IDENT.has(lower)) {
        fail(at, `${name} is not allowed`);
      }
    } else {
      i += 1;
    }
  }

  if (errors.length > 0) return { ok: false, errors };
  let out = "";
  let last = 0;
  for (const r of replacements) {
    out += css.slice(last, r.start) + r.text;
    last = r.end;
  }
  return { ok: true, css: out + css.slice(last) };
}
