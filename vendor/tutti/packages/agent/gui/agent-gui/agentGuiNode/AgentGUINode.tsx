import { memo } from "react";
import { AgentGUIFrame } from "./AgentGUIFrame";
import {
  areAgentGUINodePropsEqual,
  type AgentGUINodeProps
} from "./AgentGUINode.types";
import { AgentGUIPageProvider } from "./AgentGUIPageProvider";

export type { AgentGUINodeProps } from "./AgentGUINode.types";

export const AgentGUINode = memo(function AgentGUINode(
  props: AgentGUINodeProps
) {
  return (
    <AgentGUIPageProvider node={props}>
      {(features) => <AgentGUIFrame {...props} {...features} />}
    </AgentGUIPageProvider>
  );
}, areAgentGUINodePropsEqual);
