import type { ActiveConversationStateInput } from "../../../state/ActiveConversationState";
import { activeConversationStateFor } from "../../../state/useActiveConversationState";

export function useAgentGUISessionState(input: ActiveConversationStateInput) {
  return activeConversationStateFor(input);
}
