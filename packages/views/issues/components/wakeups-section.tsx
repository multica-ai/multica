"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { toast } from "sonner";
import { issueWakeupsOptions, useDisableIssueWakeup, issueTasksOptions } from "@multica/core/issues";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Switch } from "@multica/ui/components/ui/switch";
import { TranscriptButton } from "../../common/task-transcript";
import { useT } from "../../i18n";

export function WakeupsSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const workspaceId = useCurrentWorkspace()?.id ?? "";
  const [open, setOpen] = useState(true);
  const { data = [], isError, refetch } = useQuery(issueWakeupsOptions(workspaceId, issueId));
  const { data: tasks = [] } = useQuery(issueTasksOptions(issueId));
  const disable = useDisableIssueWakeup(workspaceId, issueId);
  if (!data.length && !isError) return null;
  return (
    <section>
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium hover:bg-accent/70">
        {t(($) => $.wakeups.title)}
        <ChevronRight className={`size-3 text-muted-foreground ${open ? "rotate-90" : ""}`} />
      </button>
      {open && <div className="space-y-3 px-2">
        {isError && <button type="button" className="text-caption text-muted-foreground hover:text-foreground" onClick={() => void refetch()}>{t(($) => $.wakeups.retry)}</button>}
        {data.map((wakeup) => {
          const task = tasks.find((item) => item.id === wakeup.last_task_id);
          const active = wakeup.enabled || (!wakeup.disabled_at && (!wakeup.last_task_id || (!!task && ["queued", "deferred"].includes(task.status))));
          const eventLabels: Record<string, string> = {
            "task.completed": t(($) => $.wakeups.run_completed), "task.failed": t(($) => $.wakeups.run_failed), "task.cancelled": t(($) => $.wakeups.run_cancelled),
            "comment.created": t(($) => $.wakeups.comment_created), "issue.status_changed": t(($) => $.wakeups.status_changed),
          };
          const trigger = wakeup.kind === "event" ? wakeup.event_types.map((event) => eventLabels[event] ?? event).join(", ") : wakeup.kind === "every" ? t(($) => $.wakeups.every, { minutes: (wakeup.interval_seconds ?? 0) / 60 }) : wakeup.kind === "cron" ? `${wakeup.cron_expression} · ${wakeup.timezone}` : t(($) => $.wakeups.once);
          return <div key={wakeup.id} className="space-y-1 text-caption">
            <div className="flex items-center justify-between gap-2">
              <span className="truncate font-medium">{wakeup.agent_name}</span>
              <Switch aria-label={t(($) => $.wakeups.disable, { agent: wakeup.agent_name })} checked={active} disabled={!active || disable.isPending} onCheckedChange={() => disable.mutate(wakeup.id, { onError: () => toast.error(t(($) => $.wakeups.disable_error)) })} />
            </div>
            <p className="break-words">{wakeup.instruction}</p>
            <p className="break-words text-muted-foreground">{trigger}{wakeup.kind !== "at" && <> · {wakeup.mode === "once" ? t(($) => $.wakeups.once) : t(($) => $.wakeups.continuous)}</>}</p>
            {wakeup.filter_task_id && <p className="truncate text-muted-foreground" title={wakeup.filter_task_id}>{t(($) => $.wakeups.source_run)} {wakeup.filter_task_id.slice(0, 8)}</p>}
            {wakeup.filter_agent_id && <p className="truncate text-muted-foreground" title={wakeup.filter_agent_id}>{t(($) => $.wakeups.source_agent)} {wakeup.filter_agent_id.slice(0, 8)}</p>}
            {wakeup.enabled && wakeup.next_fire_at && <p className="text-muted-foreground">{t(($) => $.wakeups.next)} {new Date(wakeup.next_fire_at).toLocaleString()}</p>}
            {wakeup.last_error && <p className="text-destructive">{wakeup.last_error}</p>}
            {task && <div className="flex items-center gap-1 text-muted-foreground"><span>{t(($) => $.wakeups.last_run)}</span><TranscriptButton task={task} agentName={wakeup.agent_name} title={t(($) => $.wakeups.last_run)} /></div>}
          </div>;
        })}
      </div>}
    </section>
  );
}
