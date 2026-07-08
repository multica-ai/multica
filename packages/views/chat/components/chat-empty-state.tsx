"use client";

import type { Agent } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

/**
 * Empty compose placeholder shown when a chat has no messages yet. Agent-aware:
 * it leads with the chosen agent's avatar + name + description so the user knows
 * exactly who they're about to talk to. The composer below is the entry point —
 * no starter prompts, they read as filler more than help.
 */
export function EmptyState({ agent }: { agent: Agent | null }) {
  const { t } = useT("chat");
  const description = agent?.description?.trim();
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 px-6 py-8">
      {agent && (
        <ActorAvatar
          actorType="agent"
          actorId={agent.id}
          size={56}
          className="ring-1 ring-inset ring-border"
        />
      )}
      <div className="max-w-sm space-y-1 text-center">
        <h3 className="text-base font-semibold">
          {agent
            ? t(($) => $.empty_state.chat_with_named, { name: agent.name })
            : t(($) => $.empty_state.first_time_title)}
        </h3>
        {description && (
          <p className="text-sm text-muted-foreground">{description}</p>
        )}
      </div>
    </div>
  );
}
