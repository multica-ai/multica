import { observer } from "mobx-react-lite";
import type { JSX } from "react";
import type { AgentProcessingRowVM } from "../contracts/agentProcessingRowVM";
const LoadingEllipsis = observer(LoadingEllipsisRender);

export const AgentProcessingRow = observer(function AgentProcessingRow({
  row,
  label,
  paused = false
}: {
  row: AgentProcessingRowVM;
  label: string;
  paused?: boolean;
}): JSX.Element {
  "use memo";

  return (
    <div
      data-row-id={row.id}
      className="workspace-agents-status-panel__detail-processing inline-flex items-center gap-1.5"
    >
      <span className="inline-flex min-w-0 items-center gap-1 font-semibold">
        <span>{processingLabel(row, label)}</span>
        {paused ? null : <LoadingEllipsis />}
      </span>
    </div>
  );
});

function processingLabel(row: AgentProcessingRowVM, fallback: string): string {
  if (row.label?.trim()) {
    return row.label.trim();
  }
  return fallback;
}

function LoadingEllipsisRender(): JSX.Element {
  "use memo";
  return (
    <span
      className="tsh-inline-loading-ellipsis tsh-inline-loading-ellipsis--entry-timing"
      aria-hidden="true"
    >
      <span />
      <span />
      <span />
    </span>
  );
}
