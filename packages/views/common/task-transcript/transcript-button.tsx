"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Loader2, ScrollText } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { api } from "@multica/core/api";
import {
  chatKeys,
  isTaskMessageTaskId,
  mergeTaskMessagesBySeq,
  taskMessagesOptions,
} from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { AgentTranscriptDialog } from "./agent-transcript-dialog";
import { buildTimeline, type TimelineItem } from "./build-timeline";

interface TranscriptButtonProps {
  task: AgentTask;
  agentName: string;
  /**
   * Pre-loaded timeline. When provided the button skips the fetch and opens
   * the dialog immediately — used by surfaces that already own an accumulating
   * timeline. Omit for terminal tasks; the button will fetch via
   * `api.listTaskMessages` on the first click and cache the result. Omit for
   * live tasks too: the button then subscribes to the shared task-messages
   * cache so the dialog keeps growing as new events arrive.
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
 *
 * Three data modes:
 *  - Provided items: parent owns the timeline, we just render it.
 *  - Live cache: `isLive` with no provided items and a persisted task id —
 *    subscribe to the shared `["task-messages", taskId]` cache (seeded by the
 *    WS `task:message` stream) so the open dialog keeps growing in real time,
 *    and force a seq-merged backfill on open to heal any WS reconnect gap.
 *  - Lazy: terminal tasks fetch once on first click and cache locally.
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
  const [loadedItems, setLoadedItems] = useState<TimelineItem[] | null>(null);

  // Live cache mode: the running task feeds the shared task-messages cache, so
  // we render straight off that cache instead of a one-shot local snapshot.
  const liveCacheMode =
    isLive && providedItems === undefined && isTaskMessageTaskId(task.id);

  // Latch the live path for the duration of an open session. The parent flips
  // `isLive` to false the moment the task finishes; without the latch the
  // dialog would drop to empty `loadedItems` mid-view. Staying on the cache
  // path keeps every delivered seq on screen and lets the dialog take a final
  // authoritative backfill on the running→terminal transition.
  const [liveSession, setLiveSession] = useState(false);
  useEffect(() => {
    if (!open) setLiveSession(false);
  }, [open]);

  // Live mode renders from the cache; lazy/provided modes from local state.
  const items = providedItems ?? loadedItems ?? [];

  const handleClick = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (liveCacheMode) {
        setLiveSession(true);
        setOpen(true);
        return;
      }
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
    [liveCacheMode, providedItems, loadedItems, task.id],
  );

  useEffect(() => {
    if (!open) return;

    const handleGlobalNavigate = () => {
      setOpen(false);
    };

    window.addEventListener("multica:navigate", handleGlobalNavigate);
    return () => {
      window.removeEventListener("multica:navigate", handleGlobalNavigate);
    };
  }, [open]);

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

      {open &&
        (liveSession ? (
          <LiveTranscriptDialog
            task={task}
            agentName={agentName}
            isLive={isLive}
            onOpenChange={setOpen}
            headerSlot={headerSlot}
          />
        ) : (
          <AgentTranscriptDialog
            open={open}
            onOpenChange={setOpen}
            task={task}
            items={items}
            agentName={agentName}
            isLive={isLive}
            headerSlot={headerSlot}
          />
        ))}
    </>
  );
}

interface LiveTranscriptDialogProps {
  task: AgentTask;
  agentName: string;
  isLive: boolean;
  onOpenChange: (open: boolean) => void;
  headerSlot?: React.ReactNode;
}

/**
 * Live transcript view backed by the shared task-messages cache. Mounted only
 * while the dialog is open, so closed live rows hold no query subscription and
 * don't widen the baseline request volume.
 *
 * The cache observer is read-only (`enabled: false`): the WS `task:message`
 * handler is the live writer, and the backfill below is the only fetch here.
 * Keeping React Query from issuing its own refetch is deliberate — its result
 * would blind-replace the cache and could drop a seq that arrived mid-flight,
 * whereas the backfill merges by seq.
 */
function LiveTranscriptDialog({
  task,
  agentName,
  isLive,
  onOpenChange,
  headerSlot,
}: LiveTranscriptDialogProps) {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    ...taskMessagesOptions(task.id),
    enabled: false,
  });

  // Force a backfill on open, and again when the task reaches a terminal state.
  // `taskMessagesOptions` is `staleTime: Infinity`, so a plain subscription
  // never refetches — a WS reconnect gap (or the final tail of messages a
  // completed issue task never re-broadcasts) would otherwise leave a hole.
  // Merge by seq so the fetch and any concurrent WS append both survive.
  useEffect(() => {
    if (!isTaskMessageTaskId(task.id)) return;
    let cancelled = false;
    api
      .listTaskMessages(task.id)
      .then((msgs) => {
        if (cancelled) return;
        queryClient.setQueryData<TaskMessagePayload[]>(
          chatKeys.taskMessages(task.id),
          (old = []) => mergeTaskMessagesBySeq(old, msgs),
        );
      })
      .catch((err) => {
        console.error(err);
      });
    return () => {
      cancelled = true;
    };
  }, [task.id, isLive, queryClient]);

  const items = useMemo(() => buildTimeline(data ?? []), [data]);

  return (
    <AgentTranscriptDialog
      open
      onOpenChange={onOpenChange}
      task={task}
      items={items}
      agentName={agentName}
      isLive={isLive}
      headerSlot={headerSlot}
    />
  );
}
