import type { MutableRefObject, RefObject } from "react";
import type { AgentTranscriptVirtualScrollController } from "../../../shared/agentConversation/components/AgentTranscriptView";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentGUINodeViewProps } from "../AgentGUINodeView";
import type { TimelineScrollAnchor } from "./agentGUIScrollMemory";

export interface AgentGUIPendingPrependScrollAnchor {
  conversationId: string;
  scrollHeight: number;
  scrollTop: number;
}

export interface AgentGUIDetailScrollInput {
  actions: AgentGUINodeViewProps["actions"];
  bottomDockRef: RefObject<HTMLDivElement | null>;
  bottomDockStoreRevision: string;
  conversation: AgentConversationVM | null;
  isVisible: boolean;
  pendingPrependScrollAnchorRef: MutableRefObject<AgentGUIPendingPrependScrollAnchor | null>;
  showTimelineSkeleton: boolean;
  submittedPromptScrollConversationRef: MutableRefObject<string | null>;
  timelineConversationId: string | null;
  timelineContentRef: RefObject<HTMLDivElement | null>;
  timelineRef: RefObject<HTMLDivElement | null>;
  timelineScrollAnchorRef: MutableRefObject<TimelineScrollAnchor | null>;
  virtualScrollControllerRef: RefObject<AgentTranscriptVirtualScrollController | null>;
  activeConversationId: string | null;
  hasOlderMessages: boolean;
  isLoadingOlderMessages: boolean;
}
