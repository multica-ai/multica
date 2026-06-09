// FIR-32: pure helpers for the "you're starting an agent" send confirm. Kept
// outside the React hook so the draft parsing + decision are cheap to unit-test
// (mirrors the reply-targets.ts split in the upstream composer).

import type { Agent, MemberRole } from "@multica/core/types";
import { canTriggerPrivateAgentMention } from "./agent-trigger";

const MENTION_AGENT_RE = /(?<!\\)\[@(?:\\.|[^\]])+\]\(mention:\/\/agent\/([^)]+)\)/g;
// Non-agent targets a draft can address instead of the assignee. These mirror
// the mention types the backend counts in commentMentionsOthersButNotAssignee
// (server/internal/util/mention.go): a tagged person, a squad, or @all.
const HAS_MEMBER_MENTION_RE = /(?<!\\)\[@(?:\\.|[^\]])+\]\(mention:\/\/member\/[^)]+\)/;
const HAS_SQUAD_MENTION_RE = /(?<!\\)\[@(?:\\.|[^\]])+\]\(mention:\/\/squad\/[^)]+\)/;
const HAS_ALL_MENTION_RE = /(?<!\\)\[@?(?:\\.|[^\]])+\]\(mention:\/\/all\/all\)/;

/** Agent ids explicitly @mentioned in the draft, in order, de-duplicated. */
export function extractMentionedAgentIds(markdown: string): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();
  for (const match of markdown.matchAll(MENTION_AGENT_RE)) {
    const id = match[1];
    if (!id || seen.has(id)) continue;
    seen.add(id);
    ids.push(id);
  }
  return ids;
}

/**
 * resolveCommentTriggerAgentIds — the agent ids a member's comment will actually
 * wake, mirroring the backend trigger logic so the composer's indicator and the
 * send confirm reflect who really receives the comment (TECH-3194).
 *
 * Backend rule (server/internal/handler/comment.go):
 *  - explicit @agent mentions are triggered via `enqueueMentionedAgentTasks`;
 *  - otherwise the issue assignee (`triggerAgentId`) is woken via the on_comment
 *    fallback — UNLESS the draft addresses someone else instead (a tagged
 *    person, a squad, or @all) without tagging the assignee. That suppression
 *    is `commentMentionsOthersButNotAssignee`, and when it fires nothing is
 *    woken, so there is no trigger target.
 */
export function resolveCommentTriggerAgentIds({
  markdown,
  triggerAgentId,
}: {
  markdown: string;
  triggerAgentId?: string;
}): string[] {
  const mentioned = extractMentionedAgentIds(markdown);
  if (mentioned.length > 0) return mentioned;
  if (!triggerAgentId) return [];
  // No @agent mention: the assignee only fires when the draft is not addressed
  // to someone else. Tagging a person / squad / @all means "talking to others,
  // not the assignee" → the assignee is not woken.
  if (
    HAS_ALL_MENTION_RE.test(markdown) ||
    HAS_MEMBER_MENTION_RE.test(markdown) ||
    HAS_SQUAD_MENTION_RE.test(markdown)
  ) {
    return [];
  }
  return [triggerAgentId];
}

/**
 * pickAgentNeedingConfirm — returns the agent the user must confirm before
 * sending, or null when no confirm is needed.
 *
 * The send wakes an agent that is neither the user's own nor a workspace agent
 * exactly when the resolved target is a private agent the user cannot trigger
 * (FIR-2349 gate). Workspace-visibility agents and the user's own private agent
 * pass straight through — no friction. The resolved target mirrors the backend
 * trigger: explicit @agent mentions win, otherwise the issue's trigger agent
 * (assignee / squad leader).
 */
export function pickAgentNeedingConfirm({
  markdown,
  triggerAgentId,
  agents,
  userId,
  role,
}: {
  markdown: string;
  triggerAgentId?: string;
  agents: Agent[];
  userId: string | null;
  role: MemberRole | null;
}): Agent | null {
  const ids = resolveCommentTriggerAgentIds({ markdown, triggerAgentId });
  const byId = new Map(agents.map((agent) => [agent.id, agent]));
  for (const id of ids) {
    const agent = byId.get(id);
    if (!agent) continue;
    // Workspace-visibility agent — anyone may start it, no confirm.
    if (agent.visibility !== "private") continue;
    // The user's own private agent triggers directly — no confirm.
    if (canTriggerPrivateAgentMention(agent, { userId, role }).allowed) continue;
    return agent;
  }
  return null;
}
