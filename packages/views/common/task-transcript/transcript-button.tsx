"use client";

import { useCallback, useEffect, useState } from "react";
import { Loader2, ScrollText } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import type { AgentTask } from "@multica/core/types/agent";
import { AgentTranscriptDialog } from "./agent-transcript-dialog";
import { buildTimeline, type TimelineItem } from "./build-timeline";
import { VisualRunHistory } from "../../chat/components/visual-run-history";

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
  /**
   * Optional content rendered above the transcript event list. Used to
   * surface autopilot webhook payloads inline with the run history.
   */
  headerSlot?: React.ReactNode;
  /**
   * Delegate opening to a parent that owns the dialog mount. When provided,
   * this button becomes a pure trigger — it calls `onOpen(task.id)` and renders
   * NO dialog of its own. Required by surfaces where the row unmounts while the
   * dialog should stay open: the issue execution log re-buckets a task
   * active→past the instant it completes, which would unmount a row-owned live
   * dialog mid-watch. The section hoists the mount and passes `onOpen` so the
   * live view survives the running→completed handoff. Omit on stable surfaces
   * (autopilot detail, agent activity) to keep the self-contained behavior.
   */
  onOpen?: (taskId: string) => void;
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
  onOpen,
}: TranscriptButtonProps) {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadedItems, setLoadedItems] = useState<TimelineItem[] | null>(null);

  // Per-user opt-in (Settings → Preferences). When on, the button opens the
  // chat-style read-only run view; when off (default) it opens the raw event
  // log. The visual view offers a "View raw log" escape hatch that swaps to
  // the raw dialog without re-fetching. `forceRaw` lets that hatch override
  // the preference for the current open session only.
  const visualEnabled = useAuthStore(
    (s) => s.user?.visual_execution_history ?? false,
  );
  const [forceRaw, setForceRaw] = useState(false);
  const showVisual = visualEnabled && !forceRaw;

  // Live mode: parent owns the timeline, we just render it.
  // Lazy mode: we fetch once and cache.
  const items = providedItems ?? loadedItems ?? [];

  // Fetch + cache the raw timeline (no-op if already loaded or provided).
  // Returns a promise so the escape hatch can await it before swapping.
  const ensureItems = useCallback(async () => {
    if (providedItems !== undefined || loadedItems !== null) return;
    setLoading(true);
    try {
      const msgs = await api.listTaskMessages(task.id);
      setLoadedItems(buildTimeline(msgs));
    } catch (err) {
      console.error(err);
      setLoadedItems([]);
    } finally {
      setLoading(false);
    }
  }, [providedItems, loadedItems, task.id]);

  const handleClick = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      // Delegated mode: hand off to the parent-owned dialog host and render
      // nothing locally. The host owns gating + the live-survival hoist.
      if (onOpen) {
        onOpen(task.id);
        return;
      }
      setForceRaw(false);
      // The visual view fetches the timeline itself (via taskMessagesOptions),
      // so it can open immediately. The raw dialog needs `items` up front.
      if (visualEnabled) {
        setOpen(true);
        return;
      }
      if (providedItems !== undefined || loadedItems !== null) {
        setOpen(true);
        return;
      }
      void ensureItems().then(() => setOpen(true));
    },
    [onOpen, task.id, visualEnabled, providedItems, loadedItems, ensureItems],
  );

  // Escape hatch: from the visual view, drop to the raw event log. Load the
  // timeline first (the visual path never fetched it) so the raw dialog has
  // data on first paint.
  const handleViewRawLog = useCallback(() => {
    void ensureItems().then(() => setForceRaw(true));
  }, [ensureItems]);

  useEffect(() => {
    if (!open) {
      // Reset the per-session escape-hatch override so the next open honours
      // the current preference again.
      setForceRaw(false);
      return;
    }

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

      {!onOpen && open && showVisual && (
        <VisualRunHistory
          open={open}
          onOpenChange={setOpen}
          task={task}
          agentName={agentName}
          onViewRawLog={handleViewRawLog}
        />
      )}

      {!onOpen && open && !showVisual && (
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
