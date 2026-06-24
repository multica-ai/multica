"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight, Check, RotateCcw, SkipForward } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useSubmitNodeRun,
  useReviewNodeRun,
  useSkipNodeRun,
} from "@multica/core/workflows/queries";
import type { WorkflowNodeRun, NodeRunStatus } from "@multica/core/types";
import { NodeRunControlActions } from "./node-run-control-actions";

const STATUS_ACTIVE: Set<NodeRunStatus> = new Set([
  "format_checking", "working", "critic_reviewing",
]);

const STATUS_COLOR: Record<string, string> = {
  pending: "bg-muted text-muted-foreground",
  format_checking: "bg-blue-500/20 text-blue-500",
  format_ok: "bg-emerald-500/20 text-emerald-500",
  format_failed: "bg-red-500/20 text-red-500",
  worker_assigned: "bg-amber-500/20 text-amber-500",
  working: "bg-blue-500/20 text-blue-500",
  awaiting_input: "bg-cyan-500/20 text-cyan-500",
  awaiting_critic: "bg-amber-500/20 text-amber-500",
  critic_reviewing: "bg-purple-500/20 text-purple-500",
  critic_approved: "bg-emerald-500/20 text-emerald-500",
  critic_rework: "bg-orange-500/20 text-orange-500",
  completed: "bg-emerald-500/20 text-emerald-500",
  failed: "bg-red-500/20 text-red-500",
  blocked: "bg-red-500/20 text-red-500",
  skipped: "bg-muted text-muted-foreground",
  cancelled: "bg-muted text-muted-foreground",
};

function CollapsibleJSON({ data, label }: { data: unknown; label: string }) {
  const [open, setOpen] = useState(false);
  if (data == null) return null;

  return (
    <div className="space-y-1">
      <button
        type="button"
        className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
        onClick={() => setOpen(!open)}
      >
        {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        {label}
      </button>
      {open && (
        <pre className="text-[11px] bg-muted rounded-md p-2 overflow-x-auto max-h-[200px] overflow-y-auto font-mono">
          {JSON.stringify(data, null, 2)}
        </pre>
      )}
    </div>
  );
}

interface NodeRunCardProps {
  nodeRun: WorkflowNodeRun;
  maxRetries?: number;
  workflowId?: string;
  runId?: string;
}

export function NodeRunCard({ nodeRun, maxRetries = 3, workflowId, runId }: NodeRunCardProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const [reviewComment, setReviewComment] = useState("");

  const submitMutation = useSubmitNodeRun(wsId);
  const reviewMutation = useReviewNodeRun(wsId);
  const skipMutation = useSkipNodeRun(wsId);

  const status = nodeRun.status as NodeRunStatus;
  const isActive = STATUS_ACTIVE.has(status);
  const canSubmit = status === "worker_assigned" || status === "working";
  const canReview = status === "awaiting_critic";
  const canSkip = !["completed", "failed", "cancelled", "skipped"].includes(status);

  return (
    <div className="border rounded-lg p-3 space-y-2">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-sm font-medium truncate">{nodeRun.node_title}</span>
          {isActive && (
            <span className="h-2 w-2 rounded-full bg-blue-500 animate-pulse shrink-0" />
          )}
        </div>
        <Badge className={STATUS_COLOR[status] ?? "bg-muted text-muted-foreground"}>
          {t(($) => ($.node_run.status as Record<string, string>)[status] ?? status)}
        </Badge>
      </div>

      {/* Retry count */}
      {nodeRun.retry_count > 0 && (
        <div className="text-[11px] text-muted-foreground">
          {t(($) => $.node_run.retry_count, { current: nodeRun.retry_count, max: maxRetries })}
        </div>
      )}

      {/* Worker output */}
      <CollapsibleJSON data={nodeRun.worker_output} label={t(($) => $.node_run.worker_output)} />
      {/* Critic comment */}
      {nodeRun.critic_comment && (
        <p className="text-xs text-muted-foreground italic">
          {nodeRun.critic_comment}
        </p>
      )}
      <CollapsibleJSON data={nodeRun.critic_output} label="Critic Output" />

      {/* Actions */}
      <div className="flex items-center gap-1.5 pt-1 flex-wrap">
        {canSubmit && (
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            onClick={() => submitMutation.mutate({ nodeRunId: nodeRun.id, output: {} })}
            disabled={submitMutation.isPending}
          >
            <Check className="h-3 w-3 mr-1" />
            {submitMutation.isPending ? t(($) => $.node_run.submitting) : t(($) => $.node_run.submit)}
          </Button>
        )}
        {canReview && (
          <>
            <div className="flex items-center gap-1 flex-1">
              <Textarea
                value={reviewComment}
                onChange={(e) => setReviewComment(e.target.value)}
                placeholder={t(($) => $.node_run.review_comment_placeholder)}
                className="h-7 text-xs min-h-0 py-1 px-2 flex-1"
                rows={1}
              />
              <Button
                size="sm"
                className="h-7 text-xs"
                onClick={() => reviewMutation.mutate({ nodeRunId: nodeRun.id, approved: true, comment: reviewComment })}
                disabled={reviewMutation.isPending}
              >
                {t(($) => $.node_run.approve)}
              </Button>
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                onClick={() => reviewMutation.mutate({ nodeRunId: nodeRun.id, approved: false, comment: reviewComment })}
                disabled={reviewMutation.isPending}
              >
                <RotateCcw className="h-3 w-3 mr-1" />
                {t(($) => $.node_run.request_rework)}
              </Button>
            </div>
          </>
        )}
        {canSkip && (
          <Button
            size="sm"
            variant="ghost"
            className="h-7 text-xs"
            onClick={() => skipMutation.mutate(nodeRun.id)}
            disabled={skipMutation.isPending}
          >
            <SkipForward className="h-3 w-3 mr-1" />
            {t(($) => $.node_run.skip)}
          </Button>
        )}
        <NodeRunControlActions
          nodeRun={nodeRun}
          workflowId={workflowId}
          runId={runId}
          wsId={wsId}
          size="sm"
        />
      </div>
    </div>
  );
}
