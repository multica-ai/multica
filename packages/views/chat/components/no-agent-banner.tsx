"use client";

import { Bot } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { CHAT_COLUMN, CHAT_GUTTER } from "./chat-column";
import { useT } from "../../i18n";

// Sibling of ChatInput, occupying the same banner slot as OfflineBanner.
// Shown when the workspace has no agent the current user can chat with —
// the input above is disabled, and this banner explains why.
//
// Pure copy by design: the banner doesn't link to /agents because the
// information ("you need an agent") is what's actionable here, not the
// destination — pushing users out of chat to a settings page mid-thought
// is more disruptive than just stating the prerequisite. Users who want
// to act go to Agents on their own.
//
// Layout comes from the shared CHAT_GUTTER / CHAT_COLUMN pair, same as
// OfflineBanner and ChatInput, so the banner's edges line up with the input at
// every surface width.
export function NoAgentBanner() {
  const { t } = useT("chat");
  return (
    <div className={cn(CHAT_GUTTER, "mb-1.5")}>
      <div className={cn(CHAT_COLUMN, "flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-caption bg-muted text-muted-foreground ring-1 ring-border")}>
        <Bot className="size-3.5 shrink-0" />
        <span className="truncate">{t(($) => $.no_agent_banner)}</span>
      </div>
    </div>
  );
}
