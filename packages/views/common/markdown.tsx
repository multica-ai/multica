"use client";

import * as React from "react";
import {
  Markdown as MarkdownBase,
  MemoizedMarkdown as MemoizedMarkdownBase,
  type MarkdownProps as MarkdownBaseProps,
  type RenderMode,
} from "@multica/ui/markdown";
import { IssueMentionCard } from "../issues/components/issue-mention-card";

export type { RenderMode };

export type MarkdownProps = MarkdownBaseProps;

/**
 * Default renderMention that delegates to IssueMentionCard for issue mentions
 * and renders a styled span for other mention types.
 */
function defaultRenderMention({
  type,
  id,
}: {
  type: string;
  id: string;
}): React.ReactNode {
  if (type === "issue") {
    return <IssueMentionCard issueId={id} />;
  }
  return null;
}

/**
 * App-level Markdown wrapper that injects IssueMentionCard via renderMention.
 * Callers that need custom mention rendering can pass their own renderMention prop.
 */
export function Markdown(props: MarkdownProps): React.JSX.Element {
  return <MarkdownBase renderMention={defaultRenderMention} {...props} />;
}

export const MemoizedMarkdown = React.memo(
  Markdown,
  (prevProps, nextProps) => {
    if (prevProps.id && nextProps.id) {
      return (
        prevProps.id === nextProps.id &&
        prevProps.children === nextProps.children &&
        prevProps.mode === nextProps.mode
      );
    }
    return (
      prevProps.children === nextProps.children &&
      prevProps.mode === nextProps.mode
    );
  },
);
MemoizedMarkdown.displayName = "MemoizedMarkdown";
