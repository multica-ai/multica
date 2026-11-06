import { afterEach, describe, expect, it, vi } from "vitest";
import {
  __WIDTH_BRIDGE_SCRIPT__,
  HTML_PREVIEW_WIDTH_KEY,
  readHtmlPreviewWidthMessage,
  withWidthBridge,
} from "./iframe-width-bridge";

const K = HTML_PREVIEW_WIDTH_KEY;

describe("withWidthBridge", () => {
  it("appends the bridge after the author's markup, leaving a doctype first", () => {
    const html = "<!doctype html><p>hi</p>";
    expect(withWidthBridge(html)).toBe(`${html}<script>${__WIDTH_BRIDGE_SCRIPT__}</script>`);
  });
});

describe("readHtmlPreviewWidthMessage", () => {
  it("reads a width report", () => {
    expect(
      readHtmlPreviewWidthMessage({ [K]: 1, type: "width", doc: "abc", viewport: 1000, needed: 1080 }),
    ).toEqual({ doc: "abc", viewport: 1000, needed: 1080 });
  });

  it.each([
    ["not an object", "width"],
    ["no marker", { type: "width", doc: "a", viewport: 1, needed: 1 }],
    ["another type", { [K]: 1, type: "location", doc: "a", viewport: 1, needed: 1 }],
    ["no doc", { [K]: 1, type: "width", viewport: 1, needed: 1 }],
    ["a long doc", { [K]: 1, type: "width", doc: "x".repeat(33), viewport: 1, needed: 1 }],
    ["a string width", { [K]: 1, type: "width", doc: "a", viewport: "1000", needed: 1 }],
    ["a zero width", { [K]: 1, type: "width", doc: "a", viewport: 0, needed: 1 }],
    ["an unbounded width", { [K]: 1, type: "width", doc: "a", viewport: 1, needed: 1e9 }],
    ["a non-finite width", { [K]: 1, type: "width", doc: "a", viewport: 1, needed: Infinity }],
  ])("drops %s", (_, data) => {
    expect(readHtmlPreviewWidthMessage(data)).toBeNull();
  });
});

// Evaluated against the current jsdom document, whose `parent` is itself;
// postMessage is stubbed to capture the reports. jsdom has no layout, so the
// widths are stubbed on the root element.
describe("the bridge script", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.documentElement.style.overflowX = "";
  });

  let listeners: Array<[string, EventListener]> = [];

  function run(layout: { viewport: number; client: number; scroll: number }) {
    const postMessage = vi.spyOn(window, "postMessage").mockImplementation(() => {});
    vi.spyOn(window, "innerWidth", "get").mockReturnValue(layout.viewport);
    vi.spyOn(document.documentElement, "clientWidth", "get").mockReturnValue(layout.client);
    vi.spyOn(document.documentElement, "scrollWidth", "get").mockReturnValue(layout.scroll);
    listeners = [];
    vi.spyOn(window, "addEventListener").mockImplementation((type: string, listener) => {
      listeners.push([type, listener as EventListener]);
    });
    new Function(__WIDTH_BRIDGE_SCRIPT__)();
    return postMessage.mock.calls.map(([m]) => readHtmlPreviewWidthMessage(m));
  }

  function fire(type: string, event: Event) {
    for (const [t, listener] of listeners) if (t === type) listener(event);
  }

  it("reports again only when the layout changed, unless the parent asks", () => {
    run({ viewport: 1000, client: 1000, scroll: 1000 });
    const postMessage = vi.mocked(window.postMessage);
    fire("resize", new Event("resize"));
    expect(postMessage).toHaveBeenCalledTimes(1);

    fire("message", new MessageEvent("message", { data: { [K]: 1, type: "measure" }, source: window }));
    expect(postMessage).toHaveBeenCalledTimes(2);
    fire("message", new MessageEvent("message", { data: { [K]: 1, type: "measure" }, source: null }));
    expect(postMessage).toHaveBeenCalledTimes(2);
  });

  it("reports a document that fits as needing its viewport", () => {
    const [report] = run({ viewport: 1000, client: 1000, scroll: 1000 });
    expect(report).toMatchObject({ viewport: 1000, needed: 1000 });
  });

  it("reports the overflow on top of the viewport, scrollbar gutter included", () => {
    const [report] = run({ viewport: 1000, client: 985, scroll: 1065 });
    expect(report).toMatchObject({ viewport: 1000, needed: 1080 });
  });

  it("takes a document that clips its own overflow at its word", () => {
    document.documentElement.style.overflowX = "hidden";
    const [report] = run({ viewport: 1000, client: 1000, scroll: 1400 });
    expect(report).toMatchObject({ viewport: 1000, needed: 1000 });
  });
});
