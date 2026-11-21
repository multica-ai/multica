"use client";

import type { ReactNode } from "react";

/**
 * Title + supporting line for a step.
 *
 * `aria-live` because the page persists across steps: a screen reader user
 * moving between steps gets no navigation event, so the heading has to
 * announce itself.
 */
export function StepHeading({
  title,
  description,
}: {
  title: ReactNode;
  description?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5" aria-live="polite">
      <h1 className="text-balance text-title-lg font-semibold text-foreground">
        {title}
      </h1>
      {description ? (
        <p className="text-pretty text-body text-muted-foreground">
          {description}
        </p>
      ) : null}
    </div>
  );
}

/**
 * The step's actions, pinned to the bottom of the column.
 *
 * Full-width stacked buttons rather than a right-aligned inline row: the
 * column is narrow, so an inline bar leaves the primary action floating in the
 * middle of the screen instead of landing where the eye finishes the form.
 * `mt-auto` against the column's `min-h-full` is what pins it.
 */
export function StepFooter({
  children,
  hint,
}: {
  children: ReactNode;
  /** Optional status line above the buttons (validation, progress). */
  hint?: ReactNode;
}) {
  return (
    <div className="mt-auto flex flex-col gap-2 pb-2 pt-10">
      {hint ? (
        <p aria-live="polite" className="text-caption text-muted-foreground">
          {hint}
        </p>
      ) : null}
      {children}
    </div>
  );
}
