"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { chatPinnedAgentsOptions } from "@multica/core/chat/queries";
import { usePinChatAgent, useUnpinChatAgent } from "@multica/core/chat/mutations";
import type { Agent } from "@multica/core/types";
import {
  ContextMenu,
  ContextMenuTrigger,
  ContextMenuContent,
  ContextMenuItem,
} from "@multica/ui/components/ui/context-menu";
import { ActorAvatar } from "../../common/actor-avatar";
import { AgentPicker } from "./new-chat-button";
import { useT } from "../../i18n";

// Consistent thin ring so photo avatars and (fainter) fallback avatars read as
// the same-size circle in the chat list.
const AVATAR_RING = "ring-1 ring-inset ring-border";

// Keep the bar compact — capped at 5, matching the server limit.
const MAX_PINNED = 5;

/**
 * Quick-agent bar — a compact, avatar-only strip of the user's pinned agents at
 * the top of the Chat list (per user · workspace, server-side). Tapping an
 * avatar starts a new chat; hovering shows the agent info card; right-clicking
 * removes it; the "+" adds one (up to 5). No title, no names — space is tight.
 */
export function QuickAgentBar({
  agents,
  userId,
  onStartNewChat,
}: {
  agents: Agent[];
  userId: string | undefined;
  onStartNewChat: (agent: Agent) => void;
}) {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const { data: pinned = [] } = useQuery(chatPinnedAgentsOptions(wsId));
  const pin = usePinChatAgent();
  const unpin = useUnpinChatAgent();

  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const pinnedAgents = useMemo(
    () => pinned.map((p) => agentById.get(p.agent_id)).filter((a): a is Agent => !!a),
    [pinned, agentById],
  );
  const pinnedIds = useMemo(() => new Set(pinned.map((p) => p.agent_id)), [pinned]);
  const addable = useMemo(() => agents.filter((a) => !pinnedIds.has(a.id)), [agents, pinnedIds]);
  const canAdd = addable.length > 0 && pinnedAgents.length < MAX_PINNED;

  // Nothing to pin and nothing pinned → hide the bar.
  if (agents.length === 0) return null;
  if (pinnedAgents.length === 0 && !canAdd) return null;

  return (
    <div className="flex items-center gap-1.5 overflow-x-auto border-b px-2 py-1.5">
      {pinnedAgents.map((agent) => (
        <ContextMenu key={agent.id}>
          <ContextMenuTrigger
            render={
              <button
                type="button"
                aria-label={agent.name}
                onClick={() => onStartNewChat(agent)}
                className="flex size-[34px] shrink-0 items-center justify-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            }
          >
            <ActorAvatar
              actorType="agent"
              actorId={agent.id}
              size={34}
              showStatusDot
              enableHoverCard
              profileLink={false}
              className={AVATAR_RING}
            />
          </ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem variant="destructive" onClick={() => unpin.mutate(agent.id)}>
              {t(($) => $.list.unpin_agent)}
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ))}

      {canAdd && (
        <AgentPicker
          agents={addable}
          userId={userId}
          onSelect={(agent) => pin.mutate(agent.id)}
          side="bottom"
          align="start"
          triggerRender={
            <button
              type="button"
              aria-label={t(($) => $.list.add_agent)}
              // Filled circle (not a dashed outline) so it reads as the exact
              // same size as the filled agent avatars — an empty outline looks
              // larger at the same pixel diameter. Same ring as the avatars.
              className="flex size-[34px] shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground ring-1 ring-inset ring-border outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
            />
          }
          trigger={<Plus className="size-4" />}
        />
      )}
    </div>
  );
}
