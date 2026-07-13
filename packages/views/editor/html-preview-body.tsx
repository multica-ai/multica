"use client";

/**
 * HtmlPreviewBody — single source of truth for rendering an HTML document
 * inside a sandboxed iframe, regardless of whether the source is an inline
 * string (```html fenced code block) or an attachment id (HTML attachment).
 *
 * The two paths used to duplicate iframe + sandbox + fragment-nav shim logic
 * across html-block-preview.tsx, html-attachment-preview.tsx, and
 * attachment-preview-modal.tsx. This component owns:
 *
 *   - srcDoc + sandbox="allow-scripts" via CodeBlockIframe
 *   - withFragmentNavShim() so anchor links inside the iframe scroll instead
 *     of silently failing in the sandbox's opaque origin
 *   - loading / typed-error placeholders for attachment sources
 *
 * Callers (the toolbar wrappers) keep their own chrome — Copy / Source
 * toggle for code blocks, Download / Open-in-new-tab for attachments — and
 * just slot HtmlPreviewBody as the body.
 */

import { cn } from "@multica/ui/lib/utils";
import {
  PreviewTooLargeError,
  PreviewUnsupportedError,
} from "@multica/core/api";
import { useT } from "../i18n";
import { CodeBlockIframe } from "./code-block-iframe";
import { withFragmentNavShim } from "./utils/iframe-fragment-nav";
import { useAttachmentHtmlText } from "./hooks/use-attachment-html-text";

export type HtmlSource =
  | { kind: "inline"; html: string }
  | { kind: "attachment"; attachmentId: string };

interface HtmlPreviewBodyProps {
  source: HtmlSource;
  /** iframe title attribute (a11y). */
  title: string;
  /** Tailwind height/sizing classes for the iframe (e.g. "h-[480px]" or "h-full"). */
  className?: string;
  /** Override iframe styling (border / radius). Tailwind-merge resolves
   *  conflicts so callers can pass "rounded-none border-0" for full-screen. */
  iframeClassName?: string;
  /** Sizing for loading / error placeholders. Defaults to className. */
  placeholderClassName?: string;
  /** Optional data-testid on the error placeholder — preserved for
   *  html-attachment-preview.test.tsx which asserts the testid is present. */
  errorTestId?: string;
}

export function HtmlPreviewBody({
  source,
  title,
  className,
  iframeClassName,
  placeholderClassName,
  errorTestId,
}: HtmlPreviewBodyProps) {
  if (source.kind === "inline") {
    return (
      <CodeBlockIframe
        html={withFragmentNavShim(source.html)}
        title={title}
        heightClassName={className}
        className={iframeClassName}
      />
    );
  }
  return (
    <AttachmentBody
      attachmentId={source.attachmentId}
      title={title}
      className={className}
      iframeClassName={iframeClassName}
      placeholderClassName={placeholderClassName ?? className}
      errorTestId={errorTestId}
    />
  );
}

function AttachmentBody({
  attachmentId,
  title,
  className,
  iframeClassName,
  placeholderClassName,
  errorTestId,
}: {
  attachmentId: string;
  title: string;
  className?: string;
  iframeClassName?: string;
  placeholderClassName?: string;
  errorTestId?: string;
}) {
  const { t } = useT("editor");
  const query = useAttachmentHtmlText(attachmentId);

  if (query.isLoading) {
    return (
      <div
        className={cn(
          "flex items-center justify-center rounded-md border border-border bg-muted/30 text-xs text-muted-foreground",
          placeholderClassName,
        )}
      >
        {t(($) => $.attachment.preview_loading)}
      </div>
    );
  }

  if (query.error || !query.data) {
    const message =
      query.error instanceof PreviewTooLargeError
        ? t(($) => $.attachment.preview_too_large)
        : query.error instanceof PreviewUnsupportedError
        ? t(($) => $.attachment.preview_unsupported)
        : t(($) => $.attachment.preview_failed);
    return (
      <div
        className={cn(
          "flex items-center rounded-md border border-border bg-muted/30 px-3 text-xs text-muted-foreground",
          placeholderClassName,
        )}
        data-testid={errorTestId}
      >
        <span className="truncate">{message}</span>
      </div>
    );
  }

  return (
    <CodeBlockIframe
      html={withFragmentNavShim(query.data.text)}
      title={title}
      heightClassName={className}
      className={iframeClassName}
    />
  );
}
