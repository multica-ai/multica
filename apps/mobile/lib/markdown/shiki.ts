/**
 * Shiki syntax highlighter for code blocks. Web↔mobile color parity is the
 * goal — same engine (Shiki), same themes (`github-light` / `github-dark`),
 * different surface (mobile renders into RN `<Text>` runs instead of HTML).
 *
 * Architecture:
 *   - One singleton `HighlighterCore` per app process (Shiki's docs are
 *     emphatic: never instantiate per component). Lazy-initialized; the
 *     first highlight() call triggers init, subsequent calls reuse the
 *     same Promise.
 *   - Native engine via `react-native-shiki-engine` (JSI + Oniguruma C++).
 *     Requires the New Architecture, which Multica mobile already runs on.
 *   - Top 12 languages are pre-registered at init for an AI/dev-tooling
 *     surface (TS/JS/TSX/JSX/Python/Go/Rust/Bash/JSON/YAML/SQL/Markdown).
 *     Unknown languages return `null` from highlight() so the caller
 *     degrades to plain monospace text — never crashes.
 *
 * Boot path:
 *   `prewarmHighlighter()` from app/_layout.tsx fires the init promise
 *   during app start, so by the time the user opens an issue with a code
 *   block, the highlighter is usually ready and there's no first-paint
 *   "plain → highlighted" flash.
 */
import {
  createHighlighterCore,
  type HighlighterCore,
  type ThemedToken,
} from "@shikijs/core";
import {
  createNativeEngine,
  isNativeEngineAvailable,
} from "react-native-shiki-engine";

// Themes — same JSON as web's packages/ui/markdown/CodeBlock.tsx so the
// resulting palette is byte-identical.
import githubLight from "@shikijs/themes/github-light";
import githubDark from "@shikijs/themes/github-dark";

// Languages — pre-load top 12. Bundle cost ~150-200 KB JSON, parsed once
// at init. Adding a language: import here, append to LANGS, add to
// KNOWN_LANGS.
import bash from "@shikijs/langs/bash";
import go from "@shikijs/langs/go";
import javascript from "@shikijs/langs/javascript";
import json from "@shikijs/langs/json";
import jsx from "@shikijs/langs/jsx";
import markdown from "@shikijs/langs/markdown";
import python from "@shikijs/langs/python";
import rust from "@shikijs/langs/rust";
import sql from "@shikijs/langs/sql";
import tsx from "@shikijs/langs/tsx";
import typescript from "@shikijs/langs/typescript";
import yaml from "@shikijs/langs/yaml";

const LANGS = [
  bash, go, javascript, json, jsx, markdown,
  python, rust, sql, tsx, typescript, yaml,
];

// Common aliases users type in fence info strings — `ts` → `typescript`,
// `sh` / `zsh` → `bash`, etc. Mirrors the alias map web uses
// (packages/ui/markdown/CodeBlock.tsx).
const LANG_ALIASES: Record<string, string> = {
  ts: "typescript",
  js: "javascript",
  py: "python",
  rs: "rust",
  sh: "bash",
  zsh: "bash",
  shell: "bash",
  yml: "yaml",
  md: "markdown",
};

const KNOWN_LANGS: ReadonlySet<string> = new Set([
  "bash", "go", "javascript", "json", "jsx", "markdown",
  "python", "rust", "sql", "tsx", "typescript", "yaml",
]);

export const SHIKI_THEME_LIGHT = "github-light";
export const SHIKI_THEME_DARK = "github-dark";

// Cached promise — null on cold start, set on first init call.
let highlighterPromise: Promise<HighlighterCore | null> | null = null;

function initHighlighter(): Promise<HighlighterCore | null> {
  if (!isNativeEngineAvailable()) {
    // Native module didn't link — usually means dev client wasn't rebuilt
    // after install. Fall back silently to plain text so the app still
    // ships content; the warning is for the developer.
    console.warn(
      "[shiki] react-native-shiki-engine native module unavailable — code blocks will render plain. Did you rebuild the dev client?",
    );
    return Promise.resolve(null);
  }
  return createHighlighterCore({
    themes: [githubLight, githubDark],
    langs: LANGS,
    engine: createNativeEngine(),
  }).catch((err) => {
    console.warn("[shiki] highlighter init failed:", err);
    return null;
  });
}

/** Kick off the singleton init. Call once at app boot. Idempotent: repeat
 *  calls reuse the cached promise. */
export function prewarmHighlighter(): void {
  highlighterPromise ??= initHighlighter();
}

/** Resolve a fence info string ("ts", "JavaScript ", "py") to a known
 *  Shiki language id, or null if we don't have a grammar for it. Caller
 *  uses null to degrade to plain text. */
export function resolveLang(input: string | undefined): string | null {
  if (!input) return null;
  const lower = input.toLowerCase().trim();
  const resolved = LANG_ALIASES[lower] ?? lower;
  return KNOWN_LANGS.has(resolved) ? resolved : null;
}

export interface HighlightedToken {
  content: string;
  /** Hex color from the active Shiki theme. May be undefined for whitespace
   *  or default-colored tokens — caller leaves them at the inherited color. */
  color?: string;
}

export interface HighlightedLine {
  tokens: HighlightedToken[];
}

/** Highlight `code` and return token runs grouped by line. Returns null
 *  when the engine is unavailable, the language is unknown, or any error
 *  occurs — caller is expected to fall back to plain monospace. */
export async function highlight(
  code: string,
  lang: string,
  theme: string,
): Promise<HighlightedLine[] | null> {
  highlighterPromise ??= initHighlighter();
  const hl = await highlighterPromise;
  if (!hl) return null;
  try {
    const tokens = hl.codeToTokensBase(code, { lang, theme });
    return tokens.map((line: ThemedToken[]) => ({
      tokens: line.map((t) => ({ content: t.content, color: t.color })),
    }));
  } catch (err) {
    console.warn(`[shiki] highlight failed for lang=${lang}:`, err);
    return null;
  }
}
