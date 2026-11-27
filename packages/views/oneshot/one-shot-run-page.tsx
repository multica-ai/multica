"use client";

import { useMemo, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { AgentTranscriptDialog, buildTimeline } from "../common/task-transcript";
import { useT } from "../i18n";

const ACTIVE = new Set(["queued", "dispatched", "running", "waiting_local_directory"]);

export function OneShotRunPage({ taskId }: { taskId: string }) {
  const wsId = useWorkspaceId();
  const { t } = useT("common");
  const [open, setOpen] = useState(true);
  const run = useQuery({
    queryKey: ["oneshot-run", wsId, taskId],
    queryFn: () => api.getOneShotRun(taskId),
    refetchInterval: (query) => ACTIVE.has(query.state.data?.task.status ?? "queued") ? 2000 : false,
  });
  const live = !!run.data && ACTIVE.has(run.data.task.status);
  const messages = useQuery({
    queryKey: ["oneshot-run-messages", wsId, taskId, live],
    placeholderData: keepPreviousData,
    queryFn: () => api.listTaskMessages(taskId),
    enabled: !!run.data,
    refetchInterval: live ? 2000 : false,
  });
  const items = useMemo(() => buildTimeline(messages.data ?? []), [messages.data]);

  if (!run.data) {
    return <div className="p-6" role="status">
      {run.isError ? t(($) => $.oneshot.load_failed) : t(($) => $.oneshot.loading)}
      {run.isError && <Button onClick={() => void run.refetch()}>{t(($) => $.oneshot.retry)}</Button>}
    </div>;
  }

  return <div className="flex h-full min-h-0 flex-col gap-4 overflow-auto p-6">
    <h1 className="text-display-sm">{t(($) => $.oneshot.title)}</h1>
    <p className="whitespace-pre-wrap break-words">{run.data.input.user_prompt}</p>
    <Button className="self-start" onClick={() => setOpen(true)}>{t(($) => $.oneshot.open_log)}</Button>
    <AgentTranscriptDialog
      open={open}
      onOpenChange={setOpen}
      task={run.data.task}
      agentName={t(($) => $.oneshot.title)}
      items={items}
      isLive={live}
      contentState={messages.isError ? <Button onClick={() => void messages.refetch()}>{t(($) => $.oneshot.retry)}</Button> : undefined}
      onTranscriptPurged={() => void messages.refetch()}
    />
  </div>;
}
