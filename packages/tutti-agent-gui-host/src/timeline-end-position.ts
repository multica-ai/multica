import { useLayoutEffect, useState } from "react";
import type { AgentGUIDetailScrollInput } from "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/view/agentGUIDetailScrollTypes";

const END_POSITION_EPSILON = 1;

// Position controls the affordance; follow mode continues to own auto-scrolling.
export function useTimelineEndPosition(input: AgentGUIDetailScrollInput, controllerRevision: number) {
  const {
    conversation, isVisible, showTimelineSkeleton, timelineConversationId,
    timelineRef, timelineContentRef, virtualScrollControllerRef,
  } = input;
  const [atEnd, setAtEnd] = useState(true);

  useLayoutEffect(() => {
    const timeline = timelineRef.current;
    const content = timelineContentRef.current;
    if (!isVisible || showTimelineSkeleton || !timelineConversationId || !timeline) {
      setAtEnd(true);
      return;
    }
    let frame: number | null = null;
    const measure = () => {
      const controller = virtualScrollControllerRef.current;
      if (controller?.enabled && controller.agentSessionId !== timelineConversationId) {
        setAtEnd(true);
        return;
      }
      // Virtual transcripts expose their total size through the DOM spacer.
      // The controller's cached end state can lag behind content resizes.
      setAtEnd(timeline.scrollHeight - timeline.clientHeight - timeline.scrollTop <= END_POSITION_EPSILON);
    };
    // Read after upstream scroll anchoring and virtual-list measurements settle.
    const schedule = () => {
      if (frame !== null) return;
      frame = window.requestAnimationFrame(() => { frame = null; measure(); });
    };
    measure();
    schedule();
    timeline.addEventListener("scroll", schedule, { passive: true });
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    observer?.observe(timeline);
    if (content) observer?.observe(content);
    return () => {
      timeline.removeEventListener("scroll", schedule);
      observer?.disconnect();
      if (frame !== null) window.cancelAnimationFrame(frame);
    };
  }, [conversation, controllerRevision, isVisible, showTimelineSkeleton, timelineConversationId,
    timelineRef, timelineContentRef, virtualScrollControllerRef]);

  return atEnd;
}
