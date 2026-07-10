"use client";

// CEREBRO-PATCH(readonly-content-cerebro): cerebro modification of upstream file

/**
 * ReadonlyContent — lightweight markdown renderer for readonly content display.
 *
 * Replaces <ContentEditor editable={false}> for comment cards and other
 * read-only surfaces. Uses react-markdown instead of a full Tiptap/ProseMirror
 * instance, eliminating EditorView, Plugin, and NodeView overhead.
 *
 * Visual parity with ContentEditor is achieved by:
 * - Wrapping output in <div class="rich-text-editor readonly"> so the same
 *   styles/index.css rules apply to standard HTML tags
 * - Using the same preprocessMarkdown pipeline (mention shortcodes + linkify)
 * - Using lowlight for code highlighting (same engine as Tiptap's CodeBlockLowlight)
 *   so .hljs-* CSS rules from styles/code.css produce identical colors
 * - Rendering mentions with the same IssueMentionCard component and .mention class
 */

import { isValidElement, memo, useMemo, useRef, useState } from "react";
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
// CEREBRO-PATCH(stacked-data-tables): tag markdown data tables + inject per-cell data-label (FIR-1727).
import { rehypeStackedDataTables } from "@multica/ui/markdown/cerebro-stacked-tables";
import { createLowlight, common } from "lowlight";
import { toHtml } from "hast-util-to-html";
import { Maximize2, Download, Link as LinkIcon, FileText, Eye } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import type { Attachment } from "@multica/core/types";
import { isViewableAttachment, viewableKind } from "@multica/cerebro-attachments/core/viewable";
// CEREBRO-PATCH(readonly-attachment-chip): FIR-2034 — posted images/files as unified cards behind cerebro_attachment_chips.
import { AttachmentChip } from "@multica/cerebro-ui";
// CEREBRO-PATCH(readonly-image-gallery): FIR-2710 — inline images join the surface image gallery.
import { useGalleryImage } from "@multica/cerebro-attachments/views";
import { useFlagValue } from "@multica/cerebro-feature-flags";
// CEREBRO-PATCH(issue-link-open-mode): honor the account-level issue-link open preference.
import { useIssueLinkOpenMode } from "@multica/cerebro-preferences/views";
// CEREBRO-PATCH(skill-mention-readonly): render `mention://skill/<id>` links as SkillMentionChip.
import { SkillMentionChip } from "@multica/cerebro-skill-mention";
// CEREBRO-PATCH(artifact-mention-readonly): FIR-1800 render `mention://artifact/<id>` as a white card.
import { ArtifactMentionChip } from "@multica/cerebro-artifact-mention";
import { useWorkspacePaths, useWorkspaceSlug } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { IssueChip } from "../issues/components/issue-chip";
import { ImageLightbox } from "./extensions/image-view";
import { ProjectChip } from "../projects/components/project-chip";
import { useLinkHover, LinkHoverCard } from "./link-hover-card";
import { useDownloadAttachment } from "./use-download-attachment";
import { openLink, isMentionHref } from "./utils/link-handler";
import { preprocessMarkdown } from "./utils/preprocess";
import { highlightToHtml } from "./utils/highlight-markdown";
import { MermaidDiagram } from "./mermaid-diagram";
import { HtmlBlockPreview } from "./html-block-preview";
import "katex/dist/katex.min.css";
import "./styles/index.css";

// Match the exact language token to decide whether the `pre` wrapper should
// be dropped. Using exact token matching (not substring includes) prevents
// false positives on adjacent languages like `language-htmlbars`.
const PRE_UNWRAP_RE = /(^|\s)language-(html|mermaid)(\s|$)/;

// ---------------------------------------------------------------------------
// Lowlight — same engine + language set as Tiptap's CodeBlockLowlight
// ---------------------------------------------------------------------------

const lowlight = createLowlight(common);

// ---------------------------------------------------------------------------
// Sanitization schema — extends GitHub defaults to allow file-card data attrs
// ---------------------------------------------------------------------------

