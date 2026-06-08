"use client";

import { lazy, Suspense } from "react";

const Toaster = lazy(() =>
  import("@multica/ui/components/ui/sonner").then((mod) => ({
    default: mod.Toaster,
  })),
);

export function ClientToaster() {
  return (
    <Suspense fallback={null}>
      <Toaster />
    </Suspense>
  );
}
