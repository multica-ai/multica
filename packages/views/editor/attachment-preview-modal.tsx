"use client";

/**
 * AttachmentPreviewModal — full-screen inline preview for an attachment.
 *
 * Sibling to the existing `ImageLightbox` (extensions/image-view.tsx) which
 * keeps owning images. This modal handles 6 other PreviewKinds:
 *
 *   - pdf   : <iframe src={download_url}> — relies on Chromium's PDFium
 *             plugin. On desktop, requires webPreferences.plugins=true
 *             (see apps/desktop/src/main/index.ts).
 *   - video : <video controls src={download_url}>
 *   - audio : <audio controls src={download_url}>
 *
 *   - markdown : fetch text via api.getAttachmentTextContent, render via
 *                the existing ReadonlyContent (full mention/mermaid/katex
 *                pipeline included).
 *   - html     : fetch text, hand to <iframe srcdoc={text} sandbox="">.
 *                Empty sandbox attribute = max restriction (no scripts,
 *                no forms, no top-nav, no popups, no same-origin) — the
 *                recommended pattern for previewing untrusted HTML.
 *   - text     : fetch text, highlight with lowlight if the extension
 *                maps to a known hljs language; otherwise plain <pre>.
 *
 * Media types load directly from the CloudFront signed `download_url`.
 * Text types go through `/api/attachments/{id}/content` to sidestep
 * CloudFront CORS (not configured) + Content-Disposition: attachment.
 */

import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { useQuery } from "@tanstack/react-query";
import { Download, FileText, Loader2, X } from "lucide-react";
import { createLowlight, common } from "lowlight";
// @ts-expect-error -- hast-util-to-html has no bundled type declarations
import { toHtml } from "hast-util-to-html";
import { cn } from "@multica/ui/lib/utils";
import {
  api,
  PreviewTooLargeError,
  PreviewUnsupportedError,
} from "@multica/core/api";
import type { Attachment } from "@multica/core/types";
import { useT } from "../i18n";
import { ReadonlyContent } from "./readonly-content";
import {
  extensionToLanguage,
  getPreviewKind,
  type PreviewKind,
} from "./utils/preview";
import { useDownloadAttachment } from "./use-download-attachment";

// ---------------------------------------------------------------------------
// Public props
// ---------------------------------------------------------------------------

interface AttachmentPreviewModalProps {
  attachment: Attachment;
  open: boolean;
  onClose: () => void;
}

// ---------------------------------------------------------------------------
// Hook — local state + ready-to-mount modal JSX
// ---------------------------------------------------------------------------
//
// Why no React context / provider: packages/views/ cannot mount a Context.Provider
// inside CoreProvider (in packages/core/), and threading a new provider through
// every app layout is more friction than it's worth for a feature with at most
// one open modal at a time. Instead each entry point gets its own local state
// and renders the returned `modal` node. Multiple entry points coexisting just
// means each carries its own (collapsed) state — they never collide because
// only one preview is open per user click.

export interface AttachmentPreviewHandle {
  /** Try to open a preview for the attachment. Returns false when the file
   *  type isn't previewable so the caller can fall back to a download flow. */
  tryOpen: (attachment: Attachment) => boolean;
  /** Force-open a preview, skipping the isPreviewable() guard. Use for cases
   *  where the caller has already filtered. */
  open: (attachment: Attachment) => void;
  /** Modal node to render somewhere in the caller's tree. Resolves to `null`
   *  when no preview is active. Safe to render inside any container — the
   *  modal portals to document.body. */
  modal: ReactNode;
}

export function useAttachmentPreview(): AttachmentPreviewHandle {
  const [current, setCurrent] = useState<Attachment | null>(null);

  const open = useCallback((att: Attachment) => setCurrent(att), []);
  const tryOpen = useCallback((att: Attachment) => {
    if (!getPreviewKind(att.content_type, att.filename)) return false;
    setCurrent(att);
    return true;
  }, []);

  const modal = current ? (
    <AttachmentPreviewModal
      attachment={current}
      open
      onClose={() => setCurrent(null)}
    />
  ) : null;

  return useMemo(() => ({ open, tryOpen, modal }), [open, tryOpen, modal]);
}

