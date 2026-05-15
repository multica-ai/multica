"use client";

/**
 * ReadonlyContent — lightweight markdown renderer for readonly content display.
 *
 * Replaces <ContentEditor editable={false}> for comment cards and other
 * read-only surfaces. Uses react-markdown instead of a full Tiptap/ProseMirror
 * instance, eliminating EditorView, Plugin, and NodeView overhead.
 *
 * Visual parity with ContentEditor is achieved by:
 * - Wrapping output in <div class="rich-text-editor readonly"> so the same
 *   content-editor.css rules apply to standard HTML tags
 * - Using the same preprocessMarkdown pipeline (mention shortcodes + linkify)
 * - Using lowlight for code highlighting (same engine as Tiptap's CodeBlockLowlight)
 *   so .hljs-* CSS rules from content-editor.css produce identical colors
 * - Rendering mentions with the same IssueMentionCard component and .mention class
 */

import { isValidElement, memo, useCallback, useMemo, useRef, useState } from "react";
import ReactMarkdown, {
  defaultUrlTransform,
  type Components,
} from "react-markdown";
import rehypeKatex from "rehype-katex";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import { createLowlight, common } from "lowlight";
// @ts-expect-error -- hast-util-to-html has no bundled type declarations
import { toHtml } from "hast-util-to-html";
import { Maximize2, Download, Eye, Link as LinkIcon, FileText } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths, useWorkspaceSlug } from "@multica/core/paths";
import type { Attachment } from "@multica/core/types";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { openExternal } from "../platform";
import { IssueMentionCard } from "../issues/components/issue-mention-card";
import { ImageLightbox } from "./extensions/image-view";
import { useLinkHover, LinkHoverCard } from "./link-hover-card";
import { openLink, isMentionHref } from "./utils/link-handler";
import { preprocessMarkdown } from "./utils/preprocess";
import { MermaidDiagram } from "./mermaid-diagram";
import { useDownloadAttachment } from "./use-download-attachment";
import { useAttachmentPreview, type PreviewSource } from "./attachment-preview-modal";
import { getPreviewKind } from "./utils/preview";
import "katex/dist/katex.min.css";
import "./content-editor.css";

// ---------------------------------------------------------------------------
// Lowlight — same engine + language set as Tiptap's CodeBlockLowlight
// ---------------------------------------------------------------------------

const lowlight = createLowlight(common);

// ---------------------------------------------------------------------------
// Sanitization schema — extends GitHub defaults to allow file-card data attrs
// ---------------------------------------------------------------------------

const sanitizeSchema = {
  ...defaultSchema,
  protocols: {
    ...defaultSchema.protocols,
    href: [...(defaultSchema.protocols?.href ?? []), "mention"],
  },
  attributes: {
    ...defaultSchema.attributes,
    div: [
      ...(defaultSchema.attributes?.div ?? []),
      "dataType",
      "dataHref",
      "dataFilename",
    ],
    code: [
      ...(defaultSchema.attributes?.code ?? []),
      ["className", /^language-/],
      ["className", /^math-/],
      ["className", /^hljs/],
    ],
    img: [
      ...(defaultSchema.attributes?.img ?? []),
      "alt",
    ],
  },
};

// ---------------------------------------------------------------------------
// URL transform — allow mention:// protocol through react-markdown's sanitizer
// ---------------------------------------------------------------------------

function urlTransform(url: string): string {
  if (url.startsWith("mention://")) return url;
  return defaultUrlTransform(url);
}

// ---------------------------------------------------------------------------
// Custom react-markdown components
// ---------------------------------------------------------------------------

function IssueMentionLink({ issueId, label }: { issueId: string; label?: string }) {
  const { push, openInNewTab } = useNavigation();
  const p = useWorkspacePaths();
  const path = p.issueDetail(issueId);
  return (
    <span
      className="inline align-middle"
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        if (e.metaKey || e.ctrlKey || e.shiftKey) {
          if (openInNewTab) {
            openInNewTab(path, label);
          }
          return;
        }
        push(path);
      }}
    >
      <IssueMentionCard issueId={issueId} fallbackLabel={label} />
    </span>
  );
}

// Named component so it can call useWorkspaceSlug() — arrow function inlined
// inside `components` below would still work, but extracting it keeps the
// hook usage explicit and avoids hook-in-object-literal surprises.
function ReadonlyLink({
  href,
  children,
}: {
  href?: string;
  children?: React.ReactNode;
}) {
  const slug = useWorkspaceSlug();

  if (isMentionHref(href)) {
    const match = href.match(/^mention:\/\/(member|agent|issue|all)\/(.+)$/);
    if (match?.[1] === "issue" && match[2]) {
      const label =
        typeof children === "string"
          ? children
          : Array.isArray(children)
            ? children.join("")
            : undefined;
      return <IssueMentionLink issueId={match[2]} label={label} />;
    }
    // Member / agent / all mentions
    return <span className="mention">{children}</span>;
  }

  // Regular links — open directly on click
  return (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        if (href) openLink(href, slug);
      }}
    >
      {children}
    </a>
  );
}

