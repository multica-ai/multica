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
import { ArrowLeft, MessageSquare } from "lucide-react";
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
import { Button } from "@multica/ui/components/ui/button";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { SlackBlock } from "@multica/cerebro-inbox-slack-block";
import { ChannelDetail } from "@multica/views/channels";
import { InboxChatPanel } from "@multica/views/inbox/components/inbox-list-item";
import { PageHeader } from "@multica/views/layout/page-header";
import {
  ChatPlacementSettings,
  showsInChat,
  useChatPlacement,
  useChatPageSettings,
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
  // FIR-4350 — the Chat page's own display options (rows per box, grouping,
  // sort, unread-first, search-by-default), persisted per user. Same controls
  // the Inbox's Chat box has; the Chat page keeps its own copy.
  const { settings, setSettings } = useChatPageSettings();
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
      limit={settings.limit}
      onSetLimit={(limit) => setSettings({ limit })}
      sort={settings.sort}
      onSetSort={(sort) => setSettings({ sort })}
      unreadFirst={settings.unreadFirst}
      onSetUnreadFirst={(unreadFirst) => setSettings({ unreadFirst })}
      groupBy={settings.groupBy}
      onSetGroupBy={(groupBy) => setSettings({ groupBy })}
      showChannels={showChannels}
      showPeople={showPeople}
      showAgents={showAgents}
      onSetShowAgents={NOOP}
      searchDefaultOpen={settings.searchDefaultOpen}
      onSetSearchDefaultOpen={(searchDefaultOpen) => setSettings({ searchDefaultOpen })}
      // Display settings on; but no removable block here, and agents are
      // inbox-only so their toggle is hidden.
      showSectionControls
      showRemove={false}
      showAgentsToggle={false}
      // FIR-4350 — the rail IS this page's left column, not a card inside the
      // inbox: no rounded border, no drag-handle inset, and the control header
      // stays pinned while the list scrolls under it.
      flush
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
          {/* Same name as the menu item that opens it ("Chat placement…"), so
              one control is not called two different things. */}
          <DialogTitle>Chat placement</DialogTitle>
        </DialogHeader>
        <ChatPlacementSettings />
      </DialogContent>
    </Dialog>
  );

  // FIR-4350 — the page's own chrome. Every other page in the app owns a
  // PageHeader (classic inbox) or an equivalent bar (dynamic inbox); without one
  // the Chat page opened into a bare split with nothing naming it. PageHeader
  // also carries the mobile hamburger, so Chat is not a dead end on a phone.
  const header = (
    <PageHeader>
      <h1 className="truncate text-sm font-semibold">Chat</h1>
    </PageHeader>
  );

  if (isMobile) {
    // Single column: the rail until something is picked, then the conversation.
    return (
      <div className="flex h-full min-h-0 flex-col">
        {settingsDialog}
        {selection ? (
          <div className="flex min-h-0 flex-1 flex-col">
            {/* Same PageHeader as the list, so the bar does not change height,
                background or padding when a conversation opens. */}
            <PageHeader>
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground"
                onClick={clearSelection}
                aria-label="Back to conversations"
                title="Back to conversations"
              >
                <ArrowLeft className="h-4 w-4" />
              </Button>
            </PageHeader>
            <div className="flex min-h-0 flex-1 flex-col">{detail}</div>
          </div>
        ) : (
          <div className="flex min-h-0 flex-1 flex-col">
            {header}
            <div className="flex min-h-0 flex-1 flex-col">{rail}</div>
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      {settingsDialog}
      {header}
      <ResizablePanelGroup orientation="horizontal" className="min-h-0 flex-1">
        <ResizablePanel id="chat-rail" defaultSize={340} minSize={260} className="flex flex-col">
          <div className="flex min-h-0 flex-1 flex-col">{rail}</div>
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel id="chat-detail" minSize="40%" className="flex flex-col">
          {detail ?? <EmptyDetail />}
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
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
        Chat placement
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

// The Chat page has no removable block and keeps agents inbox-only, so the
// remove / show-agents setters stay inert.
const NOOP = () => {};
