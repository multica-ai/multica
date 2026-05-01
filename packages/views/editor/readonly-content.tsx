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

import { isValidElement, memo, useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
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
import { Maximize2, Download, Link as LinkIcon, FileText } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths, useWorkspaceSlug } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { IssueMentionCard } from "../issues/components/issue-mention-card";
import { ImageLightbox } from "./extensions/image-view";
import { useLinkHover, LinkHoverCard } from "./link-hover-card";
import { openLink, isMentionHref } from "./utils/link-handler";
import { preprocessMarkdown } from "./utils/preprocess";
import "katex/dist/katex.min.css";
import "./content-editor.css";

// ---------------------------------------------------------------------------
// Lowlight — same engine + language set as Tiptap's CodeBlockLowlight
// ---------------------------------------------------------------------------

const lowlight = createLowlight(common);

type MermaidAPI = typeof import("mermaid").default;

type MermaidLayout = {
  width?: number;
  height?: number;
};

let mermaidPromise: Promise<MermaidAPI> | null = null;

function getMermaid(): Promise<MermaidAPI> {
  mermaidPromise ??= import("mermaid").then(({ default: mermaid }) => mermaid);

  return mermaidPromise;
}

function toLegacyColor(color: string, fallback: string, ownerDocument: Document): string {
  const canvas = ownerDocument.createElement("canvas");
  canvas.width = 1;
  canvas.height = 1;
  const context = canvas.getContext("2d", { willReadFrequently: true });
  if (!context) return fallback;

  // Mermaid's color parser only supports legacy color syntax. Canvas can parse
  // modern CSS Color 4 values such as oklch(), then getImageData gives concrete
  // 8-bit sRGB bytes that Mermaid can consume safely.
  context.fillStyle = "#000";
  context.fillStyle = color || fallback;
  context.fillRect(0, 0, 1, 1);
  const [red, green, blue] = context.getImageData(0, 0, 1, 1).data;

  return `rgb(${red}, ${green}, ${blue})`;
}

function resolveCssColor(
  host: HTMLElement,
  variableName: string,
  fallback: string,
): string {
  const probe = host.ownerDocument.createElement("span");
  probe.style.color = `var(${variableName})`;
  probe.style.display = "none";
  host.appendChild(probe);
  const color = getComputedStyle(probe).color;
  probe.remove();

  return toLegacyColor(color || fallback, fallback, host.ownerDocument);
}

function getMermaidThemeVariables(host: HTMLElement | null) {
  if (!host) {
    return {
      primaryColor: "rgb(245, 245, 245)",
      primaryBorderColor: "rgb(59, 130, 246)",
      primaryTextColor: "rgb(17, 24, 39)",
      lineColor: "rgb(107, 114, 128)",
      fontFamily: "inherit",
    };
  }

  return {
    primaryColor: resolveCssColor(host, "--muted", "rgb(245, 245, 245)"),
    primaryBorderColor: resolveCssColor(host, "--primary", "rgb(59, 130, 246)"),
    primaryTextColor: resolveCssColor(host, "--foreground", "rgb(17, 24, 39)"),
    lineColor: resolveCssColor(host, "--muted-foreground", "rgb(107, 114, 128)"),
    fontFamily: "inherit",
  };
}

function getSandboxCssVariables(host: HTMLElement | null): string {
  const styles = host ? getComputedStyle(host) : null;
  return ["--muted", "--primary", "--foreground", "--muted-foreground"]
    .map((name) => `${name}: ${styles?.getPropertyValue(name).trim() || "initial"};`)
    .join(" ");
}

function getMermaidLayout(svg: string): MermaidLayout {
  const viewBoxMatch = svg.match(
    /viewBox=["']\s*([\d.-]+)\s+([\d.-]+)\s+([\d.-]+)\s+([\d.-]+)\s*["']/i,
  );
  const [, , , widthValue, heightValue] = viewBoxMatch ?? [];
  const width = widthValue ? Number.parseFloat(widthValue) : undefined;
  const height = heightValue ? Number.parseFloat(heightValue) : undefined;

  if (width && height && width > 0 && height > 0) {
    return {
      width: Math.ceil(width),
      height: Math.ceil(height),
    };
  }

  return {};
}

function buildSandboxedMermaidDocument(svg: string, host: HTMLElement | null): string {
  const cssVariables = getSandboxCssVariables(host);

  return `<!doctype html><html><head><style>:root { ${cssVariables} } body { margin: 0; display: flex; justify-content: center; background: transparent; } svg { max-width: 100%; height: auto; }</style></head><body>${svg}</body></html>`;
}

function buildExpandedMermaidDocument(svg: string, host: HTMLElement | null): string {
  const cssVariables = getSandboxCssVariables(host);

  return `<!doctype html><html><head><style>:root { ${cssVariables} } html, body { width: 100%; height: 100%; } body { margin: 0; display: flex; align-items: center; justify-content: center; background: transparent; } svg { max-width: 100%; max-height: 100%; width: auto; height: auto; }</style></head><body>${svg}</body></html>`;
}

function useThemeVersion() {
  const [themeVersion, setThemeVersion] = useState(0);

  useEffect(() => {
    const bumpThemeVersion = () => setThemeVersion((version) => version + 1);
    const observer = new MutationObserver(bumpThemeVersion);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "style", "data-theme"],
    });
    if (document.body) {
      observer.observe(document.body, {
        attributes: true,
        attributeFilter: ["class", "style", "data-theme"],
      });
    }

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    mediaQuery.addEventListener("change", bumpThemeVersion);

    return () => {
      observer.disconnect();
      mediaQuery.removeEventListener("change", bumpThemeVersion);
    };
  }, []);

  return themeVersion;
}

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

