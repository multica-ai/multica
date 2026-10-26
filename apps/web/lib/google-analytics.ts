import { isSensitiveAnalyticsUrl, sanitizeAnalyticsUrl } from "../config/google-analytics";

type Gtag = (...args: unknown[]) => void;
type GaWindow = Window & {
  dataLayer?: unknown[];
  gtag?: Gtag;
  __multicaGoogleAnalytics?: string;
  [disableKey: `ga-disable-${string}`]: boolean | undefined;
};

/**
 * Google tag (gtag.js) for GA4. Rendered only when the root layout resolved a
 * Measurement ID from GOOGLE_ANALYTICS_ID (config/google-analytics.ts).
 *
 * GA's automatic history events can retain a sensitive referrer after the
 * browser has left it. Once the tag may have observed a sensitive URL, keep
 * its documented opt-out flag set for the rest of this document's lifetime.
 * Re-enabling it on a later history event can release credential-bearing
 * events, including when the tag's download finished on a sensitive URL.
 * A fresh document can start again, with its landing referrer sanitized.
 * The same applies to activating a credential-bearing link or form (e.g. a
 * signed attachment download), which GA reports without a URL change.
 */
export function installGoogleAnalytics(measurementId: string) {
  const w = window as unknown as GaWindow;
  // Install once per page: the root layout never unmounts, and StrictMode /
  // Fast Refresh re-runs must not stack history hooks or reload gtag.js.
  if (w.__multicaGoogleAnalytics === measurementId) return;
  w.__multicaGoogleAnalytics = measurementId;
  const disableKey = `ga-disable-${measurementId}` as const;
  let loaded = false;
  let stopped = false;
  const stop = () => {
    stopped = true;
    w[disableKey] = true;
  };

  const load = () => {
    loaded = true;
    w[disableKey] = false;
    w.dataLayer = w.dataLayer || [];
    w.gtag = function gtag() {
      // gtag.js consumes the `arguments` objects queued here (Google's shim).
      // eslint-disable-next-line prefer-rest-params
      w.dataLayer!.push(arguments);
    };
    w.gtag("js", new Date());
    // The landing referrer may be a same-origin secret URL (full-page hop
    // off /login?cli_state=…); pin a sanitized copy instead.
    w.gtag("config", measurementId, {
      page_location: sanitizeAnalyticsUrl(w.location.href),
      page_referrer: sanitizeAnalyticsUrl(document.referrer),
    });

    const script = document.createElement("script");
    script.async = true;
    script.src = `https://www.googletagmanager.com/gtag/js?id=${encodeURIComponent(measurementId)}`;
    document.head.appendChild(script);
  };

  const maybeLoad = () => {
    if (stopped || loaded) return;
    if (isSensitiveAnalyticsUrl(w.location.href)) {
      w[disableKey] = true;
      return;
    }
    load();
  };

  const originalPush = w.history.pushState;
  const originalReplace = w.history.replaceState;
  const wrap =
    (original: History["pushState"]): History["pushState"] =>
    function (this: History, data, unused, url) {
      const target = url == null ? w.location.href : new URL(String(url), w.location.href).href;
      // Stop before any tag history listener can observe the new URL.
      if (loaded && (isSensitiveAnalyticsUrl(w.location.href) || isSensitiveAnalyticsUrl(target))) {
        stop();
      }
      const result = original.call(this, data, unused, url);
      // Initial loading must happen after a successful navigation, with the
      // real location already clean. No tag has observed earlier URLs yet.
      maybeLoad();
      return result;
    };
  w.history.pushState = wrap(originalPush);
  w.history.replaceState = wrap(originalReplace);

  // Capture phase, registered before gtag.js: runs ahead of gtag's listener.
  const onPopState = () => {
    if (loaded && isSensitiveAnalyticsUrl(w.location.href)) stop();
    maybeLoad();
  };
  w.addEventListener("popstate", onPopState, true);

  // GA's enhanced measurement also reports the full target URL of outbound
  // link clicks and file downloads (`link_url`), and the form action of form
  // submissions (`form_destination`) — without the page URL changing. Signed
  // attachment downloads (CloudFront / S3 presign / proxy capability) click a
  // credential-bearing link, so stop before gtag's own listeners see such an
  // interaction. Window capture runs ahead of any document-level listener.
  const onLinkInteraction = (event: Event) => {
    if (!loaded || stopped) return;
    const href = interactionLinkHref(event);
    if (href && isSensitiveInteractionUrl(href)) stop();
  };
  w.addEventListener("click", onLinkInteraction, true);
  w.addEventListener("auxclick", onLinkInteraction, true);
  w.addEventListener(
    "submit",
    (event) => {
      if (!loaded || stopped) return;
      const form = event.target;
      if (form instanceof HTMLFormElement && isSensitiveInteractionUrl(form.action)) stop();
    },
    true,
  );

  maybeLoad();
}

// The href of the link an interaction activates, resolved to an absolute URL.
// composedPath() also reaches anchors inside open shadow roots and SVG links.
function interactionLinkHref(event: Event): string | null {
  for (const node of event.composedPath()) {
    if (!(node instanceof Element)) continue;
    if (node.localName !== "a" && node.localName !== "area") continue;
    const raw = node.getAttribute("href") ?? node.getAttribute("xlink:href");
    if (raw == null) continue;
    try {
      return new URL(raw, document.baseURI).href;
    } catch {
      // Unparseable: treat as opaque and let the sensitivity check decide.
      return raw;
    }
  }
  return null;
}

// Object URLs (CSV / diagram exports) are an opaque same-origin UUID and carry
// no credential; everything else uses the page-URL rule (non-allowlisted query
// params, capability paths, or a non-http(s) scheme).
function isSensitiveInteractionUrl(url: string): boolean {
  if (url.startsWith("blob:")) return false;
  return isSensitiveAnalyticsUrl(url);
}
