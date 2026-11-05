/**
 * Closes the network to agent-written HTML in previews.
 *
 * `sandbox="allow-scripts"` keeps the document away from the app (an opaque
 * origin: no cookies, storage or parent DOM), but not from the network: a
 * script could still send what the document shows anywhere with `fetch`, an
 * image, a beacon or a prefetch, or navigate its frame to an address that
 * carries it. Two layers close that:
 *
 *   - the document gets a Content-Security-Policy in front of its markup with
 *     no network of its own: inline scripts and styles, `data:` / `blob:`
 *     media, and libraries from jsDelivr's npm path (the one CDN the agent
 *     guide tells authors to use) — nothing that can carry data back;
 *   - it runs in a frame inside a wrapper document whose `frame-src 'none'`
 *     refuses every navigation of that frame. A document's own CSP can't stop
 *     it navigating itself away (`location = "https://…?" + data`); only its
 *     embedder's `frame-src` can, and the app page can't send one without
 *     also refusing plugin surfaces.
 *
 * The wrapper carries the same policy as the document: a srcdoc frame
 * inherits its embedder's policy too, so a stricter wrapper would refuse the
 * document's libraries. `frame-src` doesn't govern the srcdoc frame itself,
 * only where it navigates.
 *
 * The wrapper relays messages both ways, so the bridges in the document (size,
 * errors, scroll, address) keep talking to the app: the app still hears its
 * frame's window, and the document still hears its parent. Both sides keep
 * validating what they receive.
 *
 * Not covered: WebRTC (Chromium has no CSP directive for it) and the requests
 * a jsDelivr script makes to jsDelivr itself.
 */

/** The document's own policy: no network, except libraries from jsDelivr's npm path. */
export const HTML_PREVIEW_CSP = [
  "default-src 'none'",
  "script-src 'unsafe-inline' 'unsafe-eval' https://cdn.jsdelivr.net/npm/",
  "style-src 'unsafe-inline' https://cdn.jsdelivr.net/npm/",
  "font-src data: https://cdn.jsdelivr.net/npm/",
  "img-src data: blob:",
  "media-src data: blob:",
  "connect-src 'none'",
  "frame-src 'none'",
  "object-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
].join("; ");

const DOCTYPE_RE = /^\s*<!doctype[^>]*>/i;

// The meta must come before any of the author's markup to govern it; a
// leading doctype has to stay first.
export const PREVIEW_POLICY_HEAD =
  `<meta http-equiv="Content-Security-Policy" content="${HTML_PREVIEW_CSP}">` +
  `<meta http-equiv="x-dns-prefetch-control" content="off">`;

// One way per message: from the document to the app, or from the app to the
// document. Anything else is dropped.
const RELAY_SCRIPT =
  "(function(){var f=document.getElementById('f');" +
  "addEventListener('message',function(e){var w=f.contentWindow;if(!w)return;" +
  "if(e.source===w)parent.postMessage(e.data,'*');" +
  "else if(e.source===parent)w.postMessage(e.data,'*');});})();";

function escapeAttribute(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/"/g, "&quot;");
}

/** `html` with the no-network policy in front of its markup. */
export function withPreviewPolicy(html: string): string {
  const doctype = DOCTYPE_RE.exec(html)?.[0];
  return doctype ? doctype + PREVIEW_POLICY_HEAD + html.slice(doctype.length) : PREVIEW_POLICY_HEAD + html;
}

/**
 * The srcdoc for a preview frame: a wrapper that frames `html` (with its
 * policy) and refuses that frame's navigations. Mount it with
 * `sandbox="allow-scripts"` as before.
 */
export function buildGuardedPreviewDocument(html: string, title: string): string {
  return (
    "<!doctype html><html><head>" +
    `<meta http-equiv="Content-Security-Policy" content="${HTML_PREVIEW_CSP}">` +
    '<meta charset="utf-8">' +
    "<style>html,body{margin:0;height:100%;overflow:hidden;background:transparent}" +
    "iframe{display:block;width:100%;height:100%;border:0}</style>" +
    "</head><body>" +
    `<iframe id="f" sandbox="allow-scripts" title="${escapeAttribute(title)}" srcdoc="${escapeAttribute(withPreviewPolicy(html))}"></iframe>` +
    `<script>${RELAY_SCRIPT}</script>` +
    "</body></html>"
  );
}
