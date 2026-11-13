/**
 * Width bridge for the HTML attachment viewer's Fit viewport (MUL-8700).
 *
 * Fit lays the document out at the stage's width. A document built for a
 * wider screen — a fixed-width table, a grid of min-width cards — overflows
 * that width and would scroll sideways, so Fit needs to know how wide the
 * document actually is to lay it out there and scale it down instead. The
 * iframe is an opaque origin, so the parent cannot read the document's
 * `scrollWidth`; this script, appended to the author's markup, reports it.
 *
 * Each report carries the frame's own width (`viewport`, `innerWidth`) and
 * the width the document needs (`needed`): the viewport plus whatever the
 * content overflows it by, so a classic scrollbar's gutter is counted. A
 * document that clips its own overflow (`overflow-x: hidden` / `clip` on
 * the root or body — the usual guard for decorative bleed) needs no more
 * than its viewport. `doc` is fresh for every document load, so the parent
 * can forget a width learned from a document that is gone. It reports on
 * load, resize and layout changes, and on a `measure` request from the
 * parent even when nothing changed.
 *
 * It runs in the iframe's opaque origin with the capabilities the author's
 * scripts already have. The parent treats every message as untrusted (see
 * readHtmlPreviewWidthMessage).
 */

/** Marker on every bridge message. */
export const HTML_PREVIEW_WIDTH_KEY = "__multicaHtmlWidth";

/** Upper bound on a reported width; anything larger is not a width we keep. */
const MAX_REPORTED_WIDTH = 100_000;

const SCRIPT = [
  "(function(){",
  `var K=${JSON.stringify(HTML_PREVIEW_WIDTH_KEY)},D=document.documentElement,`,
  "ID=Math.random().toString(36).slice(2),last='';",
  "function clips(el){if(!el)return false;var o=getComputedStyle(el).overflowX;return o==='hidden'||o==='clip'}",
  "function measure(force){var b=document.body,vw=window.innerWidth,cw=D.clientWidth;",
  "var sw=clips(D)||clips(b)?cw:Math.max(D.scrollWidth,b?b.scrollWidth:0);",
  "var need=vw+Math.max(0,Math.ceil(sw-cw));var key=vw+'/'+need;if(!force&&key===last)return;last=key;",
  "var m={type:'width',doc:ID,viewport:vw,needed:need};m[K]=1;try{parent.postMessage(m,'*')}catch(_){}}",
  "function report(){measure(false)}",
  "report();window.addEventListener('load',report);window.addEventListener('resize',report);",
  "if(document.fonts&&document.fonts.ready)document.fonts.ready.then(report);",
  "if(typeof ResizeObserver==='function'){var ro=new ResizeObserver(report);ro.observe(D);if(document.body)ro.observe(document.body)}",
  "window.addEventListener('message',function(e){if(e.source!==parent)return;var d=e.data;",
  "if(d&&d[K]===1&&d.type==='measure')measure(true)});",
  "})();",
].join("");

/** Exposed for tests so they can evaluate the script in jsdom. */
export const __WIDTH_BRIDGE_SCRIPT__ = SCRIPT;

/** The document with the width bridge appended after the author's markup. */
export function withWidthBridge(html: string): string {
  return `${html}<script>${SCRIPT}</script>`;
}

export interface HtmlPreviewWidthMessage {
  /** Identifies the document load the report came from. */
  doc: string;
  /** The frame's width, in the document's CSS px. */
  viewport: number;
  /** The width the document needs to show without scrolling sideways. */
  needed: number;
}

function isWidth(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 && value <= MAX_REPORTED_WIDTH;
}

/**
 * Parses a bridge message. Returns null for anything that is not one; the
 * sandboxed document can post whatever it likes, so only a short id and two
 * bounded widths get through.
 */
export function readHtmlPreviewWidthMessage(data: unknown): HtmlPreviewWidthMessage | null {
  if (typeof data !== "object" || data === null) return null;
  const record = data as Record<string, unknown>;
  if (record[HTML_PREVIEW_WIDTH_KEY] !== 1 || record.type !== "width") return null;
  const { doc, viewport, needed } = record;
  if (typeof doc !== "string" || doc.length === 0 || doc.length > 32) return null;
  if (!isWidth(viewport) || !isWidth(needed)) return null;
  return { doc, viewport, needed };
}
