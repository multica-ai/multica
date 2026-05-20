/**
 * Fragment-navigation shim for sandboxed HTML attachment iframes.
 *
 * HTML attachment previews mount the user-supplied document inside a
 * `<iframe sandbox="allow-scripts" srcdoc={...}>` — deliberately WITHOUT
 * `allow-same-origin`, because the source is untrusted user upload and
 * same-origin would let it reach cookies / localStorage / parent.document.
 *
 * That security posture has a side effect: in Chromium, a sandboxed srcdoc
 * iframe sits in an opaque origin, and the browser treats clicks on
 * `<a href="#section">` as cross-origin frame navigation — silently rejected,
 * no scroll, no error. See whatwg/html#3537 and crbug 40191760; it's a spec +
 * implementation consensus, not a bug we can wait out.
 *
 * The fix is in-iframe: append a tiny script to the document that listens for
 * fragment-link clicks and calls `scrollIntoView` itself. The script runs in
 * the iframe's own opaque origin — same capabilities the user's HTML already
 * has under `allow-scripts`; it cannot reach the parent. The shim only
 * intercepts `href="#..."` clicks, defers to any preventDefault handler the
 * user's HTML installed, and stays out of the way when the target id is
 * missing (so SPA / tab-style routers in the document can still handle it).
 */
const FRAGMENT_NAV_SHIM = `<script>
(function(){
  document.addEventListener('click', function(e) {
    if (e.defaultPrevented) return;
    var t = e.target;
    if (!t || typeof t.closest !== 'function') return;
    var a = t.closest('a[href]');
    if (!a) return;
    var href = a.getAttribute('href');
    if (!href || href.charAt(0) !== '#' || href === '#') return;
    var id;
    try { id = decodeURIComponent(href.slice(1)); } catch (_) { return; }
    if (!id) return;
    var dest = document.getElementById(id);
    if (!dest && typeof CSS !== 'undefined' && CSS.escape) {
      dest = document.querySelector('a[name="' + CSS.escape(id) + '"]');
    }
    if (!dest) return;
    e.preventDefault();
    dest.scrollIntoView({ behavior: 'smooth', block: 'start' });
  });
})();
</script>`;

export function withFragmentNavShim(html: string | undefined): string {
  return (html ?? "") + FRAGMENT_NAV_SHIM;
}

/** Exposed for unit tests so they can assert the shim was appended verbatim. */
export const __FRAGMENT_NAV_SHIM__ = FRAGMENT_NAV_SHIM;
