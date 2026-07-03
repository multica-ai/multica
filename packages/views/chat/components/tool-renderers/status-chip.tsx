import { Check, AlertTriangle } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Badge } from "@multica/ui/components/ui/badge";
import { UnicodeSpinner } from "@multica/ui/components/common/unicode-spinner";
import type { ToolStatus } from "../../../common/task-transcript/build-timeline";
import { formatToolDuration, prefersReducedMotion } from "./util";

/**
 * Per-tool status indicator with a deliberate weight hierarchy: `done` recedes
 * (quiet muted ✓ + duration, no filled color), `error` shouts (the only loud,
 * tinted chip), `running` is a subtle secondary chip with a spinner. This makes
 * failures stand out at a glance instead of drowning in a wall of green.
 *
 * Accessibility: every state carries an icon/glyph AND text (never color
 * alone), the region is aria-live=polite so a status change is announced, and
 * the running spinner (JS timer, invisible to CSS `prefers-reduced-motion`) is
 * paused when the user asks for reduced motion.
 */
export function ToolStatusChip({
  status,
  durationMs,
  className,
}: {
  status: ToolStatus;
  durationMs?: number;
  className?: string;
}) {
  const duration = durationMs !== undefined ? formatToolDuration(durationMs) : "";

  if (status === "running") {
    return (
      <span
        aria-live="polite"
        className={cn("inline-flex items-center", className)}
      >
        <Badge variant="secondary" className="gap-1 text-muted-foreground">
          <UnicodeSpinner name="braille" paused={prefersReducedMotion()} className="opacity-70" />
          <span>Running</span>
        </Badge>
      </span>
    );
  }

  if (status === "error") {
    return (
      <span
        aria-live="polite"
        className={cn("inline-flex items-center gap-1", className)}
      >
        <Badge variant="destructive" className="gap-1">
          <AlertTriangle aria-hidden className="size-3" />
          <span>Error</span>
        </Badge>
        {duration && <span className="text-xs text-muted-foreground tabular-nums">{duration}</span>}
      </span>
    );
  }

  // done — recede: no filled color, just a muted check and the duration.
  return (
    <span
      aria-live="polite"
      className={cn("inline-flex items-center gap-1 text-xs text-muted-foreground", className)}
    >
      <Check aria-hidden className="size-3" />
      <span className="sr-only">Done</span>
      {duration && <span className="tabular-nums">{duration}</span>}
    </span>
  );
}
