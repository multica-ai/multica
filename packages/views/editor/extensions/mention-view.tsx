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
import { useQuery } from "@tanstack/react-query";
import { useWorkspacePaths } from "@multica/core/paths";
import { getCurrentWsId } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Lock } from "lucide-react";
// CEREBRO-PATCH(skill-mention-view): render `mention://skill/<id>` nodes as a SkillMentionChip.
import { SkillMentionChip } from "@multica/cerebro-skill-mention";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { IssueChip } from "../../issues/components/issue-chip";

export function MentionView({ node }: NodeViewProps) {
  const { type, id, label } = node.attrs;

  if (type === "skill") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <SkillMentionChip skillId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  if (type === "issue") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <IssueMention issueId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  if (type === "agent") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <AgentMention agentId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  return (
    <NodeViewWrapper as="span" className="inline">
      <span className="mention">@{label ?? id}</span>
    </NodeViewWrapper>
  );
}

// CEREBRO-PATCH(private-agent-owner-only-trigger): an @mention of a private
// agent only wakes it for its owner (FIR-2349). When the current viewer isn't
// the owner, the chip is muted with a lock + "won't trigger" tooltip so a
// non-owner sees their tag is inert. Renders as a plain mention otherwise, and
// also when the agent isn't in cache (a member who can't see it never tagged
// it). Mirrors the Go gate `canTriggerPrivateAgent`.
function AgentMention({
  agentId,
  fallbackLabel,
}: {
  agentId: string;
  fallbackLabel?: string;
}) {
  const { t } = useT("editor");
  const wsId = getCurrentWsId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: agents } = useQuery({
    ...agentListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const agent = agents?.find((a) => a.id === agentId);
  const wontTrigger =
    !!agent &&
    agent.visibility === "private" &&
    (agent.owner_id === null || agent.owner_id !== userId);

  if (wontTrigger) {
    const noTriggerLabel = t(($) => $.mention.no_trigger_private);
    return (
      <span
        className="mention inline-flex items-center gap-0.5 opacity-60"
        title={noTriggerLabel}
      >
        <Lock aria-label={noTriggerLabel} className="size-3 shrink-0" />@
        {fallbackLabel ?? agentId}
      </span>
    );
  }

  return <span className="mention">@{fallbackLabel ?? agentId}</span>;
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
    // CEREBRO-PATCH(issue-chip-inline-link-wrap): inline (not inline-flex) so the <a> participates in text flow and allows box-decoration-clone to paint across wrapped lines (JEH-1593)
    <a href={issuePath} onClick={handleClick} className="issue-mention inline">
      <IssueChip
        issueId={issueId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
  );
}
