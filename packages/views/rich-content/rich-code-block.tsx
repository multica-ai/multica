"use client";

/**
 * RichCodeBlock — the ONLY fenced-code dispatcher in the product (MUL-4922).
 *
 * Every product surface (Chat, Issue description, Comment) reaches fenced code
 * through this file. Adding a language branch anywhere else — a per-surface
 * `if (lang === "…")` in a message list or a comment card — is exactly the
 * drift this sweep deleted, so new languages get added HERE and nowhere else.
 *
 * Dispatch is on a whole language token, never a substring: `language-htmlbars`
 * and `language-mermaidx` are ordinary code, not an HTML preview / diagram.
 *
 * Upgrading to a rich block additionally requires the fence to be CLOSED (see
 * streaming-fence.ts). A half-streamed fence renders as plain source, so
 * Mermaid never parses a partial diagram and no iframe is created for HTML that
 * is still arriving.
 *
 * Leaf components (MermaidDiagram / HtmlBlockPreview / lowlight static code)
 * are surface-agnostic and shared with the Tiptap editor's NodeViews. They are
 * imported by direct path — never through the `editor` barrel — so this module
 * does not pull the editor's Tiptap graph into Chat.
 */

import { memo, useMemo, useState, type ReactNode } from "react";
import { toHtml } from "hast-util-to-html";
import { Check, Copy } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../i18n";
import { MermaidDiagram } from "../editor/mermaid-diagram";
import { HtmlBlockPreview } from "../editor/html-block-preview";
import { highlightCode } from "../editor/syntax-highlight";

/**
 * Languages that may become a rich block. Anything else — including unknown
 * and absent languages — renders as static highlighted code.
 */
export type RichFenceLanguage = "mermaid" | "html";

export function isRichFenceLanguage(
  language: string | undefined,
): language is RichFenceLanguage {
  return language === "mermaid" || language === "html";
}

/**
 * Whether a fenced block should render as a rich block rather than source.
 * Both conditions are required: a rich-capable language AND a closed fence.
 */
export function shouldUpgradeFence(
  language: string | undefined,
  isFenceClosed: boolean,
): boolean {
  return isRichFenceLanguage(language) && isFenceClosed;
}

// Memoized on source so appending text elsewhere in a streaming message does
// not re-run Mermaid's async render or reload an already-mounted iframe. React
// reconciliation keeps the instance mounted (same element type, same position);
// memo additionally keeps it from re-rendering.
const MemoMermaidDiagram = memo(MermaidDiagram);
const MemoHtmlBlockPreview = memo(HtmlBlockPreview);

/**
 * Static lowlight-highlighted `<code>`, matching the editable Tiptap code
 * block's engine and `.hljs-*` CSS so a fence looks identical in every surface.
 */
export function StaticCodeBody({
  language,
  body,
  className,
}: {
  language: string | undefined;
  body: string;
  className?: string;
}) {
  const html = useMemo(() => {
    const code = body.replace(/\n$/, "");
    try {
      const tree = highlightCode(code, language);
      return toHtml(tree) as string;
    } catch {
      return null;
    }
  }, [body, language]);

  if (html == null) {
    // Highlighter failure must not blank the code — render it unhighlighted.
    return (
      <code className={cn("hljs", className)}>{body.replace(/\n$/, "")}</code>
    );
  }

  return (
    <code
      className={cn("hljs", language && `language-${language}`, className)}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}

/**
 * `<pre>` shell with hover copy chrome, matching the editable code block's
 * header (language label + copy) in code-block-view.tsx.
 */
export function CodeBlockShell({
  language,
  code,
  children,
}: {
  language?: string;
  code: string;
  children: ReactNode;
}) {
  const { t } = useT("editor");
  const [copied, setCopied] = useState(false);
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

/**
 * The rich leaf for an upgraded fence. Only reached when shouldUpgradeFence()
 * returned true, so the fence is known-closed here.
 */
export function RichFenceBlock({
  language,
  body,
}: {
  language: RichFenceLanguage;
  body: string;
}) {
  if (language === "mermaid") {
    return <MemoMermaidDiagram chart={body} />;
  }
  return <MemoHtmlBlockPreview html={body} />;
}
