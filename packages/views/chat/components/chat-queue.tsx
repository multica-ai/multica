"use client";

import { useState } from "react";
import { Loader2, X } from "lucide-react";
import type { ChatQueuedTask } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

interface ChatQueueProps {
  tasks: ChatQueuedTask[];
  onRemove: (taskId: string) => Promise<void> | void;
}

export function ChatQueue({ tasks, onRemove }: ChatQueueProps) {
  const { t } = useT("chat");
  const [removingTaskId, setRemovingTaskId] = useState<string | null>(null);

  if (tasks.length === 0) return null;

  const remove = async (taskId: string) => {
    setRemovingTaskId(taskId);
    try {
      await onRemove(taskId);
    } finally {
      setRemovingTaskId((current) => current === taskId ? null : current);
    }
  };

  return (
    <div className="mx-3 mb-2 rounded-lg border bg-muted/30 p-2" aria-live="polite">
      <div className="mb-1 px-1 text-caption font-medium text-muted-foreground">
        {t(($) => $.queue.title, { count: tasks.length })}
      </div>
      <div className="space-y-0.5">
        {tasks.map((task, index) => {
          const removing = removingTaskId === task.task_id;
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
                disabled={removing}
                aria-label={t(($) => $.queue.remove)}
                onClick={() => void remove(task.task_id)}
              >
                {removing ? (
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
