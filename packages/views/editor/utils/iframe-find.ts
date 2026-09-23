/**
 * In-page find shim for sandboxed HTML attachment iframes (#5259).
 *
 * HTML attachment previews mount user HTML inside a
 * `<iframe sandbox="allow-scripts" srcdoc={...}>` WITHOUT `allow-same-origin`
 * (see code-block-iframe.tsx / attachment-preview-page.tsx). That opaque-origin
 * sandbox is deliberate — it isolates untrusted uploads — but it has a side
 * effect: the browser's native Ctrl+F / Cmd+F find-in-page cannot search into
 * an opaque-origin iframe, and the parent document cannot reach the iframe's
 * DOM across the origin boundary either. So maximizing an HTML preview left the
 * user with no way to search a large document.
 *
 * The fix mirrors withFragmentNavShim: inject a tiny script into the srcdoc that
 * runs in the iframe's own opaque origin (same capability `allow-scripts`
 * already grants) and does the search on its own document, driven by the parent
 * over postMessage. It adds NO new capability and does NOT relax the sandbox.
 *
 * Protocol:
 *   parent -> iframe: { source: FIND_CMD, action: "search"|"next"|"prev"|"clear",
 *                       query, caseSensitive }
 *   iframe -> parent: { source: FIND_RESULT, found, total, current }
 *                     { source: FIND_OPEN }   // Ctrl/Cmd+F pressed inside iframe
 *
 * Security model:
 *   The shim rejects any message whose e.source is not the parent window. This
 *   prevents sandboxed content (which can call window.parent.postMessage) or a
 *   third-party frame from faking find commands or injecting spurious results.
 *   The guard is skipped when parent === window (top-level context, not an
 *   iframe) so the shim stays fully functional in unit tests running in jsdom.
 *
 * Matching:
 *   Matches are collected across the flat concatenated text of all visible text
 *   nodes (cross-node matching). A MutationObserver marks the cache dirty on any
 *   DOM change so dynamic HTML content is always re-indexed before the next
 *   search.
 */

/** postMessage `source` tag for parent -> iframe commands. */
export const FIND_CMD = "multica-find-cmd";
/** postMessage `source` tag for iframe -> parent result updates. */
export const FIND_RESULT = "multica-find-result";
/** postMessage `source` tag for iframe -> parent "open the find bar" signal. */
export const FIND_OPEN = "multica-find-open";

