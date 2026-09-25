"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Check, CornerDownRight, Loader2, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { useCreateComment, useRetryTaskSupplement } from "@multica/core/issues/mutations";
import { api } from "@multica/core/api";
import { issueKeys, issueTasksOptions } from "@multica/core/issues/queries";
import { commentSupplementReceipts, isSupplementInFlight } from "@multica/core/issues/run-steering";
import type { AgentTask, CommentSupplementReceipt, TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { useLocale, useT } from "../../i18n";

const TERMINAL = new Set<AgentTask["status"]>(["completed", "failed", "cancelled"]);

function useReceiptAgentName(issueId: string, receipt: CommentSupplementReceipt) {
  const { getActorName } = useActorName();
  // Servers that predate multi-run receipts omit the agent; the run knows it.
  const { data: task } = useQuery({
    ...issueTasksOptions(issueId),
    enabled: !!issueId,
    select: (tasks) => tasks.find((candidate) => candidate.id === receipt.task_id),
  });
  const agentId = receipt.agent_id || task?.agent_id;
  return { name: agentId ? getActorName("agent", agentId) : "", task };
}

const HAN = /\p{Script=Han}/u;

/**
 * Agent names joined for the badge. Chinese copy spaces a Latin name from the
 * Chinese joiner ("Lambda 和 Orion"), which Intl.ListFormat does not; the
 * enumeration comma stays unspaced.
 */
export function formatAgentNames(locale: string, names: string[]): string {
  const parts = new Intl.ListFormat(locale, { type: "conjunction" }).formatToParts(names);
  if (!locale.startsWith("zh")) return parts.map((part) => part.value).join("");
  return parts.map((part, i) => {
    if (part.type !== "literal" || !HAN.test(part.value)) return part.value;
    const before = parts[i - 1]?.value.slice(-1) ?? "";
    const after = parts[i + 1]?.value.charAt(0) ?? "";
    return `${before && !HAN.test(before) ? " " : ""}${part.value}${after && !HAN.test(after) ? " " : ""}`;
  }).join("");
}

/** "Added to Lambda's run": marks a message that steered a running turn. */
export function SteerBadge({ issueId, entry }: { issueId: string; entry: TimelineEntry }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const receipts = commentSupplementReceipts(entry);
  const { getActorName } = useActorName();
  if (receipts.length === 0) return null;
  // Every receipt from a current server names its agent; the legacy single
  // receipt resolves the name from its run instead.
  const names = receipts.every((receipt) => receipt.agent_id)
    ? formatAgentNames(locale, receipts.map((receipt) => getActorName("agent", receipt.agent_id!)))
    : null;
  return (
    <span className="inline-flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
      <CornerDownRight aria-hidden className="size-3 shrink-0" />
      <span className="truncate">
        {names !== null
          ? t(($) => $.inline_run.steer_badge, { names })
          : <BadgeLegacyName issueId={issueId} receipt={receipts[0]!} />}
      </span>
    </span>
  );
}

function BadgeLegacyName({ issueId, receipt }: { issueId: string; receipt: CommentSupplementReceipt }) {
  const { t } = useT("issues");
  const { name } = useReceiptAgentName(issueId, receipt);
  return <>{t(($) => $.inline_run.steer_badge, { names: name })}</>;
}

/** One line per running turn the message steered, following its delivery. */
export function SteerReceipts({ issueId, entry }: { issueId: string; entry: TimelineEntry }) {
  const receipts = commentSupplementReceipts(entry);
  if (receipts.length === 0) return null;
  return (
    <div className="mt-1.5 flex flex-col gap-1">
      {receipts.map((receipt) => (
        <SteerReceipt key={receipt.task_id} issueId={issueId} entry={entry} receipt={receipt} />
      ))}
    </div>
  );
}

function SteerReceipt({ issueId, entry, receipt }: {
  issueId: string;
  entry: TimelineEntry;
  receipt: CommentSupplementReceipt;
}) {
  const { t } = useT("issues");
  const { name, task } = useReceiptAgentName(issueId, receipt);
  const retry = useRetryTaskSupplement(issueId);
  const resend = useCreateComment(issueId);
  const [resent, setResent] = useState(false);
  const terminal = !!task && TERMINAL.has(task.status);
  const inFlight = isSupplementInFlight(receipt);
  const queryClient = useQueryClient();
  const [resending, setResending] = useState(false);
  // Only this receipt's agent missed the message: the same text as a new
  // comment must not reach anyone it already served, or who chose to skip it.
  const resendToAgent = async () => {
    const content = entry.content ?? "";
    const parentId = entry.parent_id ?? undefined;
    const agentId = receipt.agent_id || task?.agent_id;
    setResending(true);
    try {
      const preview = await queryClient.fetchQuery({
        queryKey: [...issueKeys.commentTriggerPreview(issueId), parentId ?? "", "", `resend:${entry.id}`],
        queryFn: () => api.previewCommentTriggers(issueId, content, parentId),
        staleTime: 0,
      });
      if (!agentId || !preview.agents.some((agent) => agent.id === agentId)) throw new Error("recipient unavailable");
      const others = preview.agents.filter((agent) => agent.id !== agentId).map((agent) => agent.id);
      await resend.mutateAsync({ content, parentId, suppressAgentIds: others.length > 0 ? others : undefined });
      setResent(true);
    } catch {
      toast.error(t(($) => $.inline_run.steer_resend_failed));
    } finally {
      setResending(false);
    }
  };

  if (inFlight && !terminal) {
    return (
      <p role="status" className="flex items-center gap-1.5 text-caption text-muted-foreground">
        <Loader2 aria-hidden className="size-3.5 shrink-0 motion-safe:animate-spin" />
        <span>{t(($) => $.inline_run.steer_waiting, { name })}</span>
      </p>
    );
  }
  if (receipt.status === "delivered") {
    return (
      <p className="flex items-center gap-1.5 text-caption text-muted-foreground">
        <Check aria-hidden className="size-3.5 shrink-0 text-success" />
        <span>{t(($) => $.inline_run.steer_read, { name })}</span>
      </p>
    );
  }

  // A run that ended before delivery settles the receipt as turn_ended; the
  // task status can show it before the receipt update arrives.
  const reasonCode = inFlight ? "turn_ended" : receipt.failure_reason;
  const reason = reasonCode === "turn_ended"
    ? t(($) => $.inline_run.supplement_failure_turn_ended)
    : reasonCode === "turn_not_started"
      ? t(($) => $.inline_run.supplement_failure_turn_not_started)
      : reasonCode === "provider_rejected"
        ? t(($) => $.inline_run.supplement_failure_provider_rejected)
        : reasonCode === "timeout"
          ? t(($) => $.inline_run.supplement_failure_timeout)
          : t(($) => $.inline_run.supplement_failure_unknown);
  const canRetry = !terminal && reasonCode !== "turn_ended";
  return (
    <div role="alert" className="flex flex-wrap items-center gap-x-2 gap-y-1 text-caption text-destructive">
      <span className="inline-flex items-center gap-1.5">
        <AlertCircle aria-hidden className="size-3.5 shrink-0" />
        {t(($) => $.inline_run.steer_not_received, { name, reason })}
      </span>
      {canRetry && (
        <Button type="button" size="xs" variant="outline" className="text-foreground" disabled={retry.isPending}
          onClick={() => retry.mutate({ taskId: receipt.task_id, commentId: entry.id }, {
            onError: () => toast.error(t(($) => $.inline_run.supplement_retry_failed)),
          })}>
          {retry.isPending ? <Loader2 className="size-3 motion-safe:animate-spin" /> : <RotateCcw className="size-3" />}
          {t(($) => $.inline_run.supplement_retry)}
        </Button>
      )}
      {!resent && (
        <Button type="button" size="xs" variant="outline" className="text-foreground" disabled={resending || resend.isPending}
          onClick={() => void resendToAgent()}>
          {resending || resend.isPending ? <Loader2 className="size-3 motion-safe:animate-spin" /> : <CornerDownRight className="size-3" />}
          {t(($) => $.inline_run.steer_resend)}
        </Button>
      )}
    </div>
  );
}
