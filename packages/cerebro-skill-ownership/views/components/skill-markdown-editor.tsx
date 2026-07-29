"use client";

import { useMemo, type ElementType } from "react";
import { splitSkillMarkdown } from "../../core/markdown";

export interface RichTextMarkdownEditorProps {
  defaultValue?: string;
  onUpdate?: (markdown: string) => void;
  placeholder?: string;
  className?: string;
  disableMentions?: boolean;
}

type RichTextMarkdownEditor = ElementType<RichTextMarkdownEditorProps>;

/**
 * Keeps skill YAML outside the rich-text conversion pipeline. The visible body
 * is edited with the shared WYSIWYG editor and saved back as Markdown.
 */
export function CerebroSkillMarkdownEditor({
  Editor,
  content,
  onChange,
  placeholder,
}: {
  Editor: RichTextMarkdownEditor;
  content: string;
  onChange: (content: string) => void;
  placeholder: string;
}) {
  const { frontmatter, body } = useMemo(
    () => splitSkillMarkdown(content),
    [content],
  );

  return (
    <div className="flex h-full min-h-0 flex-col">
      {frontmatter && (
        <pre className="shrink-0 overflow-x-auto border-b bg-muted/30 px-4 py-3 font-mono text-xs leading-relaxed text-muted-foreground">
          {frontmatter}
        </pre>
      )}
      <Editor
        key={frontmatter}
        defaultValue={body}
        onUpdate={(markdown) => onChange(`${frontmatter}${markdown}`)}
        placeholder={placeholder}
        className="min-h-full flex-1 px-4 py-3 text-sm"
        disableMentions
      />
    </div>
  );
}