const sanitizeSchema = {
  ...defaultSchema,
  // CEREBRO-PATCH(todo-list-editor): allow readonly checkbox markup from GFM task lists.
  // Allow <mark> (text highlight) — emitted by highlightToHtml from `==text==`.
  // It carries no attributes, so only the tag name needs whitelisting.
  tagNames: [...(defaultSchema.tagNames ?? []), "input", "mark"],
  protocols: {
    ...defaultSchema.protocols,
    href: [...(defaultSchema.protocols?.href ?? []), "mention", "slash"],
  },
  attributes: {
    ...defaultSchema.attributes,
    ul: [
      ...(defaultSchema.attributes?.ul ?? []),
      ["className", "contains-task-list"],
    ],
    li: [
      ...(defaultSchema.attributes?.li ?? []),
      ["className", "task-list-item"],
    ],
    input: [
      ...(defaultSchema.attributes?.input ?? []),
      ["type", "checkbox"],
      "checked",
      "disabled",
    ],
    div: [
      ...(defaultSchema.attributes?.div ?? []),
      "dataType",
      "dataHref",
      "dataFilename",
      "dataAttachmentId",
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
    // CEREBRO-PATCH(stacked-data-tables): keep the data-table class + per-cell data-label through sanitize (FIR-1727).
    table: [...(defaultSchema.attributes?.table ?? []), ["className", "data-table"]],
    td: [...(defaultSchema.attributes?.td ?? []), "dataLabel"],
  },
};

// ---------------------------------------------------------------------------
// URL transform — allow mention:// protocol through react-markdown's sanitizer
// ---------------------------------------------------------------------------

function urlTransform(url: string): string {
  if (url.startsWith("mention://")) return url;
  if (url.startsWith("slash://skill/")) return url;
  return defaultUrlTransform(url);
}

// ---------------------------------------------------------------------------
// Custom react-markdown components
// ---------------------------------------------------------------------------

// CEREBRO-PATCH(issue-mention-new-tab): open issue mentions inside readonly
// content (comments, descriptions) in a new tab via the relative workspace
// path, so href resolves against the current origin (sara.local / app.multica.io /
// localhost) instead of hard-coding `appUrl` — which defaulted to multica.ai
// on desktop and made the link visibly wrong for cerebro deployments (JEH-1048).
// JEH-1112: on mobile the PWA renders in `display: standalone` (no tab UI), so
// `target="_blank"` / `window.open(_, "_blank")` either navigates the current
// window or breaks out into the system browser — both lose thread context.
// Use SPA push so the browser back-button restores scroll + tiptap draft via
// the Next.js router cache, matching mobile-native patterns.
function IssueMentionLink({ issueId, label }: { issueId: string; label?: string }) {
  const { openInNewTab, push } = useNavigation();
  const isMobile = useIsMobile();
  const openMode = useIssueLinkOpenMode();
  const p = useWorkspacePaths();
  const path = p.issueDetail(issueId);
  const opensInNewTab = !isMobile && openMode === "always_new_tab";
  return (
    <a
      href={path}
      target={opensInNewTab ? "_blank" : undefined}
      rel={opensInNewTab ? "noopener noreferrer" : undefined}
      // CEREBRO-PATCH(issue-chip-inline-link-wrap): inline (not inline-flex) so box-decoration-clone wraps across lines (JEH-1593)
      className="issue-mention not-prose inline"
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        if (isMobile || openMode === "modifier_click") {
          if (!isMobile && (e.metaKey || e.ctrlKey || e.shiftKey)) {
            if (openInNewTab) openInNewTab(path, label);
            else window.open(path, "_blank", "noopener,noreferrer");
            return;
          }
          push(path);
          return;
        }
        if (openInNewTab) openInNewTab(path, label);
        else window.open(path, "_blank", "noopener,noreferrer");
      }}
    >
      <IssueChip
        issueId={issueId}
        fallbackLabel={label}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
  );
}

