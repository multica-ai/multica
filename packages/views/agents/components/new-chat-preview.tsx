"use client";

import { selectConversationStarters } from "@multica/core/agents";
import type { AgentConversationStarter } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import {
  ConversationStarterList,
  useFallbackConversationStarters,
} from "../../chat/components/conversation-starter-list";
import { useT } from "../../i18n";

/**
 * What a new chat with this agent shows before anyone types, built from the
 * profile being edited rather than the saved agent, so every field change is
 * visible at once. Starters go through the same component and resolver as the
 * real empty state — including the fallback, which is how an author learns
 * that suggestions they never configured are defaults they can replace.
 */
export function NewChatPreview({
  name,
  description,
  avatarUrl,
  starters,
}: {
  name: string;
  description: string;
  avatarUrl: string | null;
  starters: AgentConversationStarter[];
}) {
  const { t } = useT("agents");
  const { t: tChat } = useT("chat");
  const fallbackStarters = useFallbackConversationStarters();
  const preview = selectConversationStarters(starters, fallbackStarters);
  const trimmedDescription = description.trim();

  return (
    <div className="rounded-xl border bg-background px-4 pt-6 pb-4">
      <div className="flex flex-col items-center gap-3.5 text-center">
        <ActorAvatar
          name={name}
          initials=""
          avatarUrl={
            avatarUrl?.startsWith("/") ? resolvePublicFileUrl(avatarUrl) : avatarUrl
          }
          isAgent
          size="xl"
          className="ring-1 ring-inset ring-border"
        />
        <div className="min-w-0 space-y-0.5">
          <p className="text-body font-semibold break-words">
            {tChat(($) => $.empty_state.chat_with_named, { name })}
          </p>
          {trimmedDescription ? (
            <p className="line-clamp-3 text-caption text-muted-foreground">
              {trimmedDescription}
            </p>
          ) : null}
        </div>
        <ConversationStarterList starters={preview.starters} />
      </div>
      {preview.isFallback ? (
        <p className="mt-3 text-caption leading-5 text-muted-foreground">
          {t(($) => $.conversation_starters.preview_defaults)}
        </p>
      ) : null}
    </div>
  );
}
