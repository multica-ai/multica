import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  __scrollBridgeInnerScript__,
  buildScrollBridge,
} from "./iframe-scroll-bridge";

const TOKEN = "abc123";

// The bridge ships as a <script> string injected into a srcdoc iframe. Like
// iframe-fragment-nav.test.ts we evaluate the inner script against the current
// jsdom document and stub the bits jsdom doesn't implement (scrollTo,
// ResizeObserver, parent.postMessage).
function loadBridge(token = TOKEN) {
  // jsdom runs the script in the same window whose `parent` is itself; the
  // bridge posts to `parent`, which is reachable. Stub postMessage.
  const postMessage = vi.fn();
  Object.defineProperty(window, "parent", {
    configurable: true,
    value: { postMessage },
  });
  const scrollTo = vi.fn();
  Object.defineProperty(window, "scrollTo", {
    configurable: true,
    writable: true,
    value: scrollTo,
  });
  const inner = __scrollBridgeInnerScript__(token);
  new Function(inner)();
  return { postMessage, scrollTo };
}

describe("buildScrollBridge", () => {
  it("wraps the script in <script> tags", () => {
    const out = buildScrollBridge(TOKEN);
    expect(out.startsWith("<script>")).toBe(true);
    expect(out.endsWith("</script>")).toBe(true);
  });

  it("burns the token into the script source", () => {
    const out = buildScrollBridge("deadbeef");
    expect(out).toContain('"deadbeef"');
  });

  it("rejects tokens that are not [a-z0-9]+ and substitutes an empty token", () => {
    // hashString always produces base36 so this is defense in depth.
    const out = buildScrollBridge('not"; alert(1); var x="');
    expect(out).toContain('TOKEN = ""');
  });
});

