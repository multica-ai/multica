import { useCallback, useEffect, useRef, useState } from "react";
import { useRestoredViewState, useViewStateWriter } from "../../platform";

interface TimelineAnchor { commentId: string; offset: number }
interface AnchorSnapshot { key: string; anchor: TimelineAnchor | null }

function readAnchor(value: string | undefined): TimelineAnchor | null {
  if (!value) return null;
  try {
    const anchor: unknown = JSON.parse(value);
    if (typeof anchor !== "object" || !anchor || !("commentId" in anchor) || !("offset" in anchor)) return null;
    return typeof anchor.commentId === "string" && /^[A-Za-z0-9_-]+$/.test(anchor.commentId)
      && typeof anchor.offset === "number" && Number.isFinite(anchor.offset)
      ? { commentId: anchor.commentId, offset: anchor.offset } : null;
  } catch { return null; }
}

/** Keep reading position in the platform's route/tab memento, not in pixel estimates. */
export function useIssueTimelineAnchor({
  containerKey, container, ready, skipRestore,
}: {
  containerKey: string;
  container: HTMLElement | null;
  ready: boolean;
  skipRestore: boolean;
}) {
  const key = `timeline-anchor:${containerKey}`;
  const saved = useRestoredViewState(key);
  const write = useViewStateWriter();
  const snapshotRef = useRef<AnchorSnapshot | null>(null);
  if (snapshotRef.current?.key !== key) snapshotRef.current = { key, anchor: readAnchor(saved) };
  const snapshot = snapshotRef.current;
  // A fresh notification selection owns the landing, even after it marks its
  // highlight consumed. It must not revive the previous reading position.
  if (skipRestore) snapshot.anchor = null;
  const [completed, setCompleted] = useState<AnchorSnapshot | null>(null);
  const pending = completed !== snapshot ? snapshot.anchor : null;
  const complete = useCallback(() => setCompleted(snapshot), [snapshot]);

  useEffect(() => {
    if (!container || !ready || skipRestore || pending) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const capture = () => {
      const c = container.getBoundingClientRect();
      if (!c.width || !c.height || typeof document.elementFromPoint !== "function") return;
      // Hit testing finds the visible reply without measuring every offscreen
      // placeholder on each scroll of a long thread.
      const el = document.elementFromPoint(c.left + c.width / 2, c.top + c.height / 2)
        ?.closest<HTMLElement>('[id^="comment-"]');
      const anchor = el && container.contains(el)
        ? { commentId: el.id.slice("comment-".length), offset: el.getBoundingClientRect().top - c.top }
        : null;
      write(key, anchor ? JSON.stringify(anchor) : undefined);
    };
    // Desktop commits view state through its tab store. Save after scrolling
    // settles, and before navigation input, instead of updating that store on
    // every animation frame.
    const schedule = () => { clearTimeout(timer); timer = setTimeout(capture, 100); };
    container.addEventListener("scroll", schedule, { passive: true });
    document.addEventListener("pointerdown", capture, true);
    document.addEventListener("keydown", capture, true);
    const frame = requestAnimationFrame(capture);
    return () => {
      clearTimeout(timer);
      cancelAnimationFrame(frame);
      container.removeEventListener("scroll", schedule);
      document.removeEventListener("pointerdown", capture, true);
      document.removeEventListener("keydown", capture, true);
    };
  }, [container, ready, skipRestore, pending, key, write]);

  return { pending, complete };
}
