/**
 * Scroll-position bridge for sandboxed HTML attachment iframes (multica-ai#6405).
 *
 * The full-page `AttachmentPreviewPage` mounts user HTML inside
 * `<iframe sandbox="allow-scripts" srcDoc={...}>` — WITHOUT
 * `allow-same-origin`, so the parent cannot read `iframe.contentWindow.scrollY`
 * (opaque origin → SecurityError) and `captureScrollEntries()` in the desktop
 * tab Coordinator cannot see the iframe's scrolling element.
 *
 * This bridge is an in-iframe `<script>` (same shape as the fragment-nav shim
 * in `iframe-fragment-nav.ts`) that:
 *
 *   - reports the iframe's scroll position to the parent on every scroll via
 *     `postMessage`, so the parent can capture it synchronously when the tab
 *     is about to unmount (bounded last-known-value semantics — see the v4
 *     plan);
 *   - answers the parent's `request-sync` with a `ready` carrying current y;
 *   - on `restore(y)`, holds a *restoring session*: a ResizeObserver keeps
 *     re-scrolling to the target while user-authored content grows the
 *     document (images/scripts that append after load would otherwise clamp
 *     the position back to top). The session ends ONLY on a user/navigation
 *     takeover signal (wheel, touchmove, scroll keys, pointerdown on the
 *     document, hashchange, or a capture-phase click on an in-page anchor —
 *     so the existing fragment-nav shim's `scrollIntoView` wins) or on
 *     iframe unload. It does NOT end on "height stable for 2 frames" — that
 *     contradicted the "arbitrarily delayed append keeps position" guarantee
 *     in v3; see v4 §1.
 *
 * The bridge never touches sessionStorage/localStorage/cookies/parent.document
 * — sandbox `allow-scripts` without `allow-same-origin` blocks sessionStorage
 * anyway (Chromium throws SecurityError), and we don't need it: the parent
 * holds the last-known value.
 *
 * `token` is the content fingerprint (DJB2 base36 of the rendered HTML),
 * burned into the script and echoed on every message so the parent can drop
 * late messages from a previous document when `srcDoc` changes. React also
 * remounts the iframe via `key={contentKey}`, so a stale document's event
 * source is gone; the token is defense in depth.
 */

// Tokens come from hashString() — base36, [a-z0-9]+, no quotes/backslashes —
// so inlining them into the script source as a bare JS string literal is
// safe. We still run a sanity check and fall back to an empty token rather
// than emit a broken script.
const TOKEN_PATTERN = /^[a-z0-9]+$/;

function buildScript(token: string): string {
  const safeToken = TOKEN_PATTERN.test(token) ? token : "";
  return `<script>
(function(){
  var TOKEN = ${JSON.stringify(safeToken)};
  var MARK = "__multica";
  function docHeight() {
    var de = document.documentElement;
    return Math.max(de.scrollHeight, document.body ? document.body.scrollHeight : 0);
  }
  function post(msg) {
    msg.__multica = MARK;
    msg.token = TOKEN;
    try { parent.postMessage(msg, "*"); } catch (_) {}
  }
  function reportScroll() {
    post({ kind: "scroll", y: window.scrollY || 0, height: docHeight() });
  }

  // -- scroll reporting: no throttle; one rAF-coalesced settled resend -----
  var rafScheduled = false;
  window.addEventListener("scroll", function () {
    reportScroll();
    if (!rafScheduled) {
      rafScheduled = true;
      window.requestAnimationFrame(function () {
        rafScheduled = false;
        reportScroll();
      });
    }
  }, { passive: true });

  // -- restoring session (v4): hold position across content growth --------
  var restoring = false;
  var restoreTarget = 0;
  var ro = null;
  var takeoverListeners = [];
  function onTakeover(target, type, fn, opts) {
    // For removeEventListener only the capture bit of the options bag
    // matters; record it so cleanup detaches exactly.
    var capture = !!(opts && opts.capture);
    target.addEventListener(type, fn, opts);
    takeoverListeners.push([target, type, fn, capture]);
  }
  function endSession() {
    if (!restoring) return;
    restoring = false;
    if (ro) { try { ro.disconnect(); } catch (_) {} ro = null; }
    for (var i = 0; i < takeoverListeners.length; i++) {
      var t = takeoverListeners[i];
      try { t[0].removeEventListener(t[1], t[2], t[3]); } catch (_) {}
    }
    takeoverListeners = [];
  }
  function applyTarget() {
    // scrollTo is clamped by max scroll; when content grows later the RO
    // re-applies the same target.
    window.scrollTo(0, restoreTarget);
  }
  function beginRestore(y) {
    restoreTarget = y | 0;
    endSession();
    restoring = true;
    applyTarget();
    if (typeof ResizeObserver === "function") {
      ro = new ResizeObserver(function () {
        if (!restoring) return;
        applyTarget();
      });
      try { ro.observe(document.documentElement); } catch (_) { ro = null; }
    }
    // User/navigation takeover — capture phase so we see events before the
    // fragment-nav shim (which calls preventDefault + scrollIntoView on
    // anchor clicks). We do NOT treat a bare "scroll" event as takeover:
    // our own applyTarget() and the ResizeObserver callbacks produce scroll
    // events asynchronously and must not end the session they belong to.
    onTakeover(window, "wheel", endSession, { passive: true });
    onTakeover(window, "touchmove", endSession, { passive: true });
    onTakeover(window, "keydown", function (e) {
      var k = e.key;
      if (k === " " || k === "PageUp" || k === "PageDown" ||
          k === "Home" || k === "End" ||
          k === "ArrowUp" || k === "ArrowDown" ||
          k === "ArrowLeft" || k === "ArrowRight") {
        endSession();
      }
    });
    onTakeover(document, "pointerdown", endSession, true);
    onTakeover(window, "hashchange", endSession);
    onTakeover(document, "click", function (e) {
      var t = e.target;
      if (!t || typeof t.closest !== "function") return;
      var a = t.closest('a[href^="#"]');
      if (a && a.getAttribute("href") !== "#") endSession();
    }, true);
  }

  // -- parent -> child messages -------------------------------------------
  window.addEventListener("message", function (e) {
    var d = e.data;
    if (!d || d.__multica !== MARK || d.token !== TOKEN) return;
    if (d.kind === "restore" && typeof d.y === "number") {
      beginRestore(d.y);
    } else if (d.kind === "request-sync") {
      post({ kind: "ready", y: window.scrollY || 0, height: docHeight() });
    }
  });

  window.addEventListener("pagehide", function () {
    reportScroll();
    endSession();
  });

  // Announce on load (and immediately if the script runs after DOMContentLoaded).
  function announce() {
    post({ kind: "ready", y: window.scrollY || 0, height: docHeight() });
  }
  if (document.readyState === "complete" || document.readyState === "interactive") {
    announce();
  } else {
    window.addEventListener("DOMContentLoaded", announce);
    window.addEventListener("load", announce);
  }
})();
</script>`;
}

export function buildScrollBridge(token: string): string {
  return buildScript(token);
}

/** Exposed for tests so they can evaluate the inner script in jsdom. */
export function __scrollBridgeInnerScript__(token: string): string {
  return buildScript(token)
    .replace(/^<script>/, "")
    .replace(/<\/script>\s*$/, "");
}
