"use client";

import { AlertCircle, WifiOff } from "lucide-react";
import { useT } from "@multica/i18n/react";
import type { AgentAvailability } from "@multica/core/agents";

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
//
// Sits outside the input card (sibling of ChatInput) so the hint reads as
// a session-level signal rather than per-message context. The outer wrapper
// (`px-5`) and the inner container (`mx-auto max-w-4xl`) mirror ChatInput's
// own layout so the banner's edges line up with the input box on every
// viewport size — without `max-w-4xl` the banner stretches wider than the
// input on large screens and looks "loose".
export function OfflineBanner({ agentName, availability }: Props) {
  const t = useT("chat");

  if (availability !== "offline" && availability !== "unstable") return null;

  const name = agentName?.trim() || t("offline_agent_fallback");
  if (availability === "unstable") {
    return (
      <div className="px-5 mb-1.5">
        <div className="mx-auto flex w-full max-w-4xl items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs bg-amber-50 dark:bg-amber-950/40 text-amber-900 dark:text-amber-200 ring-1 ring-amber-200/60 dark:ring-amber-900/40">
          <AlertCircle className="size-3.5 shrink-0" />
          <span className="truncate">
            {t("offline_unstable", { name })}
          </span>
        </div>
      </div>
    );
  }
  return (
    <div className="px-5 mb-1.5">
      <div className="mx-auto flex w-full max-w-4xl items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs bg-muted text-muted-foreground ring-1 ring-border">
        <WifiOff className="size-3.5 shrink-0" />
        <span className="truncate">
          {t("offline_offline", { name })}
        </span>
      </div>
    </div>
  );
}
