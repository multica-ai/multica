import { observer } from "mobx-react-lite";
import { useState, type JSX } from "react";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import { AgentToolCallHeader } from "./AgentToolCallHeader";
import { CollapsibleReveal } from "./CollapsibleReveal";
import { AgentExpandedToolContent } from "./tool-renderers/AgentExpandedToolContent";
import { hasAgentToolContent } from "./tool-renderers/agentToolContentShared";

export interface AgentToolCallCardProps {
  call: AgentToolCallVM;
  onLinkClick?: (href: string) => void;
  defaultExpanded?: boolean;
  nonCollapsible?: boolean;
  variantClassName?: string;
}

export const AgentToolCallCard = observer(function AgentToolCallCard({
  call,
  onLinkClick,
  defaultExpanded = false,
  nonCollapsible = false,
  variantClassName
}: AgentToolCallCardProps): JSX.Element {
  "use memo";
  const [expanded, setExpanded] = useState(defaultExpanded);
  const hasDetail = hasAgentToolContent(call);
  const canCollapse = hasDetail && !nonCollapsible;
  const ariaLabel = toolCallAriaLabel(call);

  return (
    <div
      className={[
        "workspace-agents-status-panel__detail-tool-row",
        variantClassName ?? ""
      ]
        .filter(Boolean)
        .join(" ")}
    >
      {canCollapse ? (
        <button
          type="button"
          className="workspace-agents-status-panel__detail-tool-row-head workspace-agents-status-panel__detail-tool-row-head--button"
          aria-expanded={expanded}
          aria-label={ariaLabel}
          onClick={() => setExpanded((value) => !value)}
        >
          <AgentToolCallHeader call={call} expanded={expanded} hasDetail />
        </button>
      ) : (
        <div className="workspace-agents-status-panel__detail-tool-row-head">
          <AgentToolCallHeader call={call} expanded={false} hasDetail={false} />
        </div>
      )}
      {hasDetail && nonCollapsible ? (
        <AgentExpandedToolContent call={call} onLinkClick={onLinkClick} />
      ) : null}
      {hasDetail && !nonCollapsible ? (
        <CollapsibleReveal expanded={expanded}>
          <AgentExpandedToolContent call={call} onLinkClick={onLinkClick} />
        </CollapsibleReveal>
      ) : null}
    </div>
  );
});

function toolCallAriaLabel(call: AgentToolCallVM): string {
  return [
    call.name,
    call.status,
    call.compactSummary?.trim() || call.summary.trim()
  ]
    .filter(Boolean)
    .join(" ");
}
