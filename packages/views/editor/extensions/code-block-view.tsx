"use client";

import { useState } from "react";
import { NodeViewWrapper, NodeViewContent } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { Code as CodeIcon, Copy, Check, Eye, Maximize2, RotateCw } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { HtmlPreviewSurface } from "../html-preview-surface";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { useDebouncedValue } from "../../common/use-debounced-value";
import { useT } from "../../i18n";
import { MermaidDiagram } from "../mermaid-diagram";
import { CodeBlockIframe } from "../code-block-iframe";

// Coalesces fast keystrokes before re-rendering live previews.
// `mermaid.initialize()` mutates a process-global config, so back-to-back
// renders during typing can race a concurrent ReadonlyContent render
// (e.g. a comment card) and clobber its theme variables. 200ms keeps the
// "live preview" feel while making concurrent inits unlikely in practice.
// HTML preview reuses the same debounce: re-keying iframe.srcDoc on every
// keystroke causes the iframe to re-load and flicker.
const PREVIEW_DEBOUNCE_MS = 200;

const HTML_PREVIEW_HEIGHT_PX = 480;

function CodeBlockView({ node }: NodeViewProps) {
  const { t } = useT("editor");
  const [copied, setCopied] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);
  const reload = () => setReloadKey((key) => key + 1);
  // HTML blocks default to "preview"; the user can flip to "source" to
  // edit the markup directly. Note: the source `<pre>` MUST stay mounted
  // (just hidden) so ProseMirror keeps its NodeView bindings — unmounting
  // it would break editing.
  const [view, setView] = useState<"preview" | "source">("preview");
  const language = node.attrs.language || "";
  const isMermaid = language === "mermaid";
  const isHtml = language === "html";
  const showHtmlPreview = isHtml && view === "preview";
  const chart = node.textContent;
  const debouncedChart = useDebouncedValue(
    isMermaid ? chart : "",
    PREVIEW_DEBOUNCE_MS,
  );
  const debouncedHtml = useDebouncedValue(
    isHtml ? chart : "",
    PREVIEW_DEBOUNCE_MS,
    showHtmlPreview || fullscreen,
  );

  const handleCopy = async () => {
    const text = node.textContent;
    if (!text) return;
    if (await copyText(text)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const toggleView = () =>
    setView((v) => (v === "preview" ? "source" : "preview"));

  return (
    <NodeViewWrapper className="code-block-wrapper group/code relative my-3">
      {isMermaid && debouncedChart.trim() && (
        <div
          contentEditable={false}
          className="mermaid-diagram-preview mb-1"
        >
          <MermaidDiagram chart={debouncedChart} />
        </div>
      )}
      {isHtml && (
        // CSS-hidden when toggled off so the `<pre>` below stays mounted —
        // unmounting either side would either lose ProseMirror bindings
        // (source) or thrash iframe.srcDoc (preview).
        <div
          contentEditable={false}
          className={showHtmlPreview ? "mb-1" : undefined}
          hidden={!showHtmlPreview && !fullscreen}
        >
          <HtmlPreviewSurface
            open={fullscreen}
            inlineHeight={showHtmlPreview ? HTML_PREVIEW_HEIGHT_PX : 0}
            onOpenChange={setFullscreen}
            onReload={reload}
            title={t(($) => $.code_block.html_preview)}
          >
            <CodeBlockIframe
              key={reloadKey}
              html={debouncedHtml}
              title={t(($) => $.code_block.html_preview)}
              heightClassName={fullscreen ? "h-full" : "h-[480px]"}
              className={fullscreen ? "rounded-none border-0" : undefined}
            />
          </HtmlPreviewSurface>
        </div>
      )}
      <div
        contentEditable={false}
        className="code-block-header absolute top-0 right-0 z-10 flex items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100 focus-within:opacity-100"
      >
        {language && (
          <span className="text-caption text-muted-foreground select-none">
            {language}
          </span>
        )}
        {isHtml && (
          <TooltipHint
            label={
              view === "preview"
                ? t(($) => $.code_block.show_source)
                : t(($) => $.code_block.show_preview)
            }
          >
            <button
              type="button"
              onClick={toggleView}
              className="flex h-6 w-6 items-center justify-center rounded-xs text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
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
          </TooltipHint>
        )}
        {isHtml && (
          <IconButton
            type="button"
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground"
            disabled={!showHtmlPreview}
            label={t(($) => $.attachment.reload)}
            onClick={reload}
          >
            <RotateCw className="size-3.5" />
          </IconButton>
        )}
        {isHtml && (
          <IconButton
            type="button"
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground"
            label={t(($) => $.code_block.fullscreen)}
            onClick={() => setFullscreen(true)}
          >
            <Maximize2 className="size-3.5" />
          </IconButton>
        )}
        <TooltipHint label={t(($) => $.code_block.copy_code)}>
          <button
            type="button"
            onClick={handleCopy}
            className="flex h-6 w-6 items-center justify-center rounded-xs text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
            aria-label={t(($) => $.code_block.copy_code)}
          >
            {copied ? (
              <Check className="h-3.5 w-3.5" />
            ) : (
              <Copy className="h-3.5 w-3.5" />
            )}
          </button>
        </TooltipHint>
      </div>
      {/* `<pre>` + NodeViewContent must remain mounted so the user can keep
          editing the code block contents. When the HTML preview is showing
          we just visually hide it — ProseMirror still tracks it. */}
      <pre
        spellCheck={false}
        className={cn(showHtmlPreview && "sr-only")}
        aria-hidden={showHtmlPreview ? "true" : undefined}
      >
        {/* @ts-expect-error -- NodeViewContent supports as="code" at runtime */}
        <NodeViewContent as="code" />
      </pre>
    </NodeViewWrapper>
  );
}

export { CodeBlockView };
