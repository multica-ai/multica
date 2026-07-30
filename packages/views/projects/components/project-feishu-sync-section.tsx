"use client";

import { useEffect, useMemo, useState } from "react";
import { Bot, Check, Copy, Loader2, MessageCircle, RefreshCw, Unlink } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Project } from "@multica/core/types";
import { larkInstallationsOptions } from "@multica/core/lark";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useBeginProjectFeishuBinding,
  useDeleteProjectFeishuBinding,
  useRetryProjectFeishuTopics,
} from "@multica/core/projects/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../../i18n";

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function ProjectFeishuSyncSection({
  project,
  canManage,
}: {
  project: Project;
  canManage: boolean;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: installationData } = useQuery(larkInstallationsOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const beginBinding = useBeginProjectFeishuBinding();
  const deleteBinding = useDeleteProjectFeishuBinding();
  const retryTopics = useRetryProjectFeishuTopics();
  const [installationId, setInstallationId] = useState("");
  const [confirmationCommand, setConfirmationCommand] = useState("");

  const installations = useMemo(
    () => (installationData?.installations ?? []).filter((installation) => installation.status === "active"),
    [installationData?.installations],
  );

  useEffect(() => {
    if (!installationId && installations.length > 0) {
      setInstallationId(installations[0]?.id ?? "");
    }
  }, [installationId, installations]);

  const sync = project.feishu_sync;

  const begin = () => {
    if (!installationId) return;
    beginBinding.mutate(
      { projectId: project.id, installationId },
      {
        onSuccess: (data) => {
          setConfirmationCommand(data.confirmation_command);
          toast.success(t(($) => $.feishu_sync.toast_code_created));
        },
        onError: (error) => {
          toast.error(errorMessage(error, t(($) => $.feishu_sync.toast_bind_failed)));
        },
      },
    );
  };

  const unbind = () => {
    deleteBinding.mutate(project.id, {
      onSuccess: () => {
        setConfirmationCommand("");
        toast.success(t(($) => $.feishu_sync.toast_unbound));
      },
      onError: (error) => {
        toast.error(errorMessage(error, t(($) => $.feishu_sync.toast_unbind_failed)));
      },
    });
  };

  const retry = () => {
    retryTopics.mutate(project.id, {
      onSuccess: (data) => {
        toast.success(
          t(($) => $.feishu_sync.toast_retry_started, {
            count: data.retried_dead_notifications,
          }),
        );
      },
      onError: (error) => {
        toast.error(errorMessage(error, t(($) => $.feishu_sync.toast_retry_failed)));
      },
    });
  };

  return (
    <div>
      <div className="mb-2 flex items-center gap-1 px-2 py-1 text-xs font-medium">
        <Bot className="size-3.5 text-muted-foreground" />
        {t(($) => $.feishu_sync.section_title)}
      </div>

      <div className="space-y-2 rounded-lg border bg-muted/20 p-3 text-xs">
        {sync ? (
          <>
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium">{sync.bot_name || t(($) => $.feishu_sync.bot_fallback)}</span>
              <span className="rounded-full bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
                {sync.state === "active"
                  ? t(($) => $.feishu_sync.state_active)
                  : t(($) => $.feishu_sync.state_pending)}
              </span>
            </div>
            <div className="space-y-1 text-muted-foreground">
              <div>{t(($) => $.feishu_sync.agent_label, { name: sync.agent_name })}</div>
              <div className="flex items-center gap-1">
                <MessageCircle className="size-3" />
                {sync.chat_name || t(($) => $.feishu_sync.group_pending)}
              </div>
              {sync.state === "active" && (
                <div>
                  {t(($) => $.feishu_sync.issue_summary, {
                    bound: sync.bound_issue_count,
                    total: sync.total_issue_count,
                    pending: sync.pending_notification_count,
                  })}
                </div>
              )}
            </div>

            {sync.state === "pending_group" && (
              <div className="rounded-md border border-dashed bg-background p-2">
                {confirmationCommand ? (
                  <>
                    <p className="mb-1 text-muted-foreground">
                      {t(($) => $.feishu_sync.confirm_hint)}
                    </p>
                    <button
                      type="button"
                      className="flex w-full items-center justify-between gap-2 rounded bg-muted px-2 py-1 font-mono"
                      onClick={() => {
                        void copyText(confirmationCommand).then((ok) => {
                          if (ok) toast.success(t(($) => $.feishu_sync.toast_command_copied));
                        });
                      }}
                    >
                      <span className="truncate">{confirmationCommand}</span>
                      <Copy className="size-3 shrink-0" />
                    </button>
                  </>
                ) : (
                  <p className="text-muted-foreground">
                    {t(($) => $.feishu_sync.pending_reload_hint)}
                  </p>
                )}
              </div>
            )}

            {canManage && (
              <div className="flex flex-wrap gap-2 pt-1">
                {sync.state === "active" && (
                  <Button size="sm" variant="outline" onClick={retry} disabled={retryTopics.isPending}>
                    {retryTopics.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                    {t(($) => $.feishu_sync.retry)}
                  </Button>
                )}
                <Button size="sm" variant="outline" onClick={unbind} disabled={deleteBinding.isPending}>
                  {deleteBinding.isPending ? <Loader2 className="animate-spin" /> : <Unlink />}
                  {t(($) => $.feishu_sync.unbind)}
                </Button>
              </div>
            )}
          </>
        ) : (
          <>
            <p className="text-muted-foreground">{t(($) => $.feishu_sync.empty_description)}</p>
            {installations.length === 0 ? (
              <p className="rounded-md border border-dashed p-2 text-muted-foreground">
                {t(($) => $.feishu_sync.no_bots)}
              </p>
            ) : (
              <select
                value={installationId}
                onChange={(event) => setInstallationId(event.target.value)}
                className="h-8 w-full rounded-md border bg-background px-2 text-xs"
                disabled={!canManage || beginBinding.isPending}
                aria-label={t(($) => $.feishu_sync.bot_select_label)}
              >
                {installations.map((installation) => {
                  const agent = agents.find((item) => item.id === installation.agent_id);
                  return (
                    <option key={installation.id} value={installation.id}>
                      {agent?.name ?? installation.agent_id} · {installation.app_id}
                    </option>
                  );
                })}
              </select>
            )}
            {canManage && installations.length > 0 && (
              <Button size="sm" onClick={begin} disabled={!installationId || beginBinding.isPending}>
                {beginBinding.isPending ? <Loader2 className="animate-spin" /> : <Check />}
                {t(($) => $.feishu_sync.bind)}
              </Button>
            )}
          </>
        )}
      </div>
    </div>
  );
}
