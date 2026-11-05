import type { ReactElement } from "react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  cn
} from "@tutti-os/ui-system";
import type {
  AgentGUIAgentTarget,
  AgentGUIAgentTargetInfoRenderer,
  AgentGUIAgentTargetInfoSurface
} from "../types";
import { observer } from "mobx-react-lite";
export const AgentTargetInfoTooltip = observer(AgentTargetInfoTooltipRender);
const AgentTargetInfoContent = observer(AgentTargetInfoContentRender);

interface AgentTargetInfoTooltipProps {
  align?: "center" | "end" | "start";
  children: ReactElement;
  fallbackLabel: string;
  onOpenChange?: (open: boolean) => void;
  open?: boolean;
  renderer?: AgentGUIAgentTargetInfoRenderer | null;
  side?: "bottom" | "left" | "right" | "top";
  sideOffset?: number;
  surface: AgentGUIAgentTargetInfoSurface;
  target: AgentGUIAgentTarget;
}

function AgentTargetInfoTooltipRender({
  align,
  children,
  fallbackLabel,
  onOpenChange,
  open,
  renderer,
  side,
  sideOffset,
  surface,
  target
}: AgentTargetInfoTooltipProps): React.JSX.Element {
  return (
    <Tooltip open={open} onOpenChange={onOpenChange}>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent
        align={align}
        className={cn(renderer && "items-stretch p-0")}
        side={side}
        sideOffset={sideOffset}
      >
        {renderer ? (
          <AgentTargetInfoContent
            fallbackLabel={fallbackLabel}
            renderer={renderer}
            surface={surface}
            target={target}
          />
        ) : (
          fallbackLabel
        )}
      </TooltipContent>
    </Tooltip>
  );
}

function AgentTargetInfoContentRender({
  fallbackLabel,
  renderer,
  surface,
  target
}: {
  fallbackLabel: string;
  renderer: AgentGUIAgentTargetInfoRenderer;
  surface: AgentGUIAgentTargetInfoSurface;
  target: AgentGUIAgentTarget;
}): React.JSX.Element {
  const content = renderer({ surface, target });
  return content ? (
    <>{content}</>
  ) : (
    <span className="px-2 py-1">{fallbackLabel}</span>
  );
}
