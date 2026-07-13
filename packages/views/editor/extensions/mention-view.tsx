"use client";

/**
 * MentionView — NodeView for rendering @mentions inline in the editor.
 *
 * Dispatch is registry-driven via `isActorMentionType` from
 * `@multica/core/mention`. Issue and project have dedicated chip components
 * with navigation; actor types (member/agent/squad/@all) share the
 * ActorMentionChip pattern with hover cards. Skill mentions render as a
 * SkillChip with a hover card showing bound agents. Unknown types fall back
 * to a plain-text mention.
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
import { isActorMentionType } from "@multica/core/mention";
import { useNavigation } from "../../navigation";
import { IssueChip } from "../../issues/components/issue-chip";
import { ProjectChip } from "../../projects/components/project-chip";
import { ActorMentionChip } from "@multica/ui/components/common/actor-mention-chip";
import { SkillMentionChip } from "@multica/ui/components/common/skill-mention-chip";
import { MentionHoverCard } from "../mention-hover-card";

export function MentionView({ node }: NodeViewProps) {
  const { type, id, label } = node.attrs;

  // Registry-driven dispatch. Issue and project have specific chip components
  // (IssueChip, ProjectChip) that need dedicated wrappers for navigation;
  // actor types share the ActorMentionChip + hover card pattern; skill
  // mentions share SkillMentionChip + hover card. Unknown types fall through
  // to a plain-text mention.
  if (type === "issue") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <IssueMention issueId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  if (type === "project") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <ProjectMention projectId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  const name = (label ?? id) as string;

  // Skill mentions render as a skill chip with bound-agents hover card.
  if (type === "skill") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <MentionHoverCard type="skill" id={id}>
          <SkillMentionChip name={name} focusable />
        </MentionHoverCard>
      </NodeViewWrapper>
    );
  }

  const initials = name.charAt(0);

  // Actor types (member/agent/squad/all) render as avatar chips with hover cards.
  if (isActorMentionType(type)) {
    return (
      <NodeViewWrapper as="span" className="inline">
        <MentionHoverCard type={type} id={id}>
          <ActorMentionChip type={type} label={name} initials={initials} focusable />
        </MentionHoverCard>
      </NodeViewWrapper>
    );
  }

  // Unknown or unhandled types — plain-text fallback.
  return (
    <NodeViewWrapper as="span" className="inline">
      <span className="mention">@{name}</span>
    </NodeViewWrapper>
  );
}

function ProjectMention({
  projectId,
  fallbackLabel,
}: {
  projectId: string;
  fallbackLabel?: string;
}) {
  const p = useWorkspacePaths();
  const { push, openInNewTab } = useNavigation();
  const projectPath = p.projectDetail(projectId);

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) openInNewTab(projectPath, fallbackLabel);
      return;
    }
    push(projectPath);
  };

  return (
    <a href={projectPath} onClick={handleClick} className="project-mention">
      <ProjectChip
        projectId={projectId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
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
    <a href={issuePath} onClick={handleClick} className="issue-mention">
      <IssueChip
        issueId={issueId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
  );
}
