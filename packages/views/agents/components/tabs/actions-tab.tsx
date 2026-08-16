"use client";

/* eslint-disable i18next/no-literal-string */

import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { api } from "@multica/core/api";

export function ActionsTab({ agent }: { agent: Agent }) {
  const { data: actions = [], isLoading, error } = useQuery({
    queryKey: ["agents", agent.id, "actions"],
    queryFn: () => api.listAgentActions(agent.id),
    staleTime: 15_000,
  });

  if (isLoading) {
    return <p className="text-xs text-muted-foreground">Loading audit actions...</p>;
  }
  if (error) {
    return <p className="text-xs text-destructive">Unable to load audit actions.</p>;
  }
  if (actions.length === 0) {
    return <p className="text-xs text-muted-foreground">No audited actions yet.</p>;
  }

  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">
        Tool calls and task lifecycle events are retained with bounded, redacted summaries.
      </p>
      <div className="divide-y rounded-md border">
        {actions.map((action) => (
          <div key={action.id} className="space-y-1 px-3 py-2 text-xs">
            <div className="flex items-center justify-between gap-3">
              <code className="truncate font-mono font-medium">{action.tool_name}</code>
              <span className="shrink-0 text-muted-foreground">
                {action.status ?? "recorded"}
              </span>
            </div>
            {action.args_summary && (
              <pre className="max-h-24 overflow-auto whitespace-pre-wrap break-words rounded bg-muted p-2 font-mono text-[10px]">
                {action.args_summary}
              </pre>
            )}
            {action.result_summary && (
              <pre className="max-h-24 overflow-auto whitespace-pre-wrap break-words rounded bg-muted p-2 font-mono text-[10px]">
                {action.result_summary}
              </pre>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
