"use client";

import { useMemo } from "react";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { parseFrontmatter } from "@multica/core/skills/frontmatter";
import { RichContent } from "../../rich-content";
import { useT } from "../../i18n";

/**
 * How the body of the selected file is rendered. Owned by the page, not by
 * this component: the mode is a property of how the user wants to work, not
 * of the file they happen to have open, so switching files must not silently
 * bounce them back to preview.
 */
export type FileMode = "preview" | "raw";

export function isMarkdownPath(path: string) {
  return path.endsWith(".md") || path.endsWith(".mdx");
}

/**
 * Body of a single skill file.
 *
 * Frontmatter is stripped from the preview because `name` / `description` are
 * surfaced as first-class properties on the Overview tab — rendering them
 * again here was the third copy of the same two fields.
 */
export function FileViewer({
  path,
  content,
  mode,
  readOnly,
  onChange,
}: {
  path: string;
  content: string;
  mode: FileMode;
  readOnly: boolean;
  onChange: (content: string) => void;
}) {
  const { t } = useT("skills");
  const isMd = isMarkdownPath(path);

  const body = useMemo(
    () => (isMd ? parseFrontmatter(content).body : content),
    [content, isMd],
  );

  // Non-markdown files have nothing to preview; they are always raw.
  if (isMd && mode === "preview") {
    return (
      <div className="h-full overflow-y-auto">
        {/* pb-24: keeps the last lines readable under the floating save pill. */}
        <div className="mx-auto max-w-[68ch] px-6 pb-24 pt-7 sm:px-8">
          <RichContent
            content={body || t(($) => $.file_viewer.no_content)}
            density="document"
            phase="settled"
          />
        </div>
      </div>
    );
  }

  return (
    <Textarea
      value={content}
      readOnly={readOnly}
      onChange={(e) => onChange(e.target.value)}
      aria-label={t(($) => $.file_viewer.raw_aria, { path })}
      placeholder={
        isMd
          ? t(($) => $.file_viewer.markdown_placeholder)
          : t(($) => $.file_viewer.raw_placeholder)
      }
      className="h-full min-h-full resize-none rounded-none border-0 px-6 pb-24 pt-5 font-mono text-body leading-relaxed read-only:cursor-default focus-visible:ring-0 sm:px-8"
    />
  );
}
