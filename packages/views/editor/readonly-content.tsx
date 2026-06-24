"use client";

/**
 * ReadonlyContent — thin wrapper around the views-level Markdown component
 * in editor-parity mode.
 *
 * Replaces the former standalone react-markdown renderer (which duplicated
 * lowlight, sanitize, and component logic from @multica/ui/markdown). All
 * rendering now flows through the unified Markdown → MarkdownBase pipeline,
 * with editor-parity mode providing lowlight code highlighting, CodeBlockHeader,
 * ==mark== syntax support, and callback-driven Mermaid/HTML/link-hover injection.
 *
 * Visual parity with ContentEditor is preserved by:
 * - Using editor-parity mode (lowlight + .rich-text-editor.readonly CSS scope)
 * - Using the same preprocessMarkdown + highlightToHtml pipeline
 * - Rendering mentions with the same IssueMentionCard / ProjectChip components
 *   (injected by the views-level Markdown wrapper)
 */

import { memo } from "react";
import { Markdown as ViewsMarkdown } from "../common/markdown";
import type { Attachment } from "@multica/core/types";

interface ReadonlyContentProps {
  content: string;
  className?: string;
  /**
   * Attachments associated with the surrounding entity (comment / issue
   * body). When the markdown contains an inline `<img>` or file card whose
   * URL matches one of these attachments, the download button re-signs the
   * URL at click time via `useDownloadAttachment` instead of opening the
   * potentially stale link embedded in the markdown.
   *
   * Callers SHOULD pass a stable reference (e.g. the field on a memoized
   * timeline entry); a fresh array on every parent render busts the memo.
   */
  attachments?: Attachment[];
}

// Memoized so a long timeline of comments (Inbox + IssueDetail) does not
// re-run the full react-markdown + rehype-* + lowlight pipeline on every
// parent re-render. Props are `content`/`className`/`attachments`, all
// shallow-comparable; stability is the caller's responsibility for the
// array.
export const ReadonlyContent = memo(function ReadonlyContent({
  content,
  className,
  attachments,
}: ReadonlyContentProps) {
  return (
    <ViewsMarkdown
      mode="editor-parity"
      attachments={attachments}
      className={className}
    >
      {content}
    </ViewsMarkdown>
  );
});
