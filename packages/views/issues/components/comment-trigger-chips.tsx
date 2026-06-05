"use client";

import { Loader2 } from "lucide-react";
import type { CommentTriggerPreviewAgent } from "@multica/core/types";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { CommentTriggerPreviewStatus } from "../hooks/use-comment-trigger-preview";

interface CommentTriggerChipsProps {
  agents: CommentTriggerPreviewAgent[];
  suppressedAgentIds: Set<string>;
  status: CommentTriggerPreviewStatus;
  onToggle: (agentId: string) => void;
}

function sourceLabel(source: string, t: ReturnType<typeof useT<"issues">>["t"]): string {
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

function sourceReason(agent: CommentTriggerPreviewAgent, t: ReturnType<typeof useT<"issues">>["t"]): string {
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

export function CommentTriggerChips({
  agents,
  suppressedAgentIds,
  status,
  onToggle,
}: CommentTriggerChipsProps) {
  const { t } = useT("issues");

  if (agents.length === 0 && status === "loading") {
    return (
      <div className="inline-flex h-6 max-w-full items-center gap-1.5 rounded-md bg-muted/60 px-2 text-[11px] text-muted-foreground">
        <Loader2 className="size-3 animate-spin" />
        <span className="truncate">{t(($) => $.comment.trigger_checking)}</span>
      </div>
    );
  }

  if (agents.length === 0 && status === "error") {
    return (
      <div className="inline-flex h-6 max-w-full items-center rounded-md bg-muted/50 px-2 text-[11px] text-muted-foreground">
        <span className="truncate">{t(($) => $.comment.trigger_preview_failed)}</span>
      </div>
    );
  }

  if (agents.length === 0) return null;

  return (
    <div className="flex min-w-0 items-center gap-1 overflow-x-auto">
      {agents.map((agent) => {
        const suppressed = suppressedAgentIds.has(agent.id);
        const label = suppressed
          ? t(($) => $.comment.trigger_suppressed)
          : sourceLabel(agent.source, t);
        const reason = sourceReason(agent, t);
        const tooltip = suppressed
          ? t(($) => $.comment.trigger_tooltip_suppressed, { reason })
          : t(($) => $.comment.trigger_tooltip_active, { reason });

        return (
          <Tooltip key={agent.id}>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  aria-pressed={suppressed}
                  aria-label={t(($) => $.comment.trigger_chip_aria, {
                    name: agent.name,
                    state: label,
                  })}
                  onClick={() => onToggle(agent.id)}
                  className={cn(
                    "inline-flex h-6 max-w-44 shrink-0 items-center gap-1 rounded-md border px-2 text-[11px] font-medium transition-colors",
                    suppressed
                      ? "border-transparent bg-muted text-muted-foreground hover:bg-muted/80"
                      : "border-primary/25 bg-primary/10 text-primary hover:bg-primary/15",
                  )}
                >
                  <span className="truncate">{agent.name}</span>
                  <span className="text-current/55">/</span>
                  <span className="shrink-0">{label}</span>
                </button>
              }
            />
            <TooltipContent side="top" className="max-w-72 text-xs">
              {tooltip}
            </TooltipContent>
          </Tooltip>
        );
      })}
    </div>
  );
}
