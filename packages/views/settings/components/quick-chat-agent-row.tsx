"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { useCurrentWorkspace } from "@multica/core/paths";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";
import { canAssignAgent } from "../../issues/components/pickers/assignee-picker";
import { SettingsRow } from "./settings-layout";

const LAST_USED_AGENT = "last-used";

/**
 * The agent Ask Multica starts a new chat with. Unset, a new chat goes to the
 * agent last chatted with. Stored on this device per workspace, like the
 * chat's other selections.
 */
export function QuickChatAgentRow() {
  const { t } = useT("settings");
  const wsId = useCurrentWorkspace()?.id ?? "";
  const user = useAuthStore((s) => s.user);
  const enabled = useChatStore((s) => s.floatingChatEnabled);
  const agentId = useChatStore((s) => s.quickChatAgentId);
  const setAgentId = useChatStore((s) => s.setQuickChatAgentId);
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: !!wsId });
  const { data: members = [] } = useQuery({ ...memberListOptions(wsId), enabled: !!wsId });
  const role = members.find((member) => member.user_id === user?.id)?.role;

  const options = useMemo(
    () => [
      { value: LAST_USED_AGENT, label: t(($) => $.chat.ask_agent_last_used) },
      ...agents
        .filter((agent) => !agent.archived_at && canAssignAgent(agent, user?.id, role))
        .map((agent) => ({ value: agent.id, label: agent.name })),
    ],
    [agents, user?.id, role, t],
  );
  // An agent that was archived or is no longer open to this member is not a
  // choice any more; the row then reads as unset, which is how chat treats it.
  const value = options.some((option) => option.value === agentId) ? agentId! : LAST_USED_AGENT;
  const label = t(($) => $.chat.ask_agent_label);

  return (
    <SettingsRow label={label} description={t(($) => $.chat.ask_agent_hint)} size="select">
      <Select
        items={options}
        value={value}
        disabled={!enabled}
        onValueChange={(next) => {
          if (next) setAgentId(next === LAST_USED_AGENT ? null : next);
        }}
      >
        <SelectTrigger size="sm" className="w-full" aria-label={label}>
          <SelectValue>{options.find((option) => option.value === value)?.label}</SelectValue>
        </SelectTrigger>
        <SelectContent align="end">
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </SettingsRow>
  );
}
