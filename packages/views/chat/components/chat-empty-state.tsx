"use client";

import type { Agent, AgentStarterPrompt } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

/** Empty compose placeholder shown before the first user message. */
export function EmptyState({
  agent,
  hasSessions = true,
  onPickPrompt,
}: {
  agent: Agent | null;
  hasSessions?: boolean;
  onPickPrompt: (prompt: string) => void;
}) {
  const { t } = useT("chat");
  const description = agent?.description?.trim();

  const fallbackPrompts: AgentStarterPrompt[] = [
    {
      label: t(($) => $.starter_prompts.capabilities.label),
      prompt: t(($) => $.starter_prompts.capabilities.prompt),
    },
    {
      label: t(($) => $.starter_prompts.first_task.label),
      prompt: t(($) => $.starter_prompts.first_task.prompt),
    },
    {
      label: t(($) => $.starter_prompts.recommend.label),
      prompt: t(($) => $.starter_prompts.recommend.prompt),
    },
  ];
  const configuredPrompts = (agent?.starter_prompts ?? []).filter(
    (item) => item.label.trim() && item.prompt.trim(),
  );
  const prompts =
    configuredPrompts.length > 0 ? configuredPrompts : fallbackPrompts;

  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center-safe gap-5 overflow-y-auto px-6 py-8">
      {agent && (
        <ActorAvatar
          actorType="agent"
          actorId={agent.id}
          size="2xl"
          className="ring-1 ring-inset ring-border"
        />
      )}
      <div className="max-w-sm space-y-1 text-center">
        <h3 className="text-title-sm font-semibold">
          {agent
            ? t(($) => $.empty_state.chat_with_named, { name: agent.name })
            : t(($) => $.empty_state.first_time_title)}
        </h3>
        {description && (
          <p className="text-body text-muted-foreground">{description}</p>
        )}
        {!hasSessions && (
          <p className="text-body text-muted-foreground">
            {t(($) => $.empty_state.first_time_actions)}
          </p>
        )}
      </div>
      {agent ? (
        <div
          className="w-full max-w-sm space-y-2"
          aria-label={t(($) => $.starter_prompts.aria_label)}
        >
          {prompts.map((item, index) => (
            <button
              key={index}
              type="button"
              onClick={() => onPickPrompt(item.prompt)}
              className="w-full rounded-lg border border-border bg-card px-3 py-2.5 text-left text-body text-foreground transition-colors hover:border-brand/40 hover:bg-accent"
            >
              {item.label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
