"use client";

// CEREBRO-PATCH(reply-target-agent-indicator): shared "who will be triggered"
// bar rendered above the editor in ReplyInput and CommentInput. The bar shows
// the agent that the backend trigger logic will actually wake when the user
// submits — the issue assignee (or squad leader) by default, replaced by any
// explicit @agent mentions in the draft. Lives outside reply-input.tsx so the
// top-level composer can reuse it without dragging the reply-only UI along.

import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, UserPlus } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { ActorAvatar } from "../../common/actor-avatar";
import { getCurrentWsId } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { MemberWithUser } from "@multica/core/types";
import { canTriggerPrivateAgentMention } from "@multica/cerebro-access/views";
import { useT } from "../../i18n";
import {
  extractMentionedAgentIds,
  getReplyTargetAgents,
  memberMentionMarkdown,
} from "./reply-targets";

interface TriggerTargetBarProps {
  /**
   * The current draft markdown. Explicit @agent mentions in the draft replace
   * the `triggerAgentId` fallback so the bar tracks what the user is typing.
   */
  markdown: string;
  /**
   * Resolved agent the backend trigger will wake when the draft has no
   * @agent mentions — issue assignee (when the assignee is an agent) or the
   * squad leader (when the assignee is a squad). `undefined` for member /
   * unassigned issues, which leaves the bar hidden.
   */
  triggerAgentId?: string;
  /**
   * Invoked when the user clicks "Tag owner" on a private-agent owner
   * popover. Receives the owner member so the parent can splice an owner
   * mention into the editor at the caret.
   */
  onTagOwner: (owner: MemberWithUser) => void;
}

function TriggerTargetBar({ markdown, triggerAgentId, onTagOwner }: TriggerTargetBarProps) {
  const { t } = useT("issues");
  const wsId = getCurrentWsId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const myRole = members.find((m) => m.user_id === userId)?.role ?? null;
  const targetAgents = useMemo(
    () => getReplyTargetAgents({ markdown, triggerAgentId, agents }),
    [agents, markdown, triggerAgentId],
  );

  // CEREBRO-PATCH(reply-trigger-auto-tag-owner): FIR-2465 — auto-tag the owner
  // once per explicitly @mentioned private agent that can't trigger. The ref
  // prevents re-injection if the user removes the owner manually, and clears
  // when the draft is empty (e.g. after submit).
  const autoTaggedRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    if (!markdown.trim()) {
      autoTaggedRef.current.clear();
      return;
    }
    const explicitIds = new Set(extractMentionedAgentIds(markdown));
    for (const agent of targetAgents) {
      if (!explicitIds.has(agent.id)) continue;
      if (autoTaggedRef.current.has(agent.id)) continue;
      if (!agent.owner_id) continue;
      const decision = canTriggerPrivateAgentMention(agent, { userId, role: myRole });
      if (decision.allowed) continue;
      const owner = members.find((m) => m.user_id === agent.owner_id);
      if (!owner) continue;
      autoTaggedRef.current.add(agent.id);
      if (markdown.includes(`mention://member/${owner.user_id}`)) continue;
      onTagOwner(owner);
    }
  }, [targetAgents, markdown, members, userId, myRole, onTagOwner]);

  if (targetAgents.length === 0) return null;

  return (
    <div className="mb-1 flex min-h-7 flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
      <span className="shrink-0 font-medium">{t(($) => $.reply.target_label)}</span>
      {targetAgents.map((agent) => {
        const decision = canTriggerPrivateAgentMention(agent, { userId, role: myRole });
        const triggerInactiveLabel = t(($) => $.reply.target_trigger_inactive);
        const owner = agent.owner_id
          ? members.find((member) => member.user_id === agent.owner_id) ?? null
          : null;

        if (!decision.allowed) {
          return (
            <Popover key={agent.id}>
              <PopoverTrigger
                render={
                  <button
                    type="button"
                    className="inline-flex min-w-0 max-w-full items-center gap-1 rounded-md border border-red-500/45 bg-red-500/10 px-1.5 py-0.5 font-medium text-red-700 transition-colors hover:bg-red-500/15 dark:text-red-300"
                    aria-label={`${agent.name} ${triggerInactiveLabel}`}
                  >
                    <AlertTriangle className="size-3 shrink-0" />
                    <span className="truncate">{agent.name}</span>
                    <span className="shrink-0">{triggerInactiveLabel}</span>
                  </button>
                }
              />
              <PopoverContent side="top" align="start" className="w-72">
                <div className="space-y-2 text-xs">
                  <div>
                    <div className="font-medium text-foreground">{t(($) => $.reply.target_trigger_title, { name: agent.name })}</div>
                    <div className="mt-0.5 text-muted-foreground">
                      {t(($) => $.reply.target_trigger_description)}
                    </div>
                  </div>
                  {owner ? (
                    <div className="flex items-center gap-2 rounded-md border bg-card p-2">
                      <ActorAvatar actorType="member" actorId={owner.user_id} size={20} />
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium text-foreground">{owner.name}</div>
                        <div className="truncate text-muted-foreground">{t(($) => $.reply.target_owner_label)}</div>
                      </div>
                      <button
                        type="button"
                        onClick={() => onTagOwner(owner)}
                        className="inline-flex h-7 shrink-0 items-center gap-1 rounded-md border px-2 font-medium text-foreground hover:bg-accent"
                      >
                        <UserPlus className="size-3.5" />
                        {t(($) => $.reply.target_tag_owner)}
                      </button>
                    </div>
                  ) : (
                    <div className="text-muted-foreground">{t(($) => $.reply.target_owner_missing)}</div>
                  )}
                </div>
              </PopoverContent>
            </Popover>
          );
        }

        return (
          <span
            key={agent.id}
            className="inline-flex min-w-0 max-w-full items-center gap-1 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-1.5 py-0.5 font-medium text-emerald-700 dark:text-emerald-300"
          >
            <ActorAvatar actorType="agent" actorId={agent.id} size={14} />
            <span className="truncate">{agent.name}</span>
          </span>
        );
      })}
    </div>
  );
}

export { TriggerTargetBar, type TriggerTargetBarProps, memberMentionMarkdown };
