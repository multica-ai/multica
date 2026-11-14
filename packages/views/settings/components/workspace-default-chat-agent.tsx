"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useSetDefaultChatAgent } from "@multica/core/workspace/mutations";
import { workspaceDefaultChatAgentId } from "@multica/core/chat";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import { isAgentRuntimeBound } from "@multica/core/agents";
import type { MemberRole, Workspace } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";
import { SettingsRow } from "./settings-layout";

export function WorkspaceDefaultChatAgent({ workspace, userId, role }: {
  workspace: Workspace;
  userId: string | null;
  role: MemberRole | undefined;
}) {
  const { t } = useT("settings");
  const { data: agents = [], isSuccess } = useQuery(agentListOptions(workspace.id));
  const mutation = useSetDefaultChatAgent(workspace.id);
  const canManage = role === "owner" || role === "admin";
  const candidates = isSuccess ? agents.filter((agent) =>
    !agent.archived_at && isAgentRuntimeBound(agent) &&
    canAssignAgentToIssue(agent, { userId, role: role ?? null }).allowed,
  ) : [];
  const id = workspaceDefaultChatAgentId(workspace.settings);
  const selected = candidates.find((agent) => agent.id === id);
  const none = t(($) => $.workspace.default_chat_agent_none);
  const options = candidates.map((agent) => ({ value: agent.id, label: agent.name }));
  const save = (id: string | null) => mutation.mutate(id, {
    onError: (error) => toast.error(
      error instanceof Error ? error.message : t(($) => $.workspace.toast_save_failed),
    ),
  });

  return (
    <SettingsRow
      anchor="default-chat-agent"
      label={t(($) => $.workspace.default_chat_agent_label)}
      description={t(($) => $.workspace.default_chat_agent_hint)}
      size="select-wide"
    >
      <div className="flex w-full min-w-0 items-center gap-2">
        <Select
          items={options}
          value={selected?.id ?? null}
          disabled={!canManage || !isSuccess || mutation.isPending}
          onValueChange={(value) => { if (value) save(value); }}
        >
          <SelectTrigger
            size="sm"
            className="w-full"
            aria-label={t(($) => $.workspace.default_chat_agent_label)}
            aria-busy={mutation.isPending}
          >
            <SelectValue>{selected?.name ?? none}</SelectValue>
          </SelectTrigger>
          <SelectContent align="end">
            {options.map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        {canManage && (
          <Button variant="ghost" size="sm" disabled={mutation.isPending} onClick={() => save(null)}>
            {t(($) => $.workspace.default_chat_agent_clear)}
          </Button>
        )}
      </div>
    </SettingsRow>
  );
}
