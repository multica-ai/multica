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
 * The route is workspace-scoped (`/{slug}/attachments/{id}/preview`) for
 * tenancy isolation; the `/api/attachments/{id}/content` proxy itself is
 * already auth-checked, so the slug is purely a URL contract.
 */

import { useEffect } from "react";
import { useT } from "../i18n";
import { useAttachmentHtmlText } from "../editor/hooks/use-attachment-html-text";
import { useHtmlPreviewScrollRestore } from "./use-html-preview-scroll-restore";

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

  const text = query.data?.text;

  // Scroll-position restoration across desktop tab switches (multica-ai#6405).
  // No-op on web (no desktop adapter). The iframe is keyed on contentKey so a
  // content change (re-upload) structurally remounts a fresh document; the
  // hook reports y=0 with the new key until that document scrolls, and
  // messages from the previous document are dropped by token.
  const { contentKey, buildSrcDoc, iframeRef, onLoad } =
    useHtmlPreviewScrollRestore(text);

  // Set document.title so desktop's MutationObserver-based tab title picks
  // up the filename. Web shows the same string in the browser tab.
  useEffect(() => {
    if (filename) document.title = filename;
  }, [filename]);

  const isLoading = query.isLoading;
  const isError = !isLoading && (!!query.error || !text);

  return (
    <div className="flex h-full w-full flex-col bg-background">
      {isLoading ? (
        <div className="flex flex-1 items-center justify-center text-body text-muted-foreground">
          {t(($) => $.attachment.preview_loading)}
        </div>
      ) : isError ? (
        <div
          className="flex flex-1 items-center justify-center px-4 text-body text-muted-foreground"
          data-testid="attachment-preview-page-error"
        >
          {t(($) => $.attachment.preview_failed)}
        </div>
      ) : (
        <iframe
          key={contentKey}
          ref={iframeRef}
          onLoad={onLoad}
          srcDoc={buildSrcDoc(text as string)}
          sandbox="allow-scripts"
          title={filename ?? "HTML attachment"}
          className="flex-1 w-full border-0 bg-background"
        />
      )}
    </div>
  );
}
