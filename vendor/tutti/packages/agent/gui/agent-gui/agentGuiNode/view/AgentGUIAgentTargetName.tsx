import { cn } from "@tutti-os/ui-system";
import type { AgentGUIAgentTarget } from "../../../types";
import { projectAgentGUIAgentTargetName } from "../model/agentGuiTargetName";
import { observer } from "mobx-react-lite";
export const AgentGUIAgentTargetName = observer(AgentGUIAgentTargetNameRender);

function AgentGUIAgentTargetNameRender({
  className,
  ownerSeparator,
  target
}: {
  className?: string;
  ownerSeparator: string;
  target: AgentGUIAgentTarget;
}): React.JSX.Element {
  const presentation = projectAgentGUIAgentTargetName({
    ownerSeparator,
    target
  });

  return (
    <span
      className={cn("flex min-w-0 max-w-full items-baseline", className)}
      title={presentation.fullLabel}
    >
      {presentation.ownerLabel ? (
        <>
          <span
            className="min-w-0 flex-1 truncate"
            data-testid="agent-target-owner-name"
          >
            {presentation.ownerLabel}
          </span>
          <span
            className="shrink-0 whitespace-pre"
            data-testid="agent-target-name-suffix"
          >
            {presentation.ownerSeparator}
            {presentation.agentLabel}
          </span>
        </>
      ) : (
        <span className="min-w-0 truncate" data-testid="agent-target-name">
          {presentation.agentLabel}
        </span>
      )}
    </span>
  );
}
