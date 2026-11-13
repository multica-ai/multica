import { EMBED_TOKENS, embedTokenDeclarations } from "@multica/core/appearance";
import type { EmbedTheme } from "../../common/embed-theme";
import { withFragmentNavShim } from "./iframe-fragment-nav";

/**
 * Document assembly for the ```html dynamic block (MUL-7649).
 *
 * The block sizes itself to its content and explains a script error in place,
 * which needs the sandboxed document to talk back. A sandbox without
 * `allow-same-origin` blocks every parent-side read of the frame, but
 * `postMessage` still crosses it, so a tiny bridge script is placed in front
 * of the author's markup. It reports two things: the content height and the
 * first uncaught error. The parent treats both as untrusted input — it checks
 * the message source and clamps every value (see readHtmlBlockMessage).
 *
 * The prefix also carries the app's theme tokens as CSS custom properties (the
 * embed token contract, EMBED_TOKENS), so HTML written against
 * `var(--foreground)` / `var(--chart-1)` follows the theme and light and dark
 * mode. Using a token is the opt-in: only then does the document also get
 * the app's `color-scheme`. That matters in dark mode, where a light document
 * inside a dark page is painted on an opaque white canvas — right for HTML
 * that was written for white, wrong for HTML that asked for the theme. HTML
 * that uses no token is left exactly as written: no reset, no scheme.
 */

/** Marker on every bridge message; anything without it is ignored. */
export const HTML_BLOCK_MESSAGE_KEY = "__multicaHtmlBlock";

// The bridge is one line on purpose: it sits on the author's first line, so a
// script error's line number still points at the author's own source.
//
// Height is measured from what the body contains — its text and its child
// elements' boxes plus their bottom margins — not from the document box. A
// body with `min-height: 100vh` is as tall as whatever the frame is set to, so
// measuring the box would report the frame's own height back and grow it
// forever. Margins are included because coming up short puts a scrollbar
// inside the frame, while a few spare pixels are invisible.
const BRIDGE_SCRIPT = [
  "(function(){",
  `var K=${JSON.stringify(HTML_BLOCK_MESSAGE_KEY)};`,
  "function post(m){m[K]=1;try{parent.postMessage(m,'*')}catch(_){}}",
  "function px(v){return parseFloat(v)||0}",
  "var last=-1;",
  "function measure(){var b=document.body;if(!b)return;var bs=getComputedStyle(b);",
  "var range=document.createRange();range.selectNodeContents(b);var rr=range.getBoundingClientRect();",
  "var bottom=(rr.width||rr.height)?rr.bottom:0;var kids=b.children;",
  "for(var i=0;i<kids.length;i++){var k=kids[i];var ks=getComputedStyle(k);",
  "if(ks.display==='none'||ks.position==='fixed')continue;var r=k.getBoundingClientRect();",
  "if(r.width||r.height)bottom=Math.max(bottom,r.bottom+px(ks.marginBottom))}",
  "var h=bottom?Math.ceil(bottom+(window.scrollY||0)+px(bs.paddingBottom)+px(bs.borderBottomWidth)+px(bs.marginBottom)):0;",
  "if(h!==last){last=h;post({type:'height',height:h})}}",
  "window.addEventListener('message',function(e){var m=e.data;",
  "if(e.source!==parent||!m||m[K]!==1||m.type!=='measure')return;last=-1;measure()});",
  "var failed=false;",
  "window.addEventListener('error',function(e){if(failed)return;var t=e.target;",
  "if(t&&t!==window&&t.tagName){if(t.tagName!=='SCRIPT')return;failed=true;",
  "post({type:'error',message:'Failed to load '+(t.getAttribute('src')||'a script'),line:0});return}",
  "failed=true;post({type:'error',message:String(e.message||'Script error'),line:e.lineno||0})},true);",
  "function watch(){measure();if(typeof ResizeObserver==='function'){var o=new ResizeObserver(measure);",
  "o.observe(document.documentElement);if(document.body)o.observe(document.body)}}",
  "if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',watch);else watch();",
  "window.addEventListener('load',measure)",
  "})();",
].join("");

/** Exposed for tests. */
export const __HTML_BLOCK_BRIDGE__ = BRIDGE_SCRIPT;

const DOCTYPE_RE = /^\s*<!doctype[^>]*>/i;

/**
 * The host's theme as the sandbox receives it.
 */
export interface HtmlBlockTheme {
  /** `:root` declarations for the theme tokens. */
  declarations: string;
  colorScheme: "light" | "dark";
}

