"use client";

/**
 * RichContent — the ONE product-level readonly content renderer (MUL-4922).
 *
 * Chat (user message, live assistant, persisted assistant, timeline text),
 * Issue descriptions and Comments all render through this component. There is
 * no second product Markdown renderer: `packages/ui/markdown` stays a generic
 * primitive for terminal/raw output and must not grow product concerns
 * (Mention, Attachment, Mermaid, HTML preview) again.
 *
 * Surfaces differ only in the shell they wrap around this component:
 *   - Comment / Issue: edit, reply, menu, thread chrome — outside.
 *   - Chat: timeline, thinking, tool, failure, copy chrome — outside.
 *
 * The public API is deliberately narrow. There is no `surface` prop, no
 * `renderMention` override and no custom code-renderer escape hatch: each of
 * those is a door through which a per-surface fork walks back in. Link,
 * mention and attachment behaviour is decided here, once.
 *
 *   density — CSS only. Never switches parser, plugins, components map or the
 *             semantic DOM; `compact` and `document` produce the same blocks.
 *   phase   — lifecycle only. Does NOT decide whether a fence upgrades; that is
 *             the fence's real closed state (see streaming-fence.ts), so a
 *             settled-but-malformed fence still renders as source.
 */

import { createContext, isValidElement, memo, useContext, useMemo, useRef } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import type { ReactNode } from "react";
import rehypeKatex from "rehype-katex";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeRaw from "rehype-raw";
import rehypeSanitize from "rehype-sanitize";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths, useWorkspaceSlug } from "@multica/core/paths";
import type { Attachment } from "@multica/core/types";
import {
  isAllowedFileCardHref,
  isIssueIdentifier,
  markdownSanitizeSchema,
  markdownUrlTransform,
} from "@multica/ui/markdown";
import { useNavigation } from "../navigation";
import { IssueMentionCard } from "../issues/components/issue-mention-card";
import { useResolveIssueIdentifier } from "../issues/hooks";
import { ProjectChip } from "../projects/components/project-chip";
import { useLinkHover, LinkHoverCard } from "../editor/link-hover-card";
import { openLink, isMentionHref } from "../editor/utils/link-handler";
import { preprocessMarkdown } from "../editor/utils/preprocess";
import { highlightToHtml } from "../editor/utils/highlight-markdown";
import { AttachmentDownloadProvider } from "../editor/attachment-download-context";
import { Attachment as AttachmentRenderer } from "../editor/attachment";
import { computeClosedFenceOffsets } from "./streaming-fence";
import {
  CodeBlockShell,
  RichFenceBlock,
  StaticCodeBody,
  isRichFenceLanguage,
  shouldUpgradeFence,
} from "./rich-code-block";
import "katex/dist/katex.min.css";
import "../editor/styles/index.css";
import "./rich-content.css";

export type RichContentDensity = "compact" | "document";
export type RichContentPhase = "streaming" | "settled";

// ---------------------------------------------------------------------------
// Fence gate context
// ---------------------------------------------------------------------------
//
// The closed-fence offsets travel by context rather than by rebuilding the
// components map. The map must stay referentially stable: react-markdown
// re-runs the whole subtree when `components` changes identity, which rewrites
// every highlighted <code>'s innerHTML and collapses an active text selection
// inside a code block (MUL-3621).

const ClosedFenceContext = createContext<ReadonlySet<number>>(new Set<number>());

function useIsFenceClosed(offset: number | undefined): boolean {
  const closed = useContext(ClosedFenceContext);
  // An offset-less node cannot be matched to a source fence; refusing to
  // upgrade is the safe direction (source instead of a possibly partial block).
  return offset != null && closed.has(offset);
}

// ---------------------------------------------------------------------------
// Mention / link renderers
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
 * `mention://issue/<identifier>` by the preprocessor. Resolves to a real issue
 * in the current workspace; renders a navigable mention on a hit, plain text on
 * a miss / while loading / cross-workspace.
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
      <ProjectChip
        projectId={projectId}
        fallbackLabel={label}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </span>
  );
}

function childrenToLabel(children: ReactNode): string | undefined {
  if (typeof children === "string") return children;
  if (Array.isArray(children)) return children.join("");
  return undefined;
}

