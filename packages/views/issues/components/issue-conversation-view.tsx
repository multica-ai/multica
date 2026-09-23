"use client";

import { useMemo, useState, type ReactNode } from "react";
import type { AgentTask } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { IssueConversation } from "./issue-conversation";
import { IssueConversationViewContext } from "./issue-conversation-view-context";

export function IssueConversationView({ issueId, tasks, children, header }: {
  issueId: string; tasks: AgentTask[]; children: ReactNode; header?: ReactNode;
}) {
  const [runId, setRunId] = useState<string | null>(null);
  const value = useMemo(() => ({ issueId, open: setRunId }), [issueId]);
  return <IssueConversationViewContext.Provider value={value}>
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      {header}
      {/* Keep editors and the issue's scroll position mounted while chatting. */}
      <div hidden={runId !== null} className={cn("relative min-h-0 flex-1", runId === null ? "flex" : "hidden")}>
        {children}
      </div>
      {runId !== null && <IssueConversation inline issueId={issueId} initialTasks={tasks}
        initialRunId={runId} onClose={() => setRunId(null)} />}
    </div>
  </IssueConversationViewContext.Provider>;
}
