"use client";

import { useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import {
  CornerDownRight,
  Ellipsis,
  ListEnd,
  Loader2,
  Pencil,
  Trash2,
} from "@multica/ui/icons";
import { useReducedMotion } from "@multica/core/appearance";
import type { ChatQueuedTask, ChatWakeup } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { CHAT_COLUMN, CHAT_GUTTER } from "./chat-column";

interface ChatQueueProps {
  tasks: ChatQueuedTask[];
  /** Names the wakeup behind a queued wakeup turn. */
  wakeups?: ChatWakeup[];
  headStatus: string | undefined;
  onSendNow: (taskId: string) => Promise<void> | void;
  /** Blocks "send now" independently of the head task's status — used when the
   *  caller may no longer invoke the agent, since steering a queued task
   *  dispatches a run the server would refuse (MUL-6380). */
  sendNowDisabled?: boolean;
  onEdit: (taskId: string) => Promise<void> | void;
  onRemove: (taskId: string) => Promise<void> | void;
  onClear: () => Promise<void> | void;
}

export function ChatQueue({
  tasks,
  wakeups,
  headStatus,
  onSendNow,
  sendNowDisabled = false,
  onEdit,
  onRemove,
  onClear,
}: ChatQueueProps) {
  const { t } = useT("chat");
  const prefersReducedMotion = useReducedMotion();
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const dispatchableHead =
    headStatus === "dispatched" ||
    headStatus === "running" ||
    headStatus === "waiting_local_directory";
  const canSendNow = !sendNowDisabled && dispatchableHead;
  // The two blocked states need different copy: "wait for the reply to start"
  // is actionable, "you cannot run this agent" is not — telling a user to wait
  // for something waiting cannot fix is the bug (MUL-6380).
  const sendNowLabel = t(($) =>
    canSendNow
      ? $.queue.steer
      : sendNowDisabled
        ? $.queue.steer_no_permission
        : $.queue.steer_unavailable,
  );

  const queuedLabel = (task: ChatQueuedTask) => {
    if (task.wakeup_id) {
      const title = wakeups?.find((w) => w.id === task.wakeup_id)?.title;
      return title
        ? t(($) => $.wakeup.queue_label, { title })
        : t(($) => $.wakeup.queue_fallback);
    }
    return task.content?.trim() || t(($) => $.queue.fallback);
  };

  const run = async (key: string, action: () => Promise<void> | void) => {
    setBusyAction(key);
    try {
      await action();
    } finally {
      setBusyAction((current) => current === key ? null : current);
    }
  };

  const shellTransition = prefersReducedMotion
    ? { duration: 0 }
    : { duration: UI_MOTION_DURATION.standard, ease: UI_EASE_OUT };

  return (
    <AnimatePresence>
      {tasks.length > 0 && (
        <motion.div
          key="chat-queue-shell"
          data-slot="chat-queue-shell"
          // Tucked under the composer: the negative margin slides this card's
          // bottom edge behind the (opaque, always z-10) composer surface, so the
          // queue reads as emerging from behind the input. z-0 keeps it there even
          // while the entrance animation's opacity/transform spawns a stacking
          // context. The composer's own chrome never changes.
          initial={{ opacity: 0, y: prefersReducedMotion ? 0 : 8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: prefersReducedMotion ? 0 : 8 }}
          transition={shellTransition}
          className={cn(CHAT_GUTTER, "relative z-0 -mb-3")}
          aria-live="polite"
          aria-busy={busyAction !== null}
        >
          <div className={CHAT_COLUMN}>
            <section
              data-slot="chat-queue"
              aria-label={t(($) => $.queue.title, { count: tasks.length })}
              className="mx-3 overflow-hidden rounded-lg border border-surface-border bg-surface pb-4"
            >
              <div
                data-slot="chat-queue-list"
                className="max-h-40 overflow-y-auto px-2 py-1"
              >
                <AnimatePresence initial={false}>
                  {tasks.map((task) => {
                    const sendNowKey = `send-now:${task.task_id}`;
                    const editKey = `edit:${task.task_id}`;
                    const removeKey = `remove:${task.task_id}`;
                    const clearKey = `clear:${task.task_id}`;
                    return (
                      <motion.div
                        key={task.task_id}
                        data-slot="chat-queue-row"
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{
                          duration: prefersReducedMotion ? 0 : UI_MOTION_DURATION.fast,
                          ease: UI_EASE_OUT,
                        }}
                        className="flex min-h-7 min-w-0 items-center gap-2 rounded-md px-1.5 py-0.5 text-caption"
                      >
                        <ListEnd
                          data-slot="chat-queue-item-icon"
                          className="size-3.5 shrink-0 text-faint-foreground"
                          aria-hidden="true"
                        />
                        <span className="min-w-0 flex-1 truncate text-muted-foreground">
                          {queuedLabel(task)}
                        </span>
                        <div className="flex shrink-0 items-center gap-0.5">
                          <span
                            className="shrink-0"
                            title={sendNowLabel}
                          >
                            <Button
                              variant="ghost"
                              size="xs"
                              className="px-1.5 font-normal text-muted-foreground"
                              disabled={busyAction !== null || !canSendNow}
                              aria-label={sendNowLabel}
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
                          <IconButton
                            variant="ghost"
                            size="icon-xs"
                            className="shrink-0 text-muted-foreground"
                            disabled={busyAction !== null}
                            label={t(($) => $.queue.remove)}
                            onClick={() => void run(removeKey, () => onRemove(task.task_id))}
                          >
                            {busyAction === removeKey ? (
                              <Loader2 className="animate-spin" aria-hidden="true" />
                            ) : (
                              <Trash2 aria-hidden="true" />
                            )}
                          </IconButton>
                          <DropdownMenu>
                            <DropdownMenuTrigger
                              render={
                                <IconButton
                                  type="button"
                                  variant="ghost"
                                  size="icon-xs"
                                  className="shrink-0 text-muted-foreground"
                                  disabled={busyAction !== null}
                                  label={t(($) => $.queue.more)}
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
                      </motion.div>
                    );
                  })}
                </AnimatePresence>
              </div>
            </section>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
