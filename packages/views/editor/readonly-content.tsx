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

import { isValidElement, memo, useMemo, useRef, useState } from "react";
import ReactMarkdown, {
  defaultUrlTransform,
  type Components,
} from "react-markdown";
import type { ReactNode } from "react";
import rehypeKatex from "rehype-katex";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import { toHtml } from "hast-util-to-html";
import { Check, Copy } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { useWorkspacePaths, useWorkspaceSlug } from "@multica/core/paths";
import type { Attachment } from "@multica/core/types";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { IssueMentionCard } from "../issues/components/issue-mention-card";
import { useResolveIssueIdentifier } from "../issues/hooks";
import { ProjectChip } from "../projects/components/project-chip";
import { useLinkHover, LinkHoverCard } from "./link-hover-card";
import { openLink, isMentionHref } from "./utils/link-handler";
import { isAllowedFileCardHref, isIssueIdentifier } from "@multica/ui/markdown";
import { preprocessMarkdown } from "./utils/preprocess";
import { highlightToHtml } from "./utils/highlight-markdown";
import { MermaidDiagram } from "./mermaid-diagram";
import { HtmlBlockPreview } from "./html-block-preview";
import { AttachmentDownloadProvider } from "./attachment-download-context";
import { Attachment as AttachmentRenderer } from "./attachment";
import { highlightCode } from "./syntax-highlight";
import "katex/dist/katex.min.css";
import "./styles/index.css";

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
  // Allow <mark> (text highlight) — emitted by highlightToHtml from `==text==`.
  // It carries no attributes, so only the tag name needs whitelisting.
  tagNames: [...(defaultSchema.tagNames ?? []), "mark"],
  protocols: {
    ...defaultSchema.protocols,
    href: [...(defaultSchema.protocols?.href ?? []), "mention", "slash"],
    // Permit inline data-URI images (QR codes, charts, base64 screenshots).
    // The scheme gate only allows `data:` through here; attributes.img below
    // narrows it to image/* so non-image data URIs are still rejected.
    src: [...(defaultSchema.protocols?.src ?? []), "data"],
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
      // Drop the default plain `src` entry so the value allow-list below is the
      // one findDefinition resolves — it returns the first match by name, so a
      // bare `src` string would otherwise shadow (and disable) the allow-list.
      ...(defaultSchema.attributes?.img ?? []).filter(
        (attr) => (typeof attr === "string" ? attr : attr[0]) !== "src",
      ),
      "alt",
      // Allow inline data:image/* URIs while leaving every other src form
      // (http/https/site-relative) exactly as before: the negative lookahead
      // keeps all non-data values, and data: is narrowed to images only.
      ["src", /^data:image\//i, /^(?!data:)/i],
    ],
  },
};

// ---------------------------------------------------------------------------
// URL transform — allow mention:// protocol through react-markdown's sanitizer
// ---------------------------------------------------------------------------

