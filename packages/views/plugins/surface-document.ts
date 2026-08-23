/** Design tokens forwarded over the bridge so a surface looks native. */
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

export const SURFACE_BRIDGE_CONNECT_MESSAGE = "multica:plugin-bridge-connect";
export const SURFACE_BRIDGE_PROTOCOL_VERSION = 2;

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

function encodeUTF8(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (let index = 0; index < bytes.length; index += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(index, index + 0x8000));
  }
  return btoa(binary);
}

export interface SurfaceFrameDocumentInput {
  /** Short-lived URL on Multica's dedicated, cookie-free content origin. */
  url: string;
  /** Single-use proof embedded in both trusted documents, never plugin code. */
  bridgeToken: string;
}

/**
 * Builds the trusted outer frame around one untrusted plugin document.
 *
 * The outer frame exists so its `frame-src` applies to only this plugin. A
 * page-wide policy would also have to allow every attachment preview origin;
 * that wider list would let a hostile surface navigate to those origins. Here
 * the only network navigation an inner frame may make is back to the exact
 * Multica content origin that served it.
 *
 * The outer frame is host-authored and same-origin with the app. The INNER
 * frame is still `sandbox="allow-scripts"` without `allow-same-origin`, so two
 * plugins receive distinct opaque origins and cannot inspect each other.
 */
export function buildSurfaceFrameDocument({ url, bridgeToken }: SurfaceFrameDocumentInput): string {
  const parsed = new URL(url);
  if ((parsed.protocol !== "https:" && parsed.protocol !== "http:") || !bridgeToken) {
    throw new Error("invalid plugin surface launch");
  }

  const config = encodeUTF8(JSON.stringify({ url: parsed.toString(), bridgeToken }));
  const csp = [
    "default-src 'none'",
    "script-src 'unsafe-inline'",
    "style-src 'unsafe-inline'",
    `frame-src ${parsed.origin}`,
    "connect-src 'none'",
    "img-src 'none'",
    "font-src 'none'",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'none'",
  ].join("; ");

  return `<!doctype html>
<html>
<head>
<meta http-equiv="Content-Security-Policy" content="${escapeAttribute(csp)}">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
html, body, #multica-plugin-root, iframe { width: 100%; height: 100%; margin: 0; padding: 0; border: 0; }
body { overflow: hidden; background: transparent; }
</style>
</head>
<body>
<div id="multica-plugin-root"></div>
<script>
(function () {
  var encoded = ${JSON.stringify(config)};
  var binary = atob(encoded);
  var bytes = new Uint8Array(binary.length);
  for (var index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
  var config = JSON.parse(new TextDecoder().decode(bytes));
  var child = document.createElement("iframe");
  child.title = "Plugin content";
  child.setAttribute("sandbox", "allow-scripts");

  var state = "launching";
  var loadCount = 0;

  function stopSurface(type) {
    if (state === "terminal") return;
    state = "terminal";
    // After connection the host owns the transferred port; this terminal
    // signal makes it close the bridge and unmount this wrapper.
    parent.postMessage({ type: type }, "*");
  }

  document.addEventListener("securitypolicyviolation", function (event) {
    if (event.effectiveDirective === "frame-src" || event.effectiveDirective === "child-src") {
      stopSurface("multica:plugin-surface-navigation-blocked");
    }
  });

  child.addEventListener("load", function () {
    loadCount += 1;
    if (loadCount > 1) stopSurface("multica:plugin-surface-navigated");
  });

  window.addEventListener("message", function (event) {
    if (!child.contentWindow || event.source !== child.contentWindow) return;
    var data = event.data || {};
    if (data.type === "multica:plugin-surface-error" || data.type === "multica:plugin-surface-navigated") {
      stopSurface(data.type);
      return;
    }
    if (data.type !== ${JSON.stringify(SURFACE_BRIDGE_CONNECT_MESSAGE)} ||
        data.version !== ${SURFACE_BRIDGE_PROTOCOL_VERSION} ||
        data.challenge !== config.bridgeToken || state !== "launching" || !event.ports[0]) return;

    state = "connected";
    parent.postMessage({
      type: ${JSON.stringify(SURFACE_BRIDGE_CONNECT_MESSAGE)},
      version: ${SURFACE_BRIDGE_PROTOCOL_VERSION},
      challenge: config.bridgeToken
    }, "*", [event.ports[0]]);
    config.bridgeToken = "";
  });

  child.src = config.url;
  config.url = "";
  document.getElementById("multica-plugin-root").appendChild(child);
})();
</script>
</body>
</html>`;
}
