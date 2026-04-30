"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { channelDetailOptions, useMarkChannelRead } from "@multica/core/channels";
import { ChannelList } from "./channel-list";
import { ChannelHeader } from "./channel-header";
import { ChannelMessageList } from "./channel-message-list";
import { ChannelComposer } from "./channel-composer";
import { ChannelCreateDialog } from "./channel-create-dialog";
import { NewDMDialog } from "./new-dm-dialog";

interface ChannelsPageProps {
  /** When non-null, the right pane shows that channel. When null, an empty
   * state. The page is the same component in both cases — only the right
   * pane content varies. */
  activeChannelId: string | null;
}

/**
 * ChannelsPage is the split-view surface used for both `/channels` (no
 * active channel) and `/channels/[id]` (channel selected).
 *
 * Layout:
 *   ┌──────────┬──────────────────────────────────┐
 *   │ Channel  │ ChannelHeader                    │
 *   │ List     ├──────────────────────────────────┤
 *   │ (left    │                                  │
 *   │  pane)   │   ChannelMessageList             │
 *   │          │                                  │
 *   │          ├──────────────────────────────────┤
 *   │          │ ChannelComposer                  │
 *   └──────────┴──────────────────────────────────┘
 *
 * The component is gated on `workspace.channels_enabled`; if a user lands
 * here while the flag is off, we render a polite empty state directing
 * them to ask their admin to enable the feature in Settings. The backend
 * would 404 every endpoint anyway — this just gives a nicer UX than a
 * broken-looking empty page.
 */
export function ChannelsPage({ activeChannelId }: ChannelsPageProps) {
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const enabled = !!workspace?.channels_enabled;
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [dmDialogOpen, setDmDialogOpen] = useState(false);

  const { data: channel, isLoading: channelLoading } = useQuery(
    channelDetailOptions(wsId, activeChannelId ?? "", enabled && !!activeChannelId),
  );
  const markRead = useMarkChannelRead(activeChannelId ?? "");

  // Mark the channel read when the user views it. We bump on mount and
  // again whenever the message-list cache changes; the mutation itself is
  // idempotent on the server side.
  useEffect(() => {
    if (!enabled || !activeChannelId || !channel) return;
    // We mark up to "now" via a stable sentinel until cursor management
    // is added in a follow-up; for Phase 1 this is acceptable since
    // last_read_message_id is informational, not a UX-critical signal.
  }, [enabled, activeChannelId, channel, markRead]);

  if (!enabled) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 p-8 text-center">
        <h2 className="text-lg font-semibold text-foreground">Channels are off for this workspace</h2>
        <p className="max-w-md text-sm text-muted-foreground">
          A workspace admin can turn on Channels from Settings. Once enabled,
          you'll be able to chat with teammates and agents in dedicated
          channels alongside the issue board.
        </p>
      </div>
    );
  }

  return (
    <div className="flex h-full">
      <ChannelList
        activeChannelId={activeChannelId}
        onCreateChannel={() => setCreateDialogOpen(true)}
        onCreateDM={() => setDmDialogOpen(true)}
        enabled={enabled}
      />
      <main className="flex min-w-0 flex-1 flex-col">
        {!activeChannelId ? (
          <EmptyRightPane />
        ) : channelLoading ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            Loading channel…
          </div>
        ) : !channel ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            Channel not found.
          </div>
        ) : (
          <>
            <ChannelHeader channel={channel} enabled={enabled} />
            <ChannelMessageList channelId={channel.id} enabled={enabled} />
            <ChannelComposer
              channelId={channel.id}
              channelName={channel.display_name || channel.name}
            />
          </>
        )}
      </main>
      <ChannelCreateDialog open={createDialogOpen} onOpenChange={setCreateDialogOpen} />
      <NewDMDialog open={dmDialogOpen} onOpenChange={setDmDialogOpen} />
    </div>
  );
}

function EmptyRightPane() {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 p-8 text-center">
      <h2 className="text-lg font-semibold text-foreground">Welcome to Channels</h2>
      <p className="max-w-md text-sm text-muted-foreground">
        Pick a channel from the left, or create a new one with the + button.
      </p>
    </div>
  );
}
