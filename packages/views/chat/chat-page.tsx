"use client";

import { useEffect, useRef, useState } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { ArrowLeft, MessageSquare } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useWorkspacePaths } from "@multica/core/paths";
import { useChatStore } from "@multica/core/chat";
import type { Agent, ChatSession } from "@multica/core/types";
import { PageHeader } from "../layout/page-header";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { ChatMessageList, ChatMessageSkeleton } from "./components/chat-message-list";
import { ChatInput } from "./components/chat-input";
import { ChatThreadList } from "./components/chat-thread-list";
import { ChatSessionHeader } from "./components/chat-session-header";
import { EmptyState } from "./components/chat-empty-state";
import { NewChatButton } from "./components/new-chat-button";
import { useChatController } from "./components/use-chat-controller";
import { OfflineBanner } from "./components/offline-banner";
import { NoAgentBanner } from "./components/no-agent-banner";
import { ArchivedAgentBanner } from "./components/archived-agent-banner";

/**
 * Chat tab — the first-class two-pane surface (thread list on the left,
 * conversation on the right), mirroring the Inbox page layout. Shares all
 * conversation logic with the floating FAB via `useChatController`; the
 * left rail reuses `ChatThreadList`.
 *
 * Selection is URL-addressable via `?session=<id>` so a thread can be
 * deep-linked, opened from a notification, and survive refresh. The chat
 * store's `activeSessionId` stays the source of truth (both surfaces read
 * it); the URL is kept in sync in both directions. `?agent=<id>` is the
 * complementary one-shot deep link for a NEW chat: it starts a fresh compose
 * bound to that agent and is then stripped from the URL.
 *
 * Starting a chat is where the agent is chosen: the header ⊕ opens an agent
 * picker (see NewChatButton), so the compose box no longer needs its own
 * agent selector. Unlike the FAB, this page passes no `contextItems` to
 * `ChatInput`, so its `@` mentions fall back to manual search (issue-comment
 * style).
 */
