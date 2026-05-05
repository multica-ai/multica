"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bot } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useT } from "@multica/i18n/react";
import { ActorAvatar } from "../../../common/actor-avatar";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
} from "../../../issues/components/pickers/property-picker";

export function AgentPicker({
  agentId,
  onChange,
  trigger: customTrigger,
  triggerRender,
  align = "start",
}: {
  agentId: string | null;
  onChange: (id: string) => void;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  align?: "start" | "center" | "end";
}) {
  const wsId = useWorkspaceId();
  const t = useT("autopilots");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const active = agents.filter((a) => !a.archived_at);
  const selected = active.find((a) => a.id === agentId);

  const query = filter.trim().toLowerCase();
  const filteredAgents = query
    ? active.filter((a) => a.name.toLowerCase().includes(query))
    : active;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-56"
      align={align}
      searchable
      searchPlaceholder={t("filter_agents")}
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      trigger={
        customTrigger ?? (
          <>
            {selected ? (
              <>
                <ActorAvatar actorType="agent" actorId={selected.id} size={16} showStatusDot />
                <span className="truncate">{selected.name}</span>
              </>
            ) : (
              <>
                <Bot className="size-3" />
                <span>{t("dialog_select_agent")}</span>
              </>
            )}
          </>
        )
      }
    >
      {filteredAgents.length === 0 ? (
        <PickerEmpty />
      ) : (
        filteredAgents.map((a) => (
          <PickerItem
            key={a.id}
            selected={a.id === agentId}
            onClick={() => {
              onChange(a.id);
              setOpen(false);
            }}
          >
            <ActorAvatar actorType="agent" actorId={a.id} size={16} showStatusDot />
            <span className="truncate">{a.name}</span>
          </PickerItem>
        ))
      )}
    </PropertyPicker>
  );
}
