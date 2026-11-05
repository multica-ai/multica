import type { AgentGUIPageInput } from "./agentGuiController.types";
import { useAgentGUICommandBindings } from "./useAgentGUICommandBindings";
import { useAgentGUISelectionState } from "./useAgentGUISelectionState";

export function useAgentGUIPage(input: AgentGUIPageInput) {
  const selection = useAgentGUISelectionState(input);
  return useAgentGUICommandBindings(input, selection);
}
