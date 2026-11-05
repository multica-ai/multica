import { observer } from "mobx-react-lite";
import type { JSX } from "react";
import type { AgentToolCallCardProps } from "./AgentToolCallCard";
import { AgentToolCallHeader } from "./AgentToolCallHeader";
import { AgentExpandedToolContent } from "./tool-renderers/AgentExpandedToolContent";
import { hasAgentToolContent } from "./tool-renderers/agentToolContentShared";

export const AgentEnterPlanModeCard = observer(function AgentEnterPlanModeCard({
  call,
  onLinkClick
}: AgentToolCallCardProps): JSX.Element {
  "use memo";
  const hasDetail = hasAgentToolContent(call);

  return (
    <div className="workspace-agents-status-panel__detail-tool-row workspace-agents-status-panel__detail-tool-row--plan-enter">
      <div className="workspace-agents-status-panel__detail-tool-row-head">
        <AgentToolCallHeader call={call} expanded={false} hasDetail={false} />
      </div>
      {!hasDetail && call.summary ? (
        <div className="workspace-agents-status-panel__detail-tool-summary">
          {call.summary}
        </div>
      ) : null}
      {hasDetail ? (
        <AgentExpandedToolContent call={call} onLinkClick={onLinkClick} />
      ) : null}
    </div>
  );
});
