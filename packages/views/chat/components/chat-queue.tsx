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
      // Tucked under the composer: the negative margin slides this card's
      // bottom edge behind the (opaque, always z-10) composer surface, so the
      // queue reads as emerging from behind the input. z-0 keeps it there even
      // while the entrance animation's opacity/transform spawns a stacking
      // context. The composer's own chrome never changes.
      className={cn(CHAT_GUTTER, "relative z-0 -mb-3")}
      aria-live="polite"
      aria-busy={busyAction !== null}
    >
      <div className={CHAT_COLUMN}>
        <section
          data-slot="chat-queue"
          aria-label={t(($) => $.queue.title, { count: tasks.length })}
          className={cn(
            // Quiet secondary surface: border only, no shadow, so the composer
            // below stays the visually dominant card. pb-4 = the 12px hidden
            // behind the composer plus a visible 4px breathing room above it.
            // mx-3 pulls the rear card in from the composer's edges so the
            // stack reads as two distinct layers.
            "mx-3 overflow-hidden rounded-lg border border-surface-border bg-surface pb-4",
            "animate-in fade-in slide-in-from-bottom-2 duration-300",
          )}
        >
          <div
            data-slot="chat-queue-list"
            className="max-h-40 overflow-y-auto px-2 py-1"
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
                  className="flex min-h-7 min-w-0 items-center gap-2 rounded-md px-1.5 py-0.5 text-caption animate-in fade-in duration-200"
                >
                  <ListEnd
                    data-slot="chat-queue-item-icon"
                    className="size-3.5 shrink-0 text-faint-foreground"
                    aria-hidden="true"
                  />
                  <span className="min-w-0 flex-1 truncate text-muted-foreground">
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
                        size="xs"
                        className="px-1.5 font-normal text-muted-foreground"
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
                        <Trash2 aria-hidden="true" />
                      )}
                    </Button>
                    <DropdownMenu>
                      <DropdownMenuTrigger
                        render={
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-xs"
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
    </div>
  );
}
