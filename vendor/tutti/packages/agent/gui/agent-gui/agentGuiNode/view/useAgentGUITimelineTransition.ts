import { useLayoutEffect, useRef, useState } from "react";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentGUIDetailViewModel } from "../model/agentGuiNodeTypes";

export const AGENT_GUI_TIMELINE_SKELETON_DELAY_MS = 300;

interface Input {
  activeConversationId: string | null;
  availability: AgentGUIDetailViewModel["availability"];
  conversation: AgentConversationVM | null;
}

interface StableTimeline {
  conversation: AgentConversationVM | null;
  conversationId: string | null;
}

export function useAgentGUITimelineTransition(input: Input) {
  const activeConversationId = input.activeConversationId;
  const committedTimelineRef = useRef<StableTimeline>({
    conversation: input.conversation,
    conversationId: activeConversationId
  });
  const [revealedSkeletonConversationId, setRevealedSkeletonConversationId] =
    useState<string | null>(null);
  const revealedSkeletonConversationIdRef = useRef<string | null>(null);
  const transitionPending =
    activeConversationId !== null &&
    input.availability === "loading" &&
    (!input.conversation || input.conversation.rows.length === 0);

  useLayoutEffect(() => {
    if (!transitionPending || activeConversationId === null) {
      committedTimelineRef.current = {
        conversation: input.conversation,
        conversationId: activeConversationId
      };
      if (revealedSkeletonConversationIdRef.current !== null) {
        revealedSkeletonConversationIdRef.current = null;
        setRevealedSkeletonConversationId(null);
      }
      return;
    }
    // timing: avoid flashing a detail skeleton during fast local conversation loads
    const timeoutId = window.setTimeout(() => {
      revealedSkeletonConversationIdRef.current = activeConversationId;
      setRevealedSkeletonConversationId(activeConversationId);
    }, AGENT_GUI_TIMELINE_SKELETON_DELAY_MS);
    return () => window.clearTimeout(timeoutId);
  }, [activeConversationId, input.conversation, transitionPending]);

  const showTimelineSkeleton =
    transitionPending &&
    revealedSkeletonConversationId === activeConversationId;
  const retainedTimeline = transitionPending
    ? committedTimelineRef.current
    : null;
  const retainsPreviousTimeline =
    !showTimelineSkeleton &&
    retainedTimeline?.conversation !== null &&
    retainedTimeline?.conversation !== undefined;

  return {
    conversation: transitionPending
      ? (retainedTimeline?.conversation ?? null)
      : input.conversation,
    showTimelineSkeleton,
    timelineConversationId: retainsPreviousTimeline
      ? (retainedTimeline?.conversationId ?? activeConversationId)
      : activeConversationId,
    transitionPending
  };
}
