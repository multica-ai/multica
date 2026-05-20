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
import { withFragmentNavShim } from "../editor/utils/iframe-fragment-nav";

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

  // Set document.title so desktop's MutationObserver-based tab title picks
  // up the filename. Web shows the same string in the browser tab.
  useEffect(() => {
    if (filename) document.title = filename;
  }, [filename]);

  const text = query.data?.text;
  const isLoading = query.isLoading;
  const isError = !isLoading && (!!query.error || !text);

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
        <iframe
          srcDoc={withFragmentNavShim(text)}
          sandbox="allow-scripts"
          title={filename ?? "HTML attachment"}
          className="flex-1 w-full border-0 bg-background"
        />
      )}
    </div>
  );
}
