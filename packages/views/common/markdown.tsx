"use client";

import * as React from "react";
import {
  Markdown as MarkdownBase,
  type MarkdownProps as MarkdownBaseProps,
  type RenderMode,
} from "@multica/ui/markdown";
import { useConfigStore } from "@multica/core/config";
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
 * App-level Markdown wrapper that injects IssueMentionCard via renderMention
 * and cdnDomain from the config store for file card rendering.
 */
export function Markdown(props: MarkdownProps): React.JSX.Element {
  const cdnDomain = useConfigStore((s) => s.cdnDomain);
  return <MarkdownBase renderMention={defaultRenderMention} cdnDomain={cdnDomain} {...props} />;
}

export const MemoizedMarkdown = React.memo(Markdown);
MemoizedMarkdown.displayName = "MemoizedMarkdown";
