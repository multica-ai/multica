"use client";

import { lazy, Suspense, type ComponentProps } from "react";
import type { Calendar } from "./calendar";
import { Skeleton } from "./skeleton";

const CalendarContent = lazy(() =>
  import("./calendar").then((module) => ({ default: module.Calendar })),
);

/** Keep the date library out of the import graph until a calendar is mounted. */
export function LazyCalendar(props: ComponentProps<typeof Calendar>) {
  return (
    <Suspense fallback={<Skeleton className="h-64 w-56" aria-busy="true" />}>
      <CalendarContent {...props} />
    </Suspense>
  );
}
