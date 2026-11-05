import { useRef, type RefObject } from "react";
import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel
} from "../model/agentGuiNodeTypes";
import type {
  AgentGUIComposerEngagement,
  AgentGUIEngagementEventSink
} from "./agentGUIEngagement.types";
import { projectAgentGUIEngagementContext } from "./projectAgentGUIEngagementContext";
import { useAgentGUIPanelEngagement } from "./useAgentGUIPanelEngagement";

export function useAgentGUINodeEngagement(input: {
  composerReady: boolean;
  isActive: boolean;
  isVisible: boolean;
  onEvent?: AgentGUIEngagementEventSink;
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
}): {
  composerEngagement: AgentGUIComposerEngagement | undefined;
  layoutElementRef: RefObject<HTMLDivElement | null>;
} {
  const layoutElementRef = useRef<HTMLDivElement | null>(null);
  const projected = projectAgentGUIEngagementContext(
    input,
    input.composerReady
  );
  return {
    composerEngagement: useAgentGUIPanelEngagement({
      ...projected,
      elementRef: layoutElementRef,
      isActive: input.isActive,
      isVisible: input.isVisible,
      onEvent: input.onEvent
    }),
    layoutElementRef
  };
}
