"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bot, Lock } from "lucide-react";
import { GROUP_ACCESS_LOCKED_TOOLTIP } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { ActorAvatar } from "../../../common/actor-avatar";
import {
  PropertyPicker,
  PickerItem,
  PickerEmpty,
} from "../../../issues/components/pickers/property-picker";
import { useT } from "../../../i18n";
import { matchesPinyin } from "../../../editor/extensions/pinyin-match";

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
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const active = agents.filter((a) => !a.archived_at);
  const selected = active.find((a) => a.id === agentId);

  const query = filter.trim().toLowerCase();
  const filteredAgents = query
    ? active.filter((a) => a.name.toLowerCase().includes(query) || matchesPinyin(a.name, query))
    : active;

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
            {selected ? (
              <>
                <ActorAvatar actorType="agent" actorId={selected.id} size={16} showStatusDot />
                <span className="truncate">{selected.name}</span>
              </>
            ) : (
              <>
                <Bot className="size-3" />
                <span>{t(($) => $.agent_picker.select_agent)}</span>
              </>
            )}
          </>
        )
      }
    >
      {filteredAgents.length === 0 ? (
        <PickerEmpty />
      ) : (
        filteredAgents.map((a) => {
          // CEREBRO-PATCH(autopilot-agent-picker-group-lock): JEH-1066 —
          // group-locked agents render in the list with a lock and a
          // disabled state so users can SEE the agent but can't pick it
          // for an autopilot trigger they'd be denied by the backend.
          const locked = a.can_trigger === false;
          return (
            <PickerItem
              key={a.id}
              selected={a.id === agentId}
              disabled={locked}
              tooltip={locked ? GROUP_ACCESS_LOCKED_TOOLTIP : undefined}
              onClick={() => {
                if (locked) return;
                onChange(a.id);
                setOpen(false);
              }}
            >
              <ActorAvatar actorType="agent" actorId={a.id} size={16} showStatusDot />
              <span className={`truncate ${locked ? "text-muted-foreground" : ""}`}>
                {a.name}
              </span>
              {locked && (
                <Lock className="ml-auto h-3 w-3 text-muted-foreground" />
              )}
            </PickerItem>
          );
        })
      )}
    </PropertyPicker>
  );
}
