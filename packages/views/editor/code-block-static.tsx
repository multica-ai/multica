"use client";

/**
 * CodeBlockStatic — read-only lowlight-highlighted code block.
 *
 * Used by:
 *   - AttachmentPreviewModal's text kinds, with numbered lines and a wrap
 *     toggle.
 *   - HtmlBlockPreview's "source" toggle in ReadonlyContent.
 *
 * NOT used by Tiptap's editable code-block NodeView: that path must keep
 * `<NodeViewContent as="code" />` so the user can continue typing into the
 * code block. Replacing it with a static lowlight component would freeze
 * the content and desync ProseMirror state from the DOM.
 */

import { useMemo, type CSSProperties } from "react";
import { toHtml } from "hast-util-to-html";
import { cn } from "@multica/ui/lib/utils";
import { highlightCode } from "./syntax-highlight";
import { highlightToLines } from "../common/task-transcript/diff-highlight";
import "./styles/code.css";

interface CodeBlockStaticProps {
  language: string | undefined;
  body: string;
  className?: string;
  /** Numbers every line in a gutter that stays put while the code scrolls. */
  lineNumbers?: boolean;
  /**
   * With `lineNumbers`: wrap long lines under their own number instead of
   * scrolling sideways. Plain blocks always wrap.
   */
  wrap?: boolean;
}

export function CodeBlockStatic({
  language,
  body,
  className,
  lineNumbers = false,
  wrap = false,
}: CodeBlockStaticProps) {
  const { html, lineCount } = useMemo(() => {
    const code = body.replace(/\n$/, "");
    if (lineNumbers) {
      // Highlighted as one block, then split — a multi-line comment or
      // string keeps its colour on every line it spans.
      const lines =
        highlightToLines(code, language) ?? code.split("\n").map(escapeHtml);
      return { html: numberedLinesHtml(lines), lineCount: lines.length };
    }
    try {
      const tree = highlightCode(code, language);
      return { html: toHtml(tree) as string, lineCount: 0 };
    } catch {
      // Keep an unexpected highlighter failure from breaking the preview.
      return { html: escapeHtml(code), lineCount: 0 };
    }
  }, [body, language, lineNumbers]);

  return (
    <pre
      className={cn(
        "rich-text-editor m-0 overflow-auto text-body",
        lineNumbers && "code-lines",
        lineNumbers && wrap && "code-lines-wrap",
        className,
      )}
      style={
        lineNumbers
          ? ({ "--line-digits": String(lineCount).length } as CSSProperties)
          : undefined
      }
    >
      <code
        className={cn("hljs", language && `language-${language}`)}
        dangerouslySetInnerHTML={{ __html: html }}
      />
    </pre>
  );
}

// One element per line, numbered through a data attribute: the number is a
// pseudo-element, so selecting and copying the code never picks it up. Built
// as one string and set once — a log file can run to tens of thousands of
// lines, far too many to hand React as elements.
function numberedLinesHtml(lines: string[]): string {
  let html = "";
  for (let i = 0; i < lines.length; i++) {
    html += `<span class="code-line" data-line="${i + 1}"><span class="code-text">${lines[i]}</span></span>`;
  }
  return html;
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
