"use client";

import { selectConversationStarters } from "@multica/core/agents";
import type { Agent } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import {
  ConversationStarterList,
  useFallbackConversationStarters,
} from "./conversation-starter-list";

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
  const fallbackStarters = useFallbackConversationStarters();
  const { starters } = selectConversationStarters(
    agent?.conversation_starters,
    fallbackStarters,
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
          aria-label={t(($) => $.conversation_starters.aria_label)}
        >
          <ConversationStarterList starters={starters} onPick={onPickPrompt} />
        </div>
      ) : null}
    </div>
  );
}