function urlTransform(url: string): string {
  if (url.startsWith("mention://")) return url;
  if (url.startsWith("slash://skill/")) return url;
  // Allow inline data:image/* URIs — defaultUrlTransform strips every data: URL
  // to '', which would blank the src even after rehype-sanitize keeps it. Kept
  // in sync with the image/* narrowing in sanitizeSchema (protocols.src +
  // attributes.img) so both gates agree on what a valid inline image is.
  if (/^data:image\//i.test(url)) return url;
  return defaultUrlTransform(url);
}

// ---------------------------------------------------------------------------
// Custom react-markdown components
// ---------------------------------------------------------------------------

/**
 * Issue mention chip. Navigation — plain click, modifier click, and the
 * "open issue links in new tab" preference — is owned by the AppLink inside
 * IssueMentionCard; the wrapper only shields surrounding click handlers
 * (e.g. collapsed-comment expanders) from mention clicks.
 */
function IssueMentionLink({ issueId, label }: { issueId: string; label?: string }) {
  return (
    <span className="inline align-middle" onClick={(e) => e.stopPropagation()}>
      <IssueMentionCard issueId={issueId} fallbackLabel={label} />
    </span>
  );
}

/**
 * Autolinked bare identifier (e.g. `MUL-123`) routed through
 * `mention://issue/<identifier>` by the readonly preprocessor. Resolves to a
 * real issue in the current workspace; renders a navigable mention on a hit,
 * plain text on a miss / while loading / cross-workspace.
 */
function AutolinkedIssueMentionLink({ identifier }: { identifier: string }) {
  const issue = useResolveIssueIdentifier(identifier);
  if (!issue) return <>{identifier}</>;
  return <IssueMentionLink issueId={issue.id} label={identifier} />;
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

function getTextContent(node: ReactNode): string {
  if (node == null || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(getTextContent).join("");
  if (isValidElement(node)) {
    const props = node.props as { children?: ReactNode };
    return getTextContent(props.children);
  }
  return "";
}

function ReadonlyCodeBlock({
  children,
  language,
}: {
  children: ReactNode;
  language?: string;
}) {
  const { t } = useT("editor");
  const [copied, setCopied] = useState(false);
  const code = useMemo(
    () => getTextContent(children).replace(/\n$/, ""),
    [children],
  );
  const copyLabel = t(($) => $.code_block.copy_code) || "Copy code";

  const handleCopy = async () => {
    if (!code) return;
    if (await copyText(code)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className="code-block-wrapper group/code relative my-3">
      <div className="absolute top-0 right-0 z-10 flex items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100 focus-within:opacity-100">
        {/* Same hover chrome as the editable code block's header
            (code-block-view.tsx): language label + copy. */}
        {language && (
          <span className="text-xs text-muted-foreground select-none">
            {language}
          </span>
        )}
        <button
          type="button"
          onClick={handleCopy}
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          title={copyLabel}
          aria-label={copyLabel}
        >
          {copied ? (
            <Check className="h-3.5 w-3.5" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </button>
      </div>
      {/* No extra right padding: `.rich-text-editor pre` outranks utility
          padding classes anyway, and the editable NodeView uses the same
          1rem — keeping them identical keeps line wrapping identical. */}
      <pre className="!m-0">{children}</pre>
    </div>
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
    const match = href.match(/^mention:\/\/(member|agent|issue|project|all)\/(.+)$/);
    if (match?.[1] === "issue" && match[2]) {
      // A bare identifier (from the autolink preprocessor) is carried as the id
      // segment; a real mention carries a UUID. Dispatch on the id shape.
      if (isIssueIdentifier(match[2])) {
        return <AutolinkedIssueMentionLink identifier={match[2]} />;
      }
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

      // Block code — highlight with lowlight, output hljs classes
      const code = String(children).replace(/\n$/, "");
      try {
        const tree = highlightCode(code, lang);
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

    // Pre — wrap regular code fences with copy chrome.
    // Special-case Mermaid / HtmlBlockPreview returned from the `code`
    // renderer above so the outer `<pre>` does not wrap them — this is the
    // standard two-layer pattern used to escape react-markdown's default
    // `<pre><code>` envelope.
    pre: ({ children }) => {
      // react-markdown calls `pre` BEFORE invoking the `code` renderer —
      // `children` is the unrendered `<code>` element from the AST. So we
      // identify "this block was meant to be unwrapped" by inspecting the
      // child's className (`language-mermaid`, `language-html`), not by
      // checking `children.type === MermaidDiagram`, which never matches.
      //
      // Match by exact class token: a substring `includes("language-html")`
      // would also fire on neighboring languages like `language-htmlbars`
      // and silently strip their <pre> wrapper.
      let language: string | undefined;
      if (isValidElement(children)) {
        const childProps = children.props as { className?: string };
        if (PRE_UNWRAP_RE.test(childProps.className ?? "")) {
          return <>{children}</>;
        }
        language = /language-(\w+)/.exec(childProps.className ?? "")?.[1];
      }
      return <ReadonlyCodeBlock language={language}>{children}</ReadonlyCodeBlock>;
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
  const processed = useMemo(
    () =>
      highlightToHtml(
        preprocessMarkdown(content, { autolinkIssueIdentifiers: true }),
      ),
    [content],
  );
  const wrapperRef = useRef<HTMLDivElement>(null);
  const hover = useLinkHover(wrapperRef);

  // Components map is now static — all attachment-aware logic lives in
  // <Attachment>, which reads the surrounding AttachmentDownloadProvider.
  const components = useMemo(() => buildComponents(), []);

  // Memoize the whole react-markdown subtree on its only real inputs
  // (`processed` + `components`). Unrelated parent re-renders (e.g. a sibling
  // agent task streaming over WebSocket fires one every ~100ms) would otherwise
  // re-run react-markdown, which hands `<code>` a fresh `dangerouslySetInnerHTML`
  // object each time; React then rewrites the highlighted innerHTML even though
  // the HTML string is byte-identical, tearing down and rebuilding every hljs
  // <span> — which collapses any active text selection inside a code block
  // (MUL-3621). A stable element reference lets React bail out of the subtree.
  const markdown = useMemo(
    () => (
      <ReactMarkdown
        remarkPlugins={[
          [remarkMath, { singleDollarTextMath: false }],
          remarkBreaks,
          [remarkGfm, { singleTilde: false }],
        ]}
        rehypePlugins={[rehypeRaw, [rehypeSanitize, sanitizeSchema], rehypeKatex]}
        urlTransform={urlTransform}
        components={components}
      >
        {processed}
      </ReactMarkdown>
    ),
    [processed, components],
  );

  return (
    <AttachmentDownloadProvider attachments={attachments}>
      <div ref={wrapperRef} className={cn("rich-text-editor readonly text-sm", className)}>
        {markdown}
        <LinkHoverCard {...hover} />
      </div>
    </AttachmentDownloadProvider>
  );
});
