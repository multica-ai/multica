"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bot } from "lucide-react";
import { isGitLabPersonaAgent } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import type { AutopilotAssigneeType } from "@multica/core/types";
import { ActorAvatar } from "../../../common/actor-avatar";
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
} from "../../../issues/components/pickers/property-picker";
import { useT } from "../../../i18n";
import { matchesPinyin } from "../../../editor/extensions/pinyin-match";

export interface AssigneeSelection {
  type: AutopilotAssigneeType;
  id: string;
}

export function AgentPicker({
  assignee,
  onChange,
  trigger: customTrigger,
  triggerRender,
  align = "start",
}: {
  assignee: AssigneeSelection | null;
  onChange: (next: AssigneeSelection) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));

  const activeAgents = useMemo(
    () => agents.filter((a) => !a.archived_at && !isGitLabPersonaAgent(a)),
    [agents],
  );
  const activeSquads = useMemo(() => squads.filter((s) => !s.archived_at), [squads]);

  const selectedAgent =
    assignee?.type === "agent" ? activeAgents.find((a) => a.id === assignee.id) : undefined;
  const selectedSquad =
    assignee?.type === "squad" ? activeSquads.find((s) => s.id === assignee.id) : undefined;
  const selectedName = selectedAgent?.name ?? selectedSquad?.name;

  const query = filter.trim().toLowerCase();
  const matches = (name: string) =>
    !query || name.toLowerCase().includes(query) || matchesPinyin(name, query);
  const filteredAgents = activeAgents.filter((a) => matches(a.name));
  const filteredSquads = activeSquads.filter((s) => matches(s.name));

  const isSelected = (type: AutopilotAssigneeType, id: string) =>
    assignee?.type === type && assignee?.id === id;

  const handlePick = (type: AutopilotAssigneeType, id: string) => {
    onChange({ type, id });
    setOpen(false);
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
      {filteredAgents.length === 0 && filteredSquads.length === 0 ? (
        <PickerEmpty />
      ) : (
        <>
          {filteredAgents.length > 0 && (
            <PickerSection label={t(($) => $.agent_picker.agents_group)}>
              {filteredAgents.map((a) => (
                <PickerItem
                  key={a.id}
                  selected={isSelected("agent", a.id)}
                  onClick={() => handlePick("agent", a.id)}
                >
                  <ActorAvatar actorType="agent" actorId={a.id} size="sm" showStatusDot />
                  <span className="truncate">{a.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
          {filteredSquads.length > 0 && (
            <PickerSection label={t(($) => $.agent_picker.squads_group)}>
              {filteredSquads.map((s) => (
                <PickerItem
                  key={s.id}
                  selected={isSelected("squad", s.id)}
                  onClick={() => handlePick("squad", s.id)}
                >
                  <ActorAvatar actorType="squad" actorId={s.id} size="sm" />
                  <span className="truncate">{s.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
        </>
      )}
    </PropertyPicker>
  );
}
