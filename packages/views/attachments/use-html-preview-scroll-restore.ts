"use client";

/**
 * Bridge a full-page opaque-origin HTML iframe into desktop scroll restoration.
 * The coordinator cannot read scrollY directly, so the iframe reports positions
 * through postMessage and this hook registers an ExternalScrollSource.
 *
 * Content keys remount changed documents and reject late messages. Until the new
 * document reports, capture zero with the new key to avoid restoring old content's
 * position. Modal previews and web adapters without external-source support do
 * not participate.
 */

import { useCallback, useLayoutEffect, useRef } from "react";
import {
  useRestoredScrollEntry,
  useScrollRestorationAdapter,
  type ExternalScrollSource,
} from "../platform";
import { withFragmentNavShim } from "../editor/utils/iframe-fragment-nav";
import { buildScrollBridge } from "../editor/utils/iframe-scroll-bridge";
import { hashString } from "../editor/utils/hash-string";

/** Container key the Coordinator stores this under in the tab memento. */
export const HTML_IFRAME_SCROLL_KEY = "html-iframe";

const BRIDGE_MARK = "__multica";

type BridgeMessage =
  | { kind: "scroll"; y: number; height: number }
  | { kind: "ready"; y: number; height: number };

export interface HtmlPreviewScrollRestore {
  /** Apply as the iframe's React `key` so content changes remount it. */
  contentKey: string;
  /** True only under the desktop adapter that registers external sources. */
  restoreActive: boolean;
  /** Build the iframe's srcDoc, appending the bridge when active. */
  buildSrcDoc: (html: string) => string;
  /** iframe ref callback — attaches the parent message listener. */
  iframeRef: (el: HTMLIFrameElement | null) => void;
  /** iframe onLoad handler — kicks off request-sync + restore. */
  onLoad: () => void;
}

