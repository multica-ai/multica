/**
 * The host generates the document a surface runs in, and now supplies the code
 * that runs in it. That is the point: CSP is a response-header/meta decision
 * made by whoever emits the document, so if the plugin author's server emitted
 * it, the `net:` scopes the admin approved would be a claim we never check. Here
 * they become a `connect-src` the browser enforces.
 *
 * The script is INLINED rather than loaded from an origin. When it came from
 * the author's server, `script-src` had to name that origin, which meant a
 * surface could always reach its author back — and it meant every panel open
 * told the author who was reading which issue. Now Multica stores the published
 * artifact and hands it to the frame, so the policy names no remote script
 * origin at all.
 *
 * The frame is mounted with `sandbox="allow-scripts"` and NOT
 * `allow-same-origin` — the pairing the HTML spec calls out as defeating the
 * sandbox. That gives the document an opaque origin: no cookies, no
 * localStorage, no access to the embedder, and no shared storage between two
 * plugins. It is the same model `packages/views/editor/code-block-iframe.tsx`
 * already uses for untrusted HTML.
 */

/** Design tokens forwarded into the frame so a surface looks native for free. */
export const SURFACE_THEME_TOKENS = [
  "--background",
  "--foreground",
  "--muted",
  "--muted-foreground",
  "--border",
  "--primary",
  "--primary-foreground",
  "--destructive",
  "--radius",
  "--text-caption",
  "--text-body",
] as const;

export function readThemeTokens(element: Element | null): Record<string, string> {
  if (!element || typeof getComputedStyle !== "function") return {};
  const computed = getComputedStyle(element);
  const tokens: Record<string, string> = {};
  for (const name of SURFACE_THEME_TOKENS) {
    const value = computed.getPropertyValue(name).trim();
    if (value) tokens[name] = value;
  }
  return tokens;
}

function escapeAttribute(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/**
 * Builds the `connect-src` list from the granted scopes only — never from the
 * manifest the source URL serves today. `net:` is an exact host by contract, so
 * a plugin that needs a subdomain declares it.
 */
export function surfaceConnectSources(grantedScopes: string[]): string[] {
  return grantedScopes
    .filter((scope) => scope.startsWith("net:"))
    .map((scope) => `https://${scope.slice("net:".length)}`);
}

export function buildSurfaceCSP(grantedScopes: string[]): string {
  const connect = surfaceConnectSources(grantedScopes);
  return [
    "default-src 'none'",
    // Inline only, and no remote origin at all. The surface's code is supplied
    // by us, so the generated document has nothing left for `script-src` to
    // fetch. Under the previous model the author's own origin was always in
    // this list.
    "script-src 'unsafe-inline'",
    "style-src 'unsafe-inline'",
    // Inline data only. An <img> or webfont URL is the cheapest possible side
    // channel, and a surface that needs artwork inlines it.
    "img-src data: blob:",
    "font-src data:",
    // With no net: scope, connection APIs in the generated document cannot
    // reach an origin. Self-navigation is a separate sandbox gap documented
    // below and tracked outside this change.
    `connect-src ${connect.length > 0 ? connect.join(" ") : "'none'"}`,
    // A sandboxed frame cannot navigate the top level anyway; saying so keeps
    // the policy honest if the sandbox attribute is ever loosened.
    "form-action 'none'",
    "base-uri 'none'",
    // frame-ancestors is deliberately absent: it is ignored when delivered via
    // <meta>, and the browser logs a warning for it. Nothing is lost — the
    // sandbox already denies this document the ability to frame anything.
  ].join("; ");
}

export interface SurfaceDocumentInput {
  /** The surface's entry script, as published. */
  code: string;
  grantedScopes: string[];
  theme: Record<string, string>;
}

/**
 * Encodes the surface's code so it can be carried inside an HTML document.
 *
 * Base64 rather than escaping `</script`: the alphabet cannot contain anything
 * the HTML tokenizer reacts to, so there is no sequence in a plugin's source
 * that can end the script element early or shift the parser into another state.
 * Escaping works too, but it is a rule about JavaScript syntax enforced by a
 * string replace, and getting it subtly wrong looks like a plugin bug.
 */
function encodeSurfaceCode(code: string): string {
  const bytes = new TextEncoder().encode(code);
  let binary = "";
  // Chunked: String.fromCharCode(...bytes) blows the argument limit somewhere
  // around a hundred kilobytes, which a real surface reaches.
  for (let index = 0; index < bytes.length; index += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(index, index + 0x8000));
  }
  return btoa(binary);
}

