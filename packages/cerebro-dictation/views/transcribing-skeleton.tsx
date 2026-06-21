"use client";

import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";

export interface TranscribingSkeletonProps {
  /** Positioning hook — defaults to a bottom overlay inside a relative host. */
  className?: string;
}

/**
 * Forslag B (FIR-1637): the "text is on its way" placeholder shown at the
 * destination while the clip is transcribing (the model works ~2-4s). Pulsing
 * grey lines — like a page skeleton — appear where the transcript will land, so
 * the user sees *where* the text is coming, not just *that* the mic is busy
 * (that is the spinner, Forslag A, on the mic button). Combined, A says "it is
 * working" and B shows "the text appears here".
 *
 * Decorative and inert: `pointer-events-none` so it never blocks the field, and
 * `aria-hidden` because MicButton already announces the transcribing state to
 * screen readers via aria-live — a second announcement would be noise.
 *
 * Render it inside the same `relative` wrapper that hosts the mic so the default
 * bottom positioning lands over the destination field.
 */
export function TranscribingSkeleton({ className }: TranscribingSkeletonProps) {
  return (
    <div
      aria-hidden="true"
      data-testid="dictation-transcribing-skeleton"
      className={cn(
        "pointer-events-none absolute inset-x-3 bottom-9 z-0 flex flex-col gap-1.5",
        className,
      )}
    >
      <Skeleton className="h-3 w-3/4" />
      <Skeleton className="h-3 w-1/2" />
    </div>
  );
}
