"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Minus, Plus } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { timelineTicks, type LaneSegment, type TraceLanes, type ToolKindTotals } from "./build-steps";
import { useT } from "../../i18n";

/**
 * Where a run's time went, on a real wall-clock axis.
 *
 * Two lanes, not one: model turns and tool calls never overlap in an agent
 * loop, so on a single row they can only occlude each other. Split, each lane
 * reads as its own comb and carries its own total — "was it the model or the
 * commands" is answerable without hovering anything.
 *
 * Two lanes, not four: the tool lane could split by what the calls did, but in
 * a real run one kind dominates by orders of magnitude (commands run for
 * minutes, reads and edits finish in milliseconds), so a lane per kind draws
 * three near-empty tracks. That breakdown rides the lane's tooltip instead.
 *
 * Colour carries state, never taxonomy: the token set has one blue lightness
 * ramp and no categorical palette, so which lane a bar sits in says what kind
 * of work it was, and colour is spent on delivered (success) and failed
 * (destructive) instead.
 */

const SEGMENT_TONE: Record<LaneSegment["kind"], string> = {
  tool: "bg-chart-2",
  think: "bg-chart-3",
  report: "bg-success",
  error: "bg-destructive",
};

/** Fit-to-width, then powers of two. Past 8x a 30-minute run is 20 screens of
 *  scrolling, which stops being navigation. */
const ZOOM_STEPS = [1, 2, 4, 8] as const;
const LABEL_WIDTH = "5.75rem";

