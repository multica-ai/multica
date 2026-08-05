"use client";

import { useState } from "react";
import {
  CornerDownRight,
  Ellipsis,
  ListEnd,
  Loader2,
  Pencil,
  Trash2,
} from "lucide-react";
import type { ChatQueuedTask } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { CHAT_COLUMN, CHAT_GUTTER } from "./chat-column";

interface ChatQueueProps {
  tasks: ChatQueuedTask[];
  headStatus: string | undefined;
  onSendNow: (taskId: string) => Promise<void> | void;
  onEdit: (taskId: string) => Promise<void> | void;
  onRemove: (taskId: string) => Promise<void> | void;
  onClear: () => Promise<void> | void;
}

export function ChatQueue({
  tasks,
  headStatus,
  onSendNow,
  onEdit,
  onRemove,
  onClear,
}: ChatQueueProps) {
  const { t } = useT("chat");
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const canSendNow =
    headStatus === "dispatched" ||
    headStatus === "running" ||
    headStatus === "waiting_local_directory";

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
      data-slot="chat-queue-shell"
      className={cn(CHAT_GUTTER, "relative z-0 -mb-8")}
      aria-live="polite"
      aria-busy={busyAction !== null}
    >
      <section
        data-slot="chat-queue"
        aria-label={t(($) => $.queue.title, { count: tasks.length })}
        className={cn(
          CHAT_COLUMN,
          // This is the rear card in the attached stack. The composer uses
          // the stronger menu shadow so it reads as the foreground surface.
          "overflow-hidden rounded-4xl border border-surface-border bg-surface pb-10 shadow-[var(--surface-shadow)]",
        )}
      >
        <div
          data-slot="chat-queue-list"
          className="max-h-48 overflow-y-auto px-4 pt-3"
        >
          {tasks.map((task) => {
            const sendNowKey = `send-now:${task.task_id}`;
            const editKey = `edit:${task.task_id}`;
            const removeKey = `remove:${task.task_id}`;
            const clearKey = `clear:${task.task_id}`;
            return (
              <div
                key={task.task_id}
                data-slot="chat-queue-row"
                className="flex min-h-12 min-w-0 items-center gap-3 rounded-xl px-1 py-1 text-body"
              >
                <ListEnd
                  data-slot="chat-queue-item-icon"
                  className="size-5 shrink-0 text-faint-foreground"
                  aria-hidden="true"
                />
                <span className="min-w-0 flex-1 truncate">
                  {task.content?.trim() || t(($) => $.queue.fallback)}
                </span>
                <div className="flex shrink-0 items-center gap-0.5">
                  <span
                    className="shrink-0"
                    title={t(($) =>
                      canSendNow ? $.queue.steer : $.queue.steer_unavailable
                    )}
                  >
                    <Button
                      variant="ghost"
                      size="sm"
                      className="gap-1.5 px-2 font-normal text-muted-foreground"
                      disabled={busyAction !== null || !canSendNow}
                      aria-label={t(($) =>
                        canSendNow
                          ? $.queue.steer
                          : $.queue.steer_unavailable
                      )}
                      onClick={() => void run(sendNowKey, () => onSendNow(task.task_id))}
                    >
                      {busyAction === sendNowKey ? (
                        <Loader2 className="animate-spin" aria-hidden="true" />
                      ) : (
                        <CornerDownRight aria-hidden="true" />
                      )}
                      {t(($) => $.queue.steer)}
                    </Button>
                  </span>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="shrink-0 text-muted-foreground"
                    disabled={busyAction !== null}
                    title={t(($) => $.queue.remove)}
                    aria-label={t(($) => $.queue.remove)}
                    onClick={() => void run(removeKey, () => onRemove(task.task_id))}
                  >
                    {busyAction === removeKey ? (
                      <Loader2 className="animate-spin" aria-hidden="true" />
                    ) : (
                      <Trash2 aria-hidden="true" />
                    )}
                  </Button>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          className="shrink-0 text-muted-foreground"
                          disabled={busyAction !== null}
                          title={t(($) => $.queue.more)}
                          aria-label={t(($) => $.queue.more)}
                        />
                      }
                    >
                      {busyAction === editKey || busyAction === clearKey ? (
                        <Loader2 className="animate-spin" aria-hidden="true" />
                      ) : (
                        <Ellipsis aria-hidden="true" />
                      )}
                    </DropdownMenuTrigger>
                    <DropdownMenuContent
                      align="end"
                      side="top"
                      sideOffset={6}
                      className="w-auto"
                    >
                      <DropdownMenuItem
                        disabled={busyAction !== null}
                        onClick={() => void run(editKey, () => onEdit(task.task_id))}
                      >
                        <Pencil aria-hidden="true" />
                        {t(($) => $.queue.edit)}
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        variant="destructive"
                        disabled={busyAction !== null}
                        onClick={() => void run(clearKey, onClear)}
                      >
                        <Trash2 aria-hidden="true" />
                        {t(($) => $.queue.clear)}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
            );
          })}
        </div>
      </section>
    </div>
  );
}
