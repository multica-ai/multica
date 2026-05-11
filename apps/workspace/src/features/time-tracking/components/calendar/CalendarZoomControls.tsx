/**
 * CalendarZoomControls — zoom in/out control rendered in the `timeGutterHeader` slot.
 *
 * Controls the `step` / `timeslots` props of the calendar:
 *   zoom -1 → step 30, timeslots 2
 *   zoom  0 → step 15, timeslots 4
 *   zoom +1 → step 10, timeslots 6
 */
import { Minus, Plus } from "lucide-react";

interface CalendarZoomControlsProps {
  /** Current zoom level: -1 | 0 | 1 */
  zoom: number;
  onZoomIn?: () => void;
  onZoomOut?: () => void;
}

export function CalendarZoomControls({ zoom, onZoomIn, onZoomOut }: CalendarZoomControlsProps) {
  return (
    <div className="flex items-center justify-center gap-1 py-2">
      <button
        type="button"
        title="Zoom out (less detail)"
        disabled={zoom <= -1}
        onClick={onZoomOut}
        className="flex size-6 items-center justify-center rounded text-muted-foreground transition hover:text-foreground disabled:cursor-not-allowed disabled:opacity-30"
      >
        <Minus className="size-3" />
      </button>
      <button
        type="button"
        title="Zoom in (more detail)"
        disabled={zoom >= 1}
        onClick={onZoomIn}
        className="flex size-6 items-center justify-center rounded text-muted-foreground transition hover:text-foreground disabled:cursor-not-allowed disabled:opacity-30"
      >
        <Plus className="size-3" />
      </button>
    </div>
  );
}
