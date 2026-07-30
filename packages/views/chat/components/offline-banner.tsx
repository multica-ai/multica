"use client";

import { AlertCircle, WifiOff } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { AgentAvailability } from "@multica/core/agents";
import { CHAT_COLUMN, CHAT_GUTTER } from "./chat-column";
import { useT } from "../../i18n";

interface Props {
  /** Display name shown in the banner copy. */
  agentName?: string;
  /**
   * Resolved presence availability. Pass `undefined` (or "loading") to
   * suppress the banner — we only surface known offline / unstable states,
   * never speculative copy.
   */
  availability: AgentAvailability | undefined;
}

// Inline notice rendered above the chat input when the active agent isn't
// reachable. Hides on `online`, `undefined`, or while presence is loading —
// users get the silent default behaviour and only see copy when there's a
// real-world implication for the message they're about to send.
export function OfflineBanner({ agentName, availability }: Props) {
  const { t } = useT("chat");
  if (availability !== "offline" && availability !== "unstable") return null;

  const name = agentName?.trim() || t(($) => $.offline_banner.fallback_name);
  if (availability === "unstable") {
    return (
      <div className={cn(CHAT_GUTTER, "mb-1.5")}>
        <div className={cn(CHAT_COLUMN, "flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-caption bg-amber-50 dark:bg-amber-950/40 text-amber-900 dark:text-amber-200 ring-1 ring-amber-200/60 dark:ring-amber-900/40")}>
          <AlertCircle className="size-3.5 shrink-0" />
          <span className="truncate">
            {t(($) => $.offline_banner.unstable, { name })}
          </span>
        </div>
      </div>
    );
  }
  return (
    <div className={cn(CHAT_GUTTER, "mb-1.5")}>
      <div className={cn(CHAT_COLUMN, "flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-caption bg-muted text-muted-foreground ring-1 ring-border")}>
        <WifiOff className="size-3.5 shrink-0" />
        <span className="truncate">
          {t(($) => $.offline_banner.offline, { name })}
        </span>
      </div>
    </div>
  );
}
