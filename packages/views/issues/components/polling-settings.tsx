"use client";

import { useState } from "react";
import { Repeat, Pencil, Square } from "lucide-react";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { useT } from "../../i18n";
import { PollingSetupDialog } from "./polling-setup-dialog";

function formatTime(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatIntervalShort(minutes: number): string {
  if (minutes >= 1440 && minutes % 1440 === 0) {
    return `${minutes / 1440}d`;
  }
  if (minutes >= 60 && minutes % 60 === 0) {
    return `${minutes / 60}h`;
  }
  return `${minutes}m`;
}

export function PollingSettings({
  issue,
  onUpdate,
}: {
  issue: Issue;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
}) {
  const { t } = useT("issues");
  const [setupOpen, setSetupOpen] = useState(false);

  const isPolling = issue.status === "polling";
  const hasConfig = issue.poll_interval_minutes != null;

  if (!hasConfig) return null;

  const interval = issue.poll_interval_minutes ?? 30;

  const handleEdit = (newInterval: number) => {
    if (isPolling) {
      onUpdate({ status: "polling", poll_interval_minutes: newInterval });
    } else {
      onUpdate({ poll_interval_minutes: newInterval });
    }
  };

  const handleStop = () => {
    onUpdate({ status: "todo" });
  };

  return (
    <div className="rounded-md border bg-purple-500/5 p-3">
      <div className="flex items-center gap-2 text-sm font-medium text-purple-600 dark:text-purple-400">
        <Repeat className="size-4" />
        <span>{t(($) => $.polling_settings.section_title)}</span>
        {!isPolling && hasConfig && (
          <span className="ml-auto rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
            {t(($) => $.polling_settings.paused)}
          </span>
        )}
      </div>

      <div className="mt-2 grid gap-1 text-xs text-muted-foreground">
        <div className="flex justify-between">
          <span>{t(($) => $.polling_settings.interval_label)}</span>
          <span className="font-medium text-foreground">{formatIntervalShort(interval)}</span>
        </div>
        {isPolling && issue.poll_next_run && (
          <div className="flex justify-between">
            <span>{t(($) => $.polling_settings.next_run_label)}</span>
            <span className="font-medium text-foreground">{formatTime(issue.poll_next_run)}</span>
          </div>
        )}
        {issue.poll_last_run && (
          <div className="flex justify-between">
            <span>{t(($) => $.polling_settings.last_run_label)}</span>
            <span className="font-medium text-foreground">{formatTime(issue.poll_last_run)}</span>
          </div>
        )}
        {issue.poll_run_count != null && issue.poll_run_count > 0 && (
          <div className="flex justify-between">
            <span>{t(($) => $.polling_settings.run_count_label)}</span>
            <span className="font-medium text-foreground">{issue.poll_run_count}</span>
          </div>
        )}
      </div>

      <div className="mt-2 flex gap-2">
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs text-muted-foreground hover:bg-accent"
          onClick={() => setSetupOpen(true)}
        >
          <Pencil className="size-3" />
          {t(($) => $.polling_settings.edit_config)}
        </button>
        {isPolling && (
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded-md border border-destructive/30 px-2 py-1 text-xs text-destructive hover:bg-destructive/10"
            onClick={handleStop}
          >
            <Square className="size-3" />
            {t(($) => $.polling_settings.stop_polling)}
          </button>
        )}
      </div>

      <PollingSetupDialog
        open={setupOpen}
        onOpenChange={setSetupOpen}
        onConfirm={handleEdit}
        defaultInterval={interval}
      />
    </div>
  );
}