export function ChatPage() {
  const { t } = useT("chat");
  const { searchParams, replace } = useNavigation();
  const wsPaths = useWorkspacePaths();
  const isMobile = useIsMobile();

  const c = useChatController({ isActive: true });
  const urlSession = searchParams.get("session") || null;
  const urlAgent = searchParams.get("agent") || null;

  // "Composing a brand-new chat" — the user hit ⊕ but hasn't sent yet, so no
  // session exists. On mobile this decides list-vs-conversation; on desktop the
  // conversation pane is always mounted so it only needs to reset itself once a
  // real session takes over.
  const [composingNew, setComposingNew] = useState(false);
  useEffect(() => {
    // Read the LIVE store value for the same reason as the session sync
    // effects below: under StrictMode's double-invoke this effect replays
    // with the render-captured snapshot, and a stale non-null session (a
    // persisted chat the URL→store effect already cleared) would revert the
    // composingNew=true that the later `?agent=` intent effect just set.
    if (useChatStore.getState().activeSessionId) setComposingNew(false);
  }, [c.activeSessionId]);

  // Two-way sync between the URL (`?session=`) and the chat store's
  // activeSessionId. Both effects read the LIVE store value via
  // `useChatStore.getState()` rather than the render-captured `c.activeSessionId`.
  // That is what keeps them from fighting on mount: a naive mirror effect fires
  // with the stale (null) snapshot and "corrects" the URL by stripping the
  // session before the URL→store effect has applied — breaking deep links and
  // making selection / new-chat feel unresponsive. Reading getState() sees the
  // value the sibling effect just wrote, so the reconciliation converges in one
  // pass and is idempotent under StrictMode's double-invoke.

  // URL → store: deep link, refresh, notification click, back/forward.
  useEffect(() => {
    if (urlSession !== useChatStore.getState().activeSessionId) {
      c.setActiveSession(urlSession);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- react to URL only
  }, [urlSession]);

  // store → URL: thread selection, "new chat", and sessions created by sending.
  useEffect(() => {
    const live = useChatStore.getState().activeSessionId;
    const current = searchParams.get("session") || null;
    if (live !== current) {
      const base = wsPaths.chat();
      replace(live ? `${base}?session=${live}` : base);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- react to store only
  }, [c.activeSessionId]);

  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_chat_layout",
  });

  // `?agent=` intent bookkeeping. The ref holds the param value already
  // consumed (or superseded) so the effect below fires at most once per deep
  // link — it also bridges the async window between replace() and the
  // searchParams actually dropping the param. Any explicit user action must
  // supersede a still-pending intent: agent/member queries can resolve late,
  // and a deferred intent firing after the user picked a thread (or started
  // another chat) would clobber that choice.
  const consumedAgentIntent = useRef<string | null>(null);
  const supersedeAgentIntent = () => {
    if (urlAgent) consumedAgentIntent.current = urlAgent;
  };

  const handleSelect = (session: ChatSession) => {
    supersedeAgentIntent();
    c.handleSelectSession(session);
    setComposingNew(false);
  };

  // Single archive path for both entry points (thread-list row + conversation
  // header). When the archived chat is the one in view, move the pane off it:
  // on desktop advance to the next chat (Inbox-style); on mobile drop back to
  // the list, which reads more naturally than being thrown into an unrelated
  // conversation full-screen. Archiving any other chat leaves the view put.
  const handleArchive = (session: ChatSession) => {
    supersedeAgentIntent();
    if (session.id === c.activeSessionId) {
      if (isMobile) {
        c.setActiveSession(null);
        setComposingNew(false);
      } else {
        c.advanceSelectionAfterArchive(session);
      }
    }
    c.archiveSession(session.id);
  };

  const startNewChat = (agent: Agent | null) => {
    // A manual ⊕ pick outranks a pending deep link; when called FROM the
    // intent effect the ref is already set to this param, so this is a no-op.
    supersedeAgentIntent();
    if (agent) c.handleStartNewChat(agent);
    else c.handleNewChat();
    setComposingNew(true);
  };

  // URL → new chat: `?agent=<id>` is the deep link used by "DM" entry points
  // (e.g. the agent detail page) to land on a fresh compose bound to that
  // agent. The permission-filtered agent list loads async, so the intent is
  // consumed on the render where the agent resolves, then the param is
  // stripped so refresh / the session sync above don't replay it. The ref
  // resets once the param is gone so a later identical deep link fires again.
  // A settled miss (access revoked, agent archived, bad id) is a denial: it
  // explains itself with a toast and consumes the intent so a later refetch
  // that surfaces the agent cannot start a chat without a fresh click. While
  // the queries are still loading the intent simply stays pending.
  useEffect(() => {
    if (!urlAgent) {
      consumedAgentIntent.current = null;
      return;
    }
    if (consumedAgentIntent.current === urlAgent) return;
    const agent = c.availableAgents.find((a) => a.id === urlAgent);
    if (agent) {
      consumedAgentIntent.current = urlAgent;
      startNewChat(agent);
      replace(wsPaths.chat());
      return;
    }
    if (c.agentsSettled) {
      consumedAgentIntent.current = urlAgent;
      toast.error(t(($) => $.page.agent_link_no_access));
      replace(wsPaths.chat());
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- consume when the URL param or the resolving agent list changes
  }, [urlAgent, c.availableAgents, c.agentsSettled]);

  const newChatButton = (
    <NewChatButton
      agents={c.availableAgents}
      userId={c.user?.id}
      onStart={startNewChat}
      side="bottom"
    />
  );

  const listHeader = (
    <PageHeader className="justify-between">
      <div className="flex items-center gap-2">
        <h1 className="text-sm font-semibold">{t(($) => $.page.title)}</h1>
      </div>
      {newChatButton}
    </PageHeader>
  );

  const listBody = (
    <div className="px-2 py-1">
      <ChatThreadList
        sessions={c.sessions}
        agents={c.agents}
        activeSessionId={c.activeSessionId}
        onSelectSession={handleSelect}
        onArchive={handleArchive}
      />
    </div>
  );

  // The conversation pane: message list / skeleton / empty above a persistent
  // banner + input. Identical composition to the floating window's body, so a
  // brand-new chat (no active session) shows the agent-aware empty state + input.
  // No compose-box agent selector — the agent is fixed when the chat starts.
  const conversation = (
    <div className="flex flex-1 flex-col min-h-0">
      {c.currentSession && (
        <ChatSessionHeader
          session={c.currentSession}
          agent={c.activeAgent}
          onArchive={handleArchive}
        />
      )}
      {c.showSkeleton ? (
        <ChatMessageSkeleton />
      ) : c.hasMessages ? (
        <ChatMessageList
          key={c.activeSessionId}
          messages={c.messages}
          pendingTask={c.pendingTask}
          availability={c.availability}
          firstItemIndex={c.firstItemIndex}
          hasOlderMessages={c.hasOlderMessages}
          isFetchingOlderMessages={c.isFetchingOlderMessages}
          onLoadOlderMessages={() => void c.fetchOlderMessages()}
        />
      ) : (
        <EmptyState agent={c.activeAgent} />
      )}

      {c.noAgent ? (
        <NoAgentBanner />
      ) : c.isAgentArchived ? (
        <ArchivedAgentBanner agentName={c.activeAgent?.name} />
      ) : (
        <OfflineBanner agentName={c.activeAgent?.name} availability={c.availability} />
      )}

      <ChatInput
        onSend={c.handleSend}
        restoreDraftRequest={c.restoreDraftRequest}
        onRestoreDraftApplied={c.handleRestoreDraftApplied}
        onUploadFile={c.handleUploadFile}
        onStop={c.handleStop}
        isRunning={!!c.pendingTaskId}
        disabled={c.isSessionArchived || c.isAgentArchived}
        noAgent={c.noAgent}
        agentArchived={c.isAgentArchived}
        agentName={c.activeAgent?.name}
        focusRequest={c.focusInputRequest}
      />
    </div>
  );

  // -- Mobile: list / conversation toggle -----------------------------------
  if (isMobile) {
    if (c.activeSessionId || composingNew) {
      return (
        <div className="flex flex-1 flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center border-b px-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                c.setActiveSession(null);
                setComposingNew(false);
              }}
              className="gap-1.5 text-muted-foreground"
            >
              <ArrowLeft className="h-4 w-4" />
              {t(($) => $.page.title)}
            </Button>
          </div>
          {conversation}
        </div>
      );
    }
    return (
      <div className="flex flex-1 flex-col min-h-0">
        {listHeader}
        <div className="flex-1 min-h-0 overflow-y-auto">{listBody}</div>
      </div>
    );
  }

  // -- Desktop: resizable two-panel. The conversation pane appears only once
  // there is a chat target — an open thread or a new chat whose agent was just
  // picked via ⊕. With nothing selected there is no agent, so we show a neutral
  // prompt instead of an orphaned compose box. -------------------------------
  const hasTarget = !!c.activeSessionId || composingNew;
  return (
    <ResizablePanelGroup
      orientation="horizontal"
      className="flex-1 min-h-0"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      <ResizablePanel
        id="list"
        defaultSize={320}
        minSize={240}
        maxSize={480}
        groupResizeBehavior="preserve-pixel-size"
      >
        <div className="flex flex-col border-r h-full">
          {listHeader}
          <div className="flex-1 min-h-0 overflow-y-auto">{listBody}</div>
        </div>
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel id="detail" minSize="40%">
        <div className="flex flex-col min-h-0 h-full">
          {hasTarget ? (
            conversation
          ) : (
            <div className="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground">
              <MessageSquare className="h-10 w-10 text-muted-foreground/30" />
              <p className="text-sm">{t(($) => $.page.select_prompt)}</p>
            </div>
          )}
        </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
