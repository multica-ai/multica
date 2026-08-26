"use client";

import { Settings2 } from "lucide-react";
import { selectStarterPrompts } from "@multica/core/agents";
import type { Agent } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import {
  StarterPromptList,
  useFallbackStarterPrompts,
} from "./starter-prompt-list";

/** Empty compose placeholder shown before the first user message. */
export function EmptyState({
  agent,
  hasSessions = true,
  onPickPrompt,
  customizeHref = null,
}: {
  agent: Agent | null;
  hasSessions?: boolean;
  onPickPrompt: (prompt: string) => void;
  /**
   * Where "customize" sends this viewer, or `null` to hide the affordance.
   * The container resolves it: only someone who may edit THIS agent on a
   * backend that persists starter prompts gets a link. Keeping it a prop
   * leaves this component presentational — and keeps the link out of the DOM
   * entirely for readers, rather than rendering a disabled tease.
   */
  customizeHref?: string | null;
}) {
  const { t } = useT("chat");
  const description = agent?.description?.trim();
  const fallbackPrompts = useFallbackStarterPrompts();
  const { prompts } = selectStarterPrompts(
    agent?.starter_prompts,
    fallbackPrompts,
  );

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
          <StarterPromptList prompts={prompts} onPick={onPickPrompt} />
          {customizeHref ? (
            <div className="flex justify-center pt-1">
              <AppLink
                href={customizeHref}
                className="inline-flex items-center gap-1.5 text-caption text-muted-foreground transition-colors hover:text-foreground"
              >
                <Settings2 className="size-3.5" aria-hidden="true" />
                {t(($) => $.starter_prompts.customize)}
              </AppLink>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
