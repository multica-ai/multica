import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  __locationBridgeScript__,
  HTML_PREVIEW_LOCATION_KEY as K,
  normalizeHtmlPreviewAddress,
  readHtmlPreviewLocationMessage,
  splitHtmlPreviewAddress,
  withLocationBridge,
} from "./iframe-location-bridge";

const PREFIX_RE = /^<script>\(function\(\)\{[^\n]*\}\)\(\);<\/script>/;

describe("withLocationBridge", () => {
  it("puts the bridge after a leading doctype so the document stays in standards mode", () => {
    const out = withLocationBridge("<!doctype html><p>hi</p>", "");
    expect(out.startsWith("<!doctype html><script>")).toBe(true);
    expect(out.endsWith("<p>hi</p>")).toBe(true);
  });

  it("keeps comments that precede the doctype in front of it", () => {
    const out = withLocationBridge("<!-- mock -->\n<!DOCTYPE html><p>hi</p>", "");
    expect(out.startsWith("<!-- mock -->\n<!DOCTYPE html><script>")).toBe(true);
  });

  it("prepends the bridge to a document without a doctype, on one line", () => {
    const out = withLocationBridge("<p>hi</p>", "?s=1");
    expect(out).toMatch(PREFIX_RE);
    expect(out.endsWith("</script><p>hi</p>")).toBe(true);
  });

  it("cannot be closed early by the address it carries", () => {
    const out = withLocationBridge("<p>hi</p>", "?x=</script><script>alert(1)</script>");
    expect(out.match(/<\/script>/g)).toHaveLength(1);
  });
});

describe("normalizeHtmlPreviewAddress", () => {
  it.each([
    ["", ""],
    ["   ", ""],
    ["?s=overview", "?s=overview"],
    [" ?s=overview#top ", "?s=overview#top"],
    ["#anatomy", "#anatomy"],
    ["mock.html?s=ia", "?s=ia"],
    ["https://example.com/other.html#x", "#x"],
    ["s=ia", "?s=ia"],
    ["mock.html", ""],
  ])("%j → %j", (input, expected) => {
    expect(normalizeHtmlPreviewAddress(input, "mock.html")).toBe(expected);
  });

  it("caps the length", () => {
    expect(normalizeHtmlPreviewAddress(`?${"a".repeat(5000)}`)).toHaveLength(2048);
  });
});

describe("splitHtmlPreviewAddress", () => {
  it.each([
    ["", { search: "", hash: "" }],
    ["?s=1", { search: "?s=1", hash: "" }],
    ["#top", { search: "", hash: "#top" }],
    ["?s=1#top", { search: "?s=1", hash: "#top" }],
  ])("%j", (address, expected) => {
    expect(splitHtmlPreviewAddress(address)).toEqual(expected);
  });
});

describe("readHtmlPreviewLocationMessage", () => {
  it("accepts a location report and a navigate request", () => {
    expect(readHtmlPreviewLocationMessage({ [K]: 1, type: "location", value: "?s=1#a" })).toEqual({
      type: "location",
      value: "?s=1#a",
    });
    expect(readHtmlPreviewLocationMessage({ [K]: 1, type: "location", value: "" })).toEqual({
      type: "location",
      value: "",
    });
    expect(readHtmlPreviewLocationMessage({ [K]: 1, type: "navigate", value: "?s=2" })).toEqual({
      type: "navigate",
      value: "?s=2",
    });
  });

  it.each([
    ["no marker", { type: "location", value: "?s=1" }],
    ["unknown type", { [K]: 1, type: "hash", value: "#a" }],
    ["non-string value", { [K]: 1, type: "location", value: 1 }],
    ["a path, not a query or fragment", { [K]: 1, type: "location", value: "/evil" }],
    ["a full URL", { [K]: 1, type: "navigate", value: "https://evil.test/?x" }],
    ["an empty navigate", { [K]: 1, type: "navigate", value: "" }],
    ["an oversized value", { [K]: 1, type: "location", value: `?${"a".repeat(3000)}` }],
    ["null", null],
    ["a string", "?s=1"],
  ])("rejects %s", (_label, data) => {
    expect(readHtmlPreviewLocationMessage(data)).toBeNull();
  });
});

// The bridge ships as a <script> string injected into a srcdoc iframe. Like
// iframe-fragment-nav.test.ts, evaluate it against the current jsdom
// document, whose `parent` is itself; its postMessage is stubbed to capture
// what the bridge reports. Each load's listeners and History wrappers are
// removed after the test so loads never stack. (The iframe's real
// `about:srcdoc` URL is Chromium/WebKit behavior jsdom does not model; its
// http URL exercises the same code paths.)
let unload: (() => void) | null = null;

