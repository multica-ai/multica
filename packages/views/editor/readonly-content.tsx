"use client";

/**
 * ReadonlyContent — compatibility wrapper over the canonical <RichContent>.
 *
 * The renderer itself moved to packages/views/rich-content/ (MUL-4922) so Chat,
 * Issue descriptions and Comments all share ONE implementation of Markdown
 * parsing, sanitize, fenced-code dispatch, mentions, links and attachments.
 * This file stays only so existing document-density callers (comment cards,
 * issue detail, autopilot detail, Markdown attachment preview) keep their
 * import path and props.
 *
 * Do not reintroduce rendering logic here. New behaviour belongs in
 * RichContent, where every surface picks it up at once.
 */

import { memo } from "react";
import type { Attachment } from "@multica/core/types";
import { RichContent } from "../rich-content";

interface ReadonlyContentProps {
  content: string;
  className?: string;
  /**
   * Attachments associated with the surrounding entity (comment / issue body).
   * Callers SHOULD pass a stable reference; a fresh array on every parent
   * render busts the memo.
   */
  attachments?: Attachment[];
}

export const ReadonlyContent = memo(function ReadonlyContent({
  content,
  className,
  attachments,
}: ReadonlyContentProps) {
  return (
    <RichContent
      content={content}
      attachments={attachments}
      density="document"
      phase="settled"
      className={className}
    />
  );
});
