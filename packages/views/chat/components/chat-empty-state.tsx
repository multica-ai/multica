"use client";

import type { Agent } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

// Three starter prompts shown on the empty compose. Each is keyed into the
// chat namespace so labels translate per locale; the icon stays raw since
// emojis are locale-neutral.
const STARTER_KEYS: ("list_open" | "summarize_today" | "plan_next")[] = [
  "list_open",
  "summarize_today",
  "plan_next",
];
const STARTER_ICONS: Record<(typeof STARTER_KEYS)[number], string> = {
  list_open: "📋",
  summarize_today: "📝",
  plan_next: "💡",
};

/**
 * Empty compose placeholder shown when a chat has no messages yet. Agent-aware:
 * it leads with the chosen agent's avatar + name + description so the user knows
 * exactly who they're about to talk to, then offers starter prompts.
 */
export function EmptyState({
  agent,
  onPickPrompt,
}: {
  agent: Agent | null;
  onPickPrompt: (text: string) => void;
}) {
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
        <p className="text-sm text-muted-foreground">
          {description || t(($) => $.empty_state.returning_subtitle)}
        </p>
      </div>
      <div className="w-full max-w-xs space-y-2">
        {STARTER_KEYS.map((key) => {
          const text = t(($) => $.starter_prompts[key]);
          return (
            <button
              key={key}
              type="button"
              onClick={() => onPickPrompt(text)}
              className="w-full rounded-lg border border-border bg-card px-3 py-2 text-left text-sm text-foreground transition-colors hover:bg-accent hover:border-brand/40"
            >
              <span className="mr-2">{STARTER_ICONS[key]}</span>
              {text}
            </button>
          );
        })}
      </div>
    </div>
  );
}
