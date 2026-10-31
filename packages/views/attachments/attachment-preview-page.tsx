"use client";

/**
 * AttachmentPreviewPage — any attachment on a page of its own.
 *
 * Destination for "Open in new tab" in the attachment viewer, for every kind
 * the viewer shows. It renders the viewer's own top bar and stage
 * (`AttachmentPreviewStandalone`) so a file looks and behaves the same in both
 * places — the HTML viewport switch, a table's sorting, a log's line wrapping
 * — just with the whole tab to itself.
 *
 * The attachment record is loaded by id: the URL carries nothing else but the
 * display name (`?name=`), which titles the tab until the record lands.
 *
 * HTML keeps the viewer's security posture: iframe sandbox "allow-scripts"
 * only — no allow-same-origin, no allow-top-navigation — so the document runs
 * in an opaque origin and cannot reach cookies, localStorage, parent, or
 * top-level navigation. Here the iframe also reports its scroll position so a
 * desktop tab switch puts the reader back where they were.
 *
 * That same opaque origin puts the document out of reach of the browser's
 * native find, so the frame injects a find shim and renders an in-app find
 * bar (MUL-7551): Ctrl/Cmd+F opens it whether focus is on the page or inside
 * the iframe, and match counts come back over postMessage.
 *
 * The route is workspace-scoped (`/{slug}/attachments/{id}/preview`) for
 * tenancy isolation; the attachment endpoints themselves are auth-checked, so
 * the slug is purely a URL contract.
 *
 * An HTML file opens at the `loc` the viewer handed over (its address bar,
 * MUL-7737). On desktop the address the reader left is kept in the tab's
 * view state, so a tab switch comes back to the same screen.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "@multica/ui/icons";
import { api } from "@multica/core/api";
import { useT } from "../i18n";
import { useRestoredViewState, useViewStateWriter } from "../platform";
import {
  AttachmentPreviewStandalone,
  type HtmlFrameRenderer,
} from "../editor/attachment-preview-modal";
import type { HtmlPreviewLocation } from "../editor/hooks/use-html-preview-location";
import { normalizeHtmlPreviewAddress } from "../editor/utils/iframe-location-bridge";
import { useHtmlPreviewScrollRestore } from "./use-html-preview-scroll-restore";
import {
  withFindShim,
  FIND_RESULT,
  FIND_OPEN,
} from "../editor/utils/iframe-find";
import {
  HtmlPreviewFindBar,
  type FindResult,
} from "../editor/html-preview-find-bar";

/** View-state key the page keeps an HTML file's address under. */
export const HTML_PREVIEW_ADDRESS_KEY = "html-preview-address";

interface AttachmentPreviewPageProps {
  attachmentId: string;
  /** Optional display name. Titles the tab until the record has loaded. */
  filename?: string;
  /** Query and fragment an HTML file opens at (the route's `loc`). */
  initialAddress?: string;
}

export function AttachmentPreviewPage({
  attachmentId,
  filename,
  initialAddress,
}: AttachmentPreviewPageProps) {
  const { t } = useT("editor");
  const query = useQuery({
    queryKey: ["attachment-preview-page", attachmentId] as const,
    queryFn: () => api.getAttachment(attachmentId),
    // The record carries signed media URLs. Refetching would re-sign them and
    // reload a playing video or an open PDF under the reader; one fetch per
    // page view is enough.
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    retry: false,
  });
  const attachment = query.data?.id ? query.data : null;

  // Set document.title so desktop's MutationObserver-based tab title picks
  // up the filename. Web shows the same string in the browser tab.
  const title = attachment?.filename || filename;
  useEffect(() => {
    if (title) document.title = title;
  }, [title]);

  const restoredAddress = useRestoredViewState(HTML_PREVIEW_ADDRESS_KEY);
  const writeViewState = useViewStateWriter();
  const keepAddress = useCallback(
    (address: string) => writeViewState(HTML_PREVIEW_ADDRESS_KEY, address),
    [writeViewState],
  );

  if (attachment) {
    return (
      <AttachmentPreviewStandalone
        attachment={attachment}
        renderHtmlFrame={renderPageHtmlFrame}
        initialHtmlAddress={normalizeHtmlPreviewAddress(
          restoredAddress ?? initialAddress ?? "",
        )}
        onHtmlAddressChange={keepAddress}
      />
    );
  }

  return (
    <div className="dark flex h-full w-full items-center justify-center gap-2 bg-black/95 px-4 text-body text-muted-foreground">
      {query.isLoading ? (
        <>
          <Loader2 className="size-4 animate-spin" />
          {t(($) => $.attachment.preview_loading)}
        </>
      ) : (
        <span data-testid="attachment-preview-page-error">
          {t(($) => $.attachment.preview_failed)}
        </span>
      )}
    </div>
  );
}

