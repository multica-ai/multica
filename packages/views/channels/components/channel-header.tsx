"use client";

import { useQuery } from "@tanstack/react-query";
import { channelMembersOptions } from "@multica/core/channels";
import { Hash, Lock, MessageCircle, Users } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
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
 * list. The leading icon distinguishes public/private/DM at a glance:
 *   - `#` for public channels
 *   - lock for private channels
 *   - chat bubble for DMs
 *
 * The members count is intentionally a separate query (not embedded in
 * the channel response) so it's cheap to invalidate independently when
 * member_added / member_removed events arrive.
 */
export function ChannelHeader({ channel, onShowMembers, enabled }: ChannelHeaderProps) {
  const Icon = channelIcon(channel);
  const { data: members = [] } = useQuery(channelMembersOptions(channel.id, enabled));

  const displayName = channel.display_name || channel.name;

  return (
    <header className="flex items-center justify-between border-b border-border px-4 py-3">
      <div className="flex min-w-0 items-center gap-2">
        <Icon className="h-5 w-5 shrink-0 text-muted-foreground" />
        <div className="min-w-0">
          <div className="truncate text-base font-semibold text-foreground">{displayName}</div>
          {channel.description ? (
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
