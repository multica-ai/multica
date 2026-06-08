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
import { toJsxRuntime } from "hast-util-to-jsx-runtime";
import { jsx, jsxs, Fragment } from "react/jsx-runtime";
import { cn } from "@multica/ui/lib/utils";
import "./styles/code.css";

const lowlight = createLowlight(common);

interface CodeBlockStaticProps {
  language: string | undefined;
  body: string;
  className?: string;
}

export function CodeBlockStatic({ language, body, className }: CodeBlockStaticProps) {
  const highlighted = useMemo(() => {
    const code = body.replace(/\n$/, "");
    try {
      const tree = language
        ? lowlight.highlight(language, code)
        : lowlight.highlightAuto(code);
      if (tree.children.length > 0) {
        return toJsxRuntime(tree, { jsx, jsxs, Fragment });
      }
    } catch {
      // Unknown language tag — fall back to plain text
    }
    return code;
  }, [body, language]);

  return (
    <pre className={cn("rich-text-editor m-0 overflow-auto text-sm select-text", className)}>
      <code className={cn("hljs", language && `language-${language}`)}>
        {highlighted}
      </code>
    </pre>
  );
}
