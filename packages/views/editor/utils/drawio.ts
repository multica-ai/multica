/**
 * draw.io viewer embedding (#6321): the one place that turns diagram XML into
 * a viewer URL, shared by fenced ```drawio blocks and .drawio / .dio
 * attachments so both entry points get the same checks.
 *
 * The diagram travels in the URL fragment using draw.io's own `#R` protocol —
 * base64 of deflate-raw over the URI-encoded XML. A fragment is never sent in
 * the HTTP request, so the viewer host receives no diagram content from us;
 * the viewer decodes it in the browser.
 */

/**
 * Upper bound for the encoded fragment. Chromium caps URLs at 2 MiB; this
 * stays well below it so a diagram that renders in one client renders in the
 * others, and a pathological file fails with an explicit error instead of a
 * blank frame.
 */
export const DRAWIO_MAX_ENCODED_CHARS = 512 * 1024;

/** How long the reachability probe waits before calling the viewer unreachable. */
export const DRAWIO_PROBE_TIMEOUT_MS = 8000;

export type DrawioEmbedError = "invalid" | "too_large" | "unreachable";

const DIAGRAM_ROOTS = new Set(["mxfile", "mxGraphModel"]);

/**
 * Whether `xml` parses as a draw.io document: well-formed XML whose root is
 * `<mxfile>` or a bare `<mxGraphModel>`. Anything else would load the viewer
 * only for it to show its own error page.
 */
export function isDrawioXml(xml: string): boolean {
  if (typeof DOMParser === "undefined") return false;
  const doc = new DOMParser().parseFromString(xml.trim(), "application/xml");
  if (doc.getElementsByTagName("parsererror").length > 0) return false;
  return DIAGRAM_ROOTS.has(doc.documentElement?.nodeName ?? "");
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}

/** draw.io's `#R` payload: base64(deflateRaw(encodeURIComponent(xml))). */
export async function encodeDrawioXml(xml: string): Promise<string> {
  // encodeURIComponent output is ASCII, so one char is one byte.
  const input = new TextEncoder().encode(encodeURIComponent(xml));
  const stream = new Response(input).body!.pipeThrough(
    new CompressionStream("deflate-raw"),
  );
  const compressed = new Uint8Array(await new Response(stream).arrayBuffer());
  return bytesToBase64(compressed);
}

/** The iframe src for `encoded` on the viewer at `viewerUrl`. */
export function buildDrawioViewerSrc(viewerUrl: string, encoded: string): string {
  const url = new URL(viewerUrl);
  // lightbox: read-only viewer chrome; nav: page and zoom controls.
  url.searchParams.set("lightbox", "1");
  url.searchParams.set("nav", "1");
  url.hash = `R${encodeURIComponent(encoded)}`;
  return url.toString();
}

/**
 * The viewer base URL to embed, or null when the preview must stay off: the
 * server sent none, it is not an absolute http(s) URL, or it shares the app's
 * origin. The iframe needs `allow-scripts allow-same-origin` for the viewer to
 * run, which only isolates it from the app while the two origins differ.
 */
export function usableDrawioViewerUrl(
  configured: string,
  appOrigin: string | null,
): string | null {
  if (!configured) return null;
  let url: URL;
  try {
    url = new URL(configured);
  } catch {
    return null;
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") return null;
  if (appOrigin && url.origin === appOrigin) return null;
  return configured;
}

const probes = new Map<string, Promise<boolean>>();

/**
 * Whether the viewer origin answers at all. One request per origin per page
 * session, shared by every diagram on the page. An opaque `no-cors` response
 * counts as reachable; a network failure, a blocked request or the timeout
 * does not. A proxy that answers with its own error page still passes — the
 * frame then shows that page, which at least names the problem.
 */
export function probeDrawioViewer(
  viewerUrl: string,
  timeoutMs = DRAWIO_PROBE_TIMEOUT_MS,
): Promise<boolean> {
  const origin = new URL(viewerUrl).origin;
  const cached = probes.get(origin);
  if (cached) return cached;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const probe = fetch(viewerUrl, {
    mode: "no-cors",
    cache: "no-store",
    credentials: "omit",
    referrerPolicy: "no-referrer",
    signal: controller.signal,
  })
    .then(() => true)
    .catch(() => false)
    .finally(() => clearTimeout(timer));
  probes.set(origin, probe);
  return probe;
}

/** Drops the cached probe for `viewerUrl`'s origin so a reload asks again. */
export function forgetDrawioViewerProbe(viewerUrl: string): void {
  try {
    probes.delete(new URL(viewerUrl).origin);
  } catch {
    // An unparsable URL was never probed.
  }
}

export type DrawioEmbedResult =
  | { ok: true; src: string }
  | { ok: false; error: DrawioEmbedError };

/** Validates, encodes and probes in the order that fails cheapest first. */
export async function prepareDrawioEmbed(
  xml: string,
  viewerUrl: string,
): Promise<DrawioEmbedResult> {
  if (!isDrawioXml(xml)) return { ok: false, error: "invalid" };
  const encoded = await encodeDrawioXml(xml.trim());
  if (encoded.length > DRAWIO_MAX_ENCODED_CHARS) {
    return { ok: false, error: "too_large" };
  }
  if (!(await probeDrawioViewer(viewerUrl))) {
    return { ok: false, error: "unreachable" };
  }
  return { ok: true, src: buildDrawioViewerSrc(viewerUrl, encoded) };
}
