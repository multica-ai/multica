"use client";

/**
 * DetailsBlock — Tiptap node extension for collapsible `<details>`/`<summary>`
 * blocks (the GitHub-flavored disclosure widget autopilots emit).
 *
 * Why this exists
 * ---------------
 * Autopilot descriptions and issue *comments* render through `ReadonlyContent`
 * (react-markdown + rehype-raw + rehype-sanitize), whose sanitize schema already
 * allows `<details>`/`<summary>`, so those collapse correctly. The issue
 * *description*, however, renders through the Tiptap `ContentEditor`. ProseMirror
 * had no node for `<details>`, so `@tiptap/markdown` dropped the tags — both on
 * load (the block flattened into its raw text) AND on save (the block was
 * silently destroyed the moment anyone edited the description).
 *
 * Approach (modeled on the math / file-card atom nodes)
 * -----------------------------------------------------
 * A block-level `markdownTokenizer` captures a whole
 * `<details><summary>…</summary>…</details>` block and stores the summary and
 * the inner body markdown **verbatim** as attributes. `renderMarkdown` re-emits
 * the exact same block, so an edit elsewhere in the description round-trips this
 * block instead of losing it. The NodeView renders the body collapsibly through
 * the SAME `ReadonlyContent` renderer the autopilot view uses, so a collapsed
 * block on an issue looks identical to the autopilot view.
 *
 * Editing: the node stays an `atom`, but in an editable editor the NodeView
 * keeps the native `<details>` chrome and puts form controls inside it — an
 * inline `<input>` in the `<summary>` and a source-edit `<textarea>` in the
 * expanded region — that write straight back to the `summary`/`body`
 * attributes via `updateAttributes`. Because the attributes remain the
 * verbatim source of truth, an edit still round-trips to `<details>` markdown
 * unchanged. Keeping the body as a markdown source field (rather than
 * promoting it to real ProseMirror content) is deliberate: it preserves that
 * verbatim round-trip for arbitrary bodies (code fences, lists), which real
 * ProseMirror content could not guarantee.
 *
 * The collapse state is local UI state initialized from the `open` attribute:
 * the block renders collapsed by default (expanded only when authored as
 * `<details open>`), and folding/unfolding never writes back to the document,
 * so merely peeking inside a block does not dirty the description.
 *
 * Known limitation: a `<details>` nested inside another `<details>` is not parsed
 * as collapsible — the tokenizer declines it (the inner block's `</details>`
 * would close the outer one), so it falls back to the previous flattened
 * rendering rather than breaking the round-trip.
 */

import { useState } from "react";
import { ReactNodeViewRenderer, NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { ReadonlyContent } from "../readonly-content";
import { useT } from "../../i18n";
import { DetailsBlockSchema } from "../schema/details";

// ---------------------------------------------------------------------------
// React NodeView. Both modes render a native <details> so the block stays
// collapsible. When the editor is read-only the summary is plain text and the
// body renders through the shared ReadonlyContent markdown renderer. When the
// editor is editable the summary becomes an inline <input> and the body a
// source <textarea> that write edits back to the node attributes; the fold
// itself is local UI state so toggling it never dirties the doc.
// ---------------------------------------------------------------------------

export function DetailsBlockView({ node, updateAttributes, editor }: NodeViewProps) {
  const { t } = useT("editor");
  const summary = String(node.attrs.summary ?? "");
  const body = String(node.attrs.body ?? "");
  const open = Boolean(node.attrs.open);
  const editable = editor?.isEditable ?? false;

  // Fold state is local UI state, initialized from the stored `open` attr so
  // a block authored as `<details open>` starts expanded. Toggling must NOT
  // write `open` back through updateAttributes — merely folding/unfolding the
  // block would otherwise mark the description as modified.
  const [expanded, setExpanded] = useState(open);

  if (!editable) {
    return (
      <NodeViewWrapper
        as="div"
        className="details-block-node"
        data-type="detailsBlock"
      >
        <details className="details-block" open={open} contentEditable={false}>
          <summary className="details-block-summary">
            {summary || t(($) => $.details_block.summary_default)}
          </summary>
          <div className="details-block-body">
            <ReadonlyContent content={body} />
          </div>
        </details>
      </NodeViewWrapper>
    );
  }

  // Keep keystrokes and pointer gestures inside the form controls so ProseMirror
  // keymaps (Backspace deleting the node, Enter splitting, etc.) don't fire while
  // the user is typing in a field. The controls live in a contentEditable=false
  // shell, matching the other atom node views.
  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();

  return (
    <NodeViewWrapper
      as="div"
      className="details-block-node"
      data-type="detailsBlock"
    >
      <details
        className="details-block details-block--editing"
        open={expanded}
        onToggle={(e) => setExpanded(e.currentTarget.open)}
        contentEditable={false}
      >
        <summary className="details-block-summary">
          <input
            className="details-block-summary-input"
            type="text"
            value={summary}
            placeholder={t(($) => $.details_block.summary_default)}
            aria-label={t(($) => $.details_block.summary_aria_label)}
            onChange={(e) => updateAttributes({ summary: e.target.value })}
            onKeyDown={stop}
            onMouseDown={stop}
            // Clicking anywhere in a <summary> toggles the <details>; swallow
            // the click's default action so focusing the field doesn't fold
            // the block. (Focus itself happens on mousedown, unaffected.)
            onClick={(e) => e.preventDefault()}
          />
        </summary>
        <textarea
          className="details-block-body-input"
          value={body}
          placeholder={t(($) => $.details_block.body_placeholder)}
          aria-label={t(($) => $.details_block.body_aria_label)}
          rows={Math.max(3, body.split("\n").length)}
          onChange={(e) => updateAttributes({ body: e.target.value })}
          onKeyDown={stop}
          onMouseDown={stop}
        />
      </details>
    </NodeViewWrapper>
  );
}

// ---------------------------------------------------------------------------
// Tiptap Node Extension
// ---------------------------------------------------------------------------

export const DetailsBlockExtension = DetailsBlockSchema.extend({
  addNodeView() {
    return ReactNodeViewRenderer(DetailsBlockView);
  },
});
