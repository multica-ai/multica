// CEREBRO-PATCH(reply-target-agent-indicator): pure helpers for the reply
// composer target bar. Kept outside the React component so the draft parsing
// behavior is cheap to unit-test.

import type { Agent, MemberWithUser } from "@multica/core/types";
// CEREBRO-PATCH(reply-target-mention-suppression): TECH-3194 — resolve the bar's
// targets through the shared backend-mirroring resolver so the indicator only
// shows agents that will actually receive the comment. Tagging a person / squad
// / @all now suppresses the assignee fallback, exactly like the backend.
import { resolveCommentTriggerAgentIds } from "@multica/cerebro-access/views";

export function getReplyTargetAgents({
  markdown,
  triggerAgentId,
  agents,
}: {
  markdown: string;
  triggerAgentId?: string;
  agents: Agent[];
}): Agent[] {
  const ids = resolveCommentTriggerAgentIds({ markdown, triggerAgentId });
  const byId = new Map(agents.map((agent) => [agent.id, agent]));
  return ids
    .map((id) => byId.get(id))
    .filter((agent): agent is Agent => !!agent);
}

export function extractMentionedAgentIds(markdown: string): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();
  const re = /(?<!\\)\[@(?:\\.|[^\]])+\]\(mention:\/\/agent\/([^)]+)\)/g;
  for (const match of markdown.matchAll(re)) {
    const id = match[1];
    if (!id || seen.has(id)) continue;
    seen.add(id);
    ids.push(id);
  }
  return ids;
}

export function memberMentionMarkdown(member: MemberWithUser): string {
  const label = member.name.replace(/\[/g, "\\[").replace(/\]/g, "\\]");
  return `[@${label}](mention://member/${member.user_id})`;
}
