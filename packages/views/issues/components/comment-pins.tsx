"use client";

import { useLayoutEffect, useRef, useState } from "react";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";
import { Pin } from "@multica/ui/icons";
import { useT } from "../../i18n";
import { orderedPins } from "../hooks/comment-pins";
import { useVisiblePinBodies } from "../hooks/use-visible-pin-bodies";

// Every pin is stacked in pin order and stays put while reading: the bar only
// changes height when someone pins or unpins. A row whose original is on
// screen is dimmed rather than removed, so scrolling never reshuffles it.
export function CommentPins({ entries, container, onJump }: { entries: readonly TimelineEntry[]; container: HTMLElement | null; onJump: (id: string) => void }) {
  const { t } = useT("issues");
  const { getActorName: actorName } = useActorName();
  const pins = orderedPins(entries);
  const barRef = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(0);
  useLayoutEffect(() => {
    const node = barRef.current;
    if (!node) { setHeight(0); return; }
    const measure = () => setHeight(node.offsetHeight);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [pins.length]);
  const visible = useVisiblePinBodies(container, height);
  if (pins.length === 0) return null;
  return <div ref={barRef} className="sticky top-0 z-[25] pt-2" data-comment-pins-overlay>
    <div className="rounded-lg border border-border bg-background py-1 shadow-sm" data-testid="comment-pins" role="list" aria-label={t(($) => $.comment.pins.bar_label)}>
      {pins.map(pin => {
        const onScreen = visible.has(pin.id);
        const author = actorName(pin.actor_type, pin.actor_id);
        return <div key={pin.id} role="listitem">
          <button type="button" data-pin-row={pin.id} data-on-screen={onScreen || undefined}
            className={cn("flex w-full min-w-0 items-center gap-2 px-3 py-1 text-left text-body hover:bg-accent focus-visible:bg-accent focus-visible:outline-none", onScreen && "opacity-50")}
            aria-label={`${t(($) => $.comment.pins.jump, { author })}: ${(pin.content ?? "").slice(0, 80)}`} onClick={() => onJump(pin.id)}>
            <Pin className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
            <span className="shrink-0 font-medium">{author}</span>
            <span className="truncate text-muted-foreground">{pin.content}</span>
          </button>
        </div>;
      })}
    </div>
  </div>;
}
