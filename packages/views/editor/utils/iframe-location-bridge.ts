/**
 * Address bridge for the HTML attachment preview's address bar (MUL-7737).
 *
 * The preview mounts the document as `<iframe sandbox="allow-scripts"
 * srcdoc>`, so its URL is `about:srcdoc` and `location.search` is always
 * empty. A self-contained mock that picks its screen with `?s=overview` —
 * the convention agent-built mocks use — could only ever show its default
 * screen. Relative URLs are no help either: a srcdoc document resolves them
 * against the host page, so `?s=x` or `#x` in a link or a `pushState` call
 * points at the app, not at the document.
 *
 * Chromium and WebKit let an opaque-origin document rewrite the query and
 * fragment of its own URL through the History API, so a script placed in
 * front of the author's markup can give the document a real address before
 * any of the author's scripts read it. The same script:
 *
 *   - resolves `?…` / `#…` arguments to pushState / replaceState against the
 *     document instead of the host page, so they stop throwing;
 *   - reports the address to the parent whenever it changes, so the bar
 *     follows in-document navigation;
 *   - turns a click on a `?…` link into a navigate request for the parent,
 *     which reloads the document at that address, and a click on a `#…` link
 *     the fragment-nav shim left alone (no element with that id — a hash
 *     router's link) into a real hash change;
 *   - applies a hash the parent sends, so a fragment-only address change
 *     navigates in place instead of reloading.
 *
 * It runs in the iframe's own opaque origin with the same capabilities the
 * author's scripts already have; it cannot reach the parent. The parent
 * treats every message as untrusted (see readHtmlPreviewLocationMessage).
 * Where the History API refuses the rewrite, the address stays empty and the
 * document renders exactly as before.
 */

/** Marker on every bridge message, in both directions. */
export const HTML_PREVIEW_LOCATION_KEY = "__multicaHtmlLocation";

/** Upper bound on an address; anything longer is not an address we keep. */
export const HTML_PREVIEW_ADDRESS_MAX_LENGTH = 2048;

// One line, so it sits on the author's first line and the line numbers in a
// script error still point at the author's own source.
function buildScript(initialAddress: string): string {
  // `<` is escaped so an address can never close the script element.
  const initial = JSON.stringify(initialAddress).replace(/</g, "\\u003c");
  return [
    "(function(){",
    `var K=${JSON.stringify(HTML_PREVIEW_LOCATION_KEY)},H=window.history,L=window.location;`,
    "var BASE=L.href.split(/[?#]/)[0],INITIAL=" + initial + ";",
    "function post(m){m[K]=1;try{parent.postMessage(m,'*')}catch(_){}}",
    "function report(){post({type:'location',value:L.search+L.hash})}",
    "if(INITIAL){try{H.replaceState(H.state,'',BASE+INITIAL)}catch(_){}}",
    "function resolve(u){if(typeof u!=='string')return u;var c=u.charAt(0);",
    "if(c==='?')return BASE+u;if(c==='#')return BASE+L.search+u;return u}",
    "['pushState','replaceState'].forEach(function(n){var o=H[n];if(typeof o!=='function')return;",
    "H[n]=function(s,t,u){var r=arguments.length>2?o.call(H,s,t,resolve(u)):o.apply(H,arguments);report();return r}});",
    "window.addEventListener('hashchange',report);window.addEventListener('popstate',report);",
    // On window, in the bubble phase: after the author's handlers and the
    // fragment-nav shim, so a click any of them claimed is left alone.
    "window.addEventListener('click',function(e){",
    "if(e.defaultPrevented||e.button!==0||e.metaKey||e.ctrlKey||e.shiftKey||e.altKey)return;",
    "var t=e.target;if(!t||typeof t.closest!=='function')return;var a=t.closest('a[href]');if(!a)return;",
    "var tg=a.getAttribute('target');if(tg&&tg!=='_self')return;var h=a.getAttribute('href')||'';",
    "if(h.charAt(0)==='?'){e.preventDefault();post({type:'navigate',value:h})}",
    "else if(h.charAt(0)==='#'&&h.length>1){e.preventDefault();L.hash=h}});",
    "window.addEventListener('message',function(e){if(e.source!==parent)return;var d=e.data;",
    "if(!d||d[K]!==1||d.type!=='hash'||typeof d.value!=='string')return;L.hash=d.value});",
    // A document loaded at `#id` lands on that element, as it would in a tab.
    "if(L.hash){window.addEventListener('load',function(){var id;try{id=decodeURIComponent(L.hash.slice(1))}catch(_){return}",
    "var el=id&&document.getElementById(id);if(el)el.scrollIntoView()})}",
    "report();",
    "})();",
  ].join("");
}

/** Exposed for tests so they can evaluate the script in jsdom. */
export function __locationBridgeScript__(initialAddress: string): string {
  return buildScript(initialAddress);
}

// A doctype must stay the first thing in the document or it renders in quirks
// mode; comments may precede it.
const LEADING_DOCTYPE_RE = /^\s*(?:<!--[\s\S]*?-->\s*)*<!doctype[^>]*>/i;

/**
 * The document with the bridge in front of the author's markup (after a
 * leading doctype), loaded at `initialAddress`.
 */
export function withLocationBridge(html: string, initialAddress: string): string {
  const prefix = `<script>${buildScript(initialAddress)}</script>`;
  const doctype = LEADING_DOCTYPE_RE.exec(html)?.[0];
  return doctype ? doctype + prefix + html.slice(doctype.length) : prefix + html;
}

/**
 * What the reader typed, as the query-and-fragment the document is loaded
 * at: `"?s=overview#top"`, or `""` for the bare document. The file name is
 * the fixed part of the address, so a pasted `mock.html?s=x` keeps only its
 * `?s=x`, and text with neither `?` nor `#` is read as a query.
 */
export function normalizeHtmlPreviewAddress(input: string, filename = ""): string {
  const value = input.trim();
  const start = value.search(/[?#]/);
  const address =
    start >= 0 ? value.slice(start) : value === "" || value === filename ? "" : `?${value}`;
  return address.slice(0, HTML_PREVIEW_ADDRESS_MAX_LENGTH);
}

/** An address split into its query and fragment, each with its prefix. */
export function splitHtmlPreviewAddress(address: string): { search: string; hash: string } {
  const at = address.indexOf("#");
  return at === -1
    ? { search: address, hash: "" }
    : { search: address.slice(0, at), hash: address.slice(at) };
}

export type HtmlPreviewLocationMessage =
  | { type: "location"; value: string }
  | { type: "navigate"; value: string };

/**
 * Parses a bridge message. Returns null for anything that is not one; the
 * sandboxed document can post whatever it likes, so only a bounded
 * query-or-fragment string gets through.
 */
export function readHtmlPreviewLocationMessage(
  data: unknown,
): HtmlPreviewLocationMessage | null {
  if (typeof data !== "object" || data === null) return null;
  const record = data as Record<string, unknown>;
  if (record[HTML_PREVIEW_LOCATION_KEY] !== 1) return null;
  const { type, value } = record;
  if (type !== "location" && type !== "navigate") return null;
  if (typeof value !== "string" || value.length > HTML_PREVIEW_ADDRESS_MAX_LENGTH) return null;
  if (value !== "" && value[0] !== "?" && value[0] !== "#") return null;
  if (type === "navigate" && value === "") return null;
  return { type, value };
}
