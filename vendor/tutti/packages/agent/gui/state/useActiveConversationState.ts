import { useConversationServices } from "../conversationServices";
import {
  ActiveConversationState,
  type ActiveConversationStateInput
} from "./ActiveConversationState";

const models = new WeakMap<
  object,
  Map<string | null, ActiveConversationState>
>();

/** Presentation instances belong to one service scope; no session payload is copied here. */
export function activeConversationStateFor(
  input: ActiveConversationStateInput
): ActiveConversationState {
  let scoped = models.get(input.conversationServices);
  if (!scoped) {
    scoped = new Map();
    models.set(input.conversationServices, scoped);
  }
  let state = scoped.get(input.activeConversationId);
  if (!state) {
    state = new ActiveConversationState(input);
    scoped.set(input.activeConversationId, state);
  }
  return state;
}

export function useActiveConversationState(
  agentSessionId: string | null
): ActiveConversationState {
  const services = useConversationServices();
  return activeConversationStateFor({
    activeConversationId: agentSessionId,
    composer: services.composer,
    prompts: services.prompts,
    conversationServices: services
  });
}
