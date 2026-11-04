import type { ReactNode } from "react";
import type { AgentComposerProps } from "../AgentComposer";

export type AgentGUIComposerFooterAccessoryContext = Pick<
  AgentComposerProps,
  | "agentSessionId"
  | "draftContent"
  | "onDraftContentChange"
  | "onProjectPathChange"
  | "isActive"
  | "isSendingTurn"
  | "isSubmittingPrompt"
  | "composerSettings"
  | "selectedAgentTarget"
>;

export type AgentGUIComposerFooterAccessoryRenderer = (
  context: AgentGUIComposerFooterAccessoryContext
) => ReactNode;

/** Host-owned action for the send button while the composer is empty. */
export interface AgentGUIComposerEmptySubmit {
  label: string;
  onSubmit: () => void;
}