const FIND_SHIM = `<script>
(function(){
  var CMD=${JSON.stringify(FIND_CMD)};
  var RESULT=${JSON.stringify(FIND_RESULT)};
  var OPEN=${JSON.stringify(FIND_OPEN)};

  // matches[i] = { startNode, startOffset, endNode, endOffset }
  // Built from a flat concatenation of all visible text nodes so matches can
  // span inline element boundaries (e.g. "hello <b>world</b>").
  var matches = [];
  var idx = -1;
  var lastQuery = null;
  var lastCase = false;
  // Set to true by MutationObserver when the DOM changes after a search.
  var dirty = false;

  // Invalidate the match cache whenever the document content changes so dynamic
  // HTML (e.g. a <script> in the srcdoc that mutates the DOM) always produces
  // an up-to-date result on the next search.
  try {
    var _mo = new MutationObserver(function(){ dirty = true; });
    _mo.observe(document.documentElement || document.body, {
      childList: true, subtree: true, characterData: true
    });
  } catch(_) {}

  // Collect all visible text nodes into a flat string and a parallel node list
  // so we can map flat character offsets back to (node, charOffset) pairs.
  // Skips <script>/<style>/<noscript> — including this injected shim itself.
  function buildFlatMap(){
    var nodes = [], offsets = [], flat = "";
    var root = document.body || document.documentElement;
    if(!root) return { nodes: nodes, offsets: offsets, flat: flat };
    var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
      acceptNode: function(node){
        var tag = node.parentNode && node.parentNode.nodeName;
        if(tag === "SCRIPT" || tag === "STYLE" || tag === "NOSCRIPT") return NodeFilter.FILTER_REJECT;
        return NodeFilter.FILTER_ACCEPT;
      }
    });
    var node;
    while((node = walker.nextNode())){
      offsets.push(flat.length);
      flat += node.nodeValue || "";
      nodes.push(node);
    }
    return { nodes: nodes, offsets: offsets, flat: flat };
  }

  // First index i in sorted arr where arr[i] > val (binary search).
  function upperBound(arr, val){
    var lo = 0, hi = arr.length;
    while(lo < hi){ var mid = (lo + hi) >> 1; if(arr[mid] <= val) lo = mid + 1; else hi = mid; }
    return lo;
  }

  function collect(query, caseSensitive){
    matches = [];
    lastQuery = query;
    lastCase = caseSensitive;
    dirty = false;
    if(!query) return;
    var map = buildFlatMap();
    if(!map.nodes.length) return;
    var needle = caseSensitive ? query : query.toLowerCase();
    var hay = caseSensitive ? map.flat : map.flat.toLowerCase();
    var i = 0;
    while((i = hay.indexOf(needle, i)) !== -1){
      var end = i + needle.length;
      var sni = upperBound(map.offsets, i) - 1;
      var eni = upperBound(map.offsets, end - 1) - 1;
      matches.push({
        startNode: map.nodes[sni], startOffset: i - map.offsets[sni],
        endNode:   map.nodes[eni], endOffset:   end - map.offsets[eni]
      });
      i = end;
    }
  }

  function clearSelection(){
    var s = window.getSelection && window.getSelection();
    if(s && s.removeAllRanges){ try { s.removeAllRanges(); } catch(_){} }
  }

  function highlightCurrent(){
    clearSelection();
    if(idx < 0 || idx >= matches.length) return;
    var m = matches[idx];
    try {
      var range = document.createRange();
      range.setStart(m.startNode, m.startOffset);
      range.setEnd(m.endNode, m.endOffset);
      var sel = window.getSelection && window.getSelection();
      if(sel && sel.addRange) sel.addRange(range);
      var el = m.startNode.parentElement ||
        (m.startNode.parentNode && m.startNode.parentNode.nodeType === 1 ? m.startNode.parentNode : null);
      if(el && el.scrollIntoView) el.scrollIntoView({ block: "center", inline: "nearest" });
    } catch(_){}
  }

  function post(){
    try {
      parent.postMessage({
        source: RESULT,
        found: idx >= 0,
        total: matches.length,
        current: idx >= 0 ? idx + 1 : 0
      }, "*");
    } catch(_){}
  }

  // Rebuild only when query/case changed or the DOM was mutated.
  function ensure(query, caseSensitive){
    if(!dirty && query === lastQuery && caseSensitive === lastCase) return false;
    collect(query, caseSensitive);
    idx = matches.length ? 0 : -1;
    return true;
  }

  function doSearch(query, caseSensitive){
    ensure(query, caseSensitive);
    idx = matches.length ? 0 : -1;
    highlightCurrent();
    post();
  }

  function step(query, caseSensitive, backwards){
    var rebuilt = ensure(query, caseSensitive);
    if(!matches.length){ idx = -1; post(); return; }
    if(!rebuilt){
      idx = backwards
        ? (idx <= 0 ? matches.length - 1 : idx - 1)
        : (idx >= matches.length - 1 ? 0 : idx + 1);
    }
    highlightCurrent();
    post();
  }

  window.addEventListener("message", function(e){
    // Only accept commands from the hosting parent window. This prevents
    // sandboxed content or a third-party frame from injecting find commands.
    // Skip the check in a top-level context (parent === window) where there is
    // no meaningful "parent" to verify against — this keeps the shim testable
    // in jsdom without relaxing the guard in real browser iframe use.
    if(parent !== window && e.source !== parent) return;
    var d = e && e.data;
    if(!d || d.source !== CMD) return;
    if(d.action === "search") doSearch(d.query || "", !!d.caseSensitive);
    else if(d.action === "next") step(d.query || "", !!d.caseSensitive, false);
    else if(d.action === "prev") step(d.query || "", !!d.caseSensitive, true);
    else if(d.action === "clear"){
      matches = []; idx = -1; lastQuery = null; dirty = false; clearSelection();
    }
  });

  window.addEventListener("keydown", function(e){
    if((e.ctrlKey || e.metaKey) && (e.key === "f" || e.key === "F")){
      e.preventDefault();
      try { parent.postMessage({ source: OPEN }, "*"); } catch(_){}
    }
  });
})();
</script>`;

/**
 * Appends the find shim to an HTML document string destined for a sandboxed
 * srcdoc iframe. Compose with withFragmentNavShim, e.g.
 * `withFindShim(withFragmentNavShim(text))`.
 */
export function withFindShim(html: string | undefined): string {
  return (html ?? "") + FIND_SHIM;
}

/** Exposed for unit tests so they can assert the shim was appended verbatim. */
export const __IFRAME_FIND_SHIM__ = FIND_SHIM;
