"use client";

import { Archive } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { CHAT_COLUMN, CHAT_GUTTER } from "./chat-column";
import { useT } from "../../i18n";

// Sibling of OfflineBanner / NoAgentBanner, occupying the same banner slot
// above the chat input. Shown when the open session's agent has been archived
// (retired): the input above is disabled and this banner explains that the
// conversation is read-only history — the agent can no longer reply.
//
// Layout comes from the shared CHAT_GUTTER / CHAT_COLUMN pair, same as its
// siblings, so the banner's edges line up with the input at every surface width.
export function ArchivedAgentBanner({ agentName }: { agentName?: string }) {
  const { t } = useT("chat");
  const name = agentName?.trim() || t(($) => $.offline_banner.fallback_name);
  return (
    <div className={cn(CHAT_GUTTER, "mb-1.5")}>
      <div className={cn(CHAT_COLUMN, "flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-caption bg-muted text-muted-foreground ring-1 ring-border")}>
        <Archive className="size-3.5 shrink-0" />
        <span className="truncate">
          {t(($) => $.archived_agent_banner, { name })}
        </span>
      </div>
    </div>
  );
}
