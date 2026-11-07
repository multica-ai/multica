import { createContext, type ComponentType } from "react";

export interface QueuedPromptMenuActionProps {
  agentSessionId?: string | null;
  promptId: string;
  disabled: boolean;
}

/** Optional host-owned action, rendered beside the queue's built-in actions. */
export const QueuedPromptMenuActionContext = createContext<
  ComponentType<QueuedPromptMenuActionProps> | null
>(null);
