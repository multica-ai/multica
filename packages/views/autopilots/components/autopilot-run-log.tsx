"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import {
  autopilotRunOptions,
  autopilotRunTranscriptOptions,
} from "@multica/core/autopilots/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { AutopilotRun } from "@multica/core/types";
import type { AgentTask } from "@multica/core/types/agent";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  currentPath,
  useNavigation,
  type NavigationAdapter,
} from "../../navigation";
import { buildTimeline, TranscriptButton } from "../../common/task-transcript";
import { useT } from "../../i18n";
import { WebhookPayloadPreview } from "./webhook-payload-preview";

export function autopilotRunLogPath(
  navigation: NavigationAdapter,
  runId?: string,
): string {
  const searchParams = new URLSearchParams(navigation.searchParams);
  if (runId) searchParams.set("run", runId);
  else searchParams.delete("run");
  return currentPath({ ...navigation, searchParams });
}

function LogState({
  children,
  close,
}: {
  children: React.ReactNode;
  close: () => void;
}) {
  const { t } = useT("autopilots");
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent>
        <DialogTitle>{t(($) => $.run.view_log)}</DialogTitle>
        {children}
      </DialogContent>
    </Dialog>
  );
}

function LogError({ error, retry }: { error: unknown; retry: () => void }) {
  const { t } = useT("autopilots");
  const unavailable =
    error instanceof ApiError && [403, 404].includes(error.status);
  return (
    <div className="space-y-3">
      <p role="alert">
        {unavailable
          ? t(($) => $.run.log_unavailable)
          : t(($) => $.run.log_load_failed)}
      </p>
      {!unavailable && (
        <Button variant="outline" onClick={retry}>
          {t(($) => $.run.retry)}
        </Button>
      )}
    </div>
  );
}

export function AutopilotRunLog({
  autopilotId,
  runId,
  agentId,
  agentName,
}: {
  autopilotId: string;
  runId: string;
  agentId: string;
  agentName: string;
}) {
  const { t } = useT("autopilots");
  const { t: tCommon } = useT("common");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const query = useQuery({
    ...autopilotRunOptions(wsId, autopilotId, runId),
    retry: false,
  });
  const close = () => navigation.replace(autopilotRunLogPath(navigation));

  if (query.isPending) {
    return (
      <LogState close={close}>
        <p role="status">{tCommon(($) => $.loading)}</p>
      </LogState>
    );
  }
  if (query.isError) {
    return (
      <LogState close={close}>
        <LogError error={query.error} retry={() => void query.refetch()} />
      </LogState>
    );
  }
  const run = query.data;
  if (!run.task_id || run.issue_id) {
    return (
      <LogState close={close}>
        <p>{t(($) => $.run.no_log)}</p>
      </LogState>
    );
  }
  return (
    <RunTranscript
      key={run.task_id}
      run={run}
      agentId={agentId}
      agentName={agentName}
      close={close}
    />
  );
}

function RunTranscript({
  run,
  agentId,
  agentName,
  close,
}: {
  run: AutopilotRun;
  agentId: string;
  agentName: string;
  close: () => void;
}) {
  const { t: tCommon } = useT("common");
  const wsId = useWorkspaceId();
  const live = run.status === "running";
  // A run opened live stays on the shared cache through its final backfill.
  const [startedLive] = useState(live);
  const query = useQuery({
    ...autopilotRunTranscriptOptions(
      wsId,
      run.autopilot_id,
      run.id,
      run.task_id!,
    ),
    enabled: !startedLive,
  });
  const items = useMemo(() => buildTimeline(query.data ?? []), [query.data]);
  const task: AgentTask = {
    id: run.task_id!,
    agent_id: agentId,
    runtime_id: "",
    issue_id: "",
    status: live
      ? "running"
      : run.status === "completed"
        ? "completed"
        : run.status === "failed"
          ? "failed"
          : "queued",
    priority: 0,
    dispatched_at: null,
    started_at: run.triggered_at || null,
    completed_at: run.completed_at || null,
    result: null,
    error: run.failure_reason || null,
    created_at: run.created_at,
  };

  if (!startedLive && query.isPending) {
    return (
      <LogState close={close}>
        <p role="status">{tCommon(($) => $.loading)}</p>
      </LogState>
    );
  }
  if (!startedLive && query.isError) {
    return (
      <LogState close={close}>
        <LogError error={query.error} retry={() => void query.refetch()} />
      </LogState>
    );
  }
  return (
    <TranscriptButton
      task={task}
      agentName={agentName}
      items={startedLive ? undefined : items}
      isLive={live}
      renderButton={false}
      closeOnNavigate={false}
      renderLoadError={(error, retry) => (
        <LogError error={error} retry={retry} />
      )}
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
      headerSlot={
        run.source === "webhook" && run.trigger_payload != null ? (
          <WebhookPayloadPreview payload={run.trigger_payload} />
        ) : undefined
      }
    />
  );
}
