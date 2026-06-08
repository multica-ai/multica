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
 *   styles/index.css rules apply to standard HTML tags
 * - Using the same preprocessMarkdown pipeline (mention shortcodes + linkify)
 * - Using lowlight for code highlighting (same engine as Tiptap's CodeBlockLowlight)
 *   so .hljs-* CSS rules from styles/code.css produce identical colors
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
import { toHtml } from "hast-util-to-html";
import { Copy, Check } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceSlug } from "@multica/core/paths";
import type { Attachment } from "@multica/core/types";
import { IssueMentionCard } from "../issues/components/issue-mention-card";
import { useLinkHover, LinkHoverCard } from "./link-hover-card";
import { openLink, isMentionHref } from "./utils/link-handler";
import { isAllowedFileCardHref } from "@multica/ui/markdown";
import { preprocessMarkdown } from "./utils/preprocess";
import { MermaidDiagram } from "./mermaid-diagram";
import { HtmlBlockPreview } from "./html-block-preview";
import { AttachmentDownloadProvider } from "./attachment-download-context";
import { Attachment as AttachmentRenderer } from "./attachment";
import "katex/dist/katex.min.css";
import "./styles/index.css";

// ---------------------------------------------------------------------------
// Lowlight — same engine + language set as Tiptap's CodeBlockLowlight
// ---------------------------------------------------------------------------

const lowlight = createLowlight(common);

// Code fences that the `code` renderer returns as a non-<code> React element
// (Mermaid diagram, HTML preview iframe). The `pre` renderer below unwraps
// these so the default <pre><code> envelope doesn't clamp their styles.
// Anchored to whole class tokens so `language-htmlbars` / `language-mermaidx`
// don't accidentally match and lose their <pre> wrapper.
const PRE_UNWRAP_RE = /(^|\s)language-(html|mermaid)(\s|$)/;

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
  return <IssueMentionCard issueId={issueId} fallbackLabel={label} />;
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

function CodeBlockHeader({ language, code }: { language?: string; code: string }) {
  const [copied, setCopied] = useState(false);
  const handleCopy = useCallback(async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [code]);
  return (
    <div className="code-block-header flex select-none items-center justify-between rounded-t-md border-b border-border bg-muted/50 px-3 py-1.5 text-xs text-muted-foreground">
      <span>{language || "code"}</span>
      <button
        type="button"
        onMouseDown={(e) => e.preventDefault()}
        onClick={handleCopy}
        className="pointer-events-auto flex items-center gap-1 rounded px-1.5 py-0.5 transition-colors hover:bg-muted hover:text-foreground"
        title="Copy code"
      >
        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        <span>{copied ? "Copied" : "Copy"}</span>
      </button>
    </div>
  );
}

/**
 * Extract plain text from a react-markdown AST node's children.
 * Used by the `code` component to ensure code block / inline code content
 * is always rendered as plain text, even if a preprocessing or parsing
 * layer accidentally wrapped part of it in an interactive element.
 */
function extractTextFromAst(node: any): string {
  if (!node?.children) return "";
  return (node.children as any[])
    .map((n: any) => {
      if (n.type === "text") return n.value as string;
      // Recurse into element children (e.g. <a> wrapping text)
      if (n.children) return extractTextFromAst(n);
      return "";
    })
    .join("")
    .replace(/\n$/, "");
}

function buildComponents(): Partial<Components> {
  return {
    // Links — route mention:// to mention components, others show preview card
    a: ReadonlyLink,

    // Images — unified through <Attachment>. The resolver context provided
    // by AttachmentDownloadProvider (mounted in ReadonlyContent below) turns
    // a CDN URL into a full record when possible; external URLs render as
    // plain images with lightbox-via-preview-modal. forceKind is mandatory
    // here because markdown `![]()` carries no content-type and alt is
    // commonly empty or descriptive — without it images fall through to
    // the file-card chrome.
    img: ({ src, alt }) => (
      <AttachmentRenderer
        attachment={{
          kind: "url",
          url: typeof src === "string" ? src : "",
          filename: alt ?? "",
          forceKind: "image",
        }}
      />
    ),

    // FileCard — intercept <div data-type="fileCard"> from preprocessMarkdown
    div: ({ node, children, ...props }) => {
      const dataType = node?.properties?.dataType as string | undefined;
      if (dataType === "fileCard") {
        const rawHref = (node?.properties?.dataHref as string) || "";
        const href = isAllowedFileCardHref(rawHref) ? rawHref : "";
        const filename = (node?.properties?.dataFilename as string) || "";
        return (
          <AttachmentRenderer
            attachment={{ kind: "url", url: href, filename }}
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

      // Extract plain text from AST node to avoid rendering interactive
      // elements (e.g. <a> links) inside code blocks/inline code.
      const codeText = extractTextFromAst(node);

      if (isBlock && lang === "mermaid") {
        return <MermaidDiagram chart={codeText} />;
      }
      if (isBlock && lang === "html") {
        // Like Mermaid, return the React element directly here and rely on
        // the `pre` renderer below to unwrap it — react-markdown otherwise
        // wraps `code` children in a `<pre>` whose monospace + overflow
        // styles would clamp the preview iframe.
        return <HtmlBlockPreview html={codeText} />;
      }

      if (!isBlock && !lang) {
        // Inline code — always render as plain text, never interactive
        return <code {...props}>{codeText || children}</code>;
      }

      // Block code — highlight with lowlight, output hljs classes
      const code = codeText || String(children).replace(/\n$/, "");
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
          {code}
        </code>
      );
    },

    // Pre — pass through (CSS handles styling via .rich-text-editor pre).
    // Special-case Mermaid / HtmlBlockPreview returned from the `code`
    // renderer above so the outer `<pre>` does not wrap them — this is the
    // standard two-layer pattern used to escape react-markdown's default
    // `<pre><code>` envelope.
    pre: ({ children, node }) => {
      if (isValidElement(children)) {
        const childProps = children.props as { className?: string };
        if (PRE_UNWRAP_RE.test(childProps.className ?? "")) {
          return <>{children}</>;
        }
      }
      // Extract text content and language for header bar
      const codeEl = (node?.children ?? []).find(
        (child: any) => child.type === "element" && child.tagName === "code"
      ) as any;
      const codeText = codeEl
        ? (codeEl.children as any[])
            .filter((n: any) => n.type === "text")
            .map((n: any) => n.value as string)
            .join("")
            .replace(/\n$/, "")
        : "";
      const classNames: string[] = codeEl?.properties?.className ?? [];
      const langClass = classNames.find((cls: string) => cls.startsWith("language-"));
      const language = langClass?.replace("language-", "");
      return (
        <div className="code-block-wrapper my-2 overflow-hidden rounded-md border border-border select-text">
          {codeText && <CodeBlockHeader language={language} code={codeText} />}
          <pre className="!mt-0 !rounded-t-none !border-0 select-text">{children}</pre>
        </div>
      );
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

  // Components map is now static — all attachment-aware logic lives in
  // <Attachment>, which reads the surrounding AttachmentDownloadProvider.
  const components = useMemo(() => buildComponents(), []);

  return (
    <AttachmentDownloadProvider attachments={attachments}>
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
      </div>
    </AttachmentDownloadProvider>
  );
});
