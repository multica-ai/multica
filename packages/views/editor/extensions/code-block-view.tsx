"use client";

import { useEffect, useState } from "react";
import { NodeViewWrapper, NodeViewContent } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { Code as CodeIcon, Copy, Check, Eye, Maximize2, Trash2 } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { MermaidDiagram, MermaidLightbox } from "../mermaid-diagram";
import { CodeBlockIframe } from "../code-block-iframe";

// Coalesces fast keystrokes before re-rendering live previews.
// `mermaid.initialize()` mutates a process-global config, so back-to-back
// renders during typing can race a concurrent ReadonlyContent render
// (e.g. a comment card) and clobber its theme variables. 200ms keeps the
// "live preview" feel while making concurrent inits unlikely in practice.
// HTML preview reuses the same debounce: re-keying iframe.srcDoc on every
// keystroke causes the iframe to re-load and flicker.
const PREVIEW_DEBOUNCE_MS = 200;

const HTML_PREVIEW_HEIGHT = "h-[480px]";

function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(id);
  }, [value, delayMs]);
  return debounced;
}

function CodeBlockView({ node, deleteNode, editor }: NodeViewProps) {
  const { t } = useT("editor");
  const [copied, setCopied] = useState(false);
  // HTML and Mermaid blocks default to "preview"; the user can flip to
  // "source" to edit the markup directly. Note: the source `<pre>` MUST stay
  // mounted (just hidden) so ProseMirror keeps its NodeView bindings —
  // unmounting it would break editing.
  const [view, setView] = useState<"preview" | "source">("preview");
  const language = node.attrs.language || "";
  const isMermaid = language === "mermaid";
  const isHtml = language === "html";
  const chart = node.textContent;
  const debouncedChart = useDebouncedValue(
    isMermaid ? chart : "",
    PREVIEW_DEBOUNCE_MS,
  );
  const debouncedHtml = useDebouncedValue(
    isHtml ? chart : "",
    PREVIEW_DEBOUNCE_MS,
  );

  const handleCopy = async () => {
    const text = node.textContent;
    if (!text) return;
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const hasPreview = isHtml || isMermaid;
  const showPreview = hasPreview && view === "preview";
  const toggleView = () =>
    setView((v) => (v === "preview" ? "source" : "preview"));

  const [mermaidExpandedDoc, setMermaidExpandedDoc] = useState<string | null>(null);
  const [lightboxOpen, setLightboxOpen] = useState(false);

  return (
    <NodeViewWrapper className="code-block-wrapper group/code relative my-2 select-text">
      {isMermaid && showPreview && debouncedChart.trim() && (
        <div
          contentEditable={false}
          className="mermaid-diagram-preview mb-1"
        >
          <MermaidDiagram chart={debouncedChart} bare onExpandedDocReady={setMermaidExpandedDoc} />
        </div>
      )}
      {isHtml && showPreview && (
        <div contentEditable={false} className="mb-1">
          <CodeBlockIframe
            html={debouncedHtml}
            title="HTML preview"
            heightClassName={HTML_PREVIEW_HEIGHT}
          />
        </div>
      )}
      <div
        contentEditable={false}
        className="code-block-header pointer-events-none absolute top-0 right-0 z-10 flex select-none items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100"
      >
        {language && (
          <span className="text-xs text-muted-foreground select-none">
            {language}
          </span>
        )}
        {hasPreview && (
          <button
            type="button"
            onMouseDown={(e) => e.preventDefault()}
            onClick={toggleView}
            className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
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
        )}
        {isMermaid && showPreview && mermaidExpandedDoc && (
          <button
            type="button"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => setLightboxOpen(true)}
            className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            title="Open fullscreen"
            aria-label="Open Mermaid diagram fullscreen"
          >
            <Maximize2 className="h-3.5 w-3.5" />
          </button>
        )}
        <button
          type="button"
          onMouseDown={(e) => e.preventDefault()}
          onClick={handleCopy}
          className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          title={t(($) => $.code_block.copy_code)}
        >
          {copied ? (
            <Check className="h-3.5 w-3.5" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </button>
        {editor?.isEditable && (
          <button
            type="button"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => deleteNode()}
            className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
            title={t(($) => $.code_block.delete)}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
      {/* `<pre>` + NodeViewContent must remain mounted so the user can keep
          editing the code block contents. When a preview is showing
          we just visually hide it — ProseMirror still tracks it. */}
      <pre
        spellCheck={false}
        className={cn(showPreview && "sr-only")}
        aria-hidden={showPreview ? "true" : undefined}
      >
        {/* @ts-expect-error -- NodeViewContent supports as="code" at runtime */}
        <NodeViewContent as="code" />
      </pre>
      {lightboxOpen && mermaidExpandedDoc && (
        <MermaidLightbox
          srcDoc={mermaidExpandedDoc}
          onClose={() => setLightboxOpen(false)}
        />
      )}
    </NodeViewWrapper>
  );
}

export { CodeBlockView };