function RichLink({ href, children }: { href?: string; children?: ReactNode }) {
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
      return <IssueMentionLink issueId={match[2]} label={childrenToLabel(children)} />;
    }
    if (match?.[1] === "project" && match[2]) {
      return <ProjectMentionLink projectId={match[2]} label={childrenToLabel(children)} />;
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

// ---------------------------------------------------------------------------
// Fenced code
// ---------------------------------------------------------------------------

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

/**
 * `code` renderer. Returns the rich leaf directly for an upgraded fence and
 * relies on the `pre` renderer below to unwrap it — react-markdown otherwise
 * wraps `code` children in a `<pre>` whose monospace/overflow styles clamp a
 * diagram or preview iframe.
 */
function RichCode({
  className,
  children,
  node,
  ...props
}: {
  className?: string;
  children?: ReactNode;
  node?: { position?: { start: { offset?: number; line: number }; end: { line: number } } };
} & Record<string, unknown>) {
  const language = /language-(\w+)/.exec(className || "")?.[1];
  const isBlock =
    node?.position && node.position.start.line !== node.position.end.line;
  const isFenceClosed = useIsFenceClosed(node?.position?.start.offset);

  if (isBlock && shouldUpgradeFence(language, isFenceClosed)) {
    // isRichFenceLanguage is re-checked for the type narrow; shouldUpgradeFence
    // already required it.
    if (isRichFenceLanguage(language)) {
      return <RichFenceBlock language={language} body={String(children).replace(/\n$/, "")} />;
    }
  }

  if (!isBlock && !language) {
    // Inline code — CSS handles styling via .rich-text-editor code
    return <code {...props}>{children}</code>;
  }

  return <StaticCodeBody language={language} body={String(children)} />;
}

/**
 * `pre` renderer. react-markdown calls this BEFORE invoking the `code`
 * renderer, so `children` is the unrendered `<code>` element from the AST: the
 * decision to unwrap is made from that child's own hast node (class token +
 * source offset), not by checking `children.type === MermaidDiagram`, which
 * never matches.
 */
function RichPre({ children }: { children?: ReactNode }) {
  const child = Array.isArray(children) ? children[0] : children;
  let language: string | undefined;
  let offset: number | undefined;

  if (isValidElement(child)) {
    const childProps = child.props as {
      className?: string;
      node?: { position?: { start: { offset?: number } } };
    };
    language = /(?:^|\s)language-(\w+)(?:\s|$)/.exec(childProps.className ?? "")?.[1];
    offset = childProps.node?.position?.start.offset;
  }

  const isFenceClosed = useIsFenceClosed(offset);

  // Upgraded fences escape the <pre><code> envelope entirely. An OPEN
  // mermaid/html fence deliberately falls through to the normal code shell, so
  // a half-streamed diagram reads as ordinary source with copy chrome.
  if (shouldUpgradeFence(language, isFenceClosed)) {
    return <>{children}</>;
  }

  return (
    <CodeBlockShell
      language={language}
      code={getTextContent(children).replace(/\n$/, "")}
    >
      {children}
    </CodeBlockShell>
  );
}

// The components map is module-level and static: it never depends on density,
// phase or the fence gate, so its identity is stable for the lifetime of the
// app and react-markdown can bail out of unchanged subtrees.
const COMPONENTS: Partial<Components> = {
  a: RichLink,

  // Images — unified through <Attachment>. The resolver context provided by
  // AttachmentDownloadProvider turns a CDN URL into a full record when
  // possible; external URLs render as plain images with lightbox-via-preview
  // -modal. forceKind is mandatory: markdown `![]()` carries no content-type
  // and alt is commonly empty, so without it images fall through to file-card
  // chrome.
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
      return <AttachmentRenderer attachment={{ kind: "url", url: href, filename }} />;
    }
    return <div {...props}>{children}</div>;
  },

  // Tables — wrap in tableWrapper div for border/radius/scroll (matches Tiptap)
  table: ({ children }) => (
    <div className="tableWrapper">
      <table>{children}</table>
    </div>
  ),

  code: RichCode as NonNullable<Components["code"]>,
  pre: RichPre as NonNullable<Components["pre"]>,
};

const REMARK_PLUGINS = [
  [remarkMath, { singleDollarTextMath: false }],
  remarkBreaks,
  [remarkGfm, { singleTilde: false }],
] as never;

const REHYPE_PLUGINS = [
  rehypeRaw,
  [rehypeSanitize, markdownSanitizeSchema],
  rehypeKatex,
] as never;

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export interface RichContentProps {
  content: string;
  /**
   * Attachments associated with the surrounding entity (comment / issue body /
   * chat message). Inline `<img>` and file-card URLs matching one of these get
   * their download URL re-signed at click time instead of using the possibly
   * stale link embedded in the markdown.
   *
   * Callers SHOULD pass a stable reference; a fresh array on every parent
   * render busts the memo.
   */
  attachments?: Attachment[];
  /** CSS density only — never changes the semantic DOM. */
  density?: RichContentDensity;
  /**
   * Streaming lifecycle. Does not gate fence upgrades (closedness does); it
   * exists so surfaces can express that content is still arriving.
   */
  phase?: RichContentPhase;
  className?: string;
}

export const RichContent = memo(function RichContent({
  content,
  attachments,
  density = "document",
  phase = "settled",
  className,
}: RichContentProps) {
  const processed = useMemo(
    () => highlightToHtml(preprocessMarkdown(content, { autolinkIssueIdentifiers: true })),
    [content],
  );

  // Derived from the SAME string handed to ReactMarkdown, so offsets line up
  // with the hast node positions the `code`/`pre` renderers observe. Computing
  // it from the raw pre-preprocess text would mis-match every rewritten node.
  const closedFences = useMemo(() => computeClosedFenceOffsets(processed), [processed]);

  const wrapperRef = useRef<HTMLDivElement>(null);
  const hover = useLinkHover(wrapperRef);

  // Memoize the react-markdown subtree on its only real inputs. Unrelated
  // parent re-renders (a sibling agent task streaming over WebSocket fires one
  // every ~100ms) would otherwise re-run react-markdown, which hands `<code>` a
  // fresh `dangerouslySetInnerHTML` object each time; React then rewrites the
  // highlighted innerHTML even though the string is byte-identical, tearing
  // down every hljs <span> and collapsing any active text selection inside a
  // code block (MUL-3621). A stable element reference lets React bail out.
  const markdown = useMemo(
    () => (
      <ClosedFenceContext.Provider value={closedFences}>
        <ReactMarkdown
          remarkPlugins={REMARK_PLUGINS}
          rehypePlugins={REHYPE_PLUGINS}
          urlTransform={markdownUrlTransform}
          components={COMPONENTS}
        >
          {processed}
        </ReactMarkdown>
      </ClosedFenceContext.Provider>
    ),
    [processed, closedFences],
  );

  return (
    <AttachmentDownloadProvider attachments={attachments}>
      <div
        ref={wrapperRef}
        data-rich-content=""
        data-density={density}
        data-phase={phase}
        className={cn(
          "rich-text-editor readonly text-sm",
          density === "compact" && "rich-content-compact",
          className,
        )}
      >
        {markdown}
        <LinkHoverCard {...hover} />
      </div>
    </AttachmentDownloadProvider>
  );
});
