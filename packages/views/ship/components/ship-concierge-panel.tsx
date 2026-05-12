"use client";

import { useMemo, useState } from "react";
import { Bot, MessageCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { channelsListOptions } from "@multica/core/channels";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@multica/ui/components/ui/sheet";
import { ChannelMessageList } from "../../channels/components/channel-message-list";
import { ChannelComposer } from "../../channels/components/channel-composer";
import { ShipConciergeSetupDialog } from "./ship-concierge-setup-dialog";

/**
 * ROA-178 Ship Concierge — a slide-in drawer on the Ship page that
 * surfaces the workspace's designated Concierge channel (the channel
 * whose `ambient_listener_agent_id` is non-null). Inside, the user
 * can chat directly with Claude about deploy / release state and
 * Claude will respond using the Ship MCP tools.
 *
 * Why a drawer (Sheet) rather than an inline panel:
 *   * The existing Ship page is "one big scroll surface" of project
 *     Kanbans; injecting a chat pane inline would force a layout
 *     decision (right rail vs top section vs bottom) that doesn't
 *     scale to varied screen widths.
 *   * A toggle-able drawer works the same on desktop + mobile.
 *   * The user is here to look at PRs first; the Concierge is
 *     opt-in conversation, not always-on noise.
 *
 * Empty state: workspace has no channel with an ambient listener yet
 * → drawer shows a setup recipe (the operator hits the PATCH
 * /api/channels/{id}/ambient_listener endpoint with the Claude agent
 * UUID; once that lands, the next channels-list refresh surfaces the
 * channel here automatically).
 */
export function ShipConciergePanel() {
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);

  // Channels list is cached at `staleTime: Infinity` — opening the
  // drawer never adds a round-trip on subsequent visits. WS events
  // invalidate the cache when the operator changes the ambient
  // listener designation.
  const { data: channels = [] } = useQuery(channelsListOptions(wsId, true));

  // Find the workspace's Concierge channel. Convention: at most one
  // channel per workspace has `ambient_listener_agent_id` set. If
  // multiple (future), we pick the first — operator can clean up
  // duplicates by clearing the others.
  const concierge = useMemo(() => {
    return (channels ?? []).find((c) => c.ambient_listener_agent_id !== null);
  }, [channels]);

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1.5 text-xs"
            data-testid="ship-concierge-toggle"
          >
            <Bot className="size-3.5" />
            Concierge
            {concierge && concierge.unread_count > 0 && (
              <span className="ml-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-medium text-primary-foreground">
                {concierge.unread_count}
              </span>
            )}
          </Button>
        }
      />

      <SheetContent
        side="right"
        // Wider than the default Sheet so the channel chat has room
        // for code blocks (Claude's typical Ship-state responses
        // include SHAs, run URLs, release UUIDs). Capped at 540 so
        // it doesn't dominate the Ship page on narrow laptops.
        className="flex w-full max-w-[540px] flex-col gap-0 p-0 sm:max-w-[540px]"
        data-testid="ship-concierge-sheet"
      >
        <SheetHeader className="border-b px-4 py-3">
          <SheetTitle className="flex items-center gap-2 text-sm">
            <Bot className="size-4 text-muted-foreground" />
            {concierge ? concierge.display_name || concierge.name : "Ship Concierge"}
          </SheetTitle>
        </SheetHeader>

        {concierge ? (
          <div className="flex min-h-0 flex-1 flex-col">
            <ChannelMessageList
              channelId={concierge.id}
              enabled={open}
            />
            <ChannelComposer channel={concierge} />
          </div>
        ) : (
          <ConciergeEmptyState />
        )}
      </SheetContent>
    </Sheet>
  );
}

/**
 * Empty state — no channel in the workspace has an ambient_listener
 * configured yet. The button opens a setup dialog that handles the
 * three-step wiring (create channel, add agent member, designate
 * ambient listener) atomically. No copy-paste curl required.
 */
function ConciergeEmptyState() {
  const [setupOpen, setSetupOpen] = useState(false);

  return (
    <div className="flex flex-1 flex-col items-start gap-4 overflow-y-auto p-5 text-sm">
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2 font-medium text-foreground">
          <MessageCircle className="size-4 text-muted-foreground" />
          No Concierge channel configured yet
        </div>
        <p className="text-muted-foreground">
          The Ship Concierge is a channel where you can talk to Claude
          naturally — no @-mention required — and Claude answers using
          the Ship Hub tools (release state, deploy diagnostics,
          merge-train control, etc.).
        </p>
      </div>

      <Button
        size="sm"
        onClick={() => setSetupOpen(true)}
        data-testid="ship-concierge-setup-open"
      >
        <Bot className="size-3.5" />
        Set up Concierge
      </Button>

      <p className="text-xs text-muted-foreground">
        Picks a workspace agent + creates the channel + wires the
        ambient-listener designation in one go.
      </p>

      <ShipConciergeSetupDialog open={setupOpen} onOpenChange={setSetupOpen} />
    </div>
  );
}
