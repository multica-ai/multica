"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useDefaultLayout } from "react-resizable-panels";
import { ArrowLeft } from "lucide-react";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@multica/ui/components/ui/resizable";
import { Button } from "@multica/ui/components/ui/button";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useWorkspaceId } from "@multica/core/hooks";
import { paths, useCurrentWorkspace, useRequiredWorkspaceSlug } from "@multica/core/paths";
import {
  channelDetailOptions,
  channelMessagesOptions,
  channelsListOptions,
  useMarkChannelRead,
} from "@multica/core/channels";
import { ChannelList } from "./channel-list";
import { ChannelHeader } from "./channel-header";
import { ChannelMessageList } from "./channel-message-list";
import { ChannelComposer } from "./channel-composer";
import { ChannelCreateDialog } from "./channel-create-dialog";
import { NewDMDialog } from "./new-dm-dialog";
import { ThreadPanel } from "./thread-panel";
import { useT } from "../../i18n";
import { PageHeader } from "../../layout/page-header";
import { useNavigation } from "../../navigation";

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
  const { t } = useT("channels");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const slug = useRequiredWorkspaceSlug();
  const navigation = useNavigation();
  const isMobile = useIsMobile();
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
  // The list response carries per-channel unread state. We freeze the
  // last_read_message_id captured at the moment the user opened this
  // channel so the unread divider doesn't jump as we mark-read.
  const { data: channelList = [] } = useQuery(channelsListOptions(wsId, enabled));
  const initialUnreadCursorRef = useRef<{ channelId: string | null; cursor: string | null }>({
    channelId: null,
    cursor: null,
  });
  const initialUnreadCursor = useMemo(() => {
    if (!activeChannelId) return null;
    if (initialUnreadCursorRef.current.channelId === activeChannelId) {
      return initialUnreadCursorRef.current.cursor;
    }
    const fromList = channelList.find((c) => c.id === activeChannelId);
    if (!fromList) return initialUnreadCursorRef.current.cursor;
    initialUnreadCursorRef.current = {
      channelId: activeChannelId,
      cursor: fromList.last_read_message_id,
    };
    return fromList.last_read_message_id;
  }, [activeChannelId, channelList]);

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
        <h2 className="text-lg font-semibold text-foreground">{t(($) => $.page.disabled_title)}</h2>
        <p className="max-w-md text-sm text-muted-foreground">
          {t(($) => $.page.disabled_description)}
        </p>
      </div>
    );
  }

  const dialogs = (
    <>
      <ChannelCreateDialog open={createDialogOpen} onOpenChange={setCreateDialogOpen} />
      <NewDMDialog open={dmDialogOpen} onOpenChange={setDmDialogOpen} />
    </>
  );

  if (isMobile) {
    if (!activeChannelId) {
      return (
        <div className="flex h-full min-h-0 flex-col">
          <PageHeader className="justify-between">
            <h1 className="text-sm font-semibold">{t(($) => $.list.section_title)}</h1>
          </PageHeader>
          <div className="min-h-0 flex-1 [&_aside]:h-full [&_aside]:w-full [&_aside]:border-r-0">
            <ChannelList
              activeChannelId={activeChannelId}
              onCreateChannel={() => setCreateDialogOpen(true)}
              onCreateDM={() => setDmDialogOpen(true)}
              enabled={enabled}
            />
          </div>
          {dialogs}
        </div>
      );
    }

    if (channelLoading) {
      return (
        <div className="flex h-full min-h-0 flex-col">
          <MobileBackHeader onBack={() => navigation.push(paths.workspace(slug).channels())} />
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            {t(($) => $.page.loading_channel)}
          </div>
          {dialogs}
        </div>
      );
    }

    if (!channel) {
      return (
        <div className="flex h-full min-h-0 flex-col">
          <MobileBackHeader onBack={() => navigation.push(paths.workspace(slug).channels())} />
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            {t(($) => $.page.channel_not_found)}
          </div>
          {dialogs}
        </div>
      );
    }

    if (threadParentId) {
      return (
        <div className="flex h-full min-h-0 flex-col">
          <MobileBackHeader onBack={() => setThreadParentId(null)} />
          <ThreadPanel
            channelId={channel.id}
            parentMessageId={threadParentId}
            onClose={() => setThreadParentId(null)}
            enabled={enabled}
          />
          {dialogs}
        </div>
      );
    }

    return (
      <div className="flex h-full min-h-0 flex-col">
        <MobileBackHeader onBack={() => navigation.push(paths.workspace(slug).channels())} />
        <ChannelHeader channel={channel} enabled={enabled} />
        <div className="flex min-h-0 flex-1 flex-col">
          <ChannelMessageList
            channelId={channel.id}
            enabled={enabled}
            onOpenThread={setThreadParentId}
            initialUnreadCursor={initialUnreadCursor}
          />
          <ChannelComposer channel={channel} />
        </div>
        {dialogs}
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
            {t(($) => $.page.loading_channel)}
          </div>
        ) : !channel ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            {t(($) => $.page.channel_not_found)}
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
                      initialUnreadCursor={initialUnreadCursor}
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
                  initialUnreadCursor={initialUnreadCursor}
                />
                <ChannelComposer channel={channel} />
              </div>
            )}
          </>
        )}
      </main>
      {dialogs}
    </div>
  );
}

function MobileBackHeader({ onBack }: { onBack: () => void }) {
  const { t } = useT("channels");
  return (
    <div className="flex h-12 shrink-0 items-center border-b px-2">
      <Button
        variant="ghost"
        size="sm"
        onClick={onBack}
        className="gap-1.5 text-muted-foreground"
      >
        <ArrowLeft className="h-4 w-4" />
        {t(($) => $.page.back)}
      </Button>
    </div>
  );
}

function EmptyRightPane() {
  const { t } = useT("channels");
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 p-8 text-center">
      <h2 className="text-lg font-semibold text-foreground">{t(($) => $.page.welcome_title)}</h2>
      <p className="max-w-md text-sm text-muted-foreground">
        {t(($) => $.page.welcome_description)}
      </p>
    </div>
  );
}