// ---------------------------------------------------------------------------
// Modal — frame + dispatch
// ---------------------------------------------------------------------------

export function AttachmentPreviewModal({
  attachment,
  open,
  onClose,
}: AttachmentPreviewModalProps) {
  const { t } = useT("editor");
  const download = useDownloadAttachment();

  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open, onClose]);

  const kind = getPreviewKind(attachment.content_type, attachment.filename);

  if (!open || typeof document === "undefined") return null;

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label={attachment.filename}
    >
      {/* Larger than the create-issue dialog (max-w-4xl, manualDialogContentClass)
          because PDF / video previews want more room. Capped to viewport
          minus the surrounding p-4 (1rem each side) so it never overflows
          the screen on small displays / split panes. */}
      <div
        className="flex h-[min(90vh,calc(100vh-2rem))] w-full max-w-6xl flex-col overflow-hidden rounded-lg bg-background shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-border bg-muted/30 px-4 py-2">
          <FileText className="size-4 shrink-0 text-muted-foreground" />
          <p className="truncate text-sm font-medium">{attachment.filename}</p>
          <span className="ml-1 shrink-0 text-xs text-muted-foreground">
            {attachment.content_type || "—"}
          </span>
          <div className="ml-auto flex items-center gap-1">
            <button
              type="button"
              className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title={t(($) => $.image.download)}
              aria-label={t(($) => $.image.download)}
              onClick={() => download(attachment.id)}
            >
              <Download className="size-4" />
            </button>
            <button
              type="button"
              className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title={t(($) => $.attachment.close)}
              aria-label={t(($) => $.attachment.close)}
              onClick={onClose}
            >
              <X className="size-4" />
            </button>
          </div>
        </div>
        <div className="min-h-0 flex-1 overflow-auto bg-background">
          <PreviewContent
            kind={kind}
            attachment={attachment}
            onDownload={() => download(attachment.id)}
          />
        </div>
      </div>
    </div>,
    document.body,
  );
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

// Dispatch on PreviewKind. New cases go here; remember that the modal frame
// (header, close, Download CTA, ESC handling) is shared — sub-renderers only
// own the content area.
function PreviewContent({
  kind,
  attachment,
  onDownload,
}: {
  kind: PreviewKind | null;
  attachment: Attachment;
  onDownload: () => void;
}) {
  const { t } = useT("editor");

  if (kind === null) {
    return (
      <UnsupportedFallback
        message={t(($) => $.attachment.preview_unsupported)}
        onDownload={onDownload}
      />
    );
  }

  switch (kind) {
    case "pdf":
      return (
        <iframe
          src={attachment.download_url}
          className="h-full w-full bg-background"
          title={attachment.filename}
        />
      );
    case "video":
      return (
        <div className="flex h-full w-full items-center justify-center bg-black">
          <video
            src={attachment.download_url}
            controls
            className="max-h-full max-w-full"
          />
        </div>
      );
    case "audio":
      return (
        <div className="flex h-full w-full items-center justify-center p-8">
          <audio src={attachment.download_url} controls className="w-full max-w-xl" />
        </div>
      );
    case "markdown":
      return (
        <TextBackedPreview
          attachmentId={attachment.id}
          onDownload={onDownload}
          render={(text) => (
            <ReadonlyContent
              content={text}
              className="px-6 py-4"
              attachments={[attachment]}
            />
          )}
        />
      );
    case "html":
      return (
        <TextBackedPreview
          attachmentId={attachment.id}
          onDownload={onDownload}
          render={(text) => (
            <iframe
              srcDoc={text}
              sandbox=""
              className="h-full w-full bg-background"
              title={attachment.filename}
            />
          )}
        />
      );
    case "text":
      return (
        <TextBackedPreview
          attachmentId={attachment.id}
          onDownload={onDownload}
          render={(text) => (
            <CodeBlock language={extensionToLanguage(attachment.filename)} body={text} />
          )}
        />
      );
  }
}

