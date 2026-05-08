// CEREBRO-PATCH(channel-listeners-panel): JEH-699 — channel × agent
// listen-mode UI. Net-new file in the upstream zone; mark on the file
// header so the upstream-zone validator catches future edits too.
"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bot } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  channelAgentSettingsOptions,
  useSetChannelAgentListenMode,
} from "@multica/core/channels";
import { useActorName } from "@multica/core/workspace/hooks";
import type {
  Channel,
  ChannelAgentListenMode,
  ChannelMember,
} from "@multica/core/types";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";

interface ChannelListenersPanelProps {
  channel: Channel;
}

/**
 * Header-anchored popover that lists every agent in the channel together
 * with a "listen always" / "only when mentioned" toggle. The default for an
 * agent without an explicit row is 'always' — the row is created on the
 * fly when the user flips the switch off.
 *
 * The button is hidden when the channel has no agent participants since the
 * toggle would have nothing to operate on.
 */
export function ChannelListenersPanel({ channel }: ChannelListenersPanelProps) {
  const agentParticipants = useMemo(
    () => channel.participants.filter((p) => p.user_type === "agent"),
    [channel.participants],
  );

  if (agentParticipants.length === 0) {
    return null;
  }

  return (
    <Popover>
      <Tooltip>
        <TooltipTrigger
          render={
            <PopoverTrigger
              render={
                <button
                  type="button"
                  aria-label="Channel listeners"
                  className="inline-flex size-7 items-center justify-center rounded border text-muted-foreground hover:bg-accent hover:text-foreground"
                />
              }
            >
              <Bot className="size-3.5" />
            </PopoverTrigger>
          }
        />
        <TooltipContent side="bottom">Listeners</TooltipContent>
      </Tooltip>
      <PopoverContent align="end" className="w-80">
        <PopoverHeader>
          <PopoverTitle>Agents in this channel</PopoverTitle>
          <PopoverDescription>
            Toggle off to require an explicit @-mention before the agent reacts.
          </PopoverDescription>
        </PopoverHeader>
        <ListenersList
          channelId={channel.id}
          agents={agentParticipants}
        />
      </PopoverContent>
    </Popover>
  );
}

interface ListenersListProps {
  channelId: string;
  agents: ChannelMember[];
}

function ListenersList({ channelId, agents }: ListenersListProps) {
  const wsId = useWorkspaceId();
  const { data, isLoading } = useQuery(channelAgentSettingsOptions(wsId, channelId));
  const setListenMode = useSetChannelAgentListenMode(channelId);

  const modes = useMemo(() => {
    const out: Record<string, ChannelAgentListenMode> = {};
    for (const s of data?.settings ?? []) {
      out[s.agent_id] = s.listen_mode;
    }
    return out;
  }, [data]);

  return (
    <ul className="flex flex-col gap-1.5">
      {agents.map((agent) => {
        const mode: ChannelAgentListenMode = modes[agent.user_id] ?? "always";
        return (
          <ListenerRow
            key={agent.user_id}
            agentId={agent.user_id}
            mode={mode}
            disabled={isLoading || setListenMode.isPending}
            onToggle={(next) =>
              setListenMode.mutate({ agentId: agent.user_id, listenMode: next })
            }
          />
        );
      })}
    </ul>
  );
}

interface ListenerRowProps {
  agentId: string;
  mode: ChannelAgentListenMode;
  disabled: boolean;
  onToggle: (mode: ChannelAgentListenMode) => void;
}

function ListenerRow({ agentId, mode, disabled, onToggle }: ListenerRowProps) {
  const { getActorName } = useActorName();
  const checked = mode === "always";
  const switchId = `channel-listen-${agentId}`;

  return (
    <li className="flex items-center justify-between gap-2 rounded px-1.5 py-1 hover:bg-muted/40">
      <label htmlFor={switchId} className="flex min-w-0 flex-1 items-center gap-2 cursor-pointer">
        <ActorAvatar actorType="agent" actorId={agentId} size={20} />
        <span className="min-w-0 flex-1 truncate text-sm">
          {getActorName("agent", agentId)}
        </span>
        <span className="text-xs text-muted-foreground">
          {checked ? "Listening" : "Mention only"}
        </span>
      </label>
      <Switch
        id={switchId}
        size="sm"
        checked={checked}
        disabled={disabled}
        onCheckedChange={(next) => onToggle(next ? "always" : "mention_only")}
      />
    </li>
  );
}
