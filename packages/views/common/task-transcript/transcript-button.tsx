"use client";

import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, ScrollText } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { taskMessagesOptions } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types/agent";
import { AgentTranscriptDialog } from "./agent-transcript-dialog";
import { buildTimeline, type TimelineItem } from "./build-timeline";

interface TranscriptButtonProps {
  task: AgentTask;
  agentName: string;
  /**
   * Pre-loaded timeline. When provided the button skips the fetch and opens
   * the dialog immediately — used by the live card where `items` already
   * accumulate via WS. Omit for lazy mode; the button will read from the
   * shared task-messages query cache so WS updates keep the dialog live.
   */
  items?: TimelineItem[];
  isLive?: boolean;
  className?: string;
  title?: string;
  /**
   * Optional content rendered above the transcript event list. Used to
   * surface autopilot webhook payloads inline with the run history.
   */
  headerSlot?: React.ReactNode;
}

/**
 * Compact icon-button that opens the full transcript dialog. Used on any
 * surface that lists agent tasks (issue activity card, agent detail
 * activity tab). Owns its own dialog state and lazy-load — the parent
 * just drops it in.
 */
export function TranscriptButton({
  task,
  agentName,
  items: providedItems,
  isLive = false,
  className,
  title = "View transcript",
  headerSlot,
}: TranscriptButtonProps) {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const usesProvidedItems = providedItems !== undefined;
  const { data: messages = [], refetch } = useQuery({
    ...taskMessagesOptions(task.id),
    enabled: open && !usesProvidedItems,
    refetchInterval: open && isLive && !usesProvidedItems ? 2000 : false,
  });

  // Provided mode: parent owns the timeline. Lazy mode: subscribe to the
  // canonical task-messages cache, which WS updates append to in real time.
  const items = providedItems ?? buildTimeline(messages);

  const handleClick = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (usesProvidedItems) {
        setOpen(true);
        return;
      }
      setLoading(true);
      refetch()
        .catch((err) => {
          console.error(err);
        })
        .finally(() => {
          setOpen(true);
          setLoading(false);
        });
    },
    [refetch, usesProvidedItems],
  );

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={<button type="button" />}
          onClick={handleClick}
          disabled={loading}
          aria-label={title}
          className={cn(
            "flex items-center justify-center rounded p-1 text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors disabled:opacity-50",
            className,
          )}
        >
          {loading ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <ScrollText className="h-3.5 w-3.5" />
          )}
        </TooltipTrigger>
        <TooltipContent>{title}</TooltipContent>
      </Tooltip>

      {open && (
        <AgentTranscriptDialog
          open={open}
          onOpenChange={setOpen}
          task={task}
          items={items}
          agentName={agentName}
          isLive={isLive}
          headerSlot={headerSlot}
        />
      )}
    </>
  );
}