/**
 * The srcdoc a surface iframe renders.
 *
 * The plugin's code arrives as base64 in a non-executable element and is
 * activated by a small bootstrap. Everything executable is inline, and rendering
 * this document fetches nothing.
 *
 * KNOWN GAP — a sandboxed frame may navigate ITSELF. `allow-scripts` without
 * `allow-top-navigation` stops a surface navigating the top level or a sibling,
 * but the HTML sandbox deliberately permits `_self`, and no shipped CSP
 * directive covers it (`navigate-to` was dropped from CSP3, and `connect-src` /
 * `form-action` govern other things). So a HOSTILE artifact can still run
 * `location.replace("https://author.example/live.html")`, which reaches the
 * author's server once and replaces this document with one whose CSP we do not
 * write. Closing it needs either an embedder `frame-src` policy or a handshake
 * the navigated document cannot complete. Tracked separately; do not describe
 * a surface as unable to reach its author until that lands. In particular, do
 * not use lifecycle events from this document as a security signal: a normal
 * host-driven `srcDoc` reload fires the same events.
 */
export function buildSurfaceDocument({ code, grantedScopes, theme }: SurfaceDocumentInput): string {
  const csp = buildSurfaceCSP(grantedScopes);
  const themeCss = Object.entries(theme)
    .map(([name, value]) => `${name}: ${value};`)
    .join(" ");

  // The meta CSP must precede anything it governs, so it is the first child of
  // <head>. Note srcdoc documents also inherit the embedder's policy — this
  // narrows, it cannot widen.
  return `<!doctype html>
<html>
<head>
<meta http-equiv="Content-Security-Policy" content="${escapeAttribute(csp)}">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
:root { ${themeCss} color-scheme: light dark; }
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body {
  background: var(--background, transparent);
  color: var(--foreground, inherit);
  font: 400 var(--text-body, 14px)/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
</style>
</head>
<body>
<div id="root"></div>
<script type="text/plain" id="multica-surface-code">${encodeSurfaceCode(code)}</script>
<script>
(function () {
  var errorReportAttempts = 0;
  var errorReportTimer = null;

  function emitSurfaceError() {
    parent.postMessage({ type: 'multica:plugin-surface-error' }, '*');
    errorReportAttempts++;
    if (errorReportAttempts >= 50 && errorReportTimer !== null) {
      clearInterval(errorReportTimer);
      errorReportTimer = null;
    }
  }

  function reportSurfaceError() {
    if (errorReportAttempts > 0) return;
    emitSurfaceError();
    if (errorReportAttempts < 50) errorReportTimer = setInterval(emitSurfaceError, 120);
  }

  // A srcdoc document may execute before the host component's effect attaches
  // its listener. Repeat a failure until the host acknowledges it, bounded to
  // the same six-second window as the SDK bridge handshake.
  window.addEventListener("message", function (event) {
    if (event.source !== parent) return;
    if (!event.data || event.data.type !== 'multica:plugin-surface-error-ack') return;
    errorReportAttempts = 50;
    if (errorReportTimer !== null) clearInterval(errorReportTimer);
    errorReportTimer = null;
  });

  // Installed before plugin code executes. Dynamically appending an inline
  // script does not rethrow its syntax/runtime errors from appendChild; the
  // browser reports them through these global events instead.
  window.addEventListener("error", reportSurfaceError);
  window.addEventListener("unhandledrejection", reportSurfaceError);

  try {
    var encoded = document.getElementById("multica-surface-code").textContent;
    var binary = atob(encoded);
    var bytes = new Uint8Array(binary.length);
    for (var i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    var element = document.createElement("script");
    element.textContent = new TextDecoder().decode(bytes);
    document.body.appendChild(element);
  } catch (error) {
    reportSurfaceError();
  }
})();
</script>
</body>
</html>`;
}