function formatTick(ms: number): string {
  const totalSeconds = Math.round(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest === 0 ? `${hours}h` : `${hours}h ${rest}m`;
}

export function formatLaneTotal(ms: number): string {
  const totalSeconds = Math.round(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return `${minutes}m ${String(seconds).padStart(2, "0")}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${String(minutes % 60).padStart(2, "0")}m`;
}

function LaneLabel({ label, total, title }: { label: string; total: string; title?: string }) {
  return (
    <div
      title={title}
      className="flex h-[15px] items-baseline gap-1.5 pr-2.5 text-micro text-muted-foreground"
    >
      <span className="truncate">{label}</span>
      <span className="ml-auto shrink-0 tabular-nums text-faint-foreground">{total}</span>
    </div>
  );
}

function LaneTrack({
  label,
  segments,
  totalMs,
  onSeek,
}: {
  label: string;
  segments: LaneSegment[];
  totalMs: number;
  onSeek: (ms: number) => void;
}) {
  return (
    <div className="relative h-[15px] rounded-sm bg-muted/55">
      {segments.map((segment) => (
        <button
          key={`${segment.kind}:${segment.startMs}`}
          type="button"
          aria-label={`${label} ${formatLaneTotal(segment.durationMs)}`}
          title={`${label} · ${formatLaneTotal(segment.durationMs)}`}
          onClick={() => onSeek(segment.startMs)}
          className={cn(
            "absolute inset-y-0 rounded-[3px] transition-opacity hover:opacity-80",
            SEGMENT_TONE[segment.kind],
          )}
          style={{
            left: `${(segment.startMs / totalMs) * 100}%`,
            // Sub-pixel bars would vanish; a visible minimum keeps a fast call
            // clickable. Zooming in is what makes the width truthful — which is
            // the whole reason the track scrolls.
            width: `max(3px, ${(segment.durationMs / totalMs) * 100}%)`,
          }}
        />
      ))}
    </div>
  );
}

export function RunTimeline({
  lanes,
  toolKinds,
  selectedOffsetMs,
  onSeek,
}: {
  lanes: TraceLanes;
  /** Breakdown behind the tool lane's total, shown on hover. */
  toolKinds?: ToolKindTotals;
  /** Offset of the selected step, drawn as a playhead across both lanes. */
  selectedOffsetMs?: number;
  onSeek: (ms: number) => void;
}) {
  const { t } = useT("agents");
  const [zoom, setZoom] = useState(1);
  const scrollerRef = useRef<HTMLDivElement>(null);

  // More screen means more room for labels: ticks scale with zoom so their
  // on-screen density stays constant instead of thinning out as you zoom in.
  const ticks = useMemo(() => timelineTicks(lanes.totalMs, 6 * zoom), [lanes.totalMs, zoom]);

  const setZoomAround = useCallback(
    (next: number) => {
      const el = scrollerRef.current;
      const previous = zoom;
      setZoom(next);
      if (!el) return;
      // Keep whatever is under the middle of the viewport under it after the
      // zoom, so zooming never teleports the reader somewhere else.
      const centerRatio = (el.scrollLeft + el.clientWidth / 2) / (el.clientWidth * previous);
      requestAnimationFrame(() => {
        el.scrollLeft = centerRatio * el.clientWidth * next - el.clientWidth / 2;
      });
    },
    [zoom],
  );

  // A selected step off-screen would leave the playhead invisible.
  useEffect(() => {
    const el = scrollerRef.current;
    if (!el || selectedOffsetMs === undefined || zoom === 1) return;
    const x = (selectedOffsetMs / lanes.totalMs) * el.clientWidth * zoom;
    if (x < el.scrollLeft || x > el.scrollLeft + el.clientWidth) {
      el.scrollLeft = Math.max(0, x - el.clientWidth / 2);
    }
  }, [selectedOffsetMs, lanes.totalMs, zoom]);

  const zoomIndex = ZOOM_STEPS.indexOf(zoom as (typeof ZOOM_STEPS)[number]);
  const canZoomIn = zoomIndex >= 0 && zoomIndex < ZOOM_STEPS.length - 1;
  const canZoomOut = zoomIndex > 0;

  const toolTitle = toolKinds
    ? [
        [t(($) => $.transcript.tool_kind_command), toolKinds.command],
        [t(($) => $.transcript.tool_kind_write), toolKinds.write],
        [t(($) => $.transcript.tool_kind_read), toolKinds.read],
        [t(($) => $.transcript.tool_kind_misc), toolKinds.other],
      ]
        .filter(([, ms]) => (ms as number) > 0)
        .map(([name, ms]) => `${name} ${formatLaneTotal(ms as number)}`)
        .join(" · ")
    : undefined;

  return (
    <div className="group/timeline relative shrink-0 border-b px-4 pb-2.5 pt-2">
      <div className="flex items-start">
        {/* Labels stay put while the track scrolls — a total that scrolls away
            is a total you cannot read against the bars it belongs to. */}
        <div className="shrink-0" style={{ width: LABEL_WIDTH }}>
          <div className="h-3" />
          <div className="mt-1 space-y-1">
            <LaneLabel
              label={t(($) => $.transcript.lane_model)}
              total={formatLaneTotal(lanes.modelMs)}
            />
            <LaneLabel
              label={t(($) => $.transcript.lane_tool)}
              total={formatLaneTotal(lanes.toolMs)}
              title={toolTitle}
            />
          </div>
        </div>

        <div
          ref={scrollerRef}
          className="min-w-0 flex-1 overflow-x-auto overscroll-x-contain"
        >
          <div className="relative" style={{ width: `${zoom * 100}%` }}>
            <div className="relative h-3">
              {ticks.map((tick, index) => (
                <span
                  key={tick}
                  className={cn(
                    "absolute top-0 whitespace-nowrap text-micro tabular-nums text-faint-foreground",
                    index === 0
                      ? ""
                      : index === ticks.length - 1
                        ? "-translate-x-full"
                        : "-translate-x-1/2",
                  )}
                  style={{ left: `${(tick / lanes.totalMs) * 100}%` }}
                >
                  {formatTick(tick)}
                </span>
              ))}
            </div>
            <div className="mt-1 space-y-1">
              <LaneTrack
                label={t(($) => $.transcript.lane_model)}
                segments={lanes.model}
                totalMs={lanes.totalMs}
                onSeek={onSeek}
              />
              <LaneTrack
                label={t(($) => $.transcript.lane_tool)}
                segments={lanes.tool}
                totalMs={lanes.totalMs}
                onSeek={onSeek}
              />
            </div>
            {selectedOffsetMs !== undefined && (
              <div
                aria-hidden
                className="pointer-events-none absolute bottom-[-2px] top-2.5 w-0.5 rounded-full bg-brand"
                style={{
                  left: `${
                    (Math.min(Math.max(selectedOffsetMs, 0), lanes.totalMs) / lanes.totalMs) * 100
                  }%`,
                }}
              />
            )}
          </div>
        </div>
      </div>

      {/* Zoom is the answer to a long run compressing its bars to slivers.
          Resting state stays clean: the control appears on hover, and stays
          visible once zoomed so the way back is never hidden. */}
      <div
        className={cn(
          "absolute right-3 top-1 flex items-center gap-0.5 rounded-md border bg-surface/95 p-0.5 shadow-sm transition-opacity",
          zoom > 1 ? "opacity-100" : "opacity-0 focus-within:opacity-100 group-hover/timeline:opacity-100",
        )}
      >
        <button
          type="button"
          disabled={!canZoomOut}
          onClick={() => setZoomAround(ZOOM_STEPS[zoomIndex - 1] ?? 1)}
          aria-label={t(($) => $.transcript.timeline_zoom_out)}
          className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent disabled:opacity-40"
        >
          <Minus className="h-3 w-3" />
        </button>
        <span className="min-w-[2rem] text-center text-micro tabular-nums text-muted-foreground">
          {zoom}×
        </span>
        <button
          type="button"
          disabled={!canZoomIn}
          onClick={() => setZoomAround(ZOOM_STEPS[zoomIndex + 1] ?? zoom)}
          aria-label={t(($) => $.transcript.timeline_zoom_in)}
          className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent disabled:opacity-40"
        >
          <Plus className="h-3 w-3" />
        </button>
      </div>
    </div>
  );
}
