import { describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import type {
  ExternalScrollSource,
  ScrollRestorationAdapter,
  ScrollRestorationEntry,
} from "../platform";
import { ScrollRestorationProvider } from "../platform";
import { useHtmlPreviewScrollRestore } from "./use-html-preview-scroll-restore";
import { buildScrollBridge } from "../editor/utils/iframe-scroll-bridge";
import { hashString } from "../editor/utils/hash-string";

function makeAdapter(initial?: ScrollRestorationEntry): {
  adapter: ScrollRestorationAdapter;
  registered: Map<string, ExternalScrollSource | null>;
  setEntry: (e: ScrollRestorationEntry | undefined) => void;
} {
  const registered = new Map<string, ExternalScrollSource | null>();
  let entry = initial;
  return {
    registered,
    setEntry: (e) => {
      entry = e;
    },
    adapter: {
      get: () => entry,
      registerExternalSource: (key, source) => {
        registered.set(key, source);
      },
    },
  };
}

function renderWith(
  text: string | undefined,
  adapter: ScrollRestorationAdapter,
) {
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <ScrollRestorationProvider adapter={adapter}>{children}</ScrollRestorationProvider>
  );
  return renderHook(() => useHtmlPreviewScrollRestore(text), { wrapper });
}

describe("useHtmlPreviewScrollRestore", () => {
  it("registers an external scroll source on mount and unregisters on unmount", () => {
    const { adapter, registered } = makeAdapter();
    const { unmount } = renderWith("<p>hi</p>", adapter);
    const source = registered.get("html-iframe");
    expect(source).toBeTruthy();
    expect(typeof source?.capture).toBe("function");
    unmount();
    expect(registered.get("html-iframe")).toBeNull();
  });

  it("capture() returns 0 + the current contentKey before the document reports anything", () => {
    const text = "<p>first version, long enough to hash</p>";
    const { adapter, registered } = makeAdapter();
    renderWith(text, adapter);
    const source = registered.get("html-iframe")!;
    const captured = source.capture();
    expect(captured).toEqual({
      top: 0,
      height: 0,
      contentKey: hashString(text),
    });
  });

  it("capture() returns the latest reported y only when the token matches", () => {
    const text = "<p>v1</p>";
    const { adapter, registered } = makeAdapter();
    const { result } = renderWith(text, adapter);
    const source = registered.get("html-iframe")!;
    const token = result.current.contentKey;

    // Attach the iframe ref so event.source validation passes.
    const iframe = document.createElement("iframe");
    document.body.appendChild(iframe);
    (result.current.iframeRef as (el: HTMLIFrameElement | null) => void)(iframe);

    const postSpy = vi
      .spyOn(iframe.contentWindow!, "postMessage")
      .mockImplementation(() => {});

    window.dispatchEvent(
      new MessageEvent("message", {
        source: iframe.contentWindow,
        data: {
          __multica: "__multica",
          kind: "scroll",
          y: 512,
          height: 4000,
          token,
        },
      }),
    );

    expect(source.capture()).toEqual({
      top: 512,
      height: 4000,
      contentKey: token,
    });

    // A stale document's late message (old token) must be ignored.
    window.dispatchEvent(
      new MessageEvent("message", {
        source: iframe.contentWindow,
        data: {
          __multica: "__multica",
          kind: "scroll",
          y: 9999,
          height: 1,
          token: "old-token",
        },
      }),
    );
    expect(source.capture()!.top).toBe(512);

    postSpy.mockRestore();
    document.body.removeChild(iframe);
  });

  it("resets capture() to 0 with the new contentKey when content changes (no stale carryover)", () => {
    const { adapter, registered } = makeAdapter();
    const { result, rerender } = renderHook(
      ({ text }: { text: string }) => useHtmlPreviewScrollRestore(text),
      {
        initialProps: { text: "<p>v1 long enough content body</p>" },
        wrapper: ({ children }) => (
          <ScrollRestorationProvider adapter={adapter}>
            {children}
          </ScrollRestorationProvider>
        ),
      },
    );
    const source = registered.get("html-iframe")!;

    // Simulate v1 document having reported a positive scroll.
    const v1Token = result.current.contentKey;
    const iframe = document.createElement("iframe");
    document.body.appendChild(iframe);
    (result.current.iframeRef as (el: HTMLIFrameElement | null) => void)(iframe);
    window.dispatchEvent(
      new MessageEvent("message", {
        source: iframe.contentWindow,
        data: {
          __multica: "__multica",
          kind: "scroll",
          y: 800,
          height: 5000,
          token: v1Token,
        },
      }),
    );
    expect(source.capture()!.top).toBe(800);

    // Now the content changes (re-upload). capture() must report 0 with the
    // NEW contentKey — never y=800 under the new key.
    const v2 = "<p>v2 completely different replacement content body</p>";
    rerender({ text: v2 });
    const captured = source.capture();
    expect(captured).toEqual({
      top: 0,
      height: 0,
      contentKey: hashString(v2),
    });

    document.body.removeChild(iframe);
  });

  it("posts a restore message only when saved contentKey matches and top > 0", () => {
    const text = "<p>matching content body for restore test</p>";
    const contentKey = hashString(text);
    const { adapter } = makeAdapter({ top: 300, height: 9000, contentKey });
    const { result } = renderWith(text, adapter);

    const iframe = document.createElement("iframe");
    document.body.appendChild(iframe);
    const postSpy = vi
      .spyOn(iframe.contentWindow!, "postMessage")
      .mockImplementation(() => {});
    (result.current.iframeRef as (el: HTMLIFrameElement | null) => void)(iframe);
    result.current.onLoad();

    const restoreCalls = postSpy.mock.calls.filter(
      (c) => c[0]?.kind === "restore",
    );
    expect(restoreCalls.length).toBeGreaterThan(0);
    expect(restoreCalls[0]![0]).toMatchObject({
      kind: "restore",
      y: 300,
      token: contentKey,
    });

    postSpy.mockRestore();
    document.body.removeChild(iframe);
  });

  it("does NOT post restore when saved contentKey differs from current content", () => {
    const text = "<p>current body that does not match saved key</p>";
    const { adapter } = makeAdapter({
      top: 300,
      height: 9000,
      contentKey: "some-other-content-key",
    });
    const { result } = renderWith(text, adapter);

    const iframe = document.createElement("iframe");
    document.body.appendChild(iframe);
    const postSpy = vi
      .spyOn(iframe.contentWindow!, "postMessage")
      .mockImplementation(() => {});
    (result.current.iframeRef as (el: HTMLIFrameElement | null) => void)(iframe);
    result.current.onLoad();

    expect(
      postSpy.mock.calls.some((c) => c[0]?.kind === "restore"),
    ).toBe(false);

    postSpy.mockRestore();
    document.body.removeChild(iframe);
  });

  it("is fully inert on web (no registerExternalSource on adapter): no source, no bridge", () => {
    const text = "<p>web shareable page, no desktop adapter</p>";
    const webAdapter: ScrollRestorationAdapter = { get: () => undefined };
    const { result } = renderWith(text, webAdapter);
    expect(result.current.restoreActive).toBe(false);
    const srcDoc = result.current.buildSrcDoc(text);
    expect(srcDoc).not.toContain("__multica");
    expect(srcDoc).not.toContain(buildScrollBridge("x"));
  });

  it("injects the bridge into srcDoc only when restoreActive (desktop)", () => {
    const text = "<p>desktop body</p>";
    const { adapter } = makeAdapter();
    const { result } = renderWith(text, adapter);
    expect(result.current.restoreActive).toBe(true);
    const srcDoc = result.current.buildSrcDoc(text);
    expect(srcDoc).toContain(buildScrollBridge(result.current.contentKey));
    // Fragment-nav shim is still applied too.
    expect(srcDoc).toContain("scrollIntoView");
  });

  it("handshake is bounded: ready never triggers request-sync and restore is sent at most once", () => {
    // Regression for the P0 ready → request-sync → ready infinite loop.
    const text = "<p>bounded handshake body content here</p>";
    const contentKey = hashString(text);
    const { adapter } = makeAdapter({ top: 420, height: 9000, contentKey });
    const { result } = renderWith(text, adapter);

    const iframe = document.createElement("iframe");
    document.body.appendChild(iframe);
    const postSpy = vi
      .spyOn(iframe.contentWindow!, "postMessage")
      .mockImplementation(() => {});

    // The two legitimate handshake moments: ref attach and load.
    (result.current.iframeRef as (el: HTMLIFrameElement | null) => void)(iframe);
    result.current.onLoad();

    const requestSyncAfterHandshake = postSpy.mock.calls.filter(
      (c) => c[0]?.kind === "request-sync",
    ).length;
    const restoreAfterHandshake = postSpy.mock.calls.filter(
      (c) => c[0]?.kind === "restore",
    ).length;
    // One request-sync from the ref callback, one from onLoad — never more.
    expect(requestSyncAfterHandshake).toBe(2);
    // Restore issued once despite both handshake points trying.
    expect(restoreAfterHandshake).toBe(1);

    postSpy.mockClear();

    // Simulate the bridge replying with several ready messages (it announces
    // on DOMContentLoaded + load AND answers each request-sync). NONE of these
    // may cause the parent to send another request-sync (that was the loop).
    for (let i = 0; i < 5; i++) {
      window.dispatchEvent(
        new MessageEvent("message", {
          source: iframe.contentWindow,
          data: {
            __multica: "__multica",
            kind: "ready",
            y: 100 + i,
            height: 9000,
            token: contentKey,
          },
        }),
      );
    }

    const requestSyncAfterReady = postSpy.mock.calls.filter(
      (c) => c[0]?.kind === "request-sync",
    ).length;
    const restoreAfterReady = postSpy.mock.calls.filter(
      (c) => c[0]?.kind === "restore",
    ).length;
    expect(requestSyncAfterReady).toBe(0);
    expect(restoreAfterReady).toBe(0);

    postSpy.mockRestore();
    document.body.removeChild(iframe);
  });
});