export function useHtmlPreviewScrollRestore(
  text: string | undefined,
): HtmlPreviewScrollRestore {
  const adapter = useScrollRestorationAdapter();
  const savedEntry = useRestoredScrollEntry(HTML_IFRAME_SCROLL_KEY);
  const restoreActive = typeof adapter?.registerExternalSource === "function";
  const contentKey = hashString(text ?? "");

  // Restore only when the saved offset belongs to the exact content we are
  // about to render. A missing/mismatched contentKey means "new content" —
  // start at 0, never post restore.
  const targetTop =
    savedEntry &&
    savedEntry.contentKey === contentKey &&
    savedEntry.top > 0
      ? savedEntry.top
      : 0;

  const iframeElRef = useRef<HTMLIFrameElement | null>(null);
  const expectedTokenRef = useRef<string>(contentKey);
  const lastReportRef = useRef<{ token: string; y: number; height: number }>({
    token: contentKey,
    y: 0,
    height: 0,
  });
  const targetTopRef = useRef<number>(targetTop);
  // Token for which we have already issued a `restore` this document's
  // lifetime. Reset implicitly when contentKey changes (the value no longer
  // matches expectedTokenRef), so the next document gets exactly one restore.
  const restoreIssuedForRef = useRef<string | null>(null);

  // Update "latest value" refs DURING RENDER (not in an effect) so that the
  // iframe's ref callback — which runs in commit BEFORE useLayoutEffect —
  // already sees the new token when contentKey changes and the iframe element
  // is replaced via `key={contentKey}`. React allows mutating refs during
  // render as long as the mutation is idempotent across re-renders.
  expectedTokenRef.current = contentKey;
  targetTopRef.current = targetTop;
  if (lastReportRef.current.token !== contentKey) {
    lastReportRef.current = { token: contentKey, y: 0, height: 0 };
  }
  // A content change must allow a fresh restore for the new document.
  if (restoreIssuedForRef.current !== contentKey) {
    restoreIssuedForRef.current = null;
  }

  const postToIframe = useCallback(
    (message: Record<string, unknown>) => {
      const win = iframeElRef.current?.contentWindow;
      if (!win) return;
      try {
        win.postMessage(message, "*");
      } catch {
        // iframe gone / cross-origin teardown — nothing to do.
      }
    },
    [],
  );

  const sendRequestSync = useCallback(() => {
    if (!restoreActive) return;
    postToIframe({
      [BRIDGE_MARK]: BRIDGE_MARK,
      kind: "request-sync",
      token: expectedTokenRef.current,
    });
  }, [postToIframe, restoreActive]);

  const sendRestore = useCallback(() => {
    if (!restoreActive) return;
    const token = expectedTokenRef.current;
    // At most one restore per document. The in-iframe bridge's restoring
    // session (ResizeObserver) holds the target across later content growth,
    // so re-sending restore is unnecessary — and re-sending it on every
    // `ready` (which the bridge emits in reply to every `request-sync`) would
    // recreate the restoring session in a loop.
    if (restoreIssuedForRef.current === token) return;
    const target = targetTopRef.current;
    if (target <= 0) return;
    restoreIssuedForRef.current = token;
    postToIframe({
      [BRIDGE_MARK]: BRIDGE_MARK,
      kind: "restore",
      y: target,
      token,
    });
  }, [postToIframe, restoreActive]);

  // Register the external scroll source with the desktop adapter for the
  // page's lifetime. The adapter is itself per-tab (createScrollRestorationAdapter
  // is memoized on tabId in tab-content.tsx), so on tab unmount both the
  // adapter and its registry go away; this cleanup is belt-and-braces.
  useLayoutEffect(() => {
    if (!restoreActive || !adapter?.registerExternalSource) return;
    const source: ExternalScrollSource = {
      capture() {
        const report = lastReportRef.current;
        // If the current document hasn't reported yet (new content before
        // any scroll/ready), its token won't match — report 0 with the NEW
        // contentKey so the stale positive offset is replaced, never the
        // old document's y.
        const matched = report.token === expectedTokenRef.current;
        return {
          top: matched ? report.y : 0,
          height: matched ? report.height : 0,
          contentKey: expectedTokenRef.current,
        };
      },
    };
    adapter.registerExternalSource(HTML_IFRAME_SCROLL_KEY, source);
    return () => {
      adapter.registerExternalSource?.(HTML_IFRAME_SCROLL_KEY, null);
    };
  }, [adapter, restoreActive]);

  // Stable message handler so add/removeEventListener pair exactly.
  const handleMessage = useCallback(
    (event: MessageEvent) => {
      const iframe = iframeElRef.current;
      if (!iframe) return;
      if (event.source !== iframe.contentWindow) return;
      const data = event.data as
        | (BridgeMessage & { [BRIDGE_MARK]?: string; token?: string })
        | null
        | undefined;
      if (!data || data[BRIDGE_MARK] !== BRIDGE_MARK) return;
      if (data.token !== expectedTokenRef.current) return;
      if (data.kind !== "scroll" && data.kind !== "ready") return;
      const y = typeof data.y === "number" ? data.y : 0;
      const height = typeof data.height === "number" ? data.height : 0;
      lastReportRef.current = { token: expectedTokenRef.current, y, height };
      if (data.kind === "ready") {
        // The document has parsed and announced its position. This is the
        // right moment to restore (it can accept scrollTo now). Do NOT reply
        // with another `request-sync`: the bridge answers every request-sync
        // with a ready, so ready → request-sync → ready would loop forever.
        // request-sync is sent only from the ref/onLoad handshake, each of
        // which gets exactly one ready in reply.
        sendRestore();
      }
    },
    [sendRequestSync, sendRestore],
  );

  // Ref callback runs during commit, BEFORE the iframe fires `load` — so the
  // window message listener is already attached when the bridge's initial
  // ready arrives.
  const iframeRef = useCallback(
    (el: HTMLIFrameElement | null) => {
      if (iframeElRef.current) {
        try {
          window.removeEventListener("message", handleMessage);
        } catch {
          // noop
        }
      }
      iframeElRef.current = el;
      if (el) {
        window.addEventListener("message", handleMessage);
        // Defense in depth: ask the new document to announce itself even if
        // its initial ready somehow raced this callback.
        sendRequestSync();
      }
    },
    [handleMessage, sendRequestSync],
  );

  const onLoad = useCallback(() => {
    sendRequestSync();
    sendRestore();
  }, [sendRequestSync, sendRestore]);

  const buildSrcDoc = useCallback(
    (html: string) => {
      const base = withFragmentNavShim(html);
      if (!restoreActive) return base;
      return base + buildScrollBridge(contentKey);
    },
    [contentKey, restoreActive],
  );

  return {
    contentKey,
    restoreActive,
    buildSrcDoc,
    iframeRef,
    onLoad,
  };
}
