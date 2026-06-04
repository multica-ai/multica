"use client";

import { Bot } from "lucide-react";
import { useActorName } from "@multica/core/workspace/hooks";
import type { InboxItem } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useRunPrivateAgentRequest } from "../mutations";
import { useCerebroInboxStrings } from "../strings";

/**
 * FIR-2385 — the owner-facing row for a `private_agent_run_request`. A member
 * tagged a private agent they don't own; the agent was NOT woken. Instead the
 * owner sees this row with a Run button that enqueues the agent on the tagged
 * comment — the owner stays in control.
 *
 * Returns null for every other inbox type, so non run-request rows keep their
 * original height. The Run button stops propagation so clicking it doesn't also
 * navigate the row to the issue.
 *
 * FIR-37 — on the mobile PWA the row also mounts CerebroInboxRowActions, whose
 * swipe surface is a transparent `absolute inset-0` overlay rendered AFTER this
 * row in the DOM. Without a stacking offset that overlay paints on top of the
 * Run button and swallows the tap, so "Run" was untappable on mobile (desktop
 * is unaffected — the overlay is `sm:hidden`). `relative z-10` (scoped to
 * mobile via `sm:z-auto`, since the overlay only exists below `sm`) lifts this
 * row above the overlay so the tap reaches the button; on desktop it stays
 * `z-auto` so the hover action icons keep painting above it. The swipe overlay
 * still covers the rest of the row, so swipe-to-archive keeps working.
 */
export function CerebroInboxRunRequestRow({ item }: { item: InboxItem }) {
  const strings = useCerebroInboxStrings();
  const { getActorName } = useActorName();
  const run = useRunPrivateAgentRequest();

  if (item.type !== "private_agent_run_request") return null;

  const requesterId = item.details?.requester_id ?? item.actor_id;
  const requester = requesterId ? getActorName("member", requesterId) : "";

  return (
    <div className="relative z-10 mt-1 flex items-center justify-between gap-2 sm:z-auto">
      <span className="inline-flex min-w-0 items-center gap-1 text-xs text-muted-foreground">
        <span className="inline-flex shrink-0 items-center gap-1 rounded-sm bg-brand/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase leading-none tracking-normal text-brand">
          <Bot className="size-3" aria-hidden />
          {strings.run_request_label}
        </span>
        {requester && (
          <span className="truncate">{`${strings.run_request_by_prefix} ${requester}`}</span>
        )}
      </span>
      <Button
        size="sm"
        className="h-6 shrink-0 px-2 text-xs"
        disabled={run.isPending}
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
          run.mutate(item.id);
        }}
      >
        {run.isPending ? strings.run_request_running : strings.run_request_run}
      </Button>
    </div>
  );
}
