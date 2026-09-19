"use client";

import { useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { ChevronDown, ChevronRight, RotateCcw, Workflow } from "lucide-react";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { clampWorkflowLaneHeight as clamp, MIN_WORKFLOW_LANE_HEIGHT as MIN_HEIGHT, MAX_WORKFLOW_LANE_HEIGHT as MAX_HEIGHT } from "@multica/core/issues/workflow-lane-layout";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

/** Height is a per-view layout preference; resizing never changes the query. */
export function WorkflowLaneFrame({ laneKey, name, count, maxColumnCount, primary = false, children }: {
  laneKey: string;
  name: string;
  count: number;
  maxColumnCount: number;
  primary?: boolean;
  children: ReactNode;
}) {
  const { t } = useT("issues");
  const collapsed = useViewStore((s) => s.collapsedWorkflowLanes.includes(laneKey) || (!primary && !s.expandedWorkflowLanes.includes(laneKey)));
  const toggle = useViewStore((s) => s.toggleWorkflowLaneCollapsed);
  const savedHeight = useViewStore((s) => s.workflowLaneHeights[laneKey]);
  const saveHeight = useViewStore((s) => s.setWorkflowLaneHeight);
  const [draftHeight, setDraftHeight] = useState<number>();
  const [measuredHeight, setMeasuredHeight] = useState<number>();
  const body = useRef<HTMLDivElement>(null);
  const drag = useRef<{ pointerId: number; y: number; start: number; height: number } | null>(null);
  const bodyId = useId();
  // Use server counts rather than the currently loaded page. Leave room for
  // card metadata and long titles, while keeping sparse lanes compact.
  const autoHeight = Math.max(MIN_HEIGHT, Math.min(800, 80 + maxColumnCount * 145));
  const height = draftHeight ?? savedHeight;
  useLayoutEffect(() => {
    const element = body.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => setMeasuredHeight(Math.round(element.getBoundingClientRect().height)));
    observer.observe(element);
    return () => observer.disconnect();
  }, [collapsed]);
  const currentHeight = () => body.current?.getBoundingClientRect().height || height || autoHeight;
  const cancelResize = () => { drag.current = null; setDraftHeight(undefined); };
  return (
    <section className="shrink-0 border-b border-border">
      <div className="sticky top-0 z-10 flex items-center bg-background">
        <button type="button" aria-label={`${name} ${count}`} aria-expanded={!collapsed} aria-controls={bodyId}
          onClick={() => toggle(laneKey, !primary)}
          className="flex min-w-0 flex-1 items-center gap-2 px-4 py-3 text-body font-medium hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          {collapsed ? <ChevronRight className="size-3.5 shrink-0" /> : <ChevronDown className="size-3.5 shrink-0" />}
          <Workflow className="size-4 shrink-0 text-muted-foreground" />
          <span className="truncate">{name}</span>
          <span className="text-caption font-normal tabular-nums text-muted-foreground">{count}</span>
        </button>
        {savedHeight !== undefined && <Button variant="ghost" size="icon" className="mr-2 shrink-0"
          aria-label={t(($) => $.board.reset_lane_height, { name })} title={t(($) => $.board.reset_lane_height, { name })}
          onClick={() => saveHeight(laneKey, null)}><RotateCcw className="size-3.5" /></Button>}
      </div>
      {!collapsed && <>
        <div ref={body} id={bodyId} className="flex min-h-0"
          style={{ height: height ?? (primary ? "max(480px, 75dvh)" : `min(${autoHeight}px, max(320px, 50dvh))`) }}>
          {children}
        </div>
        <div role="separator" tabIndex={0} aria-orientation="horizontal" aria-controls={bodyId}
          aria-label={t(($) => $.board.resize_lane, { name })}
          aria-valuemin={MIN_HEIGHT} aria-valuemax={MAX_HEIGHT} aria-valuenow={measuredHeight ?? height ?? autoHeight}
          title={t(($) => $.board.resize_lane_hint)}
          className="group flex h-4 touch-none select-none items-center justify-center cursor-row-resize hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          onDoubleClick={() => saveHeight(laneKey, null)}
          onPointerDown={(event) => {
            if (event.button !== 0 || drag.current) return;
            event.preventDefault();
            event.currentTarget.focus();
            event.currentTarget.setPointerCapture(event.pointerId);
            const start = currentHeight();
            drag.current = { pointerId: event.pointerId, y: event.clientY, start, height: start };
          }}
          onPointerMove={(event) => {
            const active = drag.current;
            if (!active || active.pointerId !== event.pointerId) return;
            active.height = clamp(active.start + event.clientY - active.y);
            setDraftHeight(active.height);
          }}
          onPointerUp={(event) => {
            if (drag.current?.pointerId !== event.pointerId) return;
            saveHeight(laneKey, drag.current.height);
            cancelResize();
            event.currentTarget.releasePointerCapture(event.pointerId);
          }}
          onPointerCancel={cancelResize} onLostPointerCapture={cancelResize}
          onKeyDown={(event) => {
            if (event.key === "Escape") { cancelResize(); return; }
            if (!["ArrowUp", "ArrowDown", "Home", "End"].includes(event.key)) return;
            event.preventDefault();
            const next = event.key === "Home" ? MIN_HEIGHT : event.key === "End" ? MAX_HEIGHT
              : currentHeight() + (event.key === "ArrowDown" ? 40 : -40);
            saveHeight(laneKey, clamp(next));
          }}>
          <span aria-hidden="true" className="h-1 w-10 rounded-full bg-border group-hover:bg-muted-foreground/50 group-focus-visible:bg-muted-foreground/50" />
        </div>
      </>}
    </section>
  );
}
