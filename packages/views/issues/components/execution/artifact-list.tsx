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

  if (!hasWorkerOutput && !hasCriticOutput) return null;

  return (
    <div className="space-y-3" data-testid="artifact-list">
      {hasWorkerOutput && (
        <div>
          <h4 className="text-[11px] font-medium text-muted-foreground mb-1">
            {t(($) => $.execution.detail_panel.worker_output)}
          </h4>
          <pre className="text-xs bg-muted/50 rounded p-2 max-h-24 overflow-auto whitespace-pre-wrap">
            {JSON.stringify(nodeRun.worker_output, null, 2)}
          </pre>
        </div>
      )}
      {hasCriticOutput && (
        <div>
          <h4 className="text-[11px] font-medium text-muted-foreground mb-1">
            {t(($) => $.execution.detail_panel.critic_output)}
          </h4>
          <pre className="text-xs bg-muted/50 rounded p-2 max-h-24 overflow-auto whitespace-pre-wrap">
            {JSON.stringify(nodeRun.critic_output, null, 2)}
          </pre>
        </div>
      )}
    </div>
  );
}
