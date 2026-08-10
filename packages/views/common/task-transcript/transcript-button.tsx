"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
  variant?: "transcript" | "cockpit";
  className?: string;
  title?: string;
  renderButton?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  /**
   * Optional content rendered above the transcript event list. Used to
   * surface autopilot webhook payloads inline with the run history.
   */
  headerSlot?: React.ReactNode;
  headerActions?: React.ReactNode | ((context: { agentName: string }) => React.ReactNode);
  terminalSlot?: React.ReactNode;
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
  variant = "transcript",
  className,
  title = "View transcript",
  renderButton = true,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  headerSlot,
  headerActions,
  terminalSlot,
}: TranscriptButtonProps) {
  const [uncontrolledOpen, setUncontrolledOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadedItems, setLoadedItems] = useState<TimelineItem[] | null>(null);
  const controlledLoadRef = useRef<{
    taskId: string;
    promise: Promise<TimelineItem[]>;
  } | null>(null);
  const open = controlledOpen ?? uncontrolledOpen;
  const setOpen = controlledOnOpenChange ?? setUncontrolledOpen;

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
    if (!open) {
      setLiveSession(false);
      return;
    }
    if (liveCacheMode) {
      setLiveSession(true);
    }
  }, [liveCacheMode, open]);

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
    [liveCacheMode, providedItems, loadedItems, setOpen, task.id],
  );

  // A parent can open the dialog without rendering/clicking this component's
  // own button (Agent Cockpit does this from an issue-row launcher). Terminal
  // tasks are not on the live cache path, so perform the same lazy backfill
  // when a controlled dialog is opened externally. Reuse one in-flight
  // promise across React Strict Mode's effect replay.
  useEffect(() => {
    if (
      controlledOpen !== true ||
      liveSession ||
      liveCacheMode ||
      providedItems !== undefined ||
      loadedItems !== null
    ) {
      return;
    }

    let request = controlledLoadRef.current;
    if (!request || request.taskId !== task.id) {
      request = {
        taskId: task.id,
        promise: api
          .listTaskMessages(task.id)
          .then((messages) => buildTimeline(messages))
          .catch((error) => {
            console.error(error);
            return [];
          }),
      };
      controlledLoadRef.current = request;
    }

    let cancelled = false;
    setLoading(true);
    void request.promise.then((timeline) => {
      if (cancelled) return;
      setLoadedItems(timeline);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [controlledOpen, liveSession, liveCacheMode, providedItems, loadedItems, task.id]);

  useEffect(() => {
    if (!open) return;

    const handleGlobalNavigate = () => {
      setOpen(false);
    };

    window.addEventListener("multica:navigate", handleGlobalNavigate);
    return () => {
      window.removeEventListener("multica:navigate", handleGlobalNavigate);
    };
  }, [open, setOpen]);

  return (
    <>
      {renderButton ? (
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
      ) : null}

      {open &&
        (liveSession ? (
          <LiveTranscriptDialog
            task={task}
            agentName={agentName}
            isLive={isLive}
            onOpenChange={setOpen}
            variant={variant}
            headerSlot={headerSlot}
            headerActions={headerActions}
            terminalSlot={terminalSlot}
          />
        ) : (
          <AgentTranscriptDialog
            open={open}
            onOpenChange={setOpen}
            task={task}
            items={items}
            agentName={agentName}
            isLive={isLive}
            variant={variant}
            headerSlot={headerSlot}
            headerActions={headerActions}
            terminalSlot={terminalSlot}
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
  variant: "transcript" | "cockpit";
  headerSlot?: React.ReactNode;
  headerActions?: React.ReactNode | ((context: { agentName: string }) => React.ReactNode);
  terminalSlot?: React.ReactNode;
}

const LIVE_TRANSCRIPT_RECONCILE_MS = 5_000;

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
  variant,
  headerSlot,
  headerActions,
  terminalSlot,
}: LiveTranscriptDialogProps) {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    ...taskMessagesOptions(task.id),
    enabled: false,
  });

  // Force a backfill on open, periodically while live, and again when the task
  // reaches a terminal state.
  // `taskMessagesOptions` is `staleTime: Infinity`, so a plain subscription
  // never refetches — a WS reconnect gap (or the final tail of messages a
  // completed issue task never re-broadcasts) would otherwise leave a hole.
  // Merge by seq so the fetch and any concurrent WS append both survive.
  useEffect(() => {
    if (!isTaskMessageTaskId(task.id)) return;
    let cancelled = false;
    let inFlight = false;
    const reconcile = async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        const msgs = await api.listTaskMessages(task.id);
        if (cancelled) return;
        queryClient.setQueryData<TaskMessagePayload[]>(
          chatKeys.taskMessages(task.id),
          (old = []) => mergeTaskMessagesBySeq(old, msgs),
        );
      } catch (err) {
        console.error(err);
      } finally {
        inFlight = false;
      }
    };

    void reconcile();
    const intervalId = isLive
      ? window.setInterval(() => void reconcile(), LIVE_TRANSCRIPT_RECONCILE_MS)
      : undefined;
    return () => {
      cancelled = true;
      if (intervalId !== undefined) window.clearInterval(intervalId);
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
      variant={variant}
      headerSlot={headerSlot}
      headerActions={headerActions}
      terminalSlot={terminalSlot}
    />
  );
}
