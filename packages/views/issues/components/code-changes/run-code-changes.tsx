"use client";

/**
 * The "changes in this run" card (MUL-7651). A run's reply — or the run's own
 * block when it posted none — carries one card per repository the run changed;
 * the card opens the code change viewer.
 */

import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitCompareArrows } from "lucide-react";
import { runNumber } from "@multica/core/code-changes";
import { issueCodeChangesOptions, issueTasksOptions, issueTimelineOptions } from "@multica/core/issues/queries";
import type { TaskCodeChange, TimelineEntry } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { DiffStat } from "../../../editor/diff/diff-view";
import { useT } from "../../../i18n";
import { CodeChangeViewer } from "./code-change-viewer";

const NO_CHANGES: TaskCodeChange[] = [];

/** The run's own changes, one per repository. */
export function RunCodeChanges({
  issueId,
  taskId,
  className,
}: {
  issueId: string;
  taskId: string;
  className?: string;
}) {
  const select = useCallback(
    (changes: TaskCodeChange[]) => {
      const own = changes.filter((c) => c.task_id === taskId && c.scope === "run");
      return own.length ? own : NO_CHANGES;
    },
    [taskId],
  );
  const selectNumber = useCallback(
    (tasks: Parameters<typeof runNumber>[0]) => runNumber(tasks, taskId),
    [taskId],
  );
  const { data: changes = NO_CHANGES } = useQuery({ ...issueCodeChangesOptions(issueId), select });
  const { data: number = 0 } = useQuery({ ...issueTasksOptions(issueId), select: selectNumber });
  if (changes.length === 0) return null;
  const showRepo = changes.length > 1;
  return (
    <div className={cn("grid grid-cols-[repeat(auto-fill,minmax(min(15rem,100%),1fr))] gap-2", className)}>
      {changes.map((change) => (
        <RunCodeChangeCard key={change.id} issueId={issueId} change={change} runNumber={number} showRepo={showRepo} />
      ))}
    </div>
  );
}

function RunCodeChangeCard({
  issueId,
  change,
  runNumber: number,
  showRepo,
}: {
  issueId: string;
  change: TaskCodeChange;
  runNumber: number;
  showRepo: boolean;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);
  const title = t(($) => $.code_changes.card_title);
  return (
    <>
      <button
        type="button"
        className="flex min-w-0 items-center gap-3 rounded-lg border border-border bg-card px-3 py-2.5 text-left transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        onClick={() => setOpen(true)}
      >
        <span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          <GitCompareArrows className="size-5" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-body font-medium">
            {showRepo ? `${title} · ${change.repo_label}` : title}
          </span>
          <span className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
            <span className="truncate tabular-nums">{t(($) => $.code_changes.files, { count: change.file_count })}</span>
            <DiffStat additions={change.additions} deletions={change.deletions} />
          </span>
        </span>
      </button>
      <CodeChangeViewer issueId={issueId} change={change} runNumber={number} open={open} onClose={close} />
    </>
  );
}

// One pass per timeline snapshot, shared by every comment on the page: each
// comment asking "am I my run's reply?" would otherwise rescan the timeline.
const repliesByTimeline = new WeakMap<readonly TimelineEntry[], Map<string, TimelineEntry>>();

/**
 * The newest agent comment a run posted — the one its changes card hangs on.
 * Mirrors how the timeline picks a run's reply (comment-runs.ts).
 */
function latestRunReplyId(entries: readonly TimelineEntry[], taskId: string): string | null {
  let replies = repliesByTimeline.get(entries);
  if (!replies) {
    replies = new Map();
    for (const entry of entries) {
      if (entry.type !== "comment" || entry.actor_type !== "agent" || !entry.source_task_id) continue;
      const latest = replies.get(entry.source_task_id);
      if (!latest || entry.created_at > latest.created_at || (entry.created_at === latest.created_at && entry.id > latest.id)) {
        replies.set(entry.source_task_id, entry);
      }
    }
    repliesByTimeline.set(entries, replies);
  }
  return replies.get(taskId)?.id ?? null;
}

/**
 * The changes card under a comment, when that comment is its run's reply.
 * Reads the issue timeline already in cache and never fetches it: outside an
 * issue page there is no run to show.
 */
export function CommentRunCodeChanges({
  issueId,
  entry,
  className,
}: {
  issueId: string;
  entry: TimelineEntry;
  className?: string;
}) {
  const taskId = entry.actor_type === "agent" ? entry.source_task_id ?? "" : "";
  const select = useMemo(
    () => (entries: TimelineEntry[]) => !!taskId && latestRunReplyId(entries, taskId) === entry.id,
    [taskId, entry.id],
  );
  const { data: isReply = false } = useQuery({
    ...issueTimelineOptions(issueId),
    enabled: false,
    select,
  });
  if (!taskId || !isReply) return null;
  return <RunCodeChanges issueId={issueId} taskId={taskId} className={className} />;
}
