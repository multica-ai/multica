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
 * The route is workspace-scoped (`/{slug}/attachments/{id}/preview`) for
 * tenancy isolation; the attachment endpoints themselves are auth-checked, so
 * the slug is purely a URL contract.
 */

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { api } from "@multica/core/api";
import { useT } from "../i18n";
import {
  AttachmentPreviewStandalone,
  type HtmlFrameRenderer,
} from "../editor/attachment-preview-modal";
import { useHtmlPreviewScrollRestore } from "./use-html-preview-scroll-restore";

interface AttachmentPreviewPageProps {
  attachmentId: string;
  /** Optional display name. Titles the tab until the record has loaded. */
  filename?: string;
}

export function AttachmentPreviewPage({
  attachmentId,
  filename,
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

  if (attachment) {
    return (
      <AttachmentPreviewStandalone
        attachment={attachment}
        renderHtmlFrame={renderPageHtmlFrame}
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
function PageHtmlFrame({ html, title }: { html: string; title: string }) {
  const { contentKey, buildSrcDoc, iframeRef, onLoad } =
    useHtmlPreviewScrollRestore(html);
  return (
    <iframe
      key={contentKey}
      ref={iframeRef}
      onLoad={onLoad}
      srcDoc={buildSrcDoc(html)}
      sandbox="allow-scripts"
      title={title}
      className="h-full w-full border-0 bg-background"
    />
  );
}

const renderPageHtmlFrame: HtmlFrameRenderer = (props) => (
  <PageHtmlFrame {...props} />
);
