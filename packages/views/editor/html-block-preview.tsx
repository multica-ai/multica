"use client";

/**
 * HtmlBlockPreview — readonly rendering of fenced ```html code blocks.
 *
 * Default view is "preview" (iframe) per the V2 plan; user can flip to
 * "source" to see the highlighted markup and Copy it.
 *
 * Mounted by ReadonlyContent's `code` renderer for `lang === "html"`. The
 * `pre` renderer in ReadonlyContent recognizes this component by reference
 * and unwraps it from the default `<pre>` envelope, matching the same
 * two-layer trick already used for MermaidDiagram.
 *
 * NOT used in the editable Tiptap NodeView — that path must keep
 * `<NodeViewContent as="code" />` so the user can continue typing.
 */

import { useState } from "react";
import { Check, Code as CodeIcon, Copy, Eye } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { CodeBlockIframe } from "./code-block-iframe";
import { CodeBlockStatic } from "./code-block-static";

const CODE_BLOCK_IFRAME_HEIGHT = "h-[480px]";

// Label shown in the code-block header. Not a translatable string — it's a
// language identifier (matches the `lang === "html"` token below).
const HTML_LANGUAGE_LABEL = "html";

interface HtmlBlockPreviewProps {
  html: string;
  className?: string;
}

export function HtmlBlockPreview({ html, className }: HtmlBlockPreviewProps) {
  const { t } = useT("editor");
  const [view, setView] = useState<"preview" | "source">("preview");
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    if (!html) return;
    try {
      await navigator.clipboard.writeText(html);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard failures are user-recoverable (click again, or copy
      // manually from the source view) — no need for a toast here.
    }
  };

  const toggleView = () =>
    setView((v) => (v === "preview" ? "source" : "preview"));

  return (
    <div className={cn("code-block-wrapper group/code relative my-2", className)}>
      <div
        className="absolute top-0 right-0 z-10 flex items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100"
      >
        <span className="text-xs text-muted-foreground select-none">{HTML_LANGUAGE_LABEL}</span>
        <button
          type="button"
          onClick={toggleView}
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          title={
            view === "preview"
              ? t(($) => $.code_block.show_source)
              : t(($) => $.code_block.show_preview)
          }
          aria-label={
            view === "preview"
              ? t(($) => $.code_block.show_source)
              : t(($) => $.code_block.show_preview)
          }
        >
          {view === "preview" ? (
            <CodeIcon className="h-3.5 w-3.5" />
          ) : (
            <Eye className="h-3.5 w-3.5" />
          )}
        </button>
        <button
          type="button"
          onClick={handleCopy}
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          title={t(($) => $.code_block.copy_code)}
          aria-label={t(($) => $.code_block.copy_code)}
        >
          {copied ? (
            <Check className="h-3.5 w-3.5" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </button>
      </div>
      {view === "preview" ? (
        <CodeBlockIframe
          html={html}
          title="HTML preview"
          heightClassName={CODE_BLOCK_IFRAME_HEIGHT}
        />
      ) : (
        <CodeBlockStatic language="xml" body={html} />
      )}
    </div>
  );
}