// ---------------------------------------------------------------------------
// Text-backed preview — fetches body once, then hands to the render prop
// ---------------------------------------------------------------------------

// React Query owns server state per the project convention; re-opening the
// same attachment hits the cache instead of re-fetching. Query is keyed on
// the attachment id alone — the 30 min TTL on the server-side signed URL
// is much longer than any plausible preview session.
function TextBackedPreview({
  attachmentId,
  onDownload,
  render,
}: {
  attachmentId: string;
  onDownload: () => void;
  render: (text: string) => ReactNode;
}) {
  const { t } = useT("editor");
  const query = useQuery({
    queryKey: ["attachment-content", attachmentId] as const,
    queryFn: () => api.getAttachmentTextContent(attachmentId),
    // Errors are surfaced as typed fallbacks, not retried — 413 / 415 won't
    // become 200 on a retry, and a transient failure is easier to recover
    // from by closing and reopening the modal than waiting on background
    // retries that have no UI affordance.
    retry: false,
    // 413 / 415 bodies are tiny; keep the result around for the session so
    // the user can flip away and back without refetching.
    staleTime: 5 * 60_000,
    gcTime: 30 * 60_000,
  });

  if (query.isLoading) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        {t(($) => $.attachment.preview_loading)}
      </div>
    );
  }
  if (query.error) {
    if (query.error instanceof PreviewTooLargeError) {
      return (
        <UnsupportedFallback
          message={t(($) => $.attachment.preview_too_large)}
          onDownload={onDownload}
        />
      );
    }
    if (query.error instanceof PreviewUnsupportedError) {
      return (
        <UnsupportedFallback
          message={t(($) => $.attachment.preview_unsupported)}
          onDownload={onDownload}
        />
      );
    }
    return (
      <UnsupportedFallback
        message={t(($) => $.attachment.preview_failed)}
        onDownload={onDownload}
      />
    );
  }
  if (!query.data) return null;
  return <>{render(query.data.text)}</>;
}

// ---------------------------------------------------------------------------
// Code block — lowlight, matches readonly-content's hljs CSS
// ---------------------------------------------------------------------------

const lowlight = createLowlight(common);

function CodeBlock({ language, body }: { language: string | undefined; body: string }) {
  const html = useMemo(() => {
    const code = body.replace(/\n$/, "");
    try {
      const tree = language
        ? lowlight.highlight(language, code)
        : lowlight.highlightAuto(code);
      return toHtml(tree) as string;
    } catch {
      // Fallthrough to a plain escaped <pre> when lowlight rejects the
      // language tag. Avoids crashing the preview on an unknown extension.
      return escapeHtml(code);
    }
  }, [body, language]);

  return (
    <pre className="rich-text-editor m-0 overflow-auto px-6 py-4 text-sm">
      <code
        className={cn("hljs", language && `language-${language}`)}
        dangerouslySetInnerHTML={{ __html: html }}
      />
    </pre>
  );
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

// ---------------------------------------------------------------------------
// Fallback — used for 413 / 415 / unknown kinds
// ---------------------------------------------------------------------------

function UnsupportedFallback({
  message,
  onDownload,
}: {
  message: string;
  onDownload: () => void;
}) {
  const { t } = useT("editor");
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-8 text-center">
      <FileText className="size-8 text-muted-foreground" />
      <p className="text-sm text-muted-foreground">{message}</p>
      <button
        type="button"
        className="inline-flex items-center gap-2 rounded-md border border-border bg-background px-3 py-1.5 text-sm transition-colors hover:bg-muted"
        onClick={onDownload}
      >
        <Download className="size-4" />
        {t(($) => $.image.download)}
      </button>
    </div>
  );
}

// Re-export the predicate from the dispatch util so entry-point components
// only need a single import to gate the Eye button.
export { isPreviewable } from "./utils/preview";
