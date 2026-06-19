"use client";

import { useState } from "react";
import type { CommentTriggerPreviewAgent } from "@multica/core/types";
import { useAgentPresenceDetail } from "@multica/core/agents";
import { useCurrentWorkspace } from "@multica/core/paths";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { AgentStatusDot } from "../../common/actor-avatar";
import { useT } from "../../i18n";

// One agent renders in full ("Walt will start working", avatar + presence
// dot, click toggles). Several agents collapse to an overlapping avatar
// stack + count sentence, mirroring WorkspaceAgentWorkingChip on the issues
// header. Hover layers stay read-only (Tooltip); per-agent toggling lives in
// a click-opened Popover so the layer survives consecutive clicks.
// Suppression is communicated by brightness alone: lit = will trigger,
// dimmed = skipped.
const AVATAR_SIZE = 16;
const MAX_STACK_HEADS = 4;

interface CommentTriggerChipsProps {
  agents: CommentTriggerPreviewAgent[];
  suppressedAgentIds: Set<string>;
  onToggle: (agentId: string) => void;
}

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

function sourceLabel(source: string, t: IssuesT): string {
  switch (source) {
    case "issue_assignee":
      return t(($) => $.comment.trigger_source_issue_assignee);
    case "mention_agent":
      return t(($) => $.comment.trigger_source_mention_agent);
    case "mention_squad_leader":
      return t(($) => $.comment.trigger_source_mention_squad_leader);
    default:
      return t(($) => $.comment.trigger_source_unknown);
  }
}

function sourceReason(agent: CommentTriggerPreviewAgent, t: IssuesT): string {
  switch (agent.source) {
    case "issue_assignee":
      return t(($) => $.comment.trigger_reason_issue_assignee, { name: agent.name });
    case "mention_agent":
      return t(($) => $.comment.trigger_reason_mention_agent, { name: agent.name });
    case "mention_squad_leader":
      return t(($) => $.comment.trigger_reason_mention_squad_leader, { name: agent.name });
    default:
      return agent.reason || t(($) => $.comment.trigger_reason_unknown, { name: agent.name });
  }
}

// Presence is display metadata only — the trigger list itself is always the
// backend preview. Online-ish agents start right away; offline ones queue.
function useTriggerPresenceLine(agentId: string, t: IssuesT): string | null {
  const ws = useCurrentWorkspace();
  const detail = useAgentPresenceDetail(ws?.id, agentId);
  if (detail === "loading") return null;
  return detail.availability === "online" || detail.availability === "unstable"
    ? t(($) => $.comment.trigger_starts_now)
    : t(($) => $.comment.trigger_starts_when_online);
}

// One tooltip body for every trigger surface (single chip, popover rows):
// who · why it fires (+ presence) · what a click does.
function TriggerAgentTooltipBody({
  agent,
  suppressed,
  t,
}: {
  agent: CommentTriggerPreviewAgent;
  suppressed: boolean;
  t: IssuesT;
}) {
  const presenceLine = useTriggerPresenceLine(agent.id, t);
  return (
    <div className="space-y-0.5">
      <div className="flex items-baseline gap-1.5">
        <span className="font-medium">{agent.name}</span>
        <span className="text-[10px] text-muted-foreground">{sourceLabel(agent.source, t)}</span>
      </div>
      {suppressed ? (
        <div>{t(($) => $.comment.trigger_click_to_restore)}</div>
      ) : (
        <>
          <div>
            {sourceReason(agent, t)}
            {presenceLine ? ` ${presenceLine}` : ""}
          </div>
          <div className="text-muted-foreground">{t(($) => $.comment.trigger_click_to_skip)}</div>
        </>
      )}
    </div>
  );
}

export function CommentTriggerChips({
  agents,
  suppressedAgentIds,
  onToggle,
}: CommentTriggerChipsProps) {
  const { t } = useT("issues");

  // Loading and errors render nothing: the preview is an enhancement, and
  // any interim chrome here reads as composer noise.
  if (agents.length === 0) return null;

  if (agents.length === 1) {
    const agent = agents[0]!;
    return (
      <SingleTriggerChip
        agent={agent}
        suppressed={suppressedAgentIds.has(agent.id)}
        onToggle={onToggle}
        t={t}
      />
    );
  }

  return (
    <MultiTriggerChip
      agents={agents}
      suppressedAgentIds={suppressedAgentIds}
      onToggle={onToggle}
      t={t}
    />
  );
}

function SingleTriggerChip({
  agent,
  suppressed,
  onToggle,
  t,
}: {
  agent: CommentTriggerPreviewAgent;
  suppressed: boolean;
  onToggle: (agentId: string) => void;
  t: IssuesT;
}) {
  const state = suppressed
    ? t(($) => $.comment.trigger_skipped_label)
    : sourceLabel(agent.source, t);
  // The avatar carries "who"; the sentence carries only condition + outcome,
  // so it stays fixed-width and never truncates on long agent names.
  const sentence = suppressed
    ? t(($) => $.comment.trigger_wont_trigger)
    : t(($) => $.comment.trigger_will_start);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            aria-pressed={suppressed}
            aria-label={t(($) => $.comment.trigger_chip_aria, { name: agent.name, state })}
            onClick={() => onToggle(agent.id)}
            className={cn(
              // Sidebar-style resting state: muted until hover so the strip
              // reads as metadata, not content (see app-sidebar nav items).
              "inline-flex h-6 min-w-0 max-w-full animate-in fade-in cursor-pointer items-center gap-1.5 rounded-md px-1.5 text-[11px] font-medium text-muted-foreground transition-colors duration-200 hover:bg-muted hover:text-foreground",
              suppressed && "opacity-60",
            )}
          >
            <TriggerAgentAvatar agent={agent} suppressed={suppressed} />
            <span className="truncate">{sentence}</span>
          </button>
        }
      />
      <TooltipContent side="top" className="max-w-72 text-xs">
        <TriggerAgentTooltipBody agent={agent} suppressed={suppressed} t={t} />
      </TooltipContent>
    </Tooltip>
  );
}

