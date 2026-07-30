"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Project } from "@multica/core/types";
import { larkInstallationsOptions } from "@multica/core/lark";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useBeginProjectFeishuBinding,
  useDeleteProjectFeishuBinding,
} from "@multica/core/projects/mutations";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../../i18n";

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function ProjectFeishuBotPicker({
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
  const sync = project.feishu_sync ?? null;
  const currentInstallationId = sync?.installation_id ?? "";
  const isPending = beginBinding.isPending || deleteBinding.isPending;

  const installations = useMemo(
    () =>
      (installationData?.installations ?? []).filter(
        (installation) => installation.status === "active",
      ),
    [installationData?.installations],
  );
  const currentIsMissing =
    !!sync &&
    !installations.some(
      (installation) => installation.id === currentInstallationId,
    );

  const selectInstallation = (installationId: string) => {
    if (!canManage || installationId === currentInstallationId) return;
    if (!installationId) {
      deleteBinding.mutate(project.id, {
        onSuccess: () => {
          toast.success(t(($) => $.feishu_sync.toast_unbound));
        },
        onError: (error) => {
          toast.error(
            errorMessage(
              error,
              t(($) => $.feishu_sync.toast_unbind_failed),
            ),
          );
        },
      });
      return;
    }

    beginBinding.mutate(
      { projectId: project.id, installationId },
      {
        onSuccess: (data) => {
          toast.success(t(($) => $.feishu_sync.toast_code_created), {
            description: data.confirmation_command,
            action: {
              label: t(($) => $.feishu_sync.copy_command),
              onClick: () => {
                void copyText(data.confirmation_command).then((ok) => {
                  if (ok) {
                    toast.success(
                      t(($) => $.feishu_sync.toast_command_copied),
                    );
                  }
                });
              },
            },
          });
        },
        onError: (error) => {
          toast.error(
            errorMessage(error, t(($) => $.feishu_sync.toast_bind_failed)),
          );
        },
      },
    );
  };

  return (
    <select
      value={currentInstallationId}
      onChange={(event) => selectInstallation(event.target.value)}
      className="h-7 w-full min-w-0 rounded-md border bg-background px-1.5 text-xs text-muted-foreground disabled:cursor-not-allowed disabled:opacity-60"
      disabled={!canManage || isPending}
      aria-label={t(($) => $.feishu_sync.bot_select_label)}
      title={
        sync?.state === "pending_group"
          ? t(($) => $.feishu_sync.pending_reload_hint)
          : undefined
      }
    >
      <option value="">{t(($) => $.feishu_sync.no_bot_option)}</option>
      {currentIsMissing && sync ? (
        <option value={sync.installation_id}>
          {sync.agent_name || sync.bot_name || sync.installation_id}
          {sync.state === "pending_group"
            ? ` · ${t(($) => $.feishu_sync.state_pending)}`
            : ""}
        </option>
      ) : null}
      {installations.map((installation) => {
        const agent = agents.find((item) => item.id === installation.agent_id);
        const isCurrent = installation.id === currentInstallationId;
        return (
          <option key={installation.id} value={installation.id}>
            {agent?.name ?? installation.agent_id}
            {isCurrent && sync?.state === "pending_group"
              ? ` · ${t(($) => $.feishu_sync.state_pending)}`
              : ""}
          </option>
        );
      })}
    </select>
  );
}
