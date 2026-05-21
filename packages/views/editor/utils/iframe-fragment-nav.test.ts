import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  __FRAGMENT_NAV_SHIM__,
  withFragmentNavShim,
} from "./iframe-fragment-nav";

describe("withFragmentNavShim", () => {
  it("appends the shim verbatim at the end of the original HTML", () => {
    const html = "<h1 id='a'>A</h1><a href='#a'>jump</a>";
    const out = withFragmentNavShim(html);
    expect(out.startsWith(html)).toBe(true);
    expect(out.endsWith(__FRAGMENT_NAV_SHIM__)).toBe(true);
    expect(out).toBe(html + __FRAGMENT_NAV_SHIM__);
  });

  it("does not mutate the input string", () => {
    const html = "<p>hi</p>";
    withFragmentNavShim(html);
    expect(html).toBe("<p>hi</p>");
  });

  it("handles empty input", () => {
    expect(withFragmentNavShim("")).toBe(__FRAGMENT_NAV_SHIM__);
  });
});

// The shim itself ships as a <script> string injected into a srcdoc iframe.
// To exercise its runtime behavior in unit tests, evaluate the inner script
// against the current document — jsdom's environment matches what runs inside
// the iframe closely enough for the click-handling contract.
//
// scrollIntoView is not implemented in jsdom; we stub it per-test.
function loadShimIntoDocument() {
  const inner = __FRAGMENT_NAV_SHIM__
    .replace(/^<script>/, "")
    .replace(/<\/script>$/, "");
  new Function(inner)();
}

describe("fragment-nav shim runtime behavior", () => {
  let scrollSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    document.body.innerHTML = "";
    scrollSpy = vi.fn();
    // Patch the prototype so any element we create inherits the stub.
    Object.defineProperty(window.Element.prototype, "scrollIntoView", {
      configurable: true,
      writable: true,
      value: scrollSpy,
    });
    loadShimIntoDocument();
  });

  afterEach(() => {
    document.body.innerHTML = "";
    // Reload document listeners by clearing the DOM; jsdom isolates the
    // listener registration to the document instance, but next test calls
    // loadShimIntoDocument() again — that double-registers handlers across
    // tests within the same document. We isolate by recreating the link
    // each test and asserting based on whether scrollIntoView fired *on the
    // intended target*, not call-count totals.
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
