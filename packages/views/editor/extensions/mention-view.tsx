"use client";

/**
 * MentionView — NodeView for rendering @mentions inline in the editor.
 *
 * Member/agent mentions: plain "@Name" text with .mention class styling.
 * Issue/project mentions render the same navigable chips as readonly content
 * (IssueMentionCard / ProjectMentionCard), so click behavior — plain click,
 * modifier click, middle click — cannot drift between an editing and a
 * readonly surface. The editor's ProseMirror click handler skips anything
 * inside `[data-node-view-wrapper]`, so the AppLink inside the card owns the
 * click alone.
 *
 * Issue chip sizing: must fit within the paragraph line box (14px * 1.625 =
 * 22.75px). Card is text-caption (12px) + py-0.5 + border ≈ 22px total. The
 * `vertical-align: middle` rule on `[data-node-view-wrapper]` in CSS handles
 * line-box alignment; setting it on an inner element has no effect because
 * the wrapper is the outermost inline element.
 */

import { NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { IssueMentionCard } from "../../issues/components/issue-mention-card";
import { ProjectMentionCard } from "../../projects/components/project-mention-card";

export function MentionView({ node }: NodeViewProps) {
  const { type, id, label } = node.attrs;

  // stopPropagation mirrors the readonly renderer's mention wrappers: a chip
  // click must not reach surrounding click handlers.
  if (type === "issue") {
    return (
      <NodeViewWrapper
        as="span"
        className="inline"
        onClick={(e: React.MouseEvent) => e.stopPropagation()}
      >
        <IssueMentionCard issueId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  if (type === "project") {
    return (
      <NodeViewWrapper
        as="span"
        className="inline"
        onClick={(e: React.MouseEvent) => e.stopPropagation()}
      >
        <ProjectMentionCard projectId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  return (
    <NodeViewWrapper as="span" className="inline">
      <span className="mention">@{label ?? id}</span>
    </NodeViewWrapper>
  );
}
