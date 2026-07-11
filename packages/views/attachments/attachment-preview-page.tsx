"use client";

/**
 * AttachmentPreviewPage — full-page HTML attachment viewer.
 *
 * Destination for `openInNewTab` from HtmlAttachmentPreview's toolbar. The
 * inline preview (HtmlAttachmentPreview) renders the same content in a 480px
 * card with a hover toolbar; this is the same content edge-to-edge so the
 * user can resize / interact with the document at full size.
 *
 * Same security posture as the inline preview: iframe sandbox is
 * "allow-scripts" only — no allow-same-origin, no allow-top-navigation. The
 * iframe runs in an opaque origin and cannot reach cookies, localStorage,
 * parent, or top-level navigation.
 *
 * Because that opaque origin also blocks the browser's native Ctrl+F from
 * searching the document (#5259), we inject a find shim (withFindShim) into the
 * srcdoc and drive it from an in-app find bar over postMessage.
 *
 * The route is workspace-scoped (`/{slug}/attachments/{id}/preview`) for
 * tenancy isolation; the `/api/attachments/{id}/content` proxy itself is
 * already auth-checked, so the slug is purely a URL contract.
 */

import { useEffect, useRef, useState } from "react";
import { useT } from "../i18n";
import { useAttachmentHtmlText } from "../editor/hooks/use-attachment-html-text";
import { withFragmentNavShim } from "../editor/utils/iframe-fragment-nav";
import { withFindShim, FIND_RESULT, FIND_OPEN } from "../editor/utils/iframe-find";
import {
  HtmlPreviewFindBar,
  type FindResult,
} from "../editor/html-preview-find-bar";

interface AttachmentPreviewPageProps {
  attachmentId: string;
  /** Optional display name. Falls back to a generic label and is only used
   *  for the document title — never echoed into the iframe sandbox. */
  filename?: string;
}

export function AttachmentPreviewPage({
  attachmentId,
  filename,
}: AttachmentPreviewPageProps) {
  const { t } = useT("editor");
  const query = useAttachmentHtmlText(attachmentId);

  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState("");
  const [findResult, setFindResult] = useState<FindResult | null>(null);

  // Set document.title so desktop's MutationObserver-based tab title picks
  // up the filename. Web shows the same string in the browser tab.
  useEffect(() => {
    if (filename) document.title = filename;
  }, [filename]);

  const text = query.data?.text;
  const isLoading = query.isLoading;
  const isError = !isLoading && (!!query.error || !text);
  const hasContent = !isLoading && !isError;

  // Intercept Ctrl/Cmd+F at the page level so it opens our in-app find bar
  // instead of the browser's native find (which cannot reach the opaque-origin
  // iframe anyway). The shim posts FIND_OPEN when the iframe itself has focus.
  useEffect(() => {
    if (!hasContent) return;
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
  }, [hasContent, findOpen]);

  // Receive match counts / open requests from the injected shim. Only trust
  // messages from THIS iframe's window and carrying our own source tag.
  useEffect(() => {
    const handler = (e: MessageEvent) => {
      if (e.source !== iframeRef.current?.contentWindow) return;
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
    <div className="flex h-full w-full flex-col bg-background">
      {isLoading ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          {t(($) => $.attachment.preview_loading)}
        </div>
      ) : isError ? (
        <div
          className="flex flex-1 items-center justify-center px-4 text-sm text-muted-foreground"
          data-testid="attachment-preview-page-error"
        >
          {t(($) => $.attachment.preview_failed)}
        </div>
      ) : (
        <div className="relative flex-1">
          <iframe
            ref={iframeRef}
            srcDoc={withFindShim(withFragmentNavShim(text))}
            sandbox="allow-scripts"
            title={filename ?? "HTML attachment"}
            className="h-full w-full border-0 bg-background"
          />
          {findOpen && (
            <HtmlPreviewFindBar
              iframeRef={iframeRef}
              result={findResult}
              query={findQuery}
              onQueryChange={setFindQuery}
              onClose={() => setFindOpen(false)}
            />
          )}
        </div>
      )}
    </div>
  );
}
