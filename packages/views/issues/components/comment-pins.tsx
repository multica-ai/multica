"use client";

import { useState } from "react";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { ChevronDown, ChevronUp, Pin } from "@multica/ui/icons";
import { useT } from "../../i18n";
import { offscreenPins } from "../hooks/comment-pins";
import { useVisiblePinBodies } from "../hooks/use-visible-pin-bodies";

export function CommentPins({ entries, container, onJump }: { entries: readonly TimelineEntry[]; container: HTMLElement | null; onJump: (id: string) => void }) {
  const { t } = useT("issues");
  const { getActorName: actorName } = useActorName();
  const [expanded, setExpanded] = useState(false);
  const visible = useVisiblePinBodies(container);
  const pins = offscreenPins(entries, visible);
  const latest = pins[0];
  return <div className="sticky top-0 z-[25] h-0" data-comment-pins-overlay>
    {latest && <div className="absolute inset-x-0 top-2 rounded-lg border border-border bg-background shadow-sm" data-testid="comment-pins">
      <div className="flex items-center gap-2 px-3 py-2">
        <Pin className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
        <Button variant="ghost" size="sm" className="min-w-0 flex-1 justify-start" aria-label={t(($) => $.comment.pins.jump, { author: actorName(latest.actor_type, latest.actor_id) })} onClick={() => { setExpanded(false); onJump(latest.id); }}>
          <span className="shrink-0 font-medium">{actorName(latest.actor_type, latest.actor_id)}</span>
          <span className="truncate text-muted-foreground">{latest.content}</span>
        </Button>
        <Button variant="ghost" size="sm" aria-expanded={expanded} aria-label={expanded ? t(($) => $.comment.pins.collapse) : t(($) => $.comment.pins.expand)} onClick={() => setExpanded(value => !value)}>
          <span aria-label={t(($) => $.comment.pins.count, { count: pins.length })}>{pins.length}</span>
          {expanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
        </Button>
      </div>
      {expanded && <div className="max-h-56 overflow-y-auto border-t border-border p-2">
        {pins.map(pin => <Button key={pin.id} variant="ghost" size="sm" className="w-full justify-start" aria-label={t(($) => $.comment.pins.jump, { author: actorName(pin.actor_type, pin.actor_id) })} onClick={() => { setExpanded(false); onJump(pin.id); }}>
          <span className="shrink-0 font-medium">{actorName(pin.actor_type, pin.actor_id)}</span><span className="truncate text-muted-foreground">{pin.content}</span>
        </Button>)}
      </div>}
    </div>}
  </div>;
}
