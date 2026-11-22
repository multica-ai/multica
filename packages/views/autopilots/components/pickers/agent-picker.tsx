"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bot, CircleDashed } from "@multica/ui/icons";
import { useAuthStore } from "@multica/core/auth";
import { useRecentAgents } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import type { Agent, AutopilotAssigneeType } from "@multica/core/types";
import { ActorAvatar } from "../../../common/actor-avatar";
import { SmartRuntimeAgentBadge } from "../../../agents/components/agent-runtime-hover";
import { isAgentRuntimeBound } from "@multica/core/agents";
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
} from "../../../issues/components/pickers/property-picker";
import { useT } from "../../../i18n";
import { matchesPinyin } from "../../../editor/extensions/pinyin-match";
import { useRecentAgentRecordError } from "../../../common/use-recent-agent-record-error";
import { RecentAgentLoadError } from "../../../common/recent-agent-load-error";

export interface AssigneeSelection {
  type: AutopilotAssigneeType;
  id: string;
}

export function AgentPicker({
  assignee,
  onChange,
  trigger: customTrigger,
  triggerRender,
  emptyOption,
  align = "start",
}: {
  assignee: AssigneeSelection | null;
  onChange: (next: AssigneeSelection) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  emptyOption?: { label: string; onSelect: () => void };
  align?: "start" | "center" | "end";
}) {
  const { t } = useT("autopilots");
  const { t: tCommon } = useT("common");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((state) => state.user?.id);
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const onRecentAgentRecordError = useRecentAgentRecordError();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));

  const activeAgents = useMemo(() => agents.filter((a) => !a.archived_at), [agents]);
  const activeSquads = useMemo(() => squads.filter((s) => !s.archived_at), [squads]);
  const selectableAgents = useMemo(
    () => activeAgents.filter(isAgentRuntimeBound),
    [activeAgents],
  );
  const {
    recentAgents,
    recentIds,
    recordRecentAgent,
    isRecentAgentsError,
    refetchRecentAgents,
  } = useRecentAgents({
    agents: selectableAgents,
    userId,
    wsId,
    enabled: open,
    onRecordError: onRecentAgentRecordError,
  });
  const agentsById = useMemo(
    () => new Map(activeAgents.map((agent) => [agent.id, agent])),
    [activeAgents],
  );

  const selectedAgent =
    assignee?.type === "agent" ? activeAgents.find((a) => a.id === assignee.id) : undefined;
  const selectedSquad =
    assignee?.type === "squad" ? activeSquads.find((s) => s.id === assignee.id) : undefined;
  const selectedName = selectedAgent?.name ?? selectedSquad?.name;

  const query = filter.trim().toLowerCase();
  const matches = (name: string) =>
    !query || name.toLowerCase().includes(query) || matchesPinyin(name, query);
  const filteredRecentAgents = recentAgents.filter((a) => matches(a.name));
  const filteredAgents = activeAgents.filter(
    (a) => !recentIds.has(a.id) && matches(a.name),
  );
  const filteredSquads = activeSquads.filter((s) => matches(s.name));

  const isSelected = (type: AutopilotAssigneeType, id: string) =>
    assignee?.type === type && assignee?.id === id;

  const handlePick = (type: AutopilotAssigneeType, id: string) => {
    if (type === "agent") recordRecentAgent(id);
    onChange({ type, id });
    setOpen(false);
  };

  const renderAgent = (agent: Agent) => {
    const runtimeBound = isAgentRuntimeBound(agent);
    return (
      <PickerItem
        key={agent.id}
        selected={isSelected("agent", agent.id)}
        disabled={!runtimeBound}
        tooltip={
          runtimeBound
            ? undefined
            : t(($) => $.agent_picker.agent_runtime_required)
        }
        onClick={() => handlePick("agent", agent.id)}
      >
        <ActorAvatar actorType="agent" actorId={agent.id} size="sm" showStatusDot />
        <span className="truncate">{agent.name}</span>
        <SmartRuntimeAgentBadge shareRuntime={agent.share_runtime} />
      </PickerItem>
    );
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-56"
      align={align}
      searchable
      searchPlaceholder={t(($) => $.agent_picker.filter_placeholder)}
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      trigger={
        customTrigger ?? (
          <>
            {assignee && (selectedAgent || selectedSquad) ? (
              <>
                <ActorAvatar
                  actorType={assignee.type}
                  actorId={assignee.id}
                  size="sm"
                  showStatusDot={assignee.type === "agent"}
                />
                <span className="truncate">{selectedName}</span>
                {selectedAgent && (
                  <SmartRuntimeAgentBadge
                    shareRuntime={selectedAgent.share_runtime}
                  />
                )}
              </>
            ) : !assignee && emptyOption ? (
              <>
                <CircleDashed className="size-3 text-muted-foreground" />
                <span>{emptyOption.label}</span>
              </>
            ) : (
              <>
                <Bot className="size-3" />
                <span>{t(($) => $.agent_picker.select_assignee)}</span>
              </>
            )}
          </>
        )
      }
    >
      {isRecentAgentsError && (
        <RecentAgentLoadError onRetry={refetchRecentAgents} />
      )}
      {emptyOption && (
        <PickerItem
          emptyValue
          selected={!assignee}
          onClick={() => {
            emptyOption.onSelect();
            setOpen(false);
          }}
        >
          <CircleDashed className="size-4 text-muted-foreground" />
          <span>{emptyOption.label}</span>
        </PickerItem>
      )}
      {filteredRecentAgents.length === 0 && filteredAgents.length === 0 && filteredSquads.length === 0 ? (
        <PickerEmpty />
      ) : (
        <>
          {filteredRecentAgents.length > 0 && (
            <PickerSection label={tCommon(($) => $.recently_used)}>
              {filteredRecentAgents.map(renderAgent)}
            </PickerSection>
          )}
          {filteredAgents.length > 0 && (
            <PickerSection label={t(($) => $.agent_picker.agents_group)}>
              {filteredAgents.map(renderAgent)}
            </PickerSection>
          )}
          {filteredSquads.length > 0 && (
            <PickerSection label={t(($) => $.agent_picker.squads_group)}>
              {filteredSquads.map((s) => {
                const leader = agentsById.get(s.leader_id);
                const runtimeBound =
                  !!leader && isAgentRuntimeBound(leader);
                return (
                  <PickerItem
                    key={s.id}
                    selected={isSelected("squad", s.id)}
                    disabled={!runtimeBound}
                    tooltip={
                      runtimeBound
                        ? undefined
                        : t(($) => $.agent_picker.squad_runtime_required)
                    }
                    onClick={() => handlePick("squad", s.id)}
                  >
                    <ActorAvatar actorType="squad" actorId={s.id} size="sm" />
                    <span className="truncate">{s.name}</span>
                  </PickerItem>
                );
              })}
            </PickerSection>
          )}
        </>
      )}
    </PropertyPicker>
  );
}