// Image renderer with a download button that prefers fresh-signed URLs.
// Lifted out of the components map so it can call hooks; receives the
// attachment lookup as props so the components map can stay a pure
// data-build inside `ReadonlyContent`'s `useMemo`.
function ReadonlyImage({
  src,
  alt,
  resolveAttachmentId,
  onDownload,
}: {
  src?: string;
  alt?: string;
  resolveAttachmentId: (url: string) => string | undefined;
  onDownload: (attachmentId: string) => void;
}) {
  const { t } = useT("editor");
  const [lightbox, setLightbox] = useState(false);
  const imgSrc = typeof src === "string" ? src : "";
  const imgAlt = alt ?? "";

  const handleView = () => setLightbox(true);
  const handleDownload = () => {
    const id = imgSrc ? resolveAttachmentId(imgSrc) : undefined;
    if (id) {
      onDownload(id);
      return;
    }
    // External image — no attachment record to re-sign through. Falling back
    // to `openExternal` keeps us off `window.open(...)` (which Electron's
    // setWindowOpenHandler would route through openExternalSafely anyway,
    // but only after rejecting non-http schemes loudly).
    if (imgSrc) openExternal(imgSrc);
  };
  const handleCopyLink = async () => {
    try {
      await navigator.clipboard.writeText(imgSrc);
      toast.success(t(($) => $.image.link_copied));
    } catch {
      toast.error(t(($) => $.image.copy_link_failed));
    }
  };

  return (
    <span className="image-node">
      <span className="image-figure" onClick={handleView}>
        <img src={imgSrc} alt={imgAlt} className="image-content" draggable={false} />
        <span
          className="image-toolbar"
          onMouseDown={(e) => e.stopPropagation()}
          onClick={(e) => e.stopPropagation()}
        >
          <button type="button" onClick={handleView} title={t(($) => $.image.view)}>
            <Maximize2 className="size-3.5" />
          </button>
          <button type="button" onClick={handleDownload} title={t(($) => $.image.download)}>
            <Download className="size-3.5" />
          </button>
          <button type="button" onClick={handleCopyLink} title={t(($) => $.image.copy_link)}>
            <LinkIcon className="size-3.5" />
          </button>
        </span>
      </span>
      {lightbox && (
        <ImageLightbox src={imgSrc} alt={imgAlt} onClose={() => setLightbox(false)} />
      )}
    </span>
  );
}

// Inline file card — same download semantics as the standalone attachment
// list: fresh-sign through `useDownloadAttachment` when the href matches a
// known attachment, otherwise hand the raw URL to the platform's external
// opener.
function ReadonlyFileCard({
  href,
  filename,
  resolveAttachment,
  onDownload,
  onPreview,
}: {
  href: string;
  filename: string;
  resolveAttachment: (url: string) => Attachment | undefined;
  onDownload: (attachmentId: string) => void;
  onPreview: (source: PreviewSource) => boolean;
}) {
  const { t } = useT("editor");
  const attachment = href ? resolveAttachment(href) : undefined;
  // Mirror file-card.tsx (NodeView) — preview gate widens to "anything that
  // can be downloaded AND whose filename is a previewable type". Media kinds
  // fall through to URL-only when the attachment record isn't reachable.
  const kind = filename
    ? getPreviewKind(attachment?.content_type ?? "", filename)
    : null;
  const isMediaKind = kind === "pdf" || kind === "video" || kind === "audio";
  const canPreview = !!href && kind !== null && (!!attachment || isMediaKind);
  const handleDownloadClick = () => {
    if (attachment) {
      onDownload(attachment.id);
      return;
    }
    openExternal(href);
  };
  const handlePreviewClick = () => {
    if (attachment) {
      onPreview({ kind: "full", attachment });
    } else if (href) {
      onPreview({ kind: "url", url: href, filename });
    }
  };
  return (
    <div className="my-1 flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted">
      <FileText className="size-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm">{filename}</p>
      </div>
      {canPreview && (
        <button
          type="button"
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          title={t(($) => $.attachment.preview)}
          aria-label={t(($) => $.attachment.preview)}
          onClick={handlePreviewClick}
        >
          <Eye className="size-3.5" />
        </button>
      )}
      {href && (
        <button
          type="button"
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          title={t(($) => $.image.download)}
          aria-label={t(($) => $.image.download)}
          onClick={handleDownloadClick}
        >
          <Download className="size-3.5" />
        </button>
      )}
    </div>
  );
}

