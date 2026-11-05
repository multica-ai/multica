import { observer } from "mobx-react-lite";
export const AgentTranscriptSkeleton = observer(AgentTranscriptSkeletonRender);
function AgentTranscriptSkeletonRender({
  label,
  testId = "agent-gui-transcript-loading-skeleton"
}: {
  label: string;
  testId?: string;
}): React.JSX.Element {
  "use memo";
  return (
    <div
      className="agent-gui-node__transcript-skeleton"
      role="status"
      aria-label={label}
      data-testid={testId}
    >
      <div className="agent-gui-node__transcript-skeleton-row agent-gui-node__transcript-skeleton-row-user">
        <span className="agent-gui-node__transcript-skeleton-bubble agent-gui-node__transcript-skeleton-line-medium" />
      </div>
      <div className="agent-gui-node__transcript-skeleton-row agent-gui-node__transcript-skeleton-row-assistant">
        <span className="agent-gui-node__transcript-skeleton-line agent-gui-node__transcript-skeleton-line-long" />
        <span className="agent-gui-node__transcript-skeleton-line agent-gui-node__transcript-skeleton-line-medium" />
        <span className="agent-gui-node__transcript-skeleton-line agent-gui-node__transcript-skeleton-line-short" />
      </div>
      <div className="agent-gui-node__transcript-skeleton-row agent-gui-node__transcript-skeleton-row-user">
        <span className="agent-gui-node__transcript-skeleton-bubble agent-gui-node__transcript-skeleton-line-short" />
      </div>
      <div className="agent-gui-node__transcript-skeleton-row agent-gui-node__transcript-skeleton-row-assistant">
        <span className="agent-gui-node__transcript-skeleton-line agent-gui-node__transcript-skeleton-line-medium" />
        <span className="agent-gui-node__transcript-skeleton-line agent-gui-node__transcript-skeleton-line-long" />
      </div>
    </div>
  );
}
