import { reaction } from "mobx";
import { useEffect, type RefObject } from "react";
import type { ConversationServices } from "../../../conversationServices";
import { findMessageLocatorScrollParent } from "./AgentMessageLocatorRail";
import { escapeCssString } from "./agentTranscriptModel";

/** Mounting or selecting a conversation is not evidence that its new result was seen. */
export function useCompletionRead(completions: ConversationServices["completions"], sessionId: string, isVisible: boolean, container: RefObject<HTMLElement | null>): void {
  useEffect(() => {
    if (!completions || !isVisible || !container.current) return;
    const host = container.current;
    let frame = 0;
    const check = () => {
      if (document.visibilityState !== "visible" || !document.hasFocus()) return;
      const position = completions.pending(sessionId);
      if (!position?.completedTurnId) return;
      const turns = host.querySelectorAll<HTMLElement>(`[data-multica-turn-id="${escapeCssString(position.completedTurnId)}"]`);
      const rows = [...turns].flatMap((turn) => [...turn.querySelectorAll<HTMLElement>('[data-agent-transcript-row][data-agent-transcript-turn-completed="true"]')]);
      const row = rows.at(-1);
      if (!row || !row.getClientRects().length) return;
      const scroll = findMessageLocatorScrollParent(row);
      const bounds = row.getBoundingClientRect();
      const viewport = scroll?.getBoundingClientRect();
      const top = Math.max(0, viewport?.top ?? 0);
      const bottom = Math.min(window.innerHeight, viewport?.bottom ?? window.innerHeight);
      const left = Math.max(0, viewport?.left ?? 0);
      const right = Math.min(window.innerWidth, viewport?.right ?? window.innerWidth);
      if (bounds.bottom > top && bounds.top < bottom && bounds.right > left && bounds.left < right) completions.markRead(sessionId, position);
    };
    const schedule = () => {
      if (!completions.pending(sessionId)) return;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(check);
    };
    const stop = reaction(() => completions.pending(sessionId), schedule, { fireImmediately: true });
    const mutations = new MutationObserver(schedule);
    mutations.observe(host, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ["data-agent-transcript-turn-completed"] });
    const resize = new ResizeObserver(schedule);
    resize.observe(host.parentElement ?? host);
    document.addEventListener("visibilitychange", schedule);
    window.addEventListener("focus", schedule);
    window.addEventListener("resize", schedule);
    document.addEventListener("scroll", schedule, true);
    return () => {
      stop(); mutations.disconnect(); resize.disconnect(); cancelAnimationFrame(frame);
      document.removeEventListener("visibilitychange", schedule);
      window.removeEventListener("focus", schedule);
      window.removeEventListener("resize", schedule);
      document.removeEventListener("scroll", schedule, true);
    };
  }, [completions, sessionId, isVisible, container]);
}
