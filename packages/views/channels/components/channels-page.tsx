"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useDefaultLayout } from "react-resizable-panels";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@multica/ui/components/ui/resizable";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  channelDetailOptions,
  channelMessagesOptions,
  useMarkChannelRead,
} from "@multica/core/channels";
import { ChannelList } from "./channel-list";
import { ChannelHeader } from "./channel-header";
import { ChannelMessageList } from "./channel-message-list";
import { ChannelComposer } from "./channel-composer";
import { ChannelCreateDialog } from "./channel-create-dialog";
import { NewDMDialog } from "./new-dm-dialog";
import { ThreadPanel } from "./thread-panel";

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
  // Phase 4 — open-thread state. Cleared when the user navigates to a
  // different channel so we don't accidentally show a stale thread side
  // panel from the previous channel.
  const [threadParentId, setThreadParentId] = useState<string | null>(null);
  useEffect(() => {
    setThreadParentId(null);
  }, [activeChannelId]);

  // Persist the thread vs. messages split between sessions. Same hook the
  // inbox / project pages use; layouts are stored in localStorage keyed
  // by `id`.
  const { defaultLayout: threadLayout, onLayoutChanged: onThreadLayoutChanged } =
    useDefaultLayout({ id: "multica_channels_thread_layout" });

  const { data: channel, isLoading: channelLoading } = useQuery(
    channelDetailOptions(wsId, activeChannelId ?? "", enabled && !!activeChannelId),
  );
  const { data: messages = [] } = useQuery(
    channelMessagesOptions(activeChannelId ?? "", enabled && !!activeChannelId),
  );
  const markRead = useMarkChannelRead(activeChannelId ?? "");

  // Mark-read on view: whenever the newest message id changes (mount, new
  // arrival, channel switch) we POST /channels/<id>/read with that id. We
  // track the last id we POSTed so a re-render with the same data is a
  // no-op. Optimistic messages (id starts with "optimistic-") are
  // skipped — we only persist canonical server ids.
  //
  // The list query returns newest-first so messages[0] is the latest.
  const lastSentRef = useRef<string | null>(null);
  useEffect(() => {
    if (!enabled || !activeChannelId || messages.length === 0) return;
    const newest = messages[0];
    if (!newest || newest.id.startsWith("optimistic-")) return;
    const key = `${activeChannelId}:${newest.id}`;
    if (lastSentRef.current === key) return;
    lastSentRef.current = key;
    markRead.mutate(newest.id);
  }, [enabled, activeChannelId, messages, markRead]);

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
            {threadParentId ? (
              // Resizable split when a thread is open. Drag the divider to
              // give more room to either pane; the layout is persisted in
              // localStorage so it survives page reloads. Without a thread
              // open we render the messages pane full-width — there's no
              // second pane to size against.
              <ResizablePanelGroup
                orientation="horizontal"
                className="flex-1 min-h-0"
                defaultLayout={threadLayout}
                onLayoutChanged={onThreadLayoutChanged}
              >
                <ResizablePanel id="messages" minSize="35%">
                  <div className="flex h-full min-w-0 flex-col">
                    <ChannelMessageList
                      channelId={channel.id}
                      enabled={enabled}
                      onOpenThread={setThreadParentId}
                    />
                    <ChannelComposer channel={channel} />
                  </div>
                </ResizablePanel>
                <ResizableHandle />
                <ResizablePanel id="thread" defaultSize={420} minSize={300} maxSize={720} groupResizeBehavior="preserve-pixel-size">
                  <ThreadPanel
                    channelId={channel.id}
                    parentMessageId={threadParentId}
                    onClose={() => setThreadParentId(null)}
                    enabled={enabled}
                  />
                </ResizablePanel>
              </ResizablePanelGroup>
            ) : (
              <div className="flex min-h-0 flex-1 flex-col">
                <ChannelMessageList
                  channelId={channel.id}
                  enabled={enabled}
                  onOpenThread={setThreadParentId}
                />
                <ChannelComposer channel={channel} />
              </div>
            )}
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
