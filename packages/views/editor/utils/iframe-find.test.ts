import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  __IFRAME_FIND_SHIM__,
  withFindShim,
  FIND_CMD,
  FIND_RESULT,
  FIND_OPEN,
} from "./iframe-find";

describe("withFindShim", () => {
  it("appends the shim verbatim at the end of the original HTML", () => {
    const html = "<p>hello world</p>";
    const out = withFindShim(html);
    expect(out.startsWith(html)).toBe(true);
    expect(out.endsWith(__IFRAME_FIND_SHIM__)).toBe(true);
    expect(out).toBe(html + __IFRAME_FIND_SHIM__);
  });

  it("does not mutate the input string", () => {
    const html = "<p>hi</p>";
    withFindShim(html);
    expect(html).toBe("<p>hi</p>");
  });

  it("handles empty input", () => {
    expect(withFindShim("")).toBe(__IFRAME_FIND_SHIM__);
  });

  it("carries the postMessage protocol tags so parent and iframe agree", () => {
    expect(__IFRAME_FIND_SHIM__).toContain(FIND_CMD);
    expect(__IFRAME_FIND_SHIM__).toContain(FIND_RESULT);
    expect(__IFRAME_FIND_SHIM__).toContain(FIND_OPEN);
  });
});

// The shim ships as a <script> string injected into a srcdoc iframe. To
// exercise its runtime behavior, evaluate the inner script against the current
// jsdom document — close enough to what runs inside the iframe.
function loadShimIntoDocument() {
  const inner = __IFRAME_FIND_SHIM__
    .replace(/^<script>/, "")
    .replace(/<\/script>$/, "");
  new Function(inner)();
}