function loadBridge(address = "") {
  const postMessage = vi.spyOn(window, "postMessage").mockImplementation(() => {});
  const listeners: Array<[string, EventListenerOrEventListenerObject]> = [];
  const add = window.addEventListener.bind(window);
  const spy = vi
    .spyOn(window, "addEventListener")
    .mockImplementation((type: string, listener: EventListenerOrEventListenerObject | null) => {
      if (!listener) return;
      listeners.push([type, listener]);
      add(type, listener);
    });
  new Function(__locationBridgeScript__(address))();
  spy.mockRestore();
  unload = () => {
    for (const [type, listener] of listeners) window.removeEventListener(type, listener);
    delete (window.history as Partial<History>).pushState;
    delete (window.history as Partial<History>).replaceState;
  };
  const messages = (type: string) =>
    postMessage.mock.calls
      .map(([m]) => m as { type: string; value: string })
      .filter((m) => m.type === type)
      .map((m) => m.value);
  return {
    reported: () => messages("location"),
    navigations: () => messages("navigate"),
  };
}

function hashChanged() {
  return new Promise<void>((resolve) =>
    window.addEventListener("hashchange", () => resolve(), { once: true }),
  );
}

function addLink(href: string, attrs: Record<string, string> = {}) {
  const link = document.createElement("a");
  link.setAttribute("href", href);
  for (const [name, value] of Object.entries(attrs)) link.setAttribute(name, value);
  document.body.appendChild(link);
  return link;
}

function click(el: Element, init: MouseEventInit = {}) {
  const event = new MouseEvent("click", { bubbles: true, cancelable: true, ...init });
  el.dispatchEvent(event);
  return event;
}

describe("location bridge runtime", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/doc");
  });

  afterEach(() => {
    unload?.();
    unload = null;
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  it("opens the document at the initial address and reports it", () => {
    const { reported } = loadBridge("?s=ia#anatomy");
    expect(window.location.search).toBe("?s=ia");
    expect(window.location.hash).toBe("#anatomy");
    expect(new URLSearchParams(window.location.search).get("s")).toBe("ia");
    expect(reported()).toEqual(["?s=ia#anatomy"]);
  });

  it("leaves the URL alone when opened at the bare address", () => {
    const { reported } = loadBridge();
    expect(window.location.pathname + window.location.search).toBe("/doc");
    expect(reported()).toEqual([""]);
  });

  it("resolves relative pushState / replaceState URLs against the document and reports them", () => {
    const { reported } = loadBridge("?s=a");
    window.history.pushState({ n: 1 }, "", "?s=b");
    expect(window.location.search).toBe("?s=b");
    expect(window.history.state).toEqual({ n: 1 });
    window.history.replaceState(null, "", "#top");
    expect(window.location.pathname + window.location.search + window.location.hash).toBe(
      "/doc?s=b#top",
    );
    expect(reported()).toEqual(["?s=a", "?s=b", "?s=b#top"]);
  });

  it("turns a click on a ?… link into a navigate request", () => {
    const { navigations } = loadBridge();
    const event = click(addLink("?s=next"));
    expect(event.defaultPrevented).toBe(true);
    expect(navigations()).toEqual(["?s=next"]);
    expect(window.location.search).toBe("");
  });

  it("makes a #… link with no target a real hash change", async () => {
    const { reported } = loadBridge();
    const changed = hashChanged();
    click(addLink("#/settings"));
    await changed;
    expect(window.location.hash).toBe("#/settings");
    expect(reported()).toContain("#/settings");
  });

  it("leaves a click alone once something else claimed it, or when it opens elsewhere", () => {
    const { navigations } = loadBridge();
    const claimed = addLink("?s=1");
    claimed.addEventListener("click", (e) => e.preventDefault());
    click(claimed);
    click(addLink("?s=2", { target: "_blank" }));
    click(addLink("?s=3"), { metaKey: true });
    expect(navigations()).toEqual([]);
  });

  it("applies a hash sent by the parent, and only by the parent", async () => {
    loadBridge("?s=a");
    window.dispatchEvent(
      new MessageEvent("message", { data: { [K]: 1, type: "hash", value: "#ignored" }, source: null }),
    );
    expect(window.location.hash).toBe("");
    const changed = hashChanged();
    window.dispatchEvent(
      new MessageEvent("message", { data: { [K]: 1, type: "hash", value: "#section" }, source: window }),
    );
    await changed;
    expect(window.location.search + window.location.hash).toBe("?s=a#section");
  });
});
