"use client";

import type { WorkflowNodeRun } from "@multica/core/types";
import { useT } from "@multica/views/i18n";

export interface ArtifactListProps {
  nodeRun: WorkflowNodeRun;
}

export function ArtifactList({ nodeRun }: ArtifactListProps) {
  const { t } = useT("issues");
  const hasWorkerOutput = nodeRun.worker_output != null;
  const hasCriticOutput = nodeRun.critic_output != null;
  const hasAnyOutput = hasWorkerOutput || hasCriticOutput;

  return (
    <section data-testid="artifact-list-section">
      <h3 className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide mb-2">
        {t(($) => $.execution.detail_panel.attachments)}
      </h3>
      {hasAnyOutput ? (
        <div className="space-y-3" data-testid="artifact-list">
          {hasWorkerOutput && (
            <div>
              <h4 className="text-[11px] font-medium text-muted-foreground mb-1">
                {t(($) => $.execution.detail_panel.worker_output)}
              </h4>
              <pre className="text-xs bg-muted/50 rounded p-2 max-h-24 overflow-auto whitespace-pre-wrap">
                {typeof nodeRun.worker_output === "string"
                  ? nodeRun.worker_output
                  : JSON.stringify(nodeRun.worker_output, null, 2)}
              </pre>
            </div>
          )}
          {hasCriticOutput && (
            <div>
              <h4 className="text-[11px] font-medium text-muted-foreground mb-1">
                {t(($) => $.execution.detail_panel.critic_output)}
              </h4>
              <pre className="text-xs bg-muted/50 rounded p-2 max-h-24 overflow-auto whitespace-pre-wrap">
                {typeof nodeRun.critic_output === "string"
                  ? nodeRun.critic_output
                  : JSON.stringify(nodeRun.critic_output, null, 2)}
              </pre>
            </div>
          )}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground italic">
          {t(($) => $.execution.detail_panel.no_output)}
        </p>
      )}
    </section>
  );
}
