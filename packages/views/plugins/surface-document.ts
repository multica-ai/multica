import type { PluginInstallation, PluginSurface } from "@multica/core/types";

/**
 * The host generates the document a surface runs in. That is the point: CSP is
 * a response-header/meta decision made by whoever emits the document, so if the
 * plugin author's server emitted it, the `net:` scopes the admin approved would
 * be a claim we never check. Here they become a `connect-src` the browser
 * enforces.
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

/**
 * Resolves a surface entry to an absolute script URL.
 *
 * `entry` is relative to the manifest, so the plugin's own host serves its code
 * — we never proxy or re-host third-party JavaScript. A `local:` source has no
 * URL to resolve against; those installs are for developing the manifest and
 * cannot render a surface, which the panel says out loud rather than showing an
 * empty frame.
 */
export function resolveSurfaceEntry(installation: PluginInstallation, surface: PluginSurface): string | null {
  const source = installation.source_url ?? "";
  let resolved: URL;
  try {
    resolved = new URL(surface.entry, source);
  } catch {
    // A `local:` source is not a URL at all — there is nothing a browser could
    // fetch, and guessing one would be worse than saying so.
    return null;
  }
  if (resolved.protocol === "https:") return resolved.toString();
  // Plain HTTP is allowed only from loopback, matching what browsers already
  // treat as a secure context. That is what makes developing a surface possible
  // — serve it from a local static server and install by URL like any other —
  // without opening plaintext delivery on a real deployment.
  if (resolved.protocol === "http:" && isLoopbackHost(resolved.hostname)) return resolved.toString();
  return null;
}

function isLoopbackHost(hostname: string): boolean {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "[::1]" || hostname === "::1";
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

export function buildSurfaceCSP(grantedScopes: string[], scriptOrigin: string): string {
  const connect = surfaceConnectSources(grantedScopes);
  return [
    "default-src 'none'",
    // The surface's own script, and nothing else executable. This origin is
    // unavoidable — the code has to load from somewhere — and it is the reason
    // `net:` bounds THIRD-PARTY destinations rather than exfiltration in
    // general: a script served by its author can always reach its author back,
    // and closing that would mean re-hosting third-party code, which this design
    // deliberately does not do.
    `script-src ${scriptOrigin} 'unsafe-inline'`,
    "style-src 'unsafe-inline'",
    // Narrowed to inline data rather than the script origin: an <img> or webfont
    // URL is the cheapest possible side channel back to the author, and a
    // surface that needs artwork can inline it. It does not make the channel
    // impossible — a dynamic import of the author's own origin remains — but it
    // removes the two that cost nothing to use.
    "img-src data: blob:",
    "font-src data:",
    // With no net: scope this is 'none', so a surface cannot reach any
    // third-party host. Its author's own origin is still reachable via
    // script-src; see above.
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
  entryUrl: string;
  grantedScopes: string[];
  theme: Record<string, string>;
}

/**
 * The srcdoc a surface iframe renders. Everything executable in it is the
 * plugin's own entry script loaded cross-origin; the inline script only forwards
 * load failures so a broken plugin reports itself instead of showing blank.
 */
export function buildSurfaceDocument({ entryUrl, grantedScopes, theme }: SurfaceDocumentInput): string {
  const scriptOrigin = new URL(entryUrl).origin;
  const csp = buildSurfaceCSP(grantedScopes, scriptOrigin);
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
<script src="${escapeAttribute(entryUrl)}" onerror="parent.postMessage({type:'multica:plugin-surface-error'},'*')"></script>
</body>
</html>`;
}
