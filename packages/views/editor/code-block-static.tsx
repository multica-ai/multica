"use client";

/**
 * CodeBlockStatic — read-only lowlight-highlighted code block.
 *
 * Used by:
 *   - AttachmentPreviewModal's text-kind fallback (extracted from there).
 *   - HtmlBlockPreview's "source" toggle in ReadonlyContent.
 *
 * NOT used by Tiptap's editable code-block NodeView: that path must keep
 * `<NodeViewContent as="code" />` so the user can continue typing into the
 * code block. Replacing it with a static lowlight component would freeze
 * the content and desync ProseMirror state from the DOM.
 */

import { useMemo } from "react";
import { createLowlight, common } from "lowlight";
import { toHtml } from "hast-util-to-html";
import { cn } from "@multica/ui/lib/utils";

const lowlight = createLowlight(common);

interface CodeBlockStaticProps {
  language: string | undefined;
  body: string;
  className?: string;
}

export function CodeBlockStatic({ language, body, className }: CodeBlockStaticProps) {
  const html = useMemo(() => {
    const code = body.replace(/\n$/, "");
    try {
      const tree = language
        ? lowlight.highlight(language, code)
        : lowlight.highlightAuto(code);
      return toHtml(tree) as string;
    } catch {
      // Unknown language tag — fall back to escaped plain text so we don't
      // crash on an esoteric extension.
      return escapeHtml(code);
    }
  }, [body, language]);

  return (
    <pre className={cn("rich-text-editor m-0 overflow-auto text-sm", className)}>
      <code
        className={cn("hljs", language && `language-${language}`)}
        dangerouslySetInnerHTML={{ __html: html }}
      />
    </pre>
  );
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