/** Read the host's tokens with `readEmbedTheme(host, HTML_BLOCK_EMBED_OPTIONS)`. */
export const HTML_BLOCK_EMBED_OPTIONS = {
  // Inside a block the page is the block: a document that paints
  // `var(--background)` sits flush in its frame instead of showing a slightly
  // different panel.
  background: "--surface",
} as const;

export function htmlBlockTheme({ tokens, colorScheme }: EmbedTheme): HtmlBlockTheme {
  return { declarations: embedTokenDeclarations(tokens), colorScheme };
}

const THEME_TOKEN_USE = new RegExp(
  `var\\(\\s*(?:${EMBED_TOKENS.map((token) => token.name).join("|")})(?![\\w-])`,
);

/** Whether the HTML reads any theme token, which opts it into the app's scheme. */
export function usesThemeTokens(html: string): boolean {
  return THEME_TOKEN_USE.test(html);
}

const THEME_TOKEN_MENTION = new RegExp(
  `(?:${EMBED_TOKENS.map((token) => token.name).join("|")})(?![\\w-])`,
);

/**
 * Whether the HTML names a theme token anywhere, in CSS or in a script's
 * `getPropertyValue`. HTML that names none cannot read the theme.
 */
export function mentionsThemeTokens(html: string): boolean {
  return THEME_TOKEN_MENTION.test(html);
}

/** The theme for a document that reads none of it. */
export const NO_HTML_BLOCK_THEME: HtmlBlockTheme = { declarations: "", colorScheme: "light" };

/**
 * The srcdoc for a ```html block: theme tokens and the bridge in front of the
 * author's markup (after a leading doctype, which must stay first), and the
 * fragment-navigation shim after it.
 */
export function buildHtmlBlockDocument(html: string, theme: HtmlBlockTheme): string {
  const root = usesThemeTokens(html)
    ? `${theme.declarations};color-scheme:${theme.colorScheme}`
    : theme.declarations;
  const prefix = `<style>:root{${root}}</style><script>${BRIDGE_SCRIPT}</script>`;
  const doctype = DOCTYPE_RE.exec(html)?.[0];
  const body = doctype
    ? doctype + prefix + html.slice(doctype.length)
    : prefix + html;
  return withFragmentNavShim(body);
}

export type HtmlBlockMessage =
  | { type: "height"; height: number }
  | { type: "error"; message: string; line: number };

/** Upper bound on any reported height; a runaway document stops here. */
export const HTML_BLOCK_MAX_CONTENT_PX = 10_000;
const MAX_ERROR_LENGTH = 500;

/**
 * Parses a bridge message. Returns null for anything that is not one; values
 * are clamped because the sandboxed document can post whatever it likes.
 */
export function readHtmlBlockMessage(data: unknown): HtmlBlockMessage | null {
  if (typeof data !== "object" || data === null) return null;
  const record = data as Record<string, unknown>;
  if (record[HTML_BLOCK_MESSAGE_KEY] !== 1) return null;
  if (record.type === "height") {
    const height = record.height;
    if (typeof height !== "number" || !Number.isFinite(height)) return null;
    return {
      type: "height",
      height: Math.min(Math.max(Math.ceil(height), 0), HTML_BLOCK_MAX_CONTENT_PX),
    };
  }
  if (record.type === "error") {
    const message = typeof record.message === "string" ? record.message : "";
    const line = typeof record.line === "number" && Number.isFinite(record.line) ? record.line : 0;
    return {
      type: "error",
      message: message.slice(0, MAX_ERROR_LENGTH),
      line: Math.max(0, Math.floor(line)),
    };
  }
  return null;
}

/**
 * Height tracking with a guard against documents whose height follows the
 * frame (a child sized in `vh`, say): each resize makes such a document grow by
 * the same step, forever. Three growths in a row by the same step read as that
 * loop, and the height stops there.
 */
export interface HtmlBlockHeight {
  height: number | null;
  step: number;
  streak: number;
  frozen: boolean;
}

export const INITIAL_HTML_BLOCK_HEIGHT: HtmlBlockHeight = {
  height: null,
  step: 0,
  streak: 0,
  frozen: false,
};

const RUNAWAY_STREAK = 3;

export function nextHtmlBlockHeight(state: HtmlBlockHeight, reported: number): HtmlBlockHeight {
  if (state.frozen || reported === state.height) return state;
  if (state.height == null || reported < state.height) {
    return { height: reported, step: 0, streak: 0, frozen: false };
  }
  const step = reported - state.height;
  const streak = Math.abs(step - state.step) <= 1 ? state.streak + 1 : 1;
  if (streak >= RUNAWAY_STREAK) return { ...state, frozen: true };
  return { height: reported, step, streak, frozen: false };
}
