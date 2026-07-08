"use client";

import { useEffect, useState } from "react";
import { ArrowLeft, Maximize2, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { useChatStore } from "@multica/core/chat";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Agent, ChatSession } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { ChatMessageList, ChatMessageSkeleton } from "./chat-message-list";
import { ChatInput } from "./chat-input";
import { ChatThreadList } from "./chat-thread-list";
import { ChatSessionHeader } from "./chat-session-header";
import { EmptyState } from "./chat-empty-state";
import { NewChatButton } from "./new-chat-button";
import { OfflineBanner } from "./offline-banner";
import { NoAgentBanner } from "./no-agent-banner";
import { useChatController } from "./use-chat-controller";
import { useChatContextItems } from "./use-chat-context-items";

/**
 * Floating chat overlay — the "summon anywhere" surface. Shares all
 * conversation logic with the Chat tab ({@link ChatPage}) via
 * {@link useChatController}, so `activeSessionId` (the Zustand source of truth)
 * stays in lockstep across both: opening or starting a thread here is
 * reflected in the tab and vice-versa. Mounting / route gating lives in
 * {@link FloatingChat}; this component assumes it is allowed to render.
 *
 * Unlike the tab, this overlay passes `contextItems`, so `@` in the composer
 * surfaces the issue/project of the page you summoned it from (the "current
 * context" affordance) — the whole point of a summon-anywhere entry point.
 */
export function ChatWindow() {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();
  const isOpen = useChatStore((s) => s.isOpen);
  const setOpen = useChatStore((s) => s.setOpen);

  const c = useChatController({ isActive: isOpen });
  const contextItems = useChatContextItems(wsId);

  // "Composing a brand-new chat": the user hit ⊕ but hasn't sent yet, so no
  // session exists. Mirrors ChatPage; resets once a real session takes over.
  const [composingNew, setComposingNew] = useState(false);
  useEffect(() => {
    if (c.activeSessionId) setComposingNew(false);
  }, [c.activeSessionId]);

  if (!isOpen) return null;

  const handleSelect = (session: ChatSession) => {
    c.handleSelectSession(session);
    setComposingNew(false);
  };

  const startNewChat = (agent: Agent | null) => {
    if (agent) c.handleStartNewChat(agent);
    else c.handleNewChat();
    setComposingNew(true);
  };

  const backToList = () => {
    c.setActiveSession(null);
    setComposingNew(false);
  };

  const openFullPage = () => {
    push(c.activeSessionId ? `${wsPaths.chat()}?session=${c.activeSessionId}` : wsPaths.chat());
    setOpen(false);
  };

  const hasTarget = !!c.activeSessionId || composingNew;

  const headerButtons = (
    <div className="flex items-center gap-0.5">
      <NewChatButton
        agents={c.availableAgents}
        userId={c.user?.id}
        onStart={startNewChat}
        side="bottom"
      />
      <Tooltip>
        <TooltipTrigger
          render={
            <Button variant="ghost" size="icon-sm" onClick={openFullPage}>
              <Maximize2 className="size-4" />
            </Button>
          }
        />
        <TooltipContent side="bottom">{t(($) => $.window.open_full_tooltip)}</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button variant="ghost" size="icon-sm" onClick={() => setOpen(false)}>
              <X className="size-4" />
            </Button>
          }
        />
        <TooltipContent side="bottom">{t(($) => $.window.minimize_tooltip)}</TooltipContent>
      </Tooltip>
    </div>
  );

  const conversation = (
    <div className="flex flex-1 flex-col min-h-0">
      <div className="flex h-11 shrink-0 items-center gap-1 border-b pr-1.5">
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={backToList}
          className="ml-1 text-muted-foreground"
        >
          <ArrowLeft className="size-4" />
        </Button>
        <div className="min-w-0 flex-1">
          {c.currentSession && (
            <ChatSessionHeader session={c.currentSession} agent={c.activeAgent} />
          )}
        </div>
      </div>
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
      ) : (
        <OfflineBanner agentName={c.activeAgent?.name} availability={c.availability} />
      )}

      <ChatInput
        onSend={c.handleSend}
        restoreDraftRequest={c.restoreDraftRequest}
        onRestoreDraftConsumed={c.handleRestoreDraftConsumed}
        onUploadFile={c.handleUploadFile}
        onStop={c.handleStop}
        isRunning={!!c.pendingTaskId}
        disabled={c.isSessionArchived}
        noAgent={c.noAgent}
        agentName={c.activeAgent?.name}
        contextItems={contextItems}
      />
    </div>
  );

  return (
    <div className="absolute bottom-2 right-2 z-50 flex h-[600px] max-h-[calc(100%-1rem)] w-[380px] max-w-[calc(100%-1rem)] flex-col overflow-hidden rounded-xl border bg-background shadow-lg">
      {!hasTarget && (
        <div className="flex h-11 shrink-0 items-center justify-between border-b pl-3 pr-1.5">
          <h2 className="text-sm font-semibold">{t(($) => $.page.title)}</h2>
          {headerButtons}
        </div>
      )}
      {hasTarget ? (
        <>
          {/* When a thread is open the ⊕ / expand / close cluster rides the
              conversation header via the shared ChatSessionHeader row above;
              expose the window controls in a compact top strip. */}
          <div className="flex h-9 shrink-0 items-center justify-end border-b pr-1.5">
            {headerButtons}
          </div>
          {conversation}
        </>
      ) : (
        <div className="flex-1 min-h-0 overflow-y-auto p-1">
          <ChatThreadList
            sessions={c.sessions}
            agents={c.agents}
            activeSessionId={c.activeSessionId}
            onSelectSession={handleSelect}
          />
        </div>
      )}
    </div>
  );
}
