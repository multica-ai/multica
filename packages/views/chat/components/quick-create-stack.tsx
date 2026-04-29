"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Check, X as XIcon } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useWSEvent } from "@multica/core/realtime";
import { useQuickCreateStore } from "@multica/core/issues/stores/quick-create-store";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import { stripQuickCreatePrefix } from "../../inbox/components/inbox-display";
import { ActorAvatar } from "../../common/actor-avatar";
import type { InboxNewPayload } from "@multica/core/types";

interface ResolvedItem {
  taskId: string;
  agentId: string;
  agentName: string;
  result:
    | { type: "done"; issueId: string; identifier: string; title: string }
    | { type: "failed"; error: string };
  exiting: boolean;
}

const DONE_VISIBLE_MS = 3000;
const FAILED_VISIBLE_MS = 5000;
const EXIT_ANIMATION_MS = 300;

/**
 * Stacked circular indicators above the Chat FAB showing in-flight
 * quick-create tasks. Each pill shows the agent avatar with a spinning
 * ring while pending, and transitions to a success/failure state when
 * the `inbox:new` WS event arrives.
 *
 * Hover expands the pill to reveal the agent name / result text.
 */
export function QuickCreateStack() {
  const pendingTasks = useQuickCreateStore((s) => s.pendingTasks);
  const removePendingTask = useQuickCreateStore((s) => s.removePendingTask);
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const [resolved, setResolved] = useState<Record<string, ResolvedItem>>({});
  const timersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  // Schedule auto-removal of a resolved item.
  const scheduleRemoval = useCallback((taskId: string, delayMs: number) => {
    // Phase 1: mark as exiting (triggers fade-out animation)
    const exitTimer = setTimeout(() => {
      setResolved((prev) => {
        const item = prev[taskId];
        if (!item) return prev;
        return { ...prev, [taskId]: { ...item, exiting: true } };
      });
      // Phase 2: remove from state after animation completes
      const removeTimer = setTimeout(() => {
        setResolved((prev) => {
          const { [taskId]: _, ...rest } = prev;
          return rest;
        });
        timersRef.current.delete(taskId);
      }, EXIT_ANIMATION_MS);
      timersRef.current.set(taskId, removeTimer);
    }, delayMs);
    timersRef.current.set(taskId, exitTimer);
  }, []);

  // Clean up timers on unmount.
  useEffect(() => {
    return () => {
      timersRef.current.forEach(clearTimeout);
    };
  }, []);

  // Listen for quick-create inbox events.
  const handler = useCallback(
    (payload: unknown) => {
      const { item } = payload as InboxNewPayload;
      if (!item) return;

      const taskId = item.details?.task_id;
      if (!taskId) return;

      const pending = useQuickCreateStore.getState().pendingTasks[taskId];
      if (!pending) return;

      if (item.type === "quick_create_done") {
        const identifier = item.details?.identifier ?? "";
        const title = stripQuickCreatePrefix(item.title, identifier);
        setResolved((prev) => ({
          ...prev,
          [taskId]: {
            taskId,
            agentId: pending.agentId,
            agentName: pending.agentName,
            result: {
              type: "done",
              issueId: item.issue_id ?? "",
              identifier,
              title: title || "Issue created",
            },
            exiting: false,
          },
        }));
        removePendingTask(taskId);
        scheduleRemoval(taskId, DONE_VISIBLE_MS);
      } else if (item.type === "quick_create_failed") {
        const error =
          item.details?.error || item.body || "Quick create did not finish";
        setResolved((prev) => ({
          ...prev,
          [taskId]: {
            taskId,
            agentId: pending.agentId,
            agentName: pending.agentName,
            result: { type: "failed", error },
            exiting: false,
          },
        }));
        removePendingTask(taskId);
        scheduleRemoval(taskId, FAILED_VISIBLE_MS);
      }
    },
    [removePendingTask, scheduleRemoval],
  );

  useWSEvent("inbox:new", handler);

  // Merge pending + resolved into a single render list.
  const pendingItems = Object.values(pendingTasks);
  const resolvedItems = Object.values(resolved);
  const allItems = [...pendingItems.map((t) => ({ ...t, resolved: null as ResolvedItem | null })),
    ...resolvedItems.map((r) => ({ taskId: r.taskId, agentId: r.agentId, agentName: r.agentName, prompt: "", resolved: r as ResolvedItem | null }))];

  if (allItems.length === 0) return null;

  return (
    <div className="absolute bottom-14 right-2 z-50 flex flex-col gap-2 items-end pointer-events-none">
      {allItems.map((item) => {
        const isDone = item.resolved?.result.type === "done";
        const isFailed = item.resolved?.result.type === "failed";
        const isPending = !item.resolved;
        const isExiting = item.resolved?.exiting ?? false;

        const handleClick = () => {
          if (isDone && item.resolved?.result.type === "done") {
            const issueId = item.resolved.result.issueId;
            if (issueId) navigation.push(paths.issueDetail(issueId));
          }
        };

        return (
          <div
            key={item.taskId}
            className={cn(
              "pointer-events-auto transition-all duration-300 ease-out",
              isExiting && "opacity-0 translate-y-2",
            )}
          >
            <div
              role={isDone ? "button" : undefined}
              onClick={isDone ? handleClick : undefined}
              className={cn(
                "group/pill relative flex items-center rounded-full bg-card shadow-sm overflow-hidden transition-all duration-200 ease-out h-8",
                "max-w-8 hover:max-w-72 hover:pr-3",
                isDone && "cursor-pointer",
              )}
            >
              {/* Avatar circle */}
              <div className="relative size-8 shrink-0 flex items-center justify-center">
                <ActorAvatar actorType="agent" actorId={item.agentId} size={20} />

                {/* Spinning ring for pending */}
                {isPending && (
                  <div className="absolute inset-0 rounded-full border-2 border-transparent border-t-brand animate-spin pointer-events-none" />
                )}

                {/* Success ring + icon */}
                {isDone && (
                  <>
                    <div className="absolute inset-0 rounded-full ring-2 ring-emerald-500/60 pointer-events-none" />
                    <div className="absolute inset-0 flex items-center justify-center rounded-full bg-emerald-500/20 pointer-events-none">
                      <Check className="size-3.5 text-emerald-600 dark:text-emerald-400" />
                    </div>
                  </>
                )}

                {/* Failure ring + icon */}
                {isFailed && (
                  <>
                    <div className="absolute inset-0 rounded-full ring-2 ring-destructive/60 pointer-events-none" />
                    <div className="absolute inset-0 flex items-center justify-center rounded-full bg-destructive/20 pointer-events-none">
                      <XIcon className="size-3.5 text-destructive" />
                    </div>
                  </>
                )}
              </div>

              {/* Expanded label (visible on hover) */}
              <span className="text-xs whitespace-nowrap text-muted-foreground opacity-0 group-hover/pill:opacity-100 transition-opacity duration-150 ml-1.5 select-none">
                {isPending && `${item.agentName} is creating…`}
                {isDone && item.resolved?.result.type === "done" && (
                  <span>
                    <span className="text-foreground font-medium">{item.resolved.result.identifier}</span>
                    {" created"}
                  </span>
                )}
                {isFailed && "Failed to create issue"}
              </span>
            </div>
          </div>
        );
      })}
    </div>
  );
}