function MermaidLightbox({
  srcDoc,
  onClose,
}: {
  srcDoc: string;
  onClose: () => void;
}) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [onClose]);

  return createPortal(
    <div
      className="mermaid-diagram-lightbox"
      role="dialog"
      aria-modal="true"
      aria-label="Mermaid diagram fullscreen view"
      onClick={onClose}
    >
      <iframe
        className="mermaid-diagram-lightbox-frame"
        sandbox=""
        srcDoc={srcDoc}
        title="Mermaid diagram fullscreen"
        onClick={(e) => e.stopPropagation()}
      />
    </div>,
    document.body,
  );
}

function MermaidDiagram({ chart }: { chart: string }) {
  const reactId = useId();
  const containerRef = useRef<HTMLDivElement>(null);
  const diagramId = useMemo(
    () => `mermaid-${reactId.replace(/[^a-zA-Z0-9_-]/g, "")}`,
    [reactId],
  );
  const themeVersion = useThemeVersion();
  const [sandboxedDocument, setSandboxedDocument] = useState<string | null>(null);
  const [expandedDocument, setExpandedDocument] = useState<string | null>(null);
  const [layout, setLayout] = useState<MermaidLayout>({});
  const [error, setError] = useState<string | null>(null);
  const [lightboxOpen, setLightboxOpen] = useState(false);

  useEffect(() => {
    let cancelled = false;

    async function renderDiagram() {
      try {
        setError(null);
        setSandboxedDocument(null);
        setExpandedDocument(null);
        setLayout({});
        const mermaid = await getMermaid();
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          theme: "base",
          themeVariables: getMermaidThemeVariables(containerRef.current),
        });
        const { svg: renderedSvg } = await mermaid.render(diagramId, chart);
        if (!cancelled) {
          setLayout(getMermaidLayout(renderedSvg));
          setSandboxedDocument(
            buildSandboxedMermaidDocument(renderedSvg, containerRef.current),
          );
          setExpandedDocument(
            buildExpandedMermaidDocument(renderedSvg, containerRef.current),
          );
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to render Mermaid diagram");
        }
      }
    }

    void renderDiagram();

    return () => {
      cancelled = true;
    };
  }, [chart, diagramId, themeVersion]);

  if (error) {
    return (
      <div ref={containerRef} className="mermaid-diagram mermaid-diagram-error">
        <p>Unable to render Mermaid diagram.</p>
        <pre>
          <code>{chart}</code>
        </pre>
      </div>
    );
  }

  return (
    <div ref={containerRef} className="mermaid-diagram" aria-label="Mermaid diagram">
      {sandboxedDocument ? (
        <>
          <iframe
            className="mermaid-diagram-frame"
            sandbox=""
            srcDoc={sandboxedDocument}
            style={{
              height: layout.height ? `${layout.height}px` : undefined,
              width: layout.width ? `${layout.width}px` : undefined,
            }}
            title="Mermaid diagram"
          />
          <div className="mermaid-diagram-toolbar">
            <button
              type="button"
              onClick={() => setLightboxOpen(true)}
              title="Open fullscreen"
              aria-label="Open Mermaid diagram fullscreen"
            >
              <Maximize2 className="size-3.5" />
            </button>
          </div>
          {lightboxOpen && expandedDocument && (
            <MermaidLightbox
              srcDoc={expandedDocument}
              onClose={() => setLightboxOpen(false)}
            />
          )}
        </>
      ) : (
        <div className="mermaid-diagram-loading">Rendering diagram…</div>
      )}
    </div>
  );
}

const components: Partial<Components> = {
  // Links — route mention:// to mention components, others show preview card
  a: ReadonlyLink,

  // Images — centered with toolbar + lightbox (matches Tiptap ImageView NodeView)
  img: function ReadonlyImage({ src, alt }) {
    const [lightbox, setLightbox] = useState(false);
    const imgSrc = typeof src === "string" ? src : "";
    const imgAlt = alt ?? "";

    const handleView = () => setLightbox(true);
    const handleDownload = () => {
      window.open(imgSrc, "_blank", "noopener,noreferrer");
    };
    const handleCopyLink = async () => {
      try {
        await navigator.clipboard.writeText(imgSrc);
        toast.success("Link copied");
      } catch {
        toast.error("Failed to copy link");
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
            <button type="button" onClick={handleView} title="View image">
              <Maximize2 className="size-3.5" />
            </button>
            <button type="button" onClick={handleDownload} title="Download">
              <Download className="size-3.5" />
            </button>
            <button type="button" onClick={handleCopyLink} title="Copy link">
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
  div: ({ node, children, ...props }) => {
    const dataType = node?.properties?.dataType as string | undefined;
    if (dataType === "fileCard") {
      const rawHref = (node?.properties?.dataHref as string) || "";
      // Only allow http(s) URLs to prevent javascript: and other dangerous schemes.
      const href = /^https?:\/\//i.test(rawHref) ? rawHref : "";
      const filename = (node?.properties?.dataFilename as string) || "";
      return (
        <div className="my-1 flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted">
          <FileText className="size-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm">{filename}</p>
          </div>
          {href && (
            <button
              type="button"
              className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              onClick={() => window.open(href, "_blank", "noopener,noreferrer")}
            >
              <Download className="size-3.5" />
            </button>
          )}
        </div>
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

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface ReadonlyContentProps {
  content: string;
  className?: string;
}

export const ReadonlyContent = memo(function ReadonlyContent({ content, className }: ReadonlyContentProps) {
  const processed = useMemo(() => preprocessMarkdown(content), [content]);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const hover = useLinkHover(wrapperRef);

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
    </div>
  );
});
