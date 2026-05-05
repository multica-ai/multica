"use client";

import { useCallback, useState } from "react";
import { Loader2, ScrollText } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { api } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types/agent";
import { useT } from "@multica/i18n/react";
import { AgentTranscriptDialog } from "./agent-transcript-dialog";
import { buildTimeline, type TimelineItem } from "./build-timeline";

interface TranscriptButtonProps {
  task: AgentTask;
  agentName: string;
  /**
   * Pre-loaded timeline. When provided the button skips the fetch and opens
   * the dialog immediately — used by the live card where `items` already
   * accumulate via WS. Omit for terminal tasks; the button will fetch via
   * `api.listTaskMessages` on the first click and cache the result.
   */
  items?: TimelineItem[];
  isLive?: boolean;
  className?: string;
  title?: string;
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
  title,
}: TranscriptButtonProps) {
  const t = useT("transcript");
  const resolvedTitle = title ?? t("view_transcript");
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadedItems, setLoadedItems] = useState<TimelineItem[] | null>(null);

  // Live mode: parent owns the timeline, we just render it.
  // Lazy mode: we fetch once and cache.
  const items = providedItems ?? loadedItems ?? [];

  const handleClick = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (providedItems !== undefined || loadedItems !== null) {
        setOpen(true);
        return;
      }
      setLoading(true);
      api
        .listTaskMessages(task.id)
        .then((msgs) => {
          setLoadedItems(buildTimeline(msgs));
          setOpen(true);
        })
        .catch((err) => {
          console.error(err);
          setLoadedItems([]);
          setOpen(true);
        })
        .finally(() => setLoading(false));
    },
    [providedItems, loadedItems, task.id],
  );

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={<button type="button" />}
          onClick={handleClick}
          disabled={loading}
          aria-label={resolvedTitle}
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
        <TooltipContent>{resolvedTitle}</TooltipContent>
      </Tooltip>

      {open && (
        <AgentTranscriptDialog
          open={open}
          onOpenChange={setOpen}
          task={task}
          items={items}
          agentName={agentName}
          isLive={isLive}
        />
      )}
    </>
  );
}
