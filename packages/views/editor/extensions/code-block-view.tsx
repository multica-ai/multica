"use client";

import { useEffect, useState } from "react";
import { NodeViewWrapper, NodeViewContent } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { Copy, Check } from "lucide-react";
import { useT } from "../../i18n";
import { MermaidDiagram } from "../mermaid-diagram";

// Coalesces fast keystrokes before re-rendering the Mermaid preview.
// `mermaid.initialize()` mutates a process-global config, so back-to-back
// renders during typing can race a concurrent ReadonlyContent render
// (e.g. a comment card) and clobber its theme variables. 200ms keeps the
// "live preview" feel while making concurrent inits unlikely in practice.
const MERMAID_PREVIEW_DEBOUNCE_MS = 200;

function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(id);
  }, [value, delayMs]);
  return debounced;
}

function CodeBlockView({ node }: NodeViewProps) {
  const { t } = useT("editor");
  const [copied, setCopied] = useState(false);
  const language = node.attrs.language || "";
  const isMermaid = language === "mermaid";
  const chart = node.textContent;
  const debouncedChart = useDebouncedValue(
    isMermaid ? chart : "",
    MERMAID_PREVIEW_DEBOUNCE_MS,
  );

  const handleCopy = async () => {
    const text = node.textContent;
    if (!text) return;
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <NodeViewWrapper className="code-block-wrapper group/code relative my-2">
      {isMermaid && debouncedChart.trim() && (
        <div
          contentEditable={false}
          className="mermaid-diagram-preview mb-1"
        >
          <MermaidDiagram chart={debouncedChart} />
        </div>
      )}
      <div
        contentEditable={false}
        className="code-block-header absolute top-0 right-0 z-10 flex items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100"
      >
        {language && (
          <span className="text-xs text-muted-foreground select-none">
            {language}
          </span>
        )}
        <button
          type="button"
          onClick={handleCopy}
          className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          title={t(($) => $.code_block.copy_code)}
        >
          {copied ? (
            <Check className="h-3.5 w-3.5" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </button>
      </div>
      <pre spellCheck={false}>
        {/* @ts-expect-error -- NodeViewContent supports as="code" at runtime */}
        <NodeViewContent as="code" />
      </pre>
    </NodeViewWrapper>
  );
}

export { CodeBlockView };
