import type { HTMLAttributes, JSX, ReactNode } from "react";
import { CustomScrollArea } from "../../../../app/renderer/components/ui/custom-scroll-area";
import { cn } from "../../../../app/renderer/lib/utils";
import { observer } from "mobx-react-lite";
export const AgentToolScrollArea = observer(AgentToolScrollAreaRender);

interface AgentToolScrollAreaProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
  viewportClassName?: string;
  maxHeightClassName?: string;
}

function AgentToolScrollAreaRender({
  children,
  className,
  viewportClassName,
  maxHeightClassName = "max-h-[240px]",
  ...props
}: AgentToolScrollAreaProps): JSX.Element {
  "use memo";
  return (
    <CustomScrollArea
      className={cn(
        "agent-tool-scroll-area group/agent-tool-scroll",
        className
      )}
      viewportClassName={cn(
        "workspace-agents-status-panel__detail-scroll-region",
        maxHeightClassName,
        viewportClassName
      )}
      scrollbarClassName="agent-tool-scroll-area__scrollbar"
      scrollbarThumbClassName="workspace-agents-status-panel__scrollbar-thumb"
      {...props}
    >
      {children}
    </CustomScrollArea>
  );
}
