import { observer } from "mobx-react-lite";
import type { JSX } from "react";
import { WorkspaceAgentSessionThinkingDisclosure } from "../../WorkspaceAgentSessionThinkingDisclosure";
import type { AgentThinkingContentVM } from "../contracts/agentMessageRowVM";
import { RawTimelineJsonDisclosure } from "./RawTimelineJsonDisclosure";

interface AgentThinkingDisclosureProps {
  thinking: AgentThinkingContentVM;
  label: string;
  onLinkClick?: (href: string) => void;
  showRawTimelineJson?: boolean;
  rawTimelineJsonLabel?: string;
}

export const AgentThinkingDisclosure = observer(
  function AgentThinkingDisclosure({
    thinking,
    label,
    onLinkClick,
    showRawTimelineJson = false,
    rawTimelineJsonLabel = ""
  }: AgentThinkingDisclosureProps): JSX.Element {
    "use memo";

    return (
      <>
        <WorkspaceAgentSessionThinkingDisclosure
          thinking={thinking}
          label={label}
          onLinkClick={onLinkClick}
        />
        {showRawTimelineJson && rawTimelineJsonLabel ? (
          <RawTimelineJsonDisclosure
            items={thinking.sourceTimelineItems}
            label={rawTimelineJsonLabel}
          />
        ) : null}
      </>
    );
  }
);
