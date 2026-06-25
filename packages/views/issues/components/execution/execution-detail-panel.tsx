"use client";

import { useEffect } from "react";
import type { WorkflowNode, WorkflowNodeRun } from "@multica/core/types";
import { X, Bot, User } from "lucide-react";
import { useT } from "@multica/views/i18n";
import { NodeRunStatusIcon } from "./node-run-status-icon";
import { ArtifactList } from "./artifact-list";
import { cn } from "@multica/ui/lib/utils";

export interface ExecutionDetailPanelProps {
  node: WorkflowNode;
  nodeRun: WorkflowNodeRun | null;
  workerName: string | null;
  criticName: string | null;
  onClose: () => void;
  wsId: string;
}

export function ExecutionDetailPanel({
  node,
  nodeRun,
  workerName,
  criticName,
  onClose,
}: ExecutionDetailPanelProps) {
  const { t } = useT("issues");

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [onClose]);

  const status = nodeRun?.status;
  const duration =
    nodeRun?.started_at && nodeRun?.completed_at
      ? Math.round(
          (new Date(nodeRun.completed_at).getTime() -
            new Date(nodeRun.started_at).getTime()) /
            1000,
        )
      : null;

  return (
    <>
      {/* Mask */}
      <div
        data-testid="detail-panel-mask"
        className="fixed inset-0 z-40 bg-slate-950/18 backdrop-blur-[1px]"
        onClick={onClose}
      />

      {/* Panel */}
      <aside className="fixed right-0 top-0 bottom-0 z-50 w-[520px] bg-background/98 backdrop-blur shadow-xl border-l border-border/60 flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-border/60 shrink-0">
          <div className="flex items-center gap-2 min-w-0">
            <h2 className="text-base font-semibold truncate">{node.title}</h2>
            {status && <NodeRunStatusIcon status={status} />}
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-md hover:bg-muted"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-6">
          {/* Status path visualization */}
          {status && (
            <section>
              <h3 className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide mb-2">
                {t(($) => $.execution.detail_panel.status_path)}
              </h3>
              <div className="flex items-center gap-2 text-xs">
                <span
                  className={cn(
                    "px-2 py-0.5 rounded",
                    status === "format_checking" || status === "format_ok"
                      ? "bg-blue-50 text-blue-700"
                      : "bg-muted/50",
                  )}
                >
                  Format
                </span>
                <span className="text-muted-foreground">→</span>
                <span
                  className={cn(
                    "px-2 py-0.5 rounded",
                    status === "working"
                      ? "bg-blue-50 text-blue-700"
                      : "bg-muted/50",
                  )}
                >
                  Worker
                </span>
                <span className="text-muted-foreground">→</span>
                <span
                  className={cn(
                    "px-2 py-0.5 rounded",
                    status === "critic_reviewing" ||
                      status === "critic_approved"
                      ? "bg-green-50 text-green-700"
                      : "bg-muted/50",
                  )}
                >
                  Critic
                </span>
              </div>
            </section>
          )}

          {/* Worker info */}
          <section>
            <h3 className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide mb-2">
              {t(($) => $.execution.detail_panel.worker)}
            </h3>
            <div className="flex items-center gap-2 text-sm">
              {node.worker_type === "agent" ? (
                <Bot className="h-4 w-4" />
              ) : (
                <User className="h-4 w-4" />
              )}
              <span className="font-medium">{workerName ?? "--"}</span>
              {nodeRun && (
                <NodeRunStatusIcon status={nodeRun.status} className="h-3.5 w-3.5" />
              )}
            </div>
          </section>

          {/* Critic info */}
          <section>
            <h3 className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide mb-2">
              {t(($) => $.execution.detail_panel.critic)}
            </h3>
            {node.critic_type || node.critic_id ? (
              <>
                <div className="flex items-center gap-2 text-sm">
                  {nodeRun?.critic_type === "agent" ? (
                    <Bot className="h-4 w-4" />
                  ) : (
                    <User className="h-4 w-4" />
                  )}
                  <span className="font-medium">{criticName ?? "--"}</span>
                </div>
                {nodeRun?.critic_comment && (
                  <p className="text-xs text-muted-foreground mt-1 italic">
                    &ldquo;{nodeRun.critic_comment}&rdquo;
                  </p>
                )}
              </>
            ) : (
              <p className="text-xs text-muted-foreground italic">
                {t(($) => $.execution.detail_panel.not_configured)}
              </p>
            )}
          </section>

          {/* Artifacts */}
          {nodeRun && <ArtifactList nodeRun={nodeRun} />}

          {/* Metadata */}
          {nodeRun && (
            <section>
              <h3 className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide mb-2">
                {t(($) => $.execution.detail_panel.metadata)}
              </h3>
              <dl className="text-xs space-y-1">
                {nodeRun.started_at && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">
                      {t(($) => $.execution.detail_panel.started_at)}
                    </dt>
                    <dd>{new Date(nodeRun.started_at).toLocaleString()}</dd>
                  </div>
                )}
                {nodeRun.completed_at && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">
                      {t(($) => $.execution.detail_panel.completed_at)}
                    </dt>
                    <dd>{new Date(nodeRun.completed_at).toLocaleString()}</dd>
                  </div>
                )}
                {duration != null && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">
                      {t(($) => $.execution.detail_panel.duration)}
                    </dt>
                    <dd>{duration}s</dd>
                  </div>
                )}
                {nodeRun.retry_count > 0 && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">
                      {t(($) => $.execution.detail_panel.retry_count)}
                    </dt>
                    <dd>{nodeRun.retry_count}</dd>
                  </div>
                )}
              </dl>
            </section>
          )}
        </div>
      </aside>
    </>
  );
}
