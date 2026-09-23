"use client";

import { useQueries } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { taskInteractionOptions } from "@multica/core/chat";
import type { AgentTask } from "@multica/core/types";
import { Alert, AlertDescription } from "@multica/ui/components/ui/alert";
import { IssueConversationChat } from "./issue-conversation-chat";
import { buildTimeline } from "../../common/task-transcript/build-timeline";
import { buildSteps } from "../../common/task-transcript/build-steps";
import { buildRunOutcome } from "../../common/task-transcript/run-outcome";
import { conversationHumanEntries, issueConversationTimeline, type ConversationTimelineItem } from "./issue-conversation-timeline";
import { useT } from "../../i18n";

/** Runs are already scoped to one issue/agent and sorted oldest first. */
export function IssueFullChat({ runs }: { runs: AgentTask[] }) {
  const workspaceId = useWorkspaceId();
  const { t } = useT("agents");
  const messages = useQueries({ queries: runs.map((run) => ({
    ...taskMessagesOptions(run.id), refetchInterval: run.status === "running" ? 1000 : false as const,
  })) });
  const records = useQueries({ queries: runs.map((run) => ({
    ...taskInteractionOptions(workspaceId, run.id, true), refetchInterval: run.status === "running" ? 1000 : false as const,
  })) });
  const labels = { human: t(($) => $.interaction.human), interrupt: t(($) => $.interaction.interrupt),
    finish: t(($) => $.interaction.finish), pending: t(($) => $.interaction.pending) };
  const items: ConversationTimelineItem[] = runs.flatMap((run, index) => [
    { seq: 0, type: "text" as const, divider: true, runId: run.id,
      content: `${t(($) => $.interaction.run, { number: index + 1 })} · ${run.status}` },
    ...issueConversationTimeline(messages[index]?.data ?? [],
      conversationHumanEntries(records[index]?.data, run.created_at, labels))
      .map((item) => ({ ...item, runId: run.id })),
  ]);
  const runSummaries = Object.fromEntries(runs.map((run, index) => [run.id, {
    outcome: buildRunOutcome(buildSteps(buildTimeline(messages[index]?.data ?? []))),
    branch: run.branch_name,
  }]));
  const failed = messages.some((query) => query.isError) || records.some((query) => query.isError);
  return <div className="flex min-h-0 flex-1 flex-col">
    {failed && <Alert variant="destructive"><AlertDescription>{t(($) => $.interaction.history_error)}</AlertDescription></Alert>}
    <div className="min-h-0 flex-1"><IssueConversationChat items={items} isLive={runs.some((run) => run.status === "running")} runSummaries={runSummaries} /></div>
  </div>;
}
