"use client";

import { useRef, useEffect, useCallback } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ChatInput } from "@multica/ui/components/chat-input";
import { MessageList } from "@multica/ui/components/message-list";
import { MemoizedMarkdown } from "@multica/ui/components/markdown";
import { MulticaIcon } from "@multica/ui/components/multica-icon";
import { ExecApprovalItem } from "@multica/ui/components/exec-approval-item";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useAutoScroll } from "@multica/ui/hooks/use-auto-scroll";
import type { Message } from "@multica/store";

export interface ChatViewError {
  code: string;
  message: string;
}

export interface ChatViewApproval {
  approvalId: string;
  command: string;
  cwd?: string;
  riskLevel: "safe" | "needs-review" | "dangerous";
  riskReasons: string[];
  expiresAtMs: number;
}

export interface ChatViewProps {
  messages: Message[];
  streamingIds: Set<string>;
  isLoading: boolean;
  isLoadingHistory: boolean;
  isLoadingMore?: boolean;
  hasMore?: boolean;
  error: ChatViewError | null;
  pendingApprovals: ChatViewApproval[];
  sendMessage: (text: string) => void;
  onAbort?: () => void;
  loadMore?: () => void;
  resolveApproval: (approvalId: string, decision: "allow-once" | "allow-always" | "deny") => void;
  onDisconnect?: () => void;
  /** Optional action button in the error banner (e.g. "Configure Provider") */
  errorAction?: { label: string; onClick: () => void };
  /** Initial prompt to pre-fill the input (e.g., from onboarding) */
  initialPrompt?: string;
  /** Optional slot rendered between the error bar and the input footer */
  bottomSlot?: React.ReactNode;
}

