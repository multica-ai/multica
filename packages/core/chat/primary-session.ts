import type { ChatSession } from "../types/chat";

/**
 * The Team page shows one long-running chat per (viewer, agent): the
 * "primary" session. It is not a stored flag — it is the most recently active
 * session the viewer started in Multica itself, so a session created by an
 * older client (or by changing project context) takes over naturally and no
 * migration is needed. Every other session with the agent is an "earlier
 * conversation": still listed, opened read-only.
 *
 * Sessions bound to an external channel (Slack, Lark, ...) never become
 * primary: their replies happen in that channel.
 */

function activityTime(s: ChatSession): number {
  return new Date(s.last_message?.created_at ?? s.updated_at).getTime();
}

export function isChannelChatSession(s: ChatSession): boolean {
  return !!s.channel_source;
}

export function pickPrimaryChatSession(
  sessions: readonly ChatSession[] | undefined,
  agentId: string,
): ChatSession | null {
  let best: ChatSession | null = null;
  for (const s of sessions ?? []) {
    if (s.agent_id !== agentId) continue;
    if (s.status === "archived" || isChannelChatSession(s)) continue;
    if (!best || activityTime(s) > activityTime(best)) best = s;
  }
  return best;
}

/** Every other session with the agent, newest activity first. */
export function earlierChatSessions(
  sessions: readonly ChatSession[] | undefined,
  agentId: string,
  primaryId: string | null,
): ChatSession[] {
  return (sessions ?? [])
    .filter((s) => s.agent_id === agentId && s.id !== primaryId)
    .sort((a, b) => activityTime(b) - activityTime(a));
}

/**
 * Unread assistant messages across every session with each agent, keyed by
 * agent id. Archived sessions never count (they cannot be opened to clear
 * them), and `excludeSessionId` drops the session on screen — the same rules
 * as `countUnreadChatMessages`.
 */
export function unreadChatMessagesByAgent(
  sessions: readonly ChatSession[] | undefined,
  excludeSessionId?: string | null,
): Map<string, number> {
  const out = new Map<string, number>();
  for (const s of sessions ?? []) {
    if (s.status === "archived" || s.id === excludeSessionId) continue;
    const n = s.unread_count ?? 0;
    if (n > 0) out.set(s.agent_id, (out.get(s.agent_id) ?? 0) + n);
  }
  return out;
}
