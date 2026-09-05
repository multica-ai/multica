"use client";

import { useQuery } from "@tanstack/react-query";
import { Bot, Circle, CircleCheck, CircleX, Hand } from "lucide-react";
import type { Issue } from "@multica/core/types";
import { issueAutomationExecutionsOptions, useTakeOverAutomationExecution } from "@multica/core/issue-lifecycles";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const ACTIVE = new Set(["pending", "queued", "running"]);

function StateIcon({ status }: { status: string }) {
  if (status === "completed") return <CircleCheck className="size-3.5 text-emerald-500" />;
  if (["failed", "cancelled", "superseded"].includes(status)) return <CircleX className="size-3.5 text-muted-foreground" />;
  if (ACTIVE.has(status)) return <Circle className="size-3.5 fill-blue-500 text-blue-500" />;
  return <Circle className="size-3.5 text-muted-foreground" />;
}

export function AutomationExecutionSection({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: executions = [] } = useQuery(issueAutomationExecutionsOptions(wsId, issue.id));
  const takeOver = useTakeOverAutomationExecution();
  const { getActorName } = useActorName();
  if (executions.length === 0) return null;

  const latest = executions[0];
  if (!latest) return null;
  const active = ACTIVE.has(latest.status);
  const executorName = latest.executor_type && latest.executor_id
    ? getActorName(latest.executor_type, latest.executor_id)
    : t(($) => $.automation.manual);

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between px-2 text-caption font-medium">
        <span>{t(($) => $.automation.title)}</span>
        <span className="font-normal text-muted-foreground">{t(($) => $.automation.entry_count, { count: executions.length })}</span>
      </div>
      <div className="rounded-lg border p-2.5 space-y-2">
        <div className="flex items-center gap-2 text-caption">
          <StateIcon status={latest.status} />
          <Bot className="size-3.5 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate">{executorName}</span>
          <span className={cn("capitalize text-muted-foreground", active && "text-blue-600 dark:text-blue-400")}>{latest.status}</span>
        </div>
        {latest.policy_snapshot.instructions && (
          <p className="line-clamp-3 whitespace-pre-wrap text-caption text-muted-foreground">{latest.policy_snapshot.instructions}</p>
        )}
        {active && (
          <Button
            size="xs"
            variant="outline"
            className="w-full"
            disabled={takeOver.isPending}
            onClick={() => takeOver.mutate({ issueId: issue.id, executionId: latest.id, expectedRevision: issue.revision })}
          >
            <Hand className="size-3" />
            {t(($) => $.automation.take_over)}
          </Button>
        )}
      </div>
    </div>
  );
}
