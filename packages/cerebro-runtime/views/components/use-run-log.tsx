"use client";

// FIR-4073 — opening the run log from an alert row, without the row having to
// be a 22px icon.
//
// TranscriptButton owns its own trigger, so the only way to open the log used
// to be that icon. On a phone the icon is both too small to hit and the first
// thing that gets painted over when the failure text overflows — which is the
// "cannot be opened" report. Splitting the dialog state out lets the row make
// the whole two-line text block the trigger, and keeps the fetch-once-and-cache
// behaviour TranscriptButton already had.
//
// The task-transcript sub-barrel, never the @multica/views root barrel: the
// root re-exports pages that import back into @multica/views, and that cycle
// blanks the dialog — the trap documented on run-retry-actions and
// run-failure-card. This is the same edge the row already had via
// TranscriptButton, so no new one is introduced.

import { useCallback, useState, type ReactNode } from "react";
import { api } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types/agent";
import {
  AgentTranscriptDialog,
  buildTimeline,
  type TimelineItem,
} from "@multica/views/common/task-transcript";

export interface RunLog {
  /** False while the task list is still loading — the row then renders static text. */
  available: boolean;
  loading: boolean;
  open: () => void;
  /** Mount once per row; null until the log has actually been opened. */
  dialog: ReactNode;
}

export function useRunLog(task: AgentTask | undefined, agentName: string): RunLog {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState<TimelineItem[] | null>(null);

  const openRunLog = useCallback(() => {
    if (!task) return;
    if (items !== null) {
      setOpen(true);
      return;
    }
    setLoading(true);
    api
      .listTaskMessages(task.id)
      .then((msgs) => {
        setItems(buildTimeline(msgs));
        setOpen(true);
      })
      .catch((err) => {
        // A log we cannot load still opens — the dialog shows the run's own
        // failure card, which is the more useful half anyway.
        console.error(err);
        setItems([]);
        setOpen(true);
      })
      .finally(() => setLoading(false));
  }, [task, items]);

  return {
    available: task !== undefined,
    loading,
    open: openRunLog,
    dialog:
      task && open ? (
        <AgentTranscriptDialog
          open={open}
          onOpenChange={setOpen}
          task={task}
          items={items ?? []}
          agentName={agentName}
        />
      ) : null,
  };
}
