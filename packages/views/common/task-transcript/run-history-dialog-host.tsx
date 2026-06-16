"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import type { AgentTask } from "@multica/core/types/agent";
import { AgentTranscriptDialog } from "./agent-transcript-dialog";
import { buildTimeline, type TimelineItem } from "./build-timeline";
import { VisualRunHistory } from "../../chat/components/visual-run-history";

interface RunHistoryDialogHostProps {
  /** The task to show, or null when nothing is open. */
  task: AgentTask | null;
  agentName?: string;
  onClose: () => void;
}

/**
 * Single, section-level dialog mount for the execution-log run history. Hoisting
 * the mount out of the row is load-bearing for the LIVE view: the issue
 * execution log re-buckets a task active→past the instant it completes, which
 * unmounts the running row. A row-owned dialog would die mid-watch exactly at
 * completion — the worst moment. Mounting here, keyed by `task.id` and fed the
 * live task object from the section's tasks query, lets the view stream while
 * running and settle into the static transcript in place when the run finishes.
 *
 * Honours the per-user `visual_execution_history` preference: on → chat-style
 * visual view (with a "View raw log" escape hatch); off → raw event log.
 */
export function RunHistoryDialogHost({
  task,
  agentName,
  onClose,
}: RunHistoryDialogHostProps) {
  const visualEnabled = useAuthStore(
    (s) => s.user?.visual_execution_history ?? false,
  );
  const [forceRaw, setForceRaw] = useState(false);
  const [loadedItems, setLoadedItems] = useState<TimelineItem[] | null>(null);

  const showVisual = visualEnabled && !forceRaw;

  // Reset per-open session state whenever the open task changes (or closes), so
  // a fresh open always honours the current preference and re-fetches its log.
  useEffect(() => {
    setForceRaw(false);
    setLoadedItems(null);
  }, [task?.id]);

  // Close the dialog on desktop navigation, mirroring the row-owned button.
  useEffect(() => {
    if (!task) return;
    const handleNavigate = () => onClose();
    window.addEventListener("multica:navigate", handleNavigate);
    return () => window.removeEventListener("multica:navigate", handleNavigate);
  }, [task, onClose]);

  const ensureItems = useCallback(async (taskId: string) => {
    try {
      const msgs = await api.listTaskMessages(taskId);
      setLoadedItems(buildTimeline(msgs));
    } catch (err) {
      console.error(err);
      setLoadedItems([]);
    }
  }, []);

  // Escape hatch: drop from the visual view to the raw log. Fetch the timeline
  // first (the visual path never loaded it) so the raw dialog paints with data.
  const handleViewRawLog = useCallback(() => {
    if (!task) return;
    void ensureItems(task.id).then(() => setForceRaw(true));
  }, [task, ensureItems]);

  // Raw path needs the timeline up front. Fetch lazily when raw is the active
  // view and we don't have it yet.
  useEffect(() => {
    if (!task || showVisual || loadedItems !== null) return;
    void ensureItems(task.id);
  }, [task, showVisual, loadedItems, ensureItems]);

  if (!task) return null;

  const handleOpenChange = (next: boolean) => {
    if (!next) onClose();
  };

  if (showVisual) {
    return (
      <VisualRunHistory
        open
        onOpenChange={handleOpenChange}
        task={task}
        agentName={agentName}
        onViewRawLog={handleViewRawLog}
      />
    );
  }

  return (
    <AgentTranscriptDialog
      open
      onOpenChange={handleOpenChange}
      task={task}
      items={loadedItems ?? []}
      agentName={agentName ?? ""}
      isLive={
        task.status === "running" ||
        task.status === "dispatched" ||
        task.status === "queued" ||
        task.status === "waiting_local_directory"
      }
    />
  );
}
