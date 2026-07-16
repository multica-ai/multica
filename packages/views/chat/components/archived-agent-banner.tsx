"use client";

import { Archive } from "lucide-react";
import { useT } from "../../i18n";

// Sibling of OfflineBanner / NoAgentBanner, occupying the same banner slot
// above the chat input. Shown when the open session's agent has been archived
// (retired): the input above is disabled and this banner explains that the
// conversation is read-only history — the agent can no longer reply.
//
// Layout (`px-5` outer, `mx-auto max-w-4xl` inner) mirrors its siblings so the
// banner's edges line up with the input on every viewport size.
export function ArchivedAgentBanner({ agentName }: { agentName?: string }) {
  const { t } = useT("chat");
  const name = agentName?.trim() || t(($) => $.offline_banner.fallback_name);
  return (
    <div className="px-5 mb-1.5">
      <div className="mx-auto flex w-full max-w-4xl items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs bg-muted text-muted-foreground ring-1 ring-border">
        <Archive className="size-3.5 shrink-0" />
        <span className="truncate">
          {t(($) => $.archived_agent_banner, { name })}
        </span>
      </div>
    </div>
  );
}
