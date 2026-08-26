"use client";

import type { Agent } from "../types";
import { useConfigStore } from "../config";
import { useWorkspacePaths } from "../paths";
import { useAgentPermissions } from "../permissions";
import { canCustomizeStarterPrompts } from "./starter-prompts";

/**
 * Where the "customize" affordance in a chat's empty state should point, or
 * `null` when this viewer should not see one at all.
 *
 * Both chat surfaces render that empty state from separate call sites, so the
 * wiring lives here and the rule itself in `canCustomizeStarterPrompts`.
 */
export function useCustomizeStarterPromptsHref(
  agent: Agent | null,
  wsId: string,
): string | null {
  const starterPromptsSupported = useConfigStore(
    (state) => state.agentStarterPromptsSupported,
  );
  const paths = useWorkspacePaths();
  const { canEdit } = useAgentPermissions(agent, wsId);

  if (
    !agent ||
    !canCustomizeStarterPrompts(agent, {
      starterPromptsSupported,
      canEditAgent: canEdit.allowed,
    })
  ) {
    return null;
  }
  return paths.agentStarterPrompts(agent.id);
}
