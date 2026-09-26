import { afterEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useHtmlPreviewLocation } from "./use-html-preview-location";
import {
  HTML_PREVIEW_LOCATION_KEY as K,
  withLocationBridge,
} from "../utils/iframe-location-bridge";

function mountFrame() {
  const iframe = document.createElement("iframe");
  document.body.appendChild(iframe);
  const postMessage = vi.spyOn(iframe.contentWindow!, "postMessage");
  return { iframe, postMessage };
}

function post(source: Window | null, data: unknown) {
  act(() => {
    window.dispatchEvent(new MessageEvent("message", { data, source }));
  });
}

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("useHtmlPreviewLocation", () => {
  it("loads the document at the initial address", () => {
    const { result } = renderHook(() => useHtmlPreviewLocation("?s=ia"));
    expect(result.current.address).toBe("?s=ia");
    expect(result.current.withAddress("<p>x</p>")).toBe(withLocationBridge("<p>x</p>", "?s=ia"));
  });

  it("follows the address its own frame reports, and no other window's", () => {
    const { iframe } = mountFrame();
    const other = mountFrame().iframe;
    const { result } = renderHook(() => useHtmlPreviewLocation());
    act(() => result.current.frameRef(iframe));

    post(other.contentWindow, { [K]: 1, type: "location", value: "?s=spoof" });
    expect(result.current.address).toBe("");

    post(iframe.contentWindow, { [K]: 1, type: "location", value: "?s=b#top" });
    expect(result.current.address).toBe("?s=b#top");
    // In-document navigation does not reload the frame.
    expect(result.current.frameKey).toBe(0);
  });

  it("reloads the frame for a new query", () => {
    const { iframe, postMessage } = mountFrame();
    const { result } = renderHook(() => useHtmlPreviewLocation("?s=a"));
    act(() => result.current.frameRef(iframe));

    act(() => result.current.navigate("?s=b#top"));
    expect(result.current.frameKey).toBe(1);
    expect(result.current.address).toBe("?s=b#top");
    expect(result.current.withAddress("")).toBe(withLocationBridge("", "?s=b#top"));
    expect(postMessage).not.toHaveBeenCalled();
  });

  it("sends a fragment-only change to the running document instead of reloading", () => {
    const { iframe, postMessage } = mountFrame();
    const { result } = renderHook(() => useHtmlPreviewLocation("?s=a"));
    act(() => result.current.frameRef(iframe));

    act(() => result.current.navigate("?s=a#anatomy"));
    expect(result.current.frameKey).toBe(0);
    expect(postMessage).toHaveBeenCalledWith({ [K]: 1, type: "hash", value: "#anatomy" }, "*");
  });

  it("reloads at the address the document navigated to", () => {
    const { iframe } = mountFrame();
    const { result } = renderHook(() => useHtmlPreviewLocation("?s=a"));
    act(() => result.current.frameRef(iframe));
    post(iframe.contentWindow, { [K]: 1, type: "location", value: "?s=c" });

    act(() => result.current.reload());
    expect(result.current.frameKey).toBe(1);
    expect(result.current.withAddress("")).toBe(withLocationBridge("", "?s=c"));
  });

  it("follows a link's navigate request only right after a click", () => {
    const { iframe } = mountFrame();
    // jsdom has no user activation; model the browser's.
    const activation = { isActive: false };
    Object.defineProperty(navigator, "userActivation", {
      configurable: true,
      value: activation,
    });
    try {
      const { result } = renderHook(() => useHtmlPreviewLocation());
      act(() => result.current.frameRef(iframe));

      post(iframe.contentWindow, { [K]: 1, type: "navigate", value: "?s=loop" });
      expect(result.current.frameKey).toBe(0);

      activation.isActive = true;
      post(iframe.contentWindow, { [K]: 1, type: "navigate", value: "?s=next" });
      expect(result.current.frameKey).toBe(1);
      expect(result.current.address).toBe("?s=next");
    } finally {
      delete (navigator as { userActivation?: unknown }).userActivation;
    }
  });
});