function MultiTriggerChip({
  agents,
  suppressedAgentIds,
  onToggle,
  t,
}: {
  agents: CommentTriggerPreviewAgent[];
  suppressedAgentIds: Set<string>;
  onToggle: (agentId: string) => void;
  t: IssuesT;
}) {
  const [open, setOpen] = useState(false);
  const [tooltipHover, setTooltipHover] = useState(false);
  const activeCount = agents.filter((a) => !suppressedAgentIds.has(a.id)).length;
  const heads = agents.slice(0, MAX_STACK_HEADS);
  const overflow = agents.length - heads.length;
  // Mirror AgentAvatarStack: ~30% overlap reads as "stacked" without
  // obscuring the next avatar.
  const overlap = Math.round(AVATAR_SIZE * 0.3);
  // The avatar stack shows who; the sentence promises only what WILL happen,
  // so the count covers non-suppressed agents — skipped ones read as the
  // dimmed heads right next to the number.
  const sentence =
    activeCount === 0
      ? t(($) => $.comment.trigger_none_will_trigger)
      : t(($) => $.comment.trigger_will_start_count, { count: activeCount });

  const popoverTrigger = (
    <PopoverTrigger
      render={
        // Ghost-button affordance (hover fill + aria-expanded pin) so the
        // stack reads as clickable, matching pickers across the app.
        <button
          type="button"
          className={cn(
            "inline-flex h-6 min-w-0 max-w-full animate-in fade-in cursor-pointer items-center gap-1.5 rounded-md px-1.5 text-[11px] font-medium text-muted-foreground transition-colors duration-200 hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground",
            activeCount === 0 && "opacity-60",
          )}
        />
      }
    >
      <span className="inline-flex items-center">
        {heads.map((agent, i) => (
          <span
            key={agent.id}
            style={{ marginLeft: i === 0 ? 0 : -overlap }}
            className="inline-flex rounded-full ring-2 ring-background"
          >
            <TriggerAgentAvatar
              agent={agent}
              suppressed={suppressedAgentIds.has(agent.id)}
              showDot={false}
            />
          </span>
        ))}
        {overflow > 0 && (
          <span
            style={{
              marginLeft: -overlap,
              width: AVATAR_SIZE,
              height: AVATAR_SIZE,
              fontSize: Math.max(9, Math.round(AVATAR_SIZE * 0.45)),
            }}
            className="inline-flex items-center justify-center rounded-full bg-muted font-medium tabular-nums text-muted-foreground ring-2 ring-background"
          >
            +{overflow}
          </span>
        )}
      </span>
      <span className="truncate">{sentence}</span>
    </PopoverTrigger>
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip open={tooltipHover && !open} onOpenChange={setTooltipHover}>
        <TooltipTrigger render={popoverTrigger} />
        <TooltipContent side="top" className="text-xs">
          {t(($) => $.comment.trigger_click_to_manage)}
        </TooltipContent>
      </Tooltip>
      <PopoverContent align="start" className="w-64 p-2">
        <div className="px-1.5 pb-1 text-xs font-medium text-muted-foreground">
          {t(($) => $.comment.trigger_preview_title)}
        </div>
        <div className="flex flex-col">
          {agents.map((agent) => {
            const suppressed = suppressedAgentIds.has(agent.id);
            const state = suppressed
              ? t(($) => $.comment.trigger_skipped_label)
              : sourceLabel(agent.source, t);
            return (
              <Tooltip key={agent.id}>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      aria-pressed={suppressed}
                      aria-label={t(($) => $.comment.trigger_chip_aria, { name: agent.name, state })}
                      onClick={() => onToggle(agent.id)}
                      className={cn(
                        "flex w-full cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-muted",
                        suppressed && "opacity-60",
                      )}
                    >
                      <TriggerAgentAvatar agent={agent} suppressed={suppressed} />
                      <span
                        className={cn(
                          "min-w-0 flex-1 truncate text-xs",
                          suppressed && "text-muted-foreground",
                        )}
                      >
                        {agent.name}
                      </span>
                      <span className="shrink-0 text-[10px] text-muted-foreground">{state}</span>
                    </button>
                  }
                />
                <TooltipContent side="right" className="max-w-72 text-xs">
                  <TriggerAgentTooltipBody agent={agent} suppressed={suppressed} t={t} />
                </TooltipContent>
              </Tooltip>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function TriggerAgentAvatar({
  agent,
  suppressed,
  showDot = true,
}: {
  agent: CommentTriggerPreviewAgent;
  suppressed: boolean;
  showDot?: boolean;
}) {
  return (
    <span
      className={cn(
        "relative inline-flex shrink-0",
        suppressed && "opacity-40 grayscale",
      )}
    >
      <ActorAvatarBase
        name={agent.name}
        initials=""
        avatarUrl={agent.avatar_url}
        isAgent
        size={AVATAR_SIZE}
      />
      {showDot && !suppressed && <AgentStatusDot agentId={agent.id} size={AVATAR_SIZE} />}
    </span>
  );
}
