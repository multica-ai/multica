"use client";

/**
 * AttachmentPreviewPage — full-page text attachment viewer.
 *
 * Destination for `openInNewTab` from the preview surfaces. The inline preview
 * renders the same content in a card with a hover toolbar; this is the same
 * content edge-to-edge so the user can resize / interact with the document at
 * full size.
 *
 * Dispatches on the preview kind (mirrors AttachmentPreviewModal):
 *   - markdown: parsed via ReadonlyContent (mentions / mermaid / katex / code)
 *   - html: sandboxed iframe (see security note below)
 *   - text: syntax-highlighted CodeBlockStatic
 *
 * HTML security posture: iframe sandbox is "allow-scripts" only — no
 * allow-same-origin, no allow-top-navigation. The iframe runs in an opaque
 * origin and cannot reach cookies, localStorage, parent, or top-level
 * navigation.
 *
 * The route is workspace-scoped (`/{slug}/attachments/{id}/preview`) for
 * tenancy isolation; the `/api/attachments/{id}/content` proxy itself is
 * already auth-checked, so the slug is purely a URL contract.
 */

import { useEffect } from "react";
import { useT } from "../i18n";
import { useAttachmentHtmlText } from "../editor/hooks/use-attachment-html-text";
import { useHtmlPreviewScrollRestore } from "./use-html-preview-scroll-restore";
import { getPreviewKind, extensionToLanguage } from "../editor/utils/preview";
import { ReadonlyContent } from "../editor/readonly-content";
import { CodeBlockStatic } from "../editor/code-block-static";

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
  const originalContentType = query.data?.originalContentType ?? "";

  // Kind dispatch matches AttachmentPreviewModal. The filename extension is
  // the authoritative fallback: a .md file is often stored as text/plain by
  // the server's content sniffer, so content type alone is not enough.
  const kind = getPreviewKind(originalContentType, filename ?? "");

  // Scroll-position restoration across desktop tab switches (multica-ai#6405).
  // No-op on web (no desktop adapter). Only the HTML branch consumes the
  // result; the hook itself is called unconditionally because hooks cannot be
  // conditional and its setup is inert for non-HTML content.
  const { contentKey, buildSrcDoc, iframeRef, onLoad } =
    useHtmlPreviewScrollRestore(text);

  // Set document.title so desktop's MutationObserver-based tab title picks
  // up the filename. Web shows the same string in the browser tab.
  useEffect(() => {
    if (filename) document.title = filename;
  }, [filename]);

  const isLoading = query.isLoading;
  const isError = !isLoading && (!!query.error || !text);

  if (isLoading) {
    return (
      <div className="flex h-full w-full flex-col bg-background">
        <div className="flex flex-1 items-center justify-center text-body text-muted-foreground">
          {t(($) => $.attachment.preview_loading)}
        </div>
      </div>
    );
  }

  if (isError) {
    return (
      <div
        className="flex h-full w-full items-center justify-center px-4 text-body text-muted-foreground"
        data-testid="attachment-preview-page-error"
      >
        {t(($) => $.attachment.preview_failed)}
      </div>
    );
  }

  if (kind === "markdown") {
    return (
      <div className="h-full w-full overflow-auto bg-background">
        <ReadonlyContent
          content={text as string}
          className="mx-auto max-w-3xl px-6 py-4"
        />
      </div>
    );
  }

  if (kind === "text") {
    return (
      <div className="h-full w-full overflow-auto bg-background">
        <CodeBlockStatic
          language={extensionToLanguage(filename ?? "")}
          body={text as string}
          className="px-6 py-4"
        />
      </div>
    );
  }

  // HTML (and any unknown-but-text kind as a defensive fallback): render in
  // the sandboxed iframe, same as the inline HtmlAttachmentPreview.
  return (
    <div className="flex h-full w-full flex-col bg-background">
      <iframe
        key={contentKey}
        ref={iframeRef}
        onLoad={onLoad}
        srcDoc={buildSrcDoc(text as string)}
        sandbox="allow-scripts"
        title={filename ?? "HTML attachment"}
        className="flex-1 w-full border-0 bg-background"
      />
    </div>
  );
}