// Scroll-position restoration across desktop tab switches (multica-ai#6405).
// No-op on web (no desktop adapter). The iframe is keyed on contentKey so a
// content change (re-upload) structurally remounts a fresh document; the
// hook reports y=0 with the new key until that document scrolls, and
// messages from the previous document are dropped by token.
function PageHtmlFrame({
  html,
  title,
  location,
}: {
  html: string;
  title: string;
  location: HtmlPreviewLocation;
}) {
  const { contentKey, buildSrcDoc, iframeRef, onLoad } =
    useHtmlPreviewScrollRestore(html);
  const { frameRef } = location;

  // Two of the three consumers own callback refs; the find bar needs an
  // object ref to postMessage into the same iframe. Fan the element out.
  const frameEl = useRef<HTMLIFrameElement | null>(null);
  const setFrame = useCallback(
    (el: HTMLIFrameElement | null) => {
      frameEl.current = el;
      iframeRef(el);
      frameRef(el);
    },
    [iframeRef, frameRef],
  );

  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState("");
  const [findResult, setFindResult] = useState<FindResult | null>(null);

  // Intercept Ctrl/Cmd+F so it opens the in-app find bar instead of the
  // browser's native find, which cannot reach the opaque-origin iframe
  // anyway. The shim posts FIND_OPEN when the iframe itself has focus.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && (e.key === "f" || e.key === "F")) {
        e.preventDefault();
        setFindOpen(true);
      } else if (e.key === "Escape" && findOpen) {
        setFindOpen(false);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [findOpen]);

  // Receive match counts / open requests from the injected shim. Only trust
  // messages from THIS iframe's window and carrying our own source tag.
  useEffect(() => {
    const handler = (e: MessageEvent) => {
      if (e.source !== frameEl.current?.contentWindow) return;
      const d = e.data as
        | { source?: string; found?: boolean; total?: number; current?: number }
        | undefined;
      if (!d || typeof d !== "object") return;
      if (d.source === FIND_RESULT) {
        setFindResult({
          found: !!d.found,
          total: d.total ?? 0,
          current: d.current ?? 0,
        });
      } else if (d.source === FIND_OPEN) {
        setFindOpen(true);
      }
    };
    window.addEventListener("message", handler);
    return () => window.removeEventListener("message", handler);
  }, []);

  return (
    <div className="relative h-full w-full">
      <iframe
        key={`${contentKey}:${location.frameKey}`}
        ref={setFrame}
        onLoad={onLoad}
        srcDoc={withFindShim(location.withAddress(buildSrcDoc(html)))}
        sandbox="allow-scripts"
        title={title}
        className="h-full w-full border-0 bg-background"
      />
      {findOpen && (
        <HtmlPreviewFindBar
          iframeRef={frameEl}
          result={findResult}
          query={findQuery}
          onQueryChange={setFindQuery}
          onClose={() => setFindOpen(false)}
        />
      )}
    </div>
  );
}

const renderPageHtmlFrame: HtmlFrameRenderer = (props) => (
  <PageHtmlFrame {...props} />
);
