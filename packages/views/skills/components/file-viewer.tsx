"use client";

import { useEffect, useRef } from "react";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { splitFrontmatter } from "@multica/core/skills/frontmatter";
import { RichContent } from "../../rich-content";
import { useT } from "../../i18n";

export function isMarkdownPath(path: string) {
  return path.endsWith(".md") || path.endsWith(".mdx");
}

/**
 * Body of a single skill file, in the same centered column as a knowledge
 * base doc.
 *
 * Editors always get the source, never a rich editor: agents read these files
 * byte for byte, and a WYSIWYG round trip rewrites what it cannot model —
 * `<example>` prompt tags come back escaped, `{{var_name}}` gains a backslash,
 * list markers and tables are reformatted. Read-only viewers get Markdown
 * rendered, without the frontmatter the side panel already shows.
 */
export function FileViewer({
  path,
  content,
  readOnly,
  autoFocus,
  onChange,
  onFocusHandled,
}: {
  path: string;
  content: string;
  readOnly: boolean;
  /** Put the caret in this file's editor once it renders (a just-added file). */
  autoFocus?: boolean;
  onChange: (content: string) => void;
  /** Called once the caret has landed, so the request is not replayed. */
  onFocusHandled?: () => void;
}) {
  const { t } = useT("skills");
  const isMd = isMarkdownPath(path);
  const ref = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (autoFocus !== true || !ref.current) return;
    ref.current.focus();
    // Caret at the top: jumping to the end of a long file would scroll away
    // the content the user just opened.
    ref.current.setSelectionRange(0, 0);
    onFocusHandled?.();
  }, [autoFocus, onFocusHandled]);

  return (
    <div className="h-full overflow-y-auto">
      {/* pb-24 keeps the last lines readable under the floating save pill. */}
      <div className="mx-auto w-full max-w-3xl px-6 pt-8 pb-24 sm:px-10">
        {readOnly && isMd ? (
          <div className="prose-document">
            <RichContent
              content={splitFrontmatter(content).body || t(($) => $.file_viewer.no_content)}
              density="document"
              phase="settled"
            />
          </div>
        ) : (
          <Textarea
            ref={ref}
            value={content}
            readOnly={readOnly}
            spellCheck={false}
            onChange={(e) => onChange(e.target.value)}
            aria-label={t(($) => $.file_viewer.raw_aria, { path })}
            placeholder={
              isMd
                ? t(($) => $.file_viewer.markdown_placeholder)
                : t(($) => $.file_viewer.raw_placeholder)
            }
            className="min-h-[50vh] resize-none rounded-none border-0 bg-transparent p-0 font-mono text-body leading-relaxed shadow-none read-only:cursor-default focus-visible:ring-0 md:text-body dark:bg-transparent"
          />
        )}
      </div>
    </div>
  );
}
