"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { channelMembersOptions } from "@multica/core/channels";
import { Hash, Lock, MessageCircle, Users, Bot } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import type { Channel } from "@multica/core/types";

interface ChannelHeaderProps {
  channel: Channel;
  onShowMembers?: () => void;
  enabled: boolean;
}

function channelIcon(channel: Channel) {
  if (channel.kind === "dm") return MessageCircle;
  if (channel.visibility === "private") return Lock;
  return Hash;
}

/**
 * ChannelHeader shows the channel name + description above the message
 * list. For DMs the title is resolved from membership rather than the
 * channel.name (which is a deterministic hash). Avatar of the "other"
 * participant replaces the kind icon for DMs.
 */
export function ChannelHeader({ channel, onShowMembers, enabled }: ChannelHeaderProps) {
  const Icon = channelIcon(channel);
  const wsId = useWorkspaceId();
  const selfUserId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(channelMembersOptions(channel.id, enabled));
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const { data: workspaceAgents = [] } = useQuery(agentListOptions(wsId));

  const dmInfo = useMemo(() => {
    if (channel.kind !== "dm") return null;
    const others = members.filter(
      (m) => !(m.member_type === "member" && m.member_id === selfUserId),
    );
    if (others.length === 0) {
      return { label: "Notes to self", type: "self" as const, avatarUrl: null as string | null };
    }
    const o = others[0]!;
    if (o.member_type === "member") {
      const wm = workspaceMembers.find((m) => m.user_id === o.member_id);
      return {
        label: wm?.name || wm?.email || "Unknown member",
        type: "member" as const,
        avatarUrl: wm?.avatar_url ?? null,
      };
    }
    const a = workspaceAgents.find((x) => x.id === o.member_id);
    return {
      label: a?.name || "Unknown agent",
      type: "agent" as const,
      avatarUrl: null,
    };
  }, [channel.kind, members, selfUserId, workspaceMembers, workspaceAgents]);

  const displayName = dmInfo?.label || channel.display_name || channel.name;

  return (
    <header className="flex items-center justify-between border-b border-border px-4 py-3">
      <div className="flex min-w-0 items-center gap-2">
        {dmInfo ? (
          <Avatar className="h-6 w-6 shrink-0">
            {dmInfo.avatarUrl ? (
              <AvatarImage src={dmInfo.avatarUrl} alt={dmInfo.label} />
            ) : null}
            <AvatarFallback className={dmInfo.type === "agent" ? "bg-purple-100 text-purple-900" : ""}>
              {dmInfo.type === "agent" ? (
                <Bot className="h-3 w-3" />
              ) : (
                dmInfo.label.charAt(0).toUpperCase() || "?"
              )}
            </AvatarFallback>
          </Avatar>
        ) : (
          <Icon className="h-5 w-5 shrink-0 text-muted-foreground" />
        )}
        <div className="min-w-0">
          <div className="truncate text-base font-semibold text-foreground">{displayName}</div>
          {channel.description && channel.kind !== "dm" ? (
            <div className="truncate text-sm text-muted-foreground">{channel.description}</div>
          ) : null}
        </div>
      </div>
      <Button
        variant="outline"
        size="sm"
        onClick={onShowMembers}
        aria-label="Show members"
      >
        <Users className="mr-2 h-4 w-4" />
        {members.length}
      </Button>
    </header>
  );
}