function ProjectMentionLink({ projectId, label }: { projectId: string; label?: string }) {
  const { push, openInNewTab } = useNavigation();
  const p = useWorkspacePaths();
  const path = p.projectDetail(projectId);
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
      <ProjectChip projectId={projectId} fallbackLabel={label} className="cursor-pointer hover:bg-accent transition-colors" />
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

  if (href?.startsWith("slash://skill/")) {
    return <span className="slash-command">{children}</span>;
  }

  if (isMentionHref(href)) {
    // CEREBRO-PATCH(skill-mention-readonly-route): `skill` joins the mention regex
    // and routes to SkillMentionChip; member/agent/all keep their plain @-text render.
    // CEREBRO-PATCH(artifact-mention-readonly-route): `artifact` joins the regex and routes to ArtifactMentionChip (FIR-1800).
    const match = href.match(/^mention:\/\/(member|agent|issue|project|all|skill|artifact)\/(.+)$/);
    if (match?.[1] === "skill" && match[2]) {
      const label =
        typeof children === "string"
          ? children
          : Array.isArray(children)
            ? children.join("")
            : undefined;
      return <SkillMentionChip skillId={match[2]} fallbackLabel={label} />;
    }
    if (match?.[1] === "artifact" && match[2]) {
      const label =
        typeof children === "string"
          ? children
          : Array.isArray(children)
            ? children.join("")
            : undefined;
      return <ArtifactMentionChip artifactId={match[2]} fallbackLabel={label} />;
    }
    if (match?.[1] === "issue" && match[2]) {
      const label =
        typeof children === "string"
          ? children
          : Array.isArray(children)
            ? children.join("")
            : undefined;
      return <IssueMentionLink issueId={match[2]} label={label} />;
    }
    if (match?.[1] === "project" && match[2]) {
      const label =
        typeof children === "string"
          ? children
          : Array.isArray(children)
            ? children.join("")
            : undefined;
      return <ProjectMentionLink projectId={match[2]} label={label} />;
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

// CEREBRO-PATCH(readonly-content-cerebro): file-card div renderer with attachment viewer
function FileCardDiv({
  node,
  children,
  ...props
}: React.ComponentProps<"div"> & { node?: { properties?: Record<string, unknown> } }) {
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const download = useDownloadAttachment();
  // CEREBRO-PATCH(readonly-attachment-chip): FIR-2034 — flag read before the early return to keep hook order stable.
  const chipsEnabled = useFlagValue("cerebro_attachment_chips");
  const dataType = node?.properties?.dataType as string | undefined;
  if (dataType !== "fileCard") {
    return <div {...props}>{children}</div>;
  }
  const rawHref = (node?.properties?.dataHref as string) || "";
  // CEREBRO-PATCH(file-card-relative-url): accept same-origin paths starting
  // with `/` so chat uploads (relative `/uploads/…`) render correctly.
  // Only allow http(s) or same-origin to block javascript: and other schemes.
  const href = /^(https?:\/\/|\/)/i.test(rawHref) ? rawHref : "";
  const filename = (node?.properties?.dataFilename as string) || "";
  const attachmentId = (node?.properties?.dataAttachmentId as string) || "";
  // We don't have content_type for inline file cards — fall back to filename
  // extension via isViewableAttachment("", filename).
  const viewable = Boolean(attachmentId) && isViewableAttachment("", filename);

  const openViewer = () => {
    const path = wsPaths.attachmentView(attachmentId);
    if (router.openInNewTab) {
      router.openInNewTab(path, filename);
    } else if (router.getShareableUrl) {
      window.open(router.getShareableUrl(path), "_blank", "noopener,noreferrer");
    } else {
      window.open(path, "_blank", "noopener,noreferrer");
    }
  };
  const openDownload = () => {
    // CEREBRO-PATCH(readonly-file-card-force-download): comments must use the same forced attachment download path as the main attachment list.
    if (attachmentId) {
      void download(attachmentId);
      return;
    }
    if (href) window.open(href, "_blank", "noopener,noreferrer");
  };

  // CEREBRO-PATCH(readonly-attachment-chip): FIR-2034 — posted file embeds as unified cards (image thumbnail / colour card), matching the composer.
  if (chipsEnabled)
    return (
      <AttachmentChip
        filename={filename}
        thumbnailSrc={viewableKind("", filename) === "image" ? href : undefined}
        onActivate={viewable ? openViewer : openDownload}
        activateLabel={viewable ? "Open in viewer" : "Download"}
        className="my-1"
      />
    );

  return (
    <div className="my-1 flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted">
      <FileText className="size-4 shrink-0 text-muted-foreground" />
      <button
        type="button"
        onClick={viewable ? openViewer : openDownload}
        className="min-w-0 flex-1 truncate text-left text-sm hover:underline"
        title={viewable ? "Open in viewer" : "Download"}
      >
        {filename}
      </button>
      {viewable && (
        <button
          type="button"
          aria-label="Open in viewer"
          title="Open in viewer"
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          onClick={openViewer}
        >
          <Eye className="size-3.5" />
        </button>
      )}
      {href && (
        <button
          type="button"
          aria-label="Download"
          title="Download"
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
          onClick={openDownload}
        >
          <Download className="size-3.5" />
        </button>
      )}
    </div>
  );
}
// CEREBRO-PATCH(readonly-image-strip): FIR-2714 — a paragraph that holds only
// images (soft-break-separated `![image N](url)` lines, e.g. the composer image
// tray appended on send) is rendered as a left-aligned, horizontally scrollable
// strip instead of centered stacked blocks. Detects the shape off the hast node
// so a paragraph mixing text and an inline image is left as a normal <p>.
function isImageOnlyParagraph(node?: {
  children?: Array<{ type?: string; tagName?: string; value?: unknown }>;
}): boolean {
  const children = node?.children;
  if (!Array.isArray(children) || children.length === 0) return false;
  let hasImage = false;
  for (const child of children) {
    if (child.type === "element" && child.tagName === "img") {
      hasImage = true;
      continue;
    }
    if (child.type === "element" && child.tagName === "br") continue;
    if (child.type === "text" && String(child.value ?? "").trim() === "") continue;
    return false;
  }
  return hasImage;
}

const components: Partial<Components> = {
  // Links — route mention:// to mention components, others show preview card
  a: ReadonlyLink,

  // CEREBRO-PATCH(readonly-image-strip): FIR-2714 — image-only paragraphs render
  // as a left-aligned scrollable strip (see .rte-image-strip in cerebro-overrides.css).
  p: function ReadonlyParagraph({
    node,
    children,
    ...props
  }: React.ComponentProps<"p"> & {
    node?: {
      children?: Array<{ type?: string; tagName?: string; value?: unknown }>;
    };
  }) {
    if (isImageOnlyParagraph(node)) {
      return <div className="rte-image-strip">{children}</div>;
    }
    return <p {...props}>{children}</p>;
  },

  // Images — centered with toolbar + lightbox (matches Tiptap ImageView NodeView)
  img: function ReadonlyImage({ src, alt }) {
    const { t } = useT("editor");
    const [lightbox, setLightbox] = useState(false);
    const imgSrc = typeof src === "string" ? src : "";
    const imgAlt = alt ?? "";
    // CEREBRO-PATCH(readonly-image-gallery): FIR-2710 — open the surface gallery when present, else the legacy lightbox.
    const gallery = useGalleryImage({ src: imgSrc, alt: imgAlt, downloadHref: imgSrc });
    const handleView = () => (gallery.enabled ? gallery.open() : setLightbox(true));
    // CEREBRO-PATCH(readonly-attachment-chip): FIR-2034 — posted image as the compact thumbnail card + existing lightbox, matching the composer.
    const chipsEnabled = useFlagValue("cerebro_attachment_chips");
    if (chipsEnabled)
      return (
        <span className="image-node" ref={gallery.ref}>
          <AttachmentChip filename={imgAlt || "image"} thumbnailSrc={imgSrc} onActivate={handleView} activateLabel={t(($) => $.image.view)} className="my-1" />
          {lightbox && <ImageLightbox src={imgSrc} alt={imgAlt} onClose={() => setLightbox(false)} />}
        </span>
      );
    const handleDownload = () => {
      window.open(imgSrc, "_blank", "noopener,noreferrer");
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
      // CEREBRO-PATCH(readonly-image-gallery): FIR-2710 — register this image with the surface gallery via ref.
      <span className="image-node" ref={gallery.ref}>
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
  },

  // FileCard — intercept <div data-type="fileCard"> from preprocessMarkdown
  div: FileCardDiv,

  // Tables — wrap in tableWrapper div for border/radius/scroll (matches Tiptap)
  // CEREBRO-PATCH(stacked-data-tables): forward the data-table class so CSS can render data tables as stacked cards on mobile (FIR-1727).
  table: ({ children, className }) => (
    <div className="tableWrapper">
      <table className={className}>{children}</table>
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
    if (isBlock && lang === "html") {
      // Like Mermaid, return the React element directly here and rely on
      // the `pre` renderer below to unwrap it — react-markdown otherwise
      // wraps `code` children in a `<pre>` whose monospace + overflow
      // styles would clamp the preview iframe.
      return <HtmlBlockPreview html={String(children).replace(/\n$/, "")} />;
    }

    if (!isBlock && !lang) {
      // Inline code — CSS handles styling via .rich-text-editor code
      return <code {...props}>{children}</code>;
    }

    // Block code — highlight with lowlight, output hljs classes.
    // Upstream #3358: when lowlight.highlightAuto returns an empty tree
    // (unknown language, low confidence), `toHtml(...)` returns "" and the
    // hljs-classed <code> renders blank. Fall back to plain text when
    // highlighting yields nothing.
    const code = String(children).replace(/\n$/, "");
    try {
      const tree = lang
        ? lowlight.highlight(lang, code)
        : lowlight.highlightAuto(code);
      const html = toHtml(tree);
      if (html) {
        return (
          <code
            className={cn("hljs", lang && `language-${lang}`)}
            dangerouslySetInnerHTML={{ __html: html }}
          />
        );
      }
    } catch {
      // fall through to plain render
    }
    return (
      <code className={cn("hljs", className)} {...props}>
        {children}
      </code>
    );
  },

  // Pre — pass through (CSS handles styling via .rich-text-editor pre).
  // react-markdown calls `pre` BEFORE the `code` renderer runs, so `children`
  // is the raw `<code>` AST element. Identify blocks to unwrap by className.
  pre: ({ children }) => {
    if (isValidElement(children)) {
      const childProps = children.props as { className?: string };
      if (PRE_UNWRAP_RE.test(childProps.className ?? "")) {
        return <>{children}</>;
      }
    }
    return <pre>{children}</pre>;
  },
};

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface ReadonlyContentProps {
  content: string;
  className?: string;
  /**
   * Attachments associated with this content. When provided, file-card divs
   * gain the matching attachment ID so the renderer can route clicks to the
   * in-app attachment viewer for viewable filetypes (HTML, Markdown, …).
   */
  attachments?: Attachment[];
}

// Memoized so a long timeline of comments (Inbox + IssueDetail) does not
// re-run the full react-markdown + rehype-* + lowlight pipeline on every
// parent re-render. Props are `content` and `className` (both strings), so
// React.memo's default shallow comparison is value-equality here.
// CEREBRO-PATCH(readonly-content-cerebro): pass attachments through preprocessing
// so file-card renderer can route clicks to in-app viewer.
export const ReadonlyContent = memo(function ReadonlyContent({
  content,
  className,
  attachments,
}: ReadonlyContentProps) {
  const attachmentsByUrl = useMemo(() => {
    if (!attachments?.length) return undefined;
    const map = new Map<string, string>();
    for (const a of attachments) map.set(a.url, a.id);
    return map;
  }, [attachments]);
  const processed = useMemo(
    () => highlightToHtml(preprocessMarkdown(content, attachmentsByUrl)),
    [content, attachmentsByUrl],
  );
  const wrapperRef = useRef<HTMLDivElement>(null);
  const hover = useLinkHover(wrapperRef);

  return (
    <div ref={wrapperRef} className={cn("rich-text-editor readonly text-sm", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkMath, remarkBreaks, [remarkGfm, { singleTilde: false }]]}
        // CEREBRO-PATCH(stacked-data-tables): run after rehypeRaw, before sanitize (which whitelists the injected class/attr) — FIR-1727.
        rehypePlugins={[rehypeRaw, rehypeStackedDataTables, [rehypeSanitize, sanitizeSchema], rehypeKatex]}
        urlTransform={urlTransform}
        components={components}
      >
        {processed}
      </ReactMarkdown>
      <LinkHoverCard {...hover} />
    </div>
  );
});
