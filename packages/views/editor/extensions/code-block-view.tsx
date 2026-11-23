"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { NodeViewWrapper, NodeViewContent } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { NodeSelection } from "@tiptap/pm/state";
import { Copy, Check } from "@multica/ui/icons";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { copyText } from "@multica/ui/lib/clipboard";
import { useDebouncedValue } from "../../common/use-debounced-value";
import { useT } from "../../i18n";
import type { DynamicBlockEditing, DynamicBlockView } from "../dynamic-block";
import { HtmlBlockPreview } from "../html-block-preview";
import { MermaidBlock } from "../mermaid-block";
import { parseFenceTitle, withFenceTitle } from "../utils/fence-title";
import { OPEN_BLOCK_SOURCE_META, isDynamicBlockLanguage } from "./code-block";

// Coalesces fast keystrokes before re-rendering live previews.
// `mermaid.initialize()` mutates a process-global config, so back-to-back
// renders during typing can race a concurrent ReadonlyContent render
// (e.g. a comment card) and clobber its theme variables. 200ms keeps the
// "live preview" feel while making concurrent inits unlikely in practice.
// The HTML preview takes the same debounce: every new document reloads its
// iframe.
const PREVIEW_DEBOUNCE_MS = 200;

function CodeBlockView(props: NodeViewProps) {
  return isDynamicBlockLanguage(props.node.attrs.language) ? (
    <DynamicCodeBlockView {...props} />
  ) : (
    <PlainCodeBlockView {...props} />
  );
}

/**
 * ```html and ```mermaid in the editor: the same dynamic block the read-only
 * renderers draw (MUL-9760) — title bar, content height, collapse past 480px —
 * with ProseMirror's editable `<pre>` as its source view and a title that
 * renames in place.
 */
function DynamicCodeBlockView({ node, editor, getPos, selected, updateAttributes }: NodeViewProps) {
  const text = node.textContent;
  // A new, empty block has nothing to preview; it opens where it is written.
  const [view, setView] = useState<DynamicBlockView>(() => (text.trim() ? "preview" : "source"));
  const debounced = useDebouncedValue(text, PREVIEW_DEBOUNCE_MS);
  const meta: string | null = node.attrs.meta ?? null;
  const title = parseFenceTitle(meta);

  // Read by editor listeners, which run outside React's render.
  const viewRef = useRef(view);
  const showView = useCallback((next: DynamicBlockView) => {
    viewRef.current = next;
    setView(next);
  }, []);

  // The source the preview hides must never hold the caret: typing there
  // would change the block unseen. A selection that lands inside it — arrow
  // keys, undo, switching back to the preview — selects the whole block
  // instead, and Enter opens the source (CodeBlockExtension).
  const containSelection = useCallback(() => {
    if (viewRef.current !== "preview" || editor.isDestroyed) return;
    const pos = getPos();
    if (typeof pos !== "number") return;
    const block = editor.state.doc.nodeAt(pos);
    const { selection } = editor.state;
    if (!block || selection instanceof NodeSelection) return;
    if (selection.from > pos && selection.to < pos + block.nodeSize) {
      editor.commands.setNodeSelection(pos);
    }
  }, [editor, getPos]);

  useEffect(() => {
    const onTransaction = ({ transaction }: { transaction: { getMeta: (key: string) => unknown } }) => {
      const pos = getPos();
      if (typeof pos === "number" && transaction.getMeta(OPEN_BLOCK_SOURCE_META) === pos) {
        showView("source");
      }
    };
    // After the dispatch that moved the selection has finished.
    const onSelectionUpdate = () => queueMicrotask(containSelection);
    editor.on("transaction", onTransaction);
    editor.on("selectionUpdate", onSelectionUpdate);
    return () => {
      editor.off("transaction", onTransaction);
      editor.off("selectionUpdate", onSelectionUpdate);
    };
  }, [editor, getPos, showView, containSelection]);

  useEffect(() => {
    if (view === "preview") containSelection();
  }, [view, containSelection]);

  const editing: DynamicBlockEditing = {
    source: (
      <pre spellCheck={false} className="!m-0 !rounded-none">
        {/* @ts-expect-error -- NodeViewContent supports as="code" at runtime */}
        <NodeViewContent as="code" />
      </pre>
    ),
    view,
    onViewChange: showView,
    onTitleChange: editor.isEditable
      ? (next) => updateAttributes({ meta: withFenceTitle(meta, next) })
      : undefined,
    selected,
  };

  return (
    // Block, against the editor's inline default for NodeView wrappers; the
    // margin matches the read-only block's shell, so the doc does not shift
    // when the live editor replaces the read-only render.
    <NodeViewWrapper className="my-5 !block" data-dynamic-code-block="">
      {node.attrs.language === "mermaid" ? (
        <MermaidBlock chart={debounced} title={title} editing={editing} />
      ) : (
        <HtmlBlockPreview html={debounced} title={title} editing={editing} />
      )}
    </NodeViewWrapper>
  );
}

function PlainCodeBlockView({ node }: NodeViewProps) {
  const { t } = useT("editor");
  const [copied, setCopied] = useState(false);
  const language = node.attrs.language || "";

  const handleCopy = async () => {
    const text = node.textContent;
    if (!text) return;
    if (await copyText(text)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <NodeViewWrapper className="code-block-wrapper group/code relative my-3">
      <div
        contentEditable={false}
        className="code-block-header absolute top-0 right-0 z-10 flex items-center gap-1.5 px-2 py-1.5 opacity-0 transition-opacity group-hover/code:opacity-100 focus-within:opacity-100"
      >
        {language && (
          <span className="text-caption text-muted-foreground select-none">
            {language}
          </span>
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
      <pre spellCheck={false}>
        {/* @ts-expect-error -- NodeViewContent supports as="code" at runtime */}
        <NodeViewContent as="code" />
      </pre>
    </NodeViewWrapper>
  );
}

export { CodeBlockView };