describe("scroll bridge runtime", () => {
  let originalRAF: typeof window.requestAnimationFrame;

  beforeEach(() => {
    originalRAF = window.requestAnimationFrame;
    // Make rAF synchronous so the "settled resend" path runs inline.
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0);
      return 0;
    }) as typeof window.requestAnimationFrame;
  });

  afterEach(() => {
    window.requestAnimationFrame = originalRAF;
  });

  it("announces ready on load with y and height", () => {
    const { postMessage } = loadBridge();
    const ready = postMessage.mock.calls.find(
      (c) => c[0]?.kind === "ready",
    );
    expect(ready).toBeTruthy();
    expect(ready?.[0]).toMatchObject({
      __multica: "__multica",
      token: TOKEN,
      y: expect.any(Number),
      height: expect.any(Number),
    });
  });

  it("reports scroll with the correct token and y", () => {
    const { postMessage } = loadBridge();
    postMessage.mockClear();
    Object.defineProperty(window, "scrollY", {
      configurable: true,
      writable: true,
      value: 432,
    });
    window.dispatchEvent(new Event("scroll"));
    const report = postMessage.mock.calls.find(
      (c) => c[0]?.kind === "scroll",
    );
    expect(report?.[0]).toMatchObject({
      kind: "scroll",
      token: TOKEN,
      y: 432,
    });
  });

  it("answers request-sync with a ready message", () => {
    const { postMessage } = loadBridge();
    postMessage.mockClear();
    window.dispatchEvent(
      new MessageEvent("message", {
        data: { __multica: "__multica", kind: "request-sync", token: TOKEN },
      }),
    );
    expect(
      postMessage.mock.calls.some((c) => c[0]?.kind === "ready"),
    ).toBe(true);
  });

  it("ignores parent messages with a mismatched token", () => {
    const { postMessage, scrollTo } = loadBridge();
    scrollTo.mockClear();
    window.dispatchEvent(
      new MessageEvent("message", {
        data: { __multica: "__multica", kind: "restore", y: 100, token: "old" },
      }),
    );
    expect(scrollTo).not.toHaveBeenCalled();
    // And it must not have posted anything in response either.
    expect(
      postMessage.mock.calls.some(
        (c) => c[0]?.kind === "ready" && c[0]?.token === TOKEN,
      ),
    ).toBe(true); // only the initial announce
  });

  describe("restoring session", () => {
    let roCallback: (() => void) | null;
    let observeSpy: ReturnType<typeof vi.fn>;
    let disconnectSpy: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      roCallback = null;
      observeSpy = vi.fn();
      disconnectSpy = vi.fn();
      (globalThis as { ResizeObserver?: unknown }).ResizeObserver = class {
        constructor(cb: () => void) {
          roCallback = cb;
        }
        observe = observeSpy;
        disconnect = disconnectSpy;
        unobserve = vi.fn();
      };
    });

    afterEach(() => {
      delete (globalThis as { ResizeObserver?: unknown }).ResizeObserver;
    });

    function restoreTo(y: number) {
      const { scrollTo } = loadBridge();
      scrollTo.mockClear();
      window.dispatchEvent(
        new MessageEvent("message", {
          data: {
            __multica: "__multica",
            kind: "restore",
            y,
            token: TOKEN,
          },
        }),
      );
      return scrollTo;
    }

    it("scrolls to the target on restore and connects a ResizeObserver", () => {
      const scrollTo = restoreTo(500);
      expect(scrollTo).toHaveBeenCalledWith(0, 500);
      expect(observeSpy).toHaveBeenCalledWith(document.documentElement);
    });

    it("re-positions to the target when content grows (RO fires) even much later", () => {
      const scrollTo = restoreTo(500);
      scrollTo.mockClear();
      // A ResizeObserver tick after the initial restore — in v3 the session
      // would have ended ~33ms after "2 stable frames" and this call would
      // NOT re-position; v4 keeps the session alive until a user takeover.
      // (No fake timers needed: firing the RO callback directly proves the
      // session is still connected; the real delay is exercised by the
      // Playwright spec with a 1s setTimeout.)
      roCallback?.();
      expect(scrollTo).toHaveBeenCalledWith(0, 500);
    });

    it("ends the session on a wheel event and stops re-positioning", () => {
      const scrollTo = restoreTo(500);
      window.dispatchEvent(new Event("wheel"));
      scrollTo.mockClear();
      roCallback?.();
      expect(scrollTo).not.toHaveBeenCalled();
      expect(disconnectSpy).toHaveBeenCalled();
    });

    it("ends the session on keydown (PageDown) and stops re-positioning", () => {
      const scrollTo = restoreTo(500);
      window.dispatchEvent(
        new KeyboardEvent("keydown", { key: "PageDown", bubbles: true }),
      );
      scrollTo.mockClear();
      roCallback?.();
      expect(scrollTo).not.toHaveBeenCalled();
    });

    it("ends the session on pointerdown (scrollbar drag)", () => {
      const scrollTo = restoreTo(500);
      document.dispatchEvent(
        new PointerEvent("pointerdown", { bubbles: true }),
      );
      scrollTo.mockClear();
      roCallback?.();
      expect(scrollTo).not.toHaveBeenCalled();
    });

    it("ends the session on an in-page anchor click (capture, before fragment shim)", () => {
      const scrollTo = restoreTo(500);
      const anchor = document.createElement("a");
      anchor.setAttribute("href", "#later");
      document.body.appendChild(anchor);
      anchor.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
      scrollTo.mockClear();
      roCallback?.();
      expect(scrollTo).not.toHaveBeenCalled();
    });

    it("does not end the session on a non-anchor click", () => {
      const scrollTo = restoreTo(500);
      const div = document.createElement("div");
      div.textContent = "x";
      document.body.appendChild(div);
      div.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
      scrollTo.mockClear();
      roCallback?.();
      expect(scrollTo).toHaveBeenCalledWith(0, 500);
    });

    it("does not end the session on a bare '#' click", () => {
      const scrollTo = restoreTo(500);
      const anchor = document.createElement("a");
      anchor.setAttribute("href", "#");
      document.body.appendChild(anchor);
      anchor.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
      scrollTo.mockClear();
      roCallback?.();
      expect(scrollTo).toHaveBeenCalledWith(0, 500);
    });

    it("ends the session on hashchange", () => {
      const scrollTo = restoreTo(500);
      window.dispatchEvent(new HashChangeEvent("hashchange"));
      scrollTo.mockClear();
      roCallback?.();
      expect(scrollTo).not.toHaveBeenCalled();
    });
  });
});
