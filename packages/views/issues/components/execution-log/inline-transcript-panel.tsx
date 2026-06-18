"use client";

import { useState, useRef, useEffect, useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Loader2, AlertCircle, RotateCcw } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types/agent";
import { TaskTranscriptTimeline, buildTimeline } from "../../../common/task-transcript";
import { useT } from "../../../i18n";

interface InlineTranscriptPanelProps {
  task: AgentTask;
  isLive?: boolean;
  defaultOpen?: boolean;
}

export function InlineTranscriptPanel({ task, isLive, defaultOpen = false }: InlineTranscriptPanelProps) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(defaultOpen);
  const [showThinking, setShowThinking] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const wasNearBottomRef = useRef(true);

  const { data: messages = [], isLoading, error, refetch } = useQuery({
    ...taskMessagesOptions(task.id),
    enabled: open,
  });

  const items = buildTimeline(messages).filter((item) => (showThinking ? true : item.type !== "thinking"));

  const scrollToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, []);

  useEffect(() => {
    if (!open || !isLive) return;
    if (wasNearBottomRef.current) {
      scrollToBottom();
    }
  }, [items, open, isLive, scrollToBottom]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    wasNearBottomRef.current = nearBottom;
  }, []);

  const isRunning = isLive && task.status === "running";

  return (
    <div className="mt-1">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
      >
        <ChevronRight className={cn("!size-3 shrink-0 stroke-[2.5] transition-transform", open && "rotate-90")} />
        {open ? t(($) => $.execution_log.hide_transcript) : t(($) => $.execution_log.show_transcript)}
        {isRunning && (
          <span className="ml-1 inline-flex items-center gap-1 text-info">
            <span className="h-1.5 w-1.5 rounded-full bg-info animate-pulse" />
            {t(($) => $.execution_log.live_indicator)}
          </span>
        )}
      </button>

      {open && (
        <div className="mt-1 rounded-md border bg-muted/20">
          <div className="flex items-center justify-between px-2 py-1 border-b">
            <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
              <input
                type="checkbox"
                checked={showThinking}
                onChange={(e) => setShowThinking(e.target.checked)}
                className="h-3 w-3 rounded border-muted-foreground/30"
              />
              {t(($) => $.execution_log.show_thinking)}
            </label>
          </div>

          {isLoading ? (
            <div className="flex items-center justify-center gap-2 py-8 text-xs text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              Loading...
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center gap-2 py-8 text-xs text-destructive">
              <AlertCircle className="h-4 w-4" />
              {t(($) => $.execution_log.transcript_error)}
              <Button variant="ghost" size="sm" onClick={() => refetch()} className="h-6 gap-1 text-xs">
                <RotateCcw className="h-3 w-3" />
                {t(($) => $.execution_log.retry)}
              </Button>
            </div>
          ) : (
            <div
              ref={scrollRef}
              onScroll={handleScroll}
              className="max-h-80 overflow-y-auto"
            >
              <TaskTranscriptTimeline
                items={items}
                isLive={isRunning}
                emptyLabel={t(($) => $.execution_log.no_events_yet)}
                liveEmptyLabel={t(($) => $.execution_log.waiting_events)}
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}
