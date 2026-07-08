"use client";

import React, { useMemo, useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  PickerEmpty,
  PickerItem,
  PickerSection,
  PropertyPicker,
} from "../../issues/components/pickers/property-picker";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import type { Agent } from "@multica/core/types";
import { useT } from "../../i18n";

/**
 * Agent picker: a searchable, grouped (My agents / Others) list of agents in a
 * PropertyPicker. The caller supplies the trigger. `currentAgentId` marks one
 * agent with a check — omit it (as "new chat" does) when there is no current
 * selection to highlight.
 */
export function AgentPicker({
  agents,
  userId,
  currentAgentId,
  onSelect,
  trigger,
  triggerRender,
  side = "bottom",
  align = "start",
}: {
  agents: Agent[];
  userId: string | undefined;
  currentAgentId?: string;
  onSelect: (agent: Agent) => void;
  trigger: React.ReactNode;
  triggerRender: React.ReactElement;
  side?: "top" | "bottom";
  align?: "start" | "center" | "end";
}) {
  const { t } = useT("chat");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const { mine, others } = useMemo(() => {
    const mine: Agent[] = [];
    const others: Agent[] = [];
    for (const a of agents) {
      if (a.owner_id === userId) mine.push(a);
      else others.push(a);
    }
    return { mine, others };
  }, [agents, userId]);

  const query = filter.trim().toLowerCase();
  const matches = (name: string) =>
    !query || name.toLowerCase().includes(query) || matchesPinyin(name, query);
  const filteredMine = mine.filter((agent) => matches(agent.name));
  const filteredOthers = others.filter((agent) => matches(agent.name));

  const handlePick = (agent: Agent) => {
    onSelect(agent);
    setOpen(false);
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-64"
      align={align}
      side={side}
      searchable
      searchPlaceholder={t(($) => $.window.agent_filter_placeholder)}
      onSearchChange={setFilter}
      triggerRender={triggerRender}
      trigger={trigger}
    >
      {filteredMine.length === 0 && filteredOthers.length === 0 ? (
        <PickerEmpty />
      ) : (
        <>
          {filteredMine.length > 0 && (
            <PickerSection label={t(($) => $.window.my_agents)}>
              {filteredMine.map((agent) => (
                <AgentPickerItem
                  key={agent.id}
                  agent={agent}
                  isCurrent={agent.id === currentAgentId}
                  onSelect={handlePick}
                />
              ))}
            </PickerSection>
          )}
          {filteredOthers.length > 0 && (
            <PickerSection label={t(($) => $.window.others)}>
              {filteredOthers.map((agent) => (
                <AgentPickerItem
                  key={agent.id}
                  agent={agent}
                  isCurrent={agent.id === currentAgentId}
                  onSelect={handlePick}
                />
              ))}
            </PickerSection>
          )}
        </>
      )}
    </PropertyPicker>
  );
}

function AgentPickerItem({
  agent,
  isCurrent,
  onSelect,
}: {
  agent: Agent;
  isCurrent: boolean;
  onSelect: (agent: Agent) => void;
}) {
  return (
    <PickerItem selected={isCurrent} onClick={() => onSelect(agent)}>
      <ActorAvatar
        actorType="agent"
        actorId={agent.id}
        size={24}
        enableHoverCard
        showStatusDot
      />
      <span className="truncate flex-1">{agent.name}</span>
    </PickerItem>
  );
}

/**
 * "New chat" ⊕ button. Per the Chat V2 design, starting a new chat is where the
 * agent is chosen — so this opens an AgentPicker and reports the pick via
 * `onStart`. No agent is pre-checked: a new chat has no "current" agent yet.
 * Shortcuts: with a single available agent it starts immediately (no point
 * showing a one-item menu); with none it still fires `onStart(null)` so the
 * surface shows its no-agent empty state.
 */
export function NewChatButton({
  agents,
  userId,
  onStart,
  side = "bottom",
}: {
  agents: Agent[];
  userId: string | undefined;
  onStart: (agent: Agent | null) => void;
  side?: "top" | "bottom";
}) {
  const { t } = useT("chat");
  const label = t(($) => $.window.new_chat_tooltip);

  if (agents.length <= 1) {
    const only = agents[0] ?? null;
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              className="rounded-full text-muted-foreground"
              aria-label={label}
              onClick={() => onStart(only)}
            />
          }
        >
          <Plus />
        </TooltipTrigger>
        <TooltipContent side={side === "top" ? "top" : "bottom"}>{label}</TooltipContent>
      </Tooltip>
    );
  }

  return (
    <AgentPicker
      agents={agents}
      userId={userId}
      onSelect={(agent) => onStart(agent)}
      side={side}
      align="start"
      triggerRender={
        <Button
          variant="ghost"
          size="icon-sm"
          className="rounded-full text-muted-foreground"
          aria-label={label}
        />
      }
      trigger={<Plus />}
    />
  );
}
