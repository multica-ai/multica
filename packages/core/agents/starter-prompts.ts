import type { Agent, AgentStarterPrompt } from "../types";

/**
 * Resolve what a new chat with this agent actually shows.
 *
 * A configured suggestion only counts once BOTH halves carry text: the editor
 * lets an author add a row and save is blocked while it is half-filled, but a
 * malformed payload from an older client must not render a blank button.
 *
 * When nothing complete survives, the caller's localized fallbacks are used
 * and `isFallback` says so — the settings preview needs to tell an author
 * "these are the defaults" rather than implying they configured them.
 */
export function selectStarterPrompts(
  configured: AgentStarterPrompt[] | undefined | null,
  fallback: AgentStarterPrompt[],
): { prompts: AgentStarterPrompt[]; isFallback: boolean } {
  const complete = (configured ?? []).filter(
    (item) => item.label.trim() !== "" && item.prompt.trim() !== "",
  );
  return complete.length > 0
    ? { prompts: complete, isFallback: false }
    : { prompts: fallback, isFallback: true };
}

/**
 * Whether this viewer should be offered a way to edit the agent's starter
 * prompts from the chat empty state those prompts render in.
 *
 * Three independent reasons to stay silent, each of which would otherwise
 * produce a link that lies:
 *
 *  - no agent, or an archived one: its conversations are read-only, so
 *    editing its starters changes nothing anyone will see,
 *  - a backend that does not persist starter prompts: the Instructions tab
 *    renders no editor there, so the link lands somewhere that cannot honour
 *    it,
 *  - a viewer who cannot edit the agent: a reader would arrive at a page they
 *    have no write access to.
 */
export function canCustomizeStarterPrompts(
  agent: Pick<Agent, "archived_at"> | null,
  opts: { starterPromptsSupported: boolean; canEditAgent: boolean },
): boolean {
  if (!agent || agent.archived_at) return false;
  if (!opts.starterPromptsSupported) return false;
  return opts.canEditAgent;
}
