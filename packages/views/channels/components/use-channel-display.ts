"use client";

import { useMemo } from "react";
import { useAuthStore } from "@multica/core/auth";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Channel, ChannelMember } from "@multica/core/types";

export interface ChannelDisplay {
  /** Title to render — `#name` for channels, comma-joined names for DMs. */
  title: string;
  /** Optional `#` prefix indicator (true for kind='channel'). */
  isChannel: boolean;
  /** Participants shown in avatar stacks — excludes the current user. */
  otherParticipants: ChannelMember[];
}

/**
 * Derive UI-friendly fields for a channel/DM row.
 *
 * - For DMs the title is the other member's name (since both members see
 *   the same row but neither wants to see "DM with myself").
 * - For named channels the title is the channel.title prefixed with `#`.
 */
export function useChannelDisplay(channel: Channel): ChannelDisplay {
  const userId = useAuthStore((s) => s.user?.id);
  const { getActorName } = useActorName();

  return useMemo(() => {
    const others = channel.participants.filter(
      (p) => !(p.user_type === "member" && p.user_id === userId),
    );

    if (channel.kind === "channel") {
      return {
        title: channel.title || "Channel",
        isChannel: true,
        otherParticipants: others,
      };
    }

    // DM: title is the peer's name. If the peer somehow isn't loaded yet,
    // fall back to channel.title (server uses a placeholder).
    const peer = others[0];
    const title = peer
      ? getActorName(peer.user_type, peer.user_id)
      : channel.title || "Direct message";
    return {
      title,
      isChannel: false,
      otherParticipants: others,
    };
  }, [channel, userId, getActorName]);
}
