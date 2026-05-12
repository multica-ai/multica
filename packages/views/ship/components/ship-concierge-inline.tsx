"use client";

import { useMemo, useState } from "react";
import { Bot, ChevronDown, ChevronUp } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { channelsListOptions } from "@multica/core/channels";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { ChannelMessageList } from "../../channels/components/channel-message-list";
import { ChannelComposer } from "../../channels/components/channel-composer";

/**
 * ROA-178 Ship Concierge — inline panel at the top of the Ship page.
 * Complements (does not replace) the drawer in the header:
 *
 *   - Inline panel (this component): ambient visibility — always
 *     mounted above the active releases rail when the workspace has
 *     a Concierge channel configured. Click chevron to expand for
 *     more breathing room.
 *   - Drawer (`ship-concierge-panel.tsx`): focused mode, dedicates
 *     the whole right side to the chat. Better for back-and-forth
 *     conversation when the Ship page itself isn't the user's focus.
 *
 * Both surfaces target the same channel; the user can read in one
 * and reply in the other without conflict — WS events keep both
 * synchronized.
 *
 * Hidden entirely when no Concierge channel is configured (no point
 * occupying real estate to advertise an empty state when the drawer
 * already carries the setup recipe).
 */
export function ShipConciergeInline() {
  const wsId = useWorkspaceId();
  const [expanded, setExpanded] = useState(false);

  const { data: channels = [] } = useQuery(channelsListOptions(wsId, true));
  const concierge = useMemo(() => {
    return (channels ?? []).find((c) => c.ambient_listener_agent_id !== null);
  }, [channels]);

  // No concierge configured → nothing to render. The header drawer
  // button carries the empty-state setup recipe; we don't need a
  // second copy of it at the top of the page.
  if (!concierge) {
    return null;
  }

  return (
    <div
      className={cn(
        "rounded-md border bg-card transition-all",
        // Heights chosen to fit ~3 messages compact, ~7-8 expanded.
        // Capped so the inline panel never dominates the Ship page —
        // if the user wants the chat bigger, the drawer is right
        // there in the header.
        expanded ? "max-h-[520px]" : "max-h-[260px]",
      )}
      data-testid="ship-concierge-inline"
      data-expanded={expanded}
    >
      <div className="flex items-center justify-between border-b px-3 py-2">
        <div className="flex items-center gap-2 text-sm">
          <Bot className="size-4 text-muted-foreground" />
          <span className="font-medium">
            {concierge.display_name || concierge.name}
          </span>
          {concierge.unread_count > 0 && (
            <span className="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-medium text-primary-foreground">
              {concierge.unread_count}
            </span>
          )}
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 gap-1 px-2 text-xs text-muted-foreground"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
          data-testid="ship-concierge-inline-toggle"
        >
          {expanded ? (
            <>
              <ChevronUp className="size-3.5" />
              Collapse
            </>
          ) : (
            <>
              <ChevronDown className="size-3.5" />
              Expand
            </>
          )}
        </Button>
      </div>

      {/* Body: message list + composer. Height-driven layout — the
          outer container's max-height controls how much chat is
          visible; the message list scrolls inside it. */}
      <div
        className={cn(
          "flex flex-col",
          expanded ? "h-[472px]" : "h-[212px]",
        )}
      >
        <ChannelMessageList
          channelId={concierge.id}
          // Always enabled — this panel is meant to be ambient. The
          // drawer's `enabled={open}` gate stays as-is for that path;
          // the two are independent caches keyed by channelId anyway.
          enabled={true}
        />
        <ChannelComposer channel={concierge} />
      </div>
    </div>
  );
}
