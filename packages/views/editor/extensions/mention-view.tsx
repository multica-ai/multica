"use client";

/**
 * MentionView — NodeView for rendering @mentions inline in the editor.
 *
 * Member/agent mentions: plain "@Name" text with .mention class styling.
 * Issue mentions: IssueChip inside a custom <a> that supports cmd/shift-click
 * to open in a new tab (AppLink doesn't expose that intent hook).
 *
 * Issue chip sizing: must fit within the paragraph line box (14px * 1.625 =
 * 22.75px). Card is text-xs (12px) + py-0.5 + border ≈ 22px total. The
 * `vertical-align: middle` rule on `[data-node-view-wrapper]` in CSS handles
 * line-box alignment; setting it on the inner <a> has no effect because the
 * wrapper is the outermost inline element.
 */

import { NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import { IssueChip } from "../../issues/components/issue-chip";

export function MentionView({ node }: NodeViewProps) {
  const { type, id, label } = node.attrs;

  if (type === "issue") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <IssueMention issueId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  return (
    <NodeViewWrapper as="span" className="inline">
      <span className="mention">@{label ?? id}</span>
    </NodeViewWrapper>
  );
}

function IssueMention({
  issueId,
  fallbackLabel,
}: {
  issueId: string;
  fallbackLabel?: string;
}) {
  const p = useWorkspacePaths();
  const { push, openInNewTab } = useNavigation();
  const issuePath = p.issueDetail(issueId);

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) openInNewTab(issuePath, fallbackLabel);
      return;
    }
    push(issuePath);
  };

  return (
    <a href={issuePath} onClick={handleClick} className="issue-mention inline-flex">
      <IssueChip
        issueId={issueId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
  );
}
