"use client";

import { useCallback, useMemo } from "react";
import { MessageCircleQuestion } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useActorName } from "@multica/core/workspace/hooks";
import type { TimelineEntry } from "@multica/core/types";
import { useT } from "../../i18n";
import { agentQuestionAnswered, agentQuestionOf } from "./agent-question-card";
import { collectThreadReplies } from "./thread-utils";

/**
 * Returns the newest agent question comment that no member has answered yet,
 * or null. Pure so the chip and its test share one definition of "pending".
 */
export function findPendingAgentQuestion(timeline: readonly TimelineEntry[]): TimelineEntry | null {
  const repliesByParent = new Map<string, TimelineEntry[]>();
  for (const entry of timeline) {
    if (entry.type === "comment" && entry.parent_id) {
      const list = repliesByParent.get(entry.parent_id) ?? [];
      list.push(entry);
      repliesByParent.set(entry.parent_id, list);
    }
  }
  let pending: TimelineEntry | null = null;
  for (const entry of timeline) {
    if (entry.type !== "comment" || entry.parent_id || !agentQuestionOf(entry)) continue;
    if (agentQuestionAnswered(collectThreadReplies(entry.id, repliesByParent))) continue;
    if (!pending || entry.created_at > pending.created_at) pending = entry;
  }
  return pending;
}

interface IssueQuestionHeaderChipProps {
  timeline: readonly TimelineEntry[];
  className?: string;
}

/**
 * Header chip shown while an agent question is waiting for a member's
 * answer (GitHub #8048). Clicking it scrolls the timeline to the question
 * card. Self-hides when nothing is pending, like the agent activity chip it
 * sits next to.
 */
export function IssueQuestionHeaderChip({ timeline, className }: IssueQuestionHeaderChipProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const pending = useMemo(() => findPendingAgentQuestion(timeline), [timeline]);

  const scrollToQuestion = useCallback(() => {
    if (!pending) return;
    const target = document.getElementById(`comment-${pending.id}`) ?? document.getElementById(`comment-body-${pending.id}`);
    target?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [pending]);

  if (!pending) return null;
  const name = pending.actor_name || getActorName(pending.actor_type, pending.actor_id);
  const label = name ? t(($) => $.agent_question.waiting_chip, { name }) : t(($) => $.agent_question.waiting_chip_fallback);

  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      onClick={scrollToQuestion}
      className={cn("h-7 gap-1.5 rounded-full px-2.5 text-caption font-medium", className)}
      data-testid="issue-question-header-chip"
    >
      <MessageCircleQuestion className="size-3.5 text-muted-foreground" aria-hidden />
      <span className="max-w-[16rem] truncate">{label}</span>
    </Button>
  );
}
