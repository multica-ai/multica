"use client";

import { useMemo, useState } from "react";
import { BotOff, Check, LoaderCircle } from "lucide-react";
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
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { copyText } from "@multica/ui/lib/clipboard";
import { ActorAvatar } from "../../common/actor-avatar";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
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
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");

  const sync = project.feishu_sync ?? null;
  const currentInstallationId = sync?.installation_id ?? "";
  const isMutating = beginBinding.isPending || deleteBinding.isPending;

  const options = useMemo(() => {
    const activeInstallations = (installationData?.installations ?? []).filter(
      (installation) => installation.status === "active",
    );
    const resolved = activeInstallations.map((installation) => {
      const agent = agents.find((item) => item.id === installation.agent_id);
      return {
        installationId: installation.id,
        agentId: installation.agent_id,
        name:
          agent?.name ??
          (installation.id === currentInstallationId
            ? sync?.agent_name
            : undefined) ??
          installation.agent_id,
        available: true,
      };
    });

    if (
      sync &&
      !resolved.some(
        (option) => option.installationId === sync.installation_id,
      )
    ) {
      resolved.push({
        installationId: sync.installation_id,
        agentId: sync.agent_id,
        name: sync.agent_name || sync.bot_name || sync.installation_id,
        available: false,
      });
    }
    return resolved;
  }, [
    agents,
    currentInstallationId,
    installationData?.installations,
    sync,
  ]);

  const query = filter.trim().toLowerCase();
  const filteredOptions = options.filter(
    (option) =>
      !query ||
      option.name.toLowerCase().includes(query) ||
      matchesPinyin(option.name, query),
  );
  const currentOption = options.find(
    (option) => option.installationId === currentInstallationId,
  );
  const currentName =
    currentOption?.name ??
    sync?.agent_name ??
    sync?.bot_name ??
    t(($) => $.feishu_sync.no_bot_option);

  const closePicker = () => {
    setOpen(false);
    setFilter("");
  };

  const selectInstallation = (installationId: string) => {
    if (!canManage || isMutating) return;
    closePicker();

    if (installationId === currentInstallationId) return;
    if (!installationId) {
      if (!currentInstallationId) return;
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
          if (!data.confirmation_command) return;
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
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (!nextOpen) setFilter("");
      }}
    >
      <PopoverTrigger
        render={
          <button
            type="button"
            disabled={!canManage || isMutating}
            aria-label={t(($) => $.feishu_sync.bot_select_label)}
            title={
              sync?.state === "pending_group"
                ? t(($) => $.feishu_sync.pending_reload_hint)
                : undefined
            }
            className="flex min-w-0 items-center gap-1.5 rounded px-1 py-0.5 text-left transition-colors hover:bg-accent/60 focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-60"
          >
            {isMutating ? (
              <LoaderCircle
                aria-hidden="true"
                className="h-[18px] w-[18px] shrink-0 animate-spin text-muted-foreground motion-reduce:animate-none"
              />
            ) : currentOption ? (
              <ActorAvatar
                actorType="agent"
                actorId={currentOption.agentId}
                size="sm"
                profileLink={false}
              />
            ) : (
              <span
                aria-hidden="true"
                className="inline-flex h-[18px] w-[18px] shrink-0 rounded-full border border-dashed border-muted-foreground/30"
              />
            )}
            <span className="min-w-0 truncate text-xs text-muted-foreground">
              {currentName}
            </span>
          </button>
        }
      />
      <PopoverContent align="start" className="w-56 p-0">
        <div className="border-b px-2 py-1.5 focus-within:ring-2 focus-within:ring-inset focus-within:ring-ring">
          <input
            type="text"
            name="feishu-bot-filter"
            autoComplete="off"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            placeholder={t(($) => $.lead.assign_placeholder)}
            aria-label={t(($) => $.feishu_sync.bot_select_label)}
            className="w-full bg-transparent text-sm placeholder:text-muted-foreground focus:outline-none"
          />
        </div>
        <div className="max-h-48 overflow-y-auto p-1">
          {!query ? (
            <button
              type="button"
              onClick={() => selectInstallation("")}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
            >
              <BotOff
                aria-hidden="true"
                className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
              />
              <span className="min-w-0 flex-1 truncate text-left text-muted-foreground">
                {t(($) => $.feishu_sync.no_bot_option)}
              </span>
              {!currentInstallationId ? (
                <Check aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
              ) : null}
            </button>
          ) : null}

          {filteredOptions.map((option) => {
            const isCurrent =
              option.installationId === currentInstallationId;
            return (
              <button
                type="button"
                key={option.installationId}
                disabled={!option.available}
                onClick={() =>
                  selectInstallation(option.installationId)
                }
                className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-60"
              >
                <ActorAvatar
                  actorType="agent"
                  actorId={option.agentId}
                  size="sm"
                  showStatusDot
                  profileLink={false}
                />
                <span className="min-w-0 flex-1 truncate text-left">
                  {option.name}
                </span>
                {isCurrent ? (
                  <Check aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
                ) : null}
              </button>
            );
          })}

          {filteredOptions.length === 0 ? (
            <div className="px-2 py-3 text-center text-sm text-muted-foreground">
              {query
                ? t(($) => $.lead.no_results)
                : t(($) => $.feishu_sync.no_bots)}
            </div>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}