export function ChatView({
  messages,
  streamingIds,
  isLoading,
  isLoadingHistory,
  isLoadingMore = false,
  hasMore = false,
  error,
  pendingApprovals,
  sendMessage,
  onAbort,
  loadMore,
  resolveApproval,
  onDisconnect,
  errorAction,
  initialPrompt,
  bottomSlot,
}: ChatViewProps) {
  const mainRef = useRef<HTMLElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(mainRef);
  const { suppressAutoScroll } = useAutoScroll(mainRef);

  // scrollHeight compensation for prepended messages
  const prevScrollHeightRef = useRef(0);
  const isPrependingRef = useRef(false);
  const unlockRef = useRef<(() => void) | null>(null);

  // Snapshot scrollHeight before prepend render
  const onLoadMore = useCallback(() => {
    if (!loadMore || !mainRef.current) return;
    const el = mainRef.current;
    prevScrollHeightRef.current = el.scrollHeight;
    isPrependingRef.current = true;
    // Lock auto-scroll during prepend
    unlockRef.current = suppressAutoScroll();
    loadMore();
  }, [loadMore, suppressAutoScroll]);

  // After messages change, compensate scroll position if we just prepended
  useEffect(() => {
    const el = mainRef.current;
    if (!el || !isPrependingRef.current) return;

    isPrependingRef.current = false;

    // Double-rAF ensures DOM layout is complete before compensating
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        const newScrollHeight = el.scrollHeight;
        const heightDiff = newScrollHeight - prevScrollHeightRef.current;
        if (heightDiff > 0) {
          el.scrollTop = el.scrollTop + heightDiff;
        }
        // Release auto-scroll lock after position is restored
        unlockRef.current?.();
        unlockRef.current = null;
      });
    });
  }, [messages]);

  // IntersectionObserver to trigger loadMore when sentinel is visible
  // Skip during initial history load to avoid premature triggering
  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel || isLoadingHistory) return;

    const observer = new IntersectionObserver(
      (entries) => {
        const entry = entries[0];
        if (entry?.isIntersecting && hasMore && !isLoadingMore) {
          onLoadMore();
        }
      },
      { rootMargin: "100px" },
    );

    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [hasMore, isLoadingMore, isLoadingHistory, onLoadMore]);

  return (
    <div className="flex-1 flex flex-col min-h-0">
      {onDisconnect && (
        <div className="container flex items-center justify-end px-4 py-2">
          <button
            onClick={onDisconnect}
            className="text-xs text-muted-foreground hover:text-foreground"
          >
            Disconnect
          </button>
        </div>
      )}

      <main ref={mainRef} className="flex-1 overflow-y-auto min-h-0" style={fadeStyle}>
        {isLoadingHistory && messages.length === 0 ? (
          <div className="px-4 py-6 max-w-4xl mx-auto">
            {/* User bubble */}
            <div className="flex justify-end my-2">
              <Skeleton className="h-8 w-[30%] rounded-md" />
            </div>
            {/* Assistant multi-line */}
            <div className="space-y-2 py-1 px-2.5 my-1">
              <Skeleton className="h-3.5 w-full" />
              <Skeleton className="h-3.5 w-[88%]" />
              <Skeleton className="h-3.5 w-[65%]" />
            </div>
            {/* Tool row */}
            <div className="px-2.5 my-1">
              <Skeleton className="h-6 w-44 rounded" />
            </div>
            {/* Assistant short reply */}
            <div className="space-y-2 py-1 px-2.5 my-1">
              <Skeleton className="h-3.5 w-[92%]" />
              <Skeleton className="h-3.5 w-[55%]" />
            </div>
            {/* User bubble */}
            <div className="flex justify-end my-2">
              <Skeleton className="h-8 w-[42%] rounded-md" />
            </div>
            {/* Assistant reply */}
            <div className="space-y-2 py-1 px-2.5 my-1">
              <Skeleton className="h-3.5 w-full" />
              <Skeleton className="h-3.5 w-[80%]" />
              <Skeleton className="h-3.5 w-[70%]" />
              <Skeleton className="h-3.5 w-[40%]" />
            </div>
            {/* User bubble */}
            <div className="flex justify-end my-2">
              <Skeleton className="h-8 w-[22%] rounded-md" />
            </div>
            {/* Assistant reply */}
            <div className="space-y-2 py-1 px-2.5 my-1">
              <Skeleton className="h-3.5 w-[75%]" />
              <Skeleton className="h-3.5 w-[50%]" />
            </div>
          </div>
        ) : messages.length === 0 && pendingApprovals.length === 0 ? (
          <div className="flex items-center justify-center h-full">
            <div className="flex items-center gap-3 text-muted-foreground">
              <MulticaIcon className="size-5 shrink-0" />
              <div>
                <p className="text-sm font-medium">Start a conversation</p>
                <p className="text-xs text-muted-foreground/70">
                  Type a message below to chat with your Agent
                </p>
              </div>
            </div>
          </div>
        ) : (
          <>
            {/* Sentinel element for IntersectionObserver load-more trigger */}
            <div ref={sentinelRef} className="h-px shrink-0" />
            {isLoadingMore && (
              <div className="flex justify-center py-3">
                <div className="text-xs text-muted-foreground animate-pulse">
                  Loading older messages...
                </div>
              </div>
            )}
            <MessageList messages={messages} streamingIds={streamingIds} />
            {pendingApprovals.length > 0 && (
              <div className="relative px-4 max-w-4xl mx-auto">
                {pendingApprovals.map((approval) => (
                  <ExecApprovalItem
                    key={approval.approvalId}
                    command={approval.command}
                    cwd={approval.cwd}
                    riskLevel={approval.riskLevel}
                    riskReasons={approval.riskReasons}
                    expiresAtMs={approval.expiresAtMs}
                    onDecision={(decision) => resolveApproval(approval.approvalId, decision)}
                  />
                ))}
              </div>
            )}
          </>
        )}
      </main>

      {error && (
        <div className="container px-4" role="alert" aria-live="polite">
          <div className="rounded-lg bg-destructive/5 border border-destructive/15 text-xs px-3 py-2 flex items-center justify-between gap-3">
            <span className="text-foreground leading-snug">
              <MemoizedMarkdown mode="minimal" id={`error-${error.code}`}>
                {error.message}
              </MemoizedMarkdown>
            </span>
            <div className="flex items-center gap-2 shrink-0">
              {errorAction && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={errorAction.onClick}
                  className="shrink-0 text-xs h-7 px-2.5"
                >
                  {errorAction.label}
                </Button>
              )}
              {onDisconnect && (
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={onDisconnect}
                  className="shrink-0 text-xs h-7 px-2.5"
                >
                  Disconnect
                </Button>
              )}
            </div>
          </div>
        </div>
      )}

      {bottomSlot}

      <footer className="container px-4 pb-3 pt-1">
        <ChatInput
          onSubmit={sendMessage}
          onAbort={onAbort}
          isLoading={isLoading}
          disabled={!!error && error.code !== 'AGENT_ERROR'}
          placeholder={error && error.code !== 'AGENT_ERROR' ? "Connection error" : "Ask your Agent..."}
          defaultValue={initialPrompt}
        />
      </footer>
    </div>
  );
}
