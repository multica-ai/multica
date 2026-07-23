"use client";

import { useCallback, type ReactNode } from "react";
import { ScrollText } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import type { AgentTask } from "@multica/core/types/agent";
import type {
  ExecutionLogActor,
  OpenExecutionLog,
} from "./use-execution-log-session";

interface ExecutionLogTriggerProps {
  task: AgentTask;
  onOpen: OpenExecutionLog;
  className?: string;
  title?: string;
  headerSlot?: ReactNode;
  actor?: ExecutionLogActor;
}

/** Stateless row action. The stable parent surface owns the Dialog session. */
export function ExecutionLogTrigger({
  task,
  onOpen,
  className,
  title = "View execution log",
  headerSlot,
  actor,
}: ExecutionLogTriggerProps) {
  const handleClick = useCallback(
    (event: React.MouseEvent) => {
      event.preventDefault();
      event.stopPropagation();
      onOpen(task, { actor, headerSlot });
    },
    [actor, headerSlot, onOpen, task],
  );

  return (
    <Tooltip>
      <TooltipTrigger
        render={<button type="button" data-testid="execution-log-trigger" />}
        onClick={handleClick}
        aria-label={title}
        className={cn(
          "flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground disabled:opacity-50",
          className,
        )}
      >
        <ScrollText className="h-3.5 w-3.5" />
      </TooltipTrigger>
      <TooltipContent>{title}</TooltipContent>
    </Tooltip>
  );
}
