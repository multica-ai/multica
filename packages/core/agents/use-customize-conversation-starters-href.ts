"use client";

import type { Agent } from "../types";
import { useConfigStore } from "../config";
import { useWorkspacePaths } from "../paths";
import { useAgentPermissions } from "../permissions";
import { canCustomizeConversationStarters } from "./conversation-starters";

/**
 * Where the "customize" affordance in a chat's empty state should point, or
 * `null` when this viewer should not see one at all.
 *
 * Both chat surfaces render that empty state from separate call sites, so the
 * wiring lives here and the rule itself in `canCustomizeConversationStarters`.
 */
export function useCustomizeConversationStartersHref(
  agent: Agent | null,
  wsId: string,
): string | null {
  const conversationStartersSupported = useConfigStore(
    (state) => state.agentConversationStartersSupported,
  );
  const paths = useWorkspacePaths();
  const { canEdit } = useAgentPermissions(agent, wsId);

  if (
    !agent ||
    !canCustomizeConversationStarters(agent, {
      conversationStartersSupported,
      canEditAgent: canEdit.allowed,
    })
  ) {
    return null;
  }
  return paths.agentConversationStarters(agent.id);
}