function buildComponents(
  resolveAttachmentId: (url: string) => string | undefined,
  resolveAttachment: (url: string) => Attachment | undefined,
  onDownload: (attachmentId: string) => void,
  onPreview: (source: PreviewSource) => boolean,
): Partial<Components> {
  return {
    // Links — route mention:// to mention components, others show preview card
    a: ReadonlyLink,

    // Images — centered with toolbar + lightbox (matches Tiptap ImageView NodeView)
    img: ({ src, alt }) => (
      <ReadonlyImage
        src={typeof src === "string" ? src : undefined}
        alt={alt ?? undefined}
        resolveAttachmentId={resolveAttachmentId}
        onDownload={onDownload}
      />
    ),

    // FileCard — intercept <div data-type="fileCard"> from preprocessMarkdown
    div: ({ node, children, ...props }) => {
      const dataType = node?.properties?.dataType as string | undefined;
      if (dataType === "fileCard") {
        const rawHref = (node?.properties?.dataHref as string) || "";
        // Only allow http(s) URLs to prevent javascript: and other dangerous schemes.
        const href = /^https?:\/\//i.test(rawHref) ? rawHref : "";
        const filename = (node?.properties?.dataFilename as string) || "";
        return (
          <ReadonlyFileCard
            href={href}
            filename={filename}
            resolveAttachment={resolveAttachment}
            onDownload={onDownload}
            onPreview={onPreview}
          />
        );
      }
      return <div {...props}>{children}</div>;
    },

    // Tables — wrap in tableWrapper div for border/radius/scroll (matches Tiptap)
    table: ({ children }) => (
      <div className="tableWrapper">
        <table>{children}</table>
      </div>
    ),

    // Code — lowlight highlighting for blocks, plain render for inline
    code: ({ className, children, node, ...props }) => {
      const lang = /language-(\w+)/.exec(className || "")?.[1];
      const isBlock =
        node?.position &&
        node.position.start.line !== node.position.end.line;

      if (isBlock && lang === "mermaid") {
        return <MermaidDiagram chart={String(children).replace(/\n$/, "")} />;
      }

      if (!isBlock && !lang) {
        // Inline code — CSS handles styling via .rich-text-editor code
        return <code {...props}>{children}</code>;
      }

      // Block code — highlight with lowlight, output hljs classes
      const code = String(children).replace(/\n$/, "");
      try {
        const tree = lang
          ? lowlight.highlight(lang, code)
          : lowlight.highlightAuto(code);
        return (
          <code
            className={cn("hljs", lang && `language-${lang}`)}
            dangerouslySetInnerHTML={{ __html: toHtml(tree) }}
          />
        );
      } catch {
        // Fallback — render without highlighting
        return (
          <code className={className} {...props}>
            {children}
          </code>
        );
      }
    },

    // Pre — pass through (CSS handles styling via .rich-text-editor pre)
    pre: ({ children }) => {
      if (isValidElement(children) && children.type === MermaidDiagram) {
        return <>{children}</>;
      }
      return <pre>{children}</pre>;
    },
  };
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface ReadonlyContentProps {
  content: string;
  className?: string;
  /**
   * Attachments associated with the surrounding entity (comment / issue
   * body). When the markdown contains an inline `<img>` or file card whose
   * URL matches one of these attachments, the download button re-signs the
   * URL at click time via `useDownloadAttachment` instead of opening the
   * potentially stale link embedded in the markdown.
   *
   * Callers SHOULD pass a stable reference (e.g. the field on a memoized
   * timeline entry); a fresh array on every parent render busts the memo.
   */
  attachments?: Attachment[];
}

// Memoized so a long timeline of comments (Inbox + IssueDetail) does not
// re-run the full react-markdown + rehype-* + lowlight pipeline on every
// parent re-render. Props are `content`/`className`/`attachments`, all
// shallow-comparable; stability is the caller's responsibility for the
// array.
export const ReadonlyContent = memo(function ReadonlyContent({
  content,
  className,
  attachments,
}: ReadonlyContentProps) {
  const processed = useMemo(() => preprocessMarkdown(content), [content]);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const hover = useLinkHover(wrapperRef);
  const download = useDownloadAttachment();

  const resolveAttachmentId = useCallback(
    (url: string): string | undefined => {
      if (!url || !attachments?.length) return undefined;
      return attachments.find((a) => a.url === url)?.id;
    },
    [attachments],
  );

  const resolveAttachment = useCallback(
    (url: string): Attachment | undefined => {
      if (!url || !attachments?.length) return undefined;
      return attachments.find((a) => a.url === url);
    },
    [attachments],
  );

  const preview = useAttachmentPreview();

  const components = useMemo(
    () => buildComponents(resolveAttachmentId, resolveAttachment, download, preview.tryOpen),
    [resolveAttachmentId, resolveAttachment, download, preview.tryOpen],
  );

  return (
    <div ref={wrapperRef} className={cn("rich-text-editor readonly text-sm", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkMath, remarkBreaks, [remarkGfm, { singleTilde: false }]]}
        rehypePlugins={[rehypeRaw, [rehypeSanitize, sanitizeSchema], rehypeKatex]}
        urlTransform={urlTransform}
        components={components}
      >
        {processed}
      </ReactMarkdown>
      <LinkHoverCard {...hover} />
      {preview.modal}
    </div>
  );
});