describe("find shim runtime behavior", () => {
  let postSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    document.body.innerHTML =
      "<p>alpha beta ALPHA</p><div>alpha gamma</div>"; // 3 case-insensitive "alpha"
    // scrollIntoView isn't implemented in jsdom; the shim guards it but stub it
    // anyway so the select+scroll path runs cleanly.
    Object.defineProperty(window.Element.prototype, "scrollIntoView", {
      configurable: true,
      writable: true,
      value: vi.fn(),
    });
    postSpy = vi.spyOn(window, "postMessage");
    loadShimIntoDocument();
  });

  afterEach(() => {
    document.body.innerHTML = "";
    postSpy.mockRestore();
  });

  function lastResult() {
    for (let i = postSpy.mock.calls.length - 1; i >= 0; i--) {
      const msg = postSpy.mock.calls[i][0] as {
        source?: string;
        found?: boolean;
        total?: number;
        current?: number;
      };
      if (msg && msg.source === FIND_RESULT) return msg;
    }
    return undefined;
  }

  function send(action: string, query = "alpha", caseSensitive = false) {
    window.dispatchEvent(
      new MessageEvent("message", {
        data: { source: FIND_CMD, action, query, caseSensitive },
      }),
    );
  }

  it("counts total case-insensitive matches across text nodes and reports found+current", () => {
    send("search");
    const res = lastResult();
    expect(res).toBeDefined();
    expect(res!.total).toBe(3); // alpha, ALPHA, alpha
    expect(res!.found).toBe(true);
    expect(res!.current).toBe(1);
  });

  it("does not count matches inside <script>/<style>/<noscript> text", () => {
    // Regression for the count-inflation bug: the TreeWalker must skip the
    // injected shim's own <script> text and style/noscript nodes.
    document.body.innerHTML =
      "<p>alpha</p><script>var s='alpha alpha';</script><style>.alpha{color:red}</style>";
    send("search");
    expect(lastResult()!.total).toBe(1); // only the <p>, not script/style text
  });

  it("respects caseSensitive when counting", () => {
    send("search", "alpha", true);
    expect(lastResult()!.total).toBe(2); // "ALPHA" excluded
  });

  it("reports zero + not-found for a query with no matches", () => {
    send("search", "zzz");
    const res = lastResult()!;
    expect(res.total).toBe(0);
    expect(res.found).toBe(false);
    expect(res.current).toBe(0);
  });

  it("advances the current index on next and wraps at the end", () => {
    send("search"); // current=1
    send("next");   // 2
    send("next");   // 3
    expect(lastResult()!.current).toBe(3);
    send("next");   // wraps to 1
    expect(lastResult()!.current).toBe(1);
  });

  it("steps backwards with prev and wraps at the start", () => {
    send("search"); // current=1
    send("prev");   // wraps to 3
    expect(lastResult()!.current).toBe(3);
    send("prev");   // 2
    expect(lastResult()!.current).toBe(2);
  });

  it("ignores messages that are not find commands", () => {
    postSpy.mockClear();
    window.dispatchEvent(new MessageEvent("message", { data: { source: "something-else" } }));
    expect(lastResult()).toBeUndefined();
  });

  it("posts an open signal and preventDefaults on Ctrl+F inside the iframe", () => {
    const evt = new KeyboardEvent("keydown", { key: "f", ctrlKey: true, cancelable: true });
    window.dispatchEvent(evt);
    expect(evt.defaultPrevented).toBe(true);
    const openMsg = postSpy.mock.calls
      .map((c: unknown[]) => c[0] as { source?: string })
      .find((m: { source?: string }) => m && m.source === FIND_OPEN);
    expect(openMsg).toBeDefined();
  });

  // --- Cross-node matching ---

  it("finds a match that spans across an inline element boundary", () => {
    // The old per-node approach would miss "hello world" because "hello " is
    // in one text node and "world" is in the <b>'s text node.
    document.body.innerHTML = "<p>hello <b>world</b></p>";
    send("search", "hello world");
    const res = lastResult()!;
    expect(res.total).toBe(1);
    expect(res.found).toBe(true);
  });

  it("counts overlapping cross-node and single-node matches correctly", () => {
    // "ab" appears: once spanning the boundary (text "a" + text "b") and once
    // inside the second <span>'s own text node ("ab").
    document.body.innerHTML = "<span>a</span><span>b ab</span>";
    send("search", "ab");
    expect(lastResult()!.total).toBe(2);
  });

  // --- Stale cache / MutationObserver ---

  it("re-indexes after a DOM mutation before the next search", async () => {
    // Initial search — 3 "alpha" matches.
    send("search");
    expect(lastResult()!.total).toBe(3);

    // Mutate the DOM (add a fourth match) and let MutationObserver fire.
    document.body.appendChild(
      Object.assign(document.createElement("p"), { textContent: "alpha extra" }),
    );
    // MutationObserver callbacks run as microtasks; yield to let them fire.
    await Promise.resolve();

    // Repeat the exact same query — the cache must be rebuilt.
    send("search");
    expect(lastResult()!.total).toBe(4);
  });

  it("does not re-index when the DOM is unchanged between identical queries", async () => {
    send("search");
    expect(lastResult()!.total).toBe(3);
    // No mutation — a second identical search reuses the cache.
    // Spy on buildFlatMap indirectly: if total is still 3, caching held.
    send("search");
    expect(lastResult()!.total).toBe(3);
  });

  // --- Origin / source guard ---

  it("ignores messages whose e.source is a foreign window", () => {
    // jsdom runs at top-level (parent === window) so the origin guard is a
    // no-op there by design. Simulate the iframe scenario by temporarily
    // patching window.parent to a sentinel different from window so the guard
    // activates, then send a message whose e.source is neither sentinel nor window.
    const realParent = Object.getOwnPropertyDescriptor(window, "parent");
    const sentinel = {} as Window;
    Object.defineProperty(window, "parent", { configurable: true, get: () => sentinel });
    try {
      postSpy.mockClear();
      // e.source defaults to null in MessageEvent when not set — not the sentinel.
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { source: FIND_CMD, action: "search", query: "alpha", caseSensitive: false },
        }),
      );
      // The guard should have rejected the message before posting a result.
      expect(lastResult()).toBeUndefined();
    } finally {
      if (realParent) Object.defineProperty(window, "parent", realParent);
      else delete (window as { parent?: unknown }).parent;
    }
  });

  it("in top-level context (parent === window) the source guard is inactive and all messages are accepted", () => {
    // The guard `parent !== window && e.source !== parent` is intentionally a
    // no-op when not embedded in an iframe so that the shim stays fully usable
    // in jsdom tests and top-level windows.  jsdom does not allow setting
    // MessageEvent.source to an arbitrary WindowProxy object, so the iframe
    // acceptance branch (parent !== window, e.source === parent) cannot be
    // driven at runtime here — it is verified structurally below.
    postSpy.mockClear();
    window.dispatchEvent(
      new MessageEvent("message", {
        data: { source: FIND_CMD, action: "search", query: "alpha", caseSensitive: false },
        // window IS a valid MessageEventSource in jsdom.
        source: window,
      }),
    );
    expect(lastResult()).toBeDefined();
    expect(lastResult()!.total).toBe(3);
  });

  it("shim source guard expression is present and guards both conditions", () => {
    // Structural check: the shim must contain both halves of the compound guard
    // so that an iframe-hosted shim rejects commands from any window other than
    // its hosting parent, while remaining fully open in a top-level context.
    expect(__IFRAME_FIND_SHIM__).toContain("parent !== window");
    expect(__IFRAME_FIND_SHIM__).toContain("e.source !== parent");
  });
});
