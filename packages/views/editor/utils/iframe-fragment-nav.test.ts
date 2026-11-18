import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { __FRAGMENT_NAV_SHIM__ } from "./iframe-fragment-nav";
import { __locationBridgeScript__, HTML_PREVIEW_LOCATION_KEY } from "./iframe-location-bridge";

// The shim itself ships as a <script> string injected into a srcdoc iframe.
// To exercise its runtime behavior in unit tests, evaluate the inner script
// against the current document — jsdom's environment matches what runs inside
// the iframe closely enough for the click-handling contract.
//
// scrollIntoView is not implemented in jsdom; we stub it per-test.
function loadShimIntoDocument() {
  const registration = vi.spyOn(document, "addEventListener");
  const inner = __FRAGMENT_NAV_SHIM__
    .replace(/^<script>/, "")
    .replace(/<\/script>$/, "");
  new Function(inner)();
  const listeners = [...registration.mock.calls];
  registration.mockRestore();
  return () => {
    for (const [type, listener, options] of listeners) {
      document.removeEventListener(type, listener, options);
    }
  };
}

describe("fragment-nav shim runtime behavior", () => {
  let scrollSpy: ReturnType<typeof vi.fn>;
  let hashChange: ReturnType<typeof vi.fn<(event: HashChangeEvent) => void>>;
  let unloadShim: () => void;
  let unloadBridge: (() => void) | undefined;

  beforeEach(() => {
    document.body.innerHTML = "";
    window.history.replaceState(null, "", "/report?s=overview");
    hashChange = vi.fn<(event: HashChangeEvent) => void>();
    window.addEventListener("hashchange", hashChange);
    scrollSpy = vi.fn();
    // Patch the prototype so any element we create inherits the stub.
    Object.defineProperty(window.Element.prototype, "scrollIntoView", {
      configurable: true,
      writable: true,
      value: scrollSpy,
    });
    unloadShim = loadShimIntoDocument();
  });

  afterEach(async () => {
    unloadShim();
    unloadBridge?.();
    unloadBridge = undefined;
    window.removeEventListener("hashchange", hashChange);
    document.body.innerHTML = "";
    vi.restoreAllMocks();
    // Drain native hashchange tasks before the next test installs listeners.
    await new Promise((resolve) => setTimeout(resolve, 0));
  });

  it.each([false, true])("updates fragments without adding history (location bridge: %s)", async (withBridge) => {
    const postMessage = vi.spyOn(window, "postMessage").mockImplementation(() => {});
    if (withBridge) {
      const registration = vi.spyOn(window, "addEventListener");
      new Function(__locationBridgeScript__(""))();
      const listeners = [...registration.mock.calls];
      registration.mockRestore();
      unloadBridge = () => {
        for (const [type, listener, options] of listeners) {
          window.removeEventListener(type, listener, options);
        }
        delete (window.history as Partial<History>).pushState;
        delete (window.history as Partial<History>).replaceState;
      };
    }
    document.body.innerHTML = '<section id="page-2"></section><section id="detail"></section><a>Go</a>';
    const link = document.querySelector("a")!;
    const historyLength = window.history.length;
    let oldURL = window.location.href;
    for (const [index, hash] of ["#page-2", "#detail"].entries()) {
      link.setAttribute("href", hash);
      link.click();
      await vi.waitFor(() => expect(hashChange).toHaveBeenCalledTimes(index + 1));
      expect(window.location.search + window.location.hash).toBe(`?s=overview${hash}`);
      expect(document.querySelector(":target")?.id).toBe(hash.slice(1));
      expect(window.history.length).toBe(historyLength);
      const event = hashChange.mock.calls[index]![0];
      expect(event.oldURL).toBe(oldURL);
      expect(event.newURL).toBe(window.location.href);
      oldURL = window.location.href;
      if (withBridge) {
        expect(postMessage).toHaveBeenCalledWith({
          [HTML_PREVIEW_LOCATION_KEY]: 1, type: "location", value: `?s=overview${hash}`,
        }, "*");
      }
    }
    // Repeating the active fragment must neither add history nor fire again.
    link.click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(hashChange).toHaveBeenCalledTimes(2);
    expect(window.history.length).toBe(historyLength);
  });

  it("scrolls the matching target into view when a fragment link is clicked", () => {
    const section = document.createElement("section");
    section.id = "intro";
    section.textContent = "intro";
    document.body.appendChild(section);

    const link = document.createElement("a");
    link.setAttribute("href", "#intro");
    link.textContent = "go";
    document.body.appendChild(link);

    const evt = new MouseEvent("click", { bubbles: true, cancelable: true });
    link.dispatchEvent(evt);

    expect(scrollSpy).toHaveBeenCalled();
    expect(scrollSpy.mock.instances[0]).toBe(section);
    expect(evt.defaultPrevented).toBe(true);
  });

  it("falls back to <a name='...'> when no element id matches", () => {
    const target = document.createElement("a");
    target.setAttribute("name", "legacy");
    document.body.appendChild(target);

    const link = document.createElement("a");
    link.setAttribute("href", "#legacy");
    document.body.appendChild(link);

    const evt = new MouseEvent("click", { bubbles: true, cancelable: true });
    link.dispatchEvent(evt);

    expect(scrollSpy).toHaveBeenCalled();
    expect(scrollSpy.mock.instances[0]).toBe(target);
    expect(evt.defaultPrevented).toBe(true);
  });

  it("decodes percent-encoded fragment ids", () => {
    const section = document.createElement("section");
    section.id = "中文";
    document.body.appendChild(section);

    const link = document.createElement("a");
    link.setAttribute("href", `#${encodeURIComponent("中文")}`);
    document.body.appendChild(link);

    link.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));

    expect(scrollSpy).toHaveBeenCalled();
    expect(scrollSpy.mock.instances[0]).toBe(section);
  });

  it("does not intercept when the click target is not inside an anchor", () => {
    const div = document.createElement("div");
    div.textContent = "not a link";
    document.body.appendChild(div);

    const evt = new MouseEvent("click", { bubbles: true, cancelable: true });
    div.dispatchEvent(evt);

    expect(scrollSpy).not.toHaveBeenCalled();
    expect(evt.defaultPrevented).toBe(false);
  });

  it("does not intercept links to external URLs", () => {
    const link = document.createElement("a");
    link.setAttribute("href", "https://example.com/page#section");
    document.body.appendChild(link);

    const evt = new MouseEvent("click", { bubbles: true, cancelable: true });
    link.dispatchEvent(evt);

    expect(scrollSpy).not.toHaveBeenCalled();
    expect(evt.defaultPrevented).toBe(false);
  });

  it("does not intercept bare '#' links", () => {
    const link = document.createElement("a");
    link.setAttribute("href", "#");
    document.body.appendChild(link);

    const evt = new MouseEvent("click", { bubbles: true, cancelable: true });
    link.dispatchEvent(evt);

    expect(scrollSpy).not.toHaveBeenCalled();
    expect(evt.defaultPrevented).toBe(false);
  });

  it("does not intercept when target id is missing — lets in-document handlers run", () => {
    const link = document.createElement("a");
    link.setAttribute("href", "#nonexistent");
    document.body.appendChild(link);

    const evt = new MouseEvent("click", { bubbles: true, cancelable: true });
    link.dispatchEvent(evt);

    expect(scrollSpy).not.toHaveBeenCalled();
    expect(evt.defaultPrevented).toBe(false);
  });

  it("yields to a user handler that already called preventDefault", () => {
    const section = document.createElement("section");
    section.id = "intro";
    document.body.appendChild(section);

    const link = document.createElement("a");
    link.setAttribute("href", "#intro");
    document.body.appendChild(link);

    // A user-installed handler that suppresses default behavior. Capture
    // phase + preventDefault — our shim must see defaultPrevented and bail.
    document.addEventListener(
      "click",
      (e) => {
        if (e.target === link) e.preventDefault();
      },
      true,
    );

    link.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));

    expect(scrollSpy).not.toHaveBeenCalled();
  });
});
