import type { Agent, ChatSession } from "../types";

/**
 * Mirrors `service.MikaSystemKey` on the server. Mika is identified by this
 * key and never by display name — the name is owner-editable, so a rename
 * would otherwise make the workspace look like it has no Mika.
 */
export const MIKA_SYSTEM_KEY = "mika";

export function isMikaAgent(agent: Pick<Agent, "system_key">): boolean {
  return agent.system_key === MIKA_SYSTEM_KEY;
}

/**
 * Whether the workspace still needs a Mika provisioned.
 *
 * Deliberately "no Mika" rather than "no agents at all": the recovery
 * entrypoint on the Runtimes page used the latter, so creating any ordinary
 * agent first hid the only surface that can mint a Mika — and the generic
 * agent endpoint cannot, since it accepts neither `kind` nor `system_key`.
 */
export function workspaceNeedsMika(agents: Pick<Agent, "system_key">[]): boolean {
  return !agents.some(isMikaAgent);
}

/**
 * Whether *this member* still needs the "Start with Mika" entrypoint.
 *
 * Not the same question as `workspaceNeedsMika`, and gating the entrypoint on
 * that one was a trap: bootstrapping is three server steps — provision the
 * agent, open the member's session, enqueue the opening turn — and the last
 * two can fail after the agent has committed. The agent's own `agent:created`
 * broadcast then invalidates the agent list, so the card unmounted (taking its
 * open dialog with it) the instant the *first* step succeeded, and never came
 * back on reload, because the agent is durable and the rest was not. The
 * member was left with a Mika they could not start.
 *
 * Every step is idempotent, so the honest condition is "has this member
 * actually ended up with a Mika conversation that has been kicked off" — and
 * re-running the flow from here is always safe.
 */
export function memberNeedsMikaSetup(
  agents: Pick<Agent, "id" | "system_key">[],
  sessions: Pick<ChatSession, "agent_id" | "last_message">[],
): boolean {
  const mika = agents.find(isMikaAgent);
  if (!mika) return true;

  // Sessions are per member, so this is the caller's own conversation.
  const session = sessions.find((s) => s.agent_id === mika.id);
  if (!session) return true;

  // The opening turn is stored as a real (hidden) message, so an empty
  // conversation means the kickoff never landed.
  return !session.last_message;
}
