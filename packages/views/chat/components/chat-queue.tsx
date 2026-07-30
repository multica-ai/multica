"use client";

import { useState } from "react";
import { Loader2, Pencil, Send, X } from "lucide-react";
import type { ChatQueuedTask } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

interface ChatQueueProps {
  tasks: ChatQueuedTask[];
  onSendNow: (taskId: string) => Promise<void> | void;
  onEdit: (taskId: string) => Promise<void> | void;
  onRemove: (taskId: string) => Promise<void> | void;
  onClear: () => Promise<void> | void;
}

export function ChatQueue({ tasks, onSendNow, onEdit, onRemove, onClear }: ChatQueueProps) {
  const { t } = useT("chat");
  const [busyAction, setBusyAction] = useState<string | null>(null);

  if (tasks.length === 0) return null;

  const run = async (key: string, action: () => Promise<void> | void) => {
    setBusyAction(key);
    try {
      await action();
    } finally {
      setBusyAction((current) => current === key ? null : current);
    }
  };

  return (
    <div
      className="mx-3 mb-2 rounded-lg border bg-muted/30 p-2"
      aria-live="polite"
      aria-busy={busyAction !== null || undefined}
    >
      <div className="mb-1 flex items-center justify-between gap-2 px-1">
        <span className="text-caption font-medium text-muted-foreground">
          {t(($) => $.queue.title, { count: tasks.length })}
        </span>
        <Button
          variant="ghost"
          size="xs"
          disabled={busyAction !== null}
          onClick={() => void run("clear", onClear)}
        >
          {busyAction === "clear" && <Loader2 className="animate-spin" aria-hidden="true" />}
          {t(($) => $.queue.clear)}
        </Button>
      </div>
      <div className="max-h-40 space-y-0.5 overflow-y-auto">
        {tasks.map((task, index) => {
          const sendNowKey = `send-now:${task.task_id}`;
          const editKey = `edit:${task.task_id}`;
          const removeKey = `remove:${task.task_id}`;
          return (
            <div
              key={task.task_id}
              className="flex min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-body"
            >
              <span className="w-4 shrink-0 text-center text-caption tabular-nums text-muted-foreground">
                {index + 1}
              </span>
              <span className="min-w-0 flex-1 truncate">
                {task.content?.trim() || t(($) => $.queue.fallback)}
              </span>
              <Button
                variant="ghost"
                size="icon-xs"
                className="shrink-0 text-muted-foreground"
                disabled={busyAction !== null}
                title={t(($) => $.queue.send_now)}
                aria-label={t(($) => $.queue.send_now)}
                onClick={() => void run(sendNowKey, () => onSendNow(task.task_id))}
              >
                {busyAction === sendNowKey ? (
                  <Loader2 className="animate-spin" aria-hidden="true" />
                ) : (
                  <Send aria-hidden="true" />
                )}
              </Button>
              <Button
                variant="ghost"
                size="icon-xs"
                className="shrink-0 text-muted-foreground"
                disabled={busyAction !== null}
                title={t(($) => $.queue.edit)}
                aria-label={t(($) => $.queue.edit)}
                onClick={() => void run(editKey, () => onEdit(task.task_id))}
              >
                {busyAction === editKey ? (
                  <Loader2 className="animate-spin" aria-hidden="true" />
                ) : (
                  <Pencil aria-hidden="true" />
                )}
              </Button>
              <Button
                variant="ghost"
                size="icon-xs"
                className="shrink-0 text-muted-foreground"
                disabled={busyAction !== null}
                title={t(($) => $.queue.remove)}
                aria-label={t(($) => $.queue.remove)}
                onClick={() => void run(removeKey, () => onRemove(task.task_id))}
              >
                {busyAction === removeKey ? (
                  <Loader2 className="animate-spin" aria-hidden="true" />
                ) : (
                  <X aria-hidden="true" />
                )}
              </Button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
