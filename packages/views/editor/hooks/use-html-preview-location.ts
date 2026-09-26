"use client";

/**
 * useHtmlPreviewLocation — the parent side of the preview's address bar
 * (MUL-7737; the in-iframe half is utils/iframe-location-bridge.ts).
 *
 * Owns two things: the address the document was loaded at, which is baked
 * into its srcdoc, and the address it is at now, which the bridge reports as
 * the document navigates itself. A new query means a new document, so
 * navigating to one reloads the frame through `frameKey`; a fragment-only
 * change is sent to the running document instead, the way a browser keeps
 * the page for `#section`.
 */

import { useCallback, useLayoutEffect, useRef, useState } from "react";
import {
  HTML_PREVIEW_LOCATION_KEY,
  readHtmlPreviewLocationMessage,
  splitHtmlPreviewAddress,
  withLocationBridge,
} from "../utils/iframe-location-bridge";

export interface HtmlPreviewLocation {
  /** Where the document is now: `"?s=overview#top"`, or `""`. */
  address: string;
  /** Bumped on every fresh load; part of the iframe's React key. */
  frameKey: number;
  /** The srcdoc for the current load: the bridge, then `html`. */
  withAddress: (html: string) => string;
  /** iframe ref — identifies which window's messages to trust. */
  frameRef: (el: HTMLIFrameElement | null) => void;
  navigate: (address: string) => void;
  /** Loads the document afresh at its current address. */
  reload: () => void;
}

export function useHtmlPreviewLocation(initialAddress = ""): HtmlPreviewLocation {
  const [load, setLoad] = useState({ address: initialAddress, key: 0 });
  const [address, setAddress] = useState(initialAddress);
  const frameElRef = useRef<HTMLIFrameElement | null>(null);
  // Latest address for the stable callbacks below; updated during render,
  // which is idempotent across re-renders.
  const addressRef = useRef(address);
  addressRef.current = address;

  const loadAt = useCallback((next: string) => {
    setLoad((previous) => ({ address: next, key: previous.key + 1 }));
    setAddress(next);
  }, []);

  const navigate = useCallback(
    (next: string) => {
      const from = splitHtmlPreviewAddress(addressRef.current);
      const to = splitHtmlPreviewAddress(next);
      const win = frameElRef.current?.contentWindow;
      if (win && to.hash && to.search === from.search) {
        win.postMessage({ [HTML_PREVIEW_LOCATION_KEY]: 1, type: "hash", value: to.hash }, "*");
        return;
      }
      loadAt(next);
    },
    [loadAt],
  );

  const reload = useCallback(() => loadAt(addressRef.current), [loadAt]);

  // Layout effect so the listener is in place before the document's first
  // report, which it posts while it is still parsing.
  useLayoutEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const win = frameElRef.current?.contentWindow;
      if (!win || event.source !== win) return;
      const message = readHtmlPreviewLocationMessage(event.data);
      if (!message) return;
      if (message.type === "location") {
        setAddress(message.value);
        return;
      }
      // A navigate request stands for a click on a `?…` link. Activation
      // from a click inside the frame reaches this window too, so a document
      // that posts the request on its own, e.g. on every load, cannot keep
      // reloading itself.
      if (navigator.userActivation && !navigator.userActivation.isActive) return;
      loadAt(message.value);
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [loadAt]);

  const frameRef = useCallback((el: HTMLIFrameElement | null) => {
    frameElRef.current = el;
  }, []);

  const loadedAddress = load.address;
  const withAddress = useCallback(
    (html: string) => withLocationBridge(html, loadedAddress),
    [loadedAddress],
  );

  return { address, frameKey: load.key, withAddress, frameRef, navigate, reload };
}
