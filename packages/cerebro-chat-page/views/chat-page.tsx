// FIR-4350 — the Chat page: conversation rail on the left, the open
// conversation on the right.
//
// This page builds nothing new. The rail is the existing SlackBlock (the same
// component the dynamic inbox renders as its "Chat" section), and the detail
// pane is ChannelDetail for a channel/DM and InboxChatPanel for an agent chat —
// the same two panes both inbox implementations use. All this file owns is the
// two-pane shell and which conversation is selected.
"use client";

import { useCallback, useState } from "react";
import { MessageSquare } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import type { Channel } from "@multica/core/types";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@multica/ui/components/ui/resizable";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { SlackBlock } from "@multica/cerebro-inbox-slack-block";
import { ChannelDetail } from "@multica/views/channels";
import { InboxChatPanel } from "@multica/views/inbox/components/inbox-list-item";
import {
  ChatPlacementSettings,
  showsInChat,
  useChatPlacement,
  useFeatureFlag,
} from "@multica/cerebro-feature-flags";

/** What the detail pane is currently showing. */
type Selection =
  | { kind: "channel"; channel: Channel }
  | { kind: "chat"; sessionId: string | null; agentId?: string };

export function ChatPage() {
  // The route itself is not gated, so a user who has the feature off must not
  // reach the page by typing the URL.
  const enabled = useFeatureFlag("cerebro_chat_page");
  const wsId = useWorkspaceId();
  const isMobile = useIsMobile();
  const { placement } = useChatPlacement();
  const [selection, setSelection] = useState<Selection | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);

  // FIR-4350 — reuse the sidebar's new-conversation modal so the "+" on the
  // Chat page starts a channel/DM/agent chat exactly like "New message" does.
  const openCreate = useCallback(() => useModalStore.getState().open("new-message"), []);

  const clearSelection = useCallback(() => setSelection(null), []);

  const openChannel = useCallback((channel: Channel) => {
    setSelection({ kind: "channel", channel });
  }, []);
  const openAgentChat = useCallback((agentId: string) => {
    setSelection({ kind: "chat", sessionId: null, agentId });
  }, []);
  const openAgentSession = useCallback((sessionId: string) => {
    setSelection({ kind: "chat", sessionId });
  }, []);

  // The rail is a settings-driven view of the same conversation set the inbox
  // has: a type the user has not placed in Chat is not listed here.
  const showChannels = showsInChat(placement, "channel");
  const showPeople = showsInChat(placement, "dm");
  const showAgents = showsInChat(placement, "agent_chat");
  const nothingInChat = !showChannels && !showPeople && !showAgents;

  const rail = nothingInChat ? (
    <EmptyRail onOpenSettings={() => setSettingsOpen(true)} />
  ) : (
    <SlackBlock
      wsId={wsId}
      selectedChannelId={selection?.kind === "channel" ? selection.channel.id : null}
      onOpenChannel={openChannel}
      onOpenAgentChat={openAgentChat}
      onOpenAgentSession={openAgentSession}
      limit={0}
      onSetLimit={NOOP_NUMBER}
      sort="recent"
      onSetSort={NOOP}
      unreadFirst
      onSetUnreadFirst={NOOP}
      groupBy="type"
      onSetGroupBy={NOOP}
      showChannels={showChannels}
      showPeople={showPeople}
      showAgents={showAgents}
      onSetShowAgents={NOOP}
      searchDefaultOpen
      onSetSearchDefaultOpen={NOOP}
      showSectionControls={false}
      onCreate={openCreate}
      onOpenSettings={() => setSettingsOpen(true)}
      onRemove={NOOP}
    />
  );

  const detail = renderDetail(selection, clearSelection, openAgentSession);

  if (!enabled) return null;

  // FIR-4350 — the placement matrix on the page itself, so a user can move a
  // conversation type into or out of Chat without leaving for the Settings tab.
  const settingsDialog = (
    <Dialog open={settingsOpen} onOpenChange={setSettingsOpen}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Chat settings</DialogTitle>
        </DialogHeader>
        <ChatPlacementSettings />
      </DialogContent>
    </Dialog>
  );

  if (isMobile) {
    // Single column: the rail until something is picked, then the conversation.
    return (
      <div className="flex h-full min-h-0 flex-col">
        {settingsDialog}
        {selection ? (
          <div className="flex min-h-0 flex-1 flex-col">
            <button
              type="button"
              onClick={clearSelection}
              className="flex shrink-0 items-center gap-1.5 px-3 py-2 text-sm text-muted-foreground hover:bg-muted"
            >
              Back
            </button>
            <div className="flex min-h-0 flex-1 flex-col">{detail}</div>
          </div>
        ) : (
          <div className="min-h-0 flex-1 overflow-y-auto">{rail}</div>
        )}
      </div>
    );
  }

  return (
    <>
      {settingsDialog}
      <ResizablePanelGroup orientation="horizontal">
        <ResizablePanel id="chat-rail" defaultSize={340} minSize={260} className="flex flex-col">
          <div className="min-h-0 flex-1 overflow-y-auto">{rail}</div>
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel id="chat-detail" minSize="40%" className="flex flex-col">
          {detail ?? <EmptyDetail />}
        </ResizablePanel>
      </ResizablePanelGroup>
    </>
  );
}

function renderDetail(
  selection: Selection | null,
  onClose: () => void,
  onSessionCreated: (sessionId: string) => void,
) {
  if (!selection) return null;
  if (selection.kind === "chat") {
    return (
      <InboxChatPanel
        key={selection.sessionId ?? `new:${selection.agentId ?? ""}`}
        sessionId={selection.sessionId}
        initialAgentId={selection.agentId ?? null}
        // FIR-4350 — keep the created session so the next send continues it
        // instead of opening a second chat (and so clicking a recent chat from
        // the new-chat state works).
        onSessionCreated={onSessionCreated}
      />
    );
  }
  return (
    <ChannelDetail
      key={selection.channel.id}
      channelId={selection.channel.id}
      initialChannel={selection.channel}
      onArchive={onClose}
    />
  );
}

function EmptyRail({ onOpenSettings }: { onOpenSettings: () => void }) {
  // Reached only when the user has moved every conversation type to Inbox-only,
  // so nothing is placed in Chat. Offer the placement settings right here rather
  // than showing a blank column or sending the user hunting in the Settings tab.
  return (
    <div className="flex h-full flex-col items-center justify-center px-6 text-center text-muted-foreground">
      <MessageSquare className="mb-3 h-10 w-10 text-muted-foreground/30" />
      <p className="text-sm">Nothing is placed in Chat yet.</p>
      <p className="mt-1 text-xs">Choose which conversations live here.</p>
      <button
        type="button"
        onClick={onOpenSettings}
        className="mt-3 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted"
      >
        Chat settings
      </button>
    </div>
  );
}

function EmptyDetail() {
  return (
    <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
      <MessageSquare className="mb-3 h-10 w-10 text-muted-foreground/30" />
      <p className="text-sm">Select a conversation to read it here.</p>
    </div>
  );
}

// SlackBlock takes its display options as controlled props. The Chat page fixes
// them instead of persisting a second layout, so the setters are inert.
const NOOP = () => {};
const NOOP_NUMBER = (_n: number) => {};
