"use client";
import { PlanListPage } from "@multica/views/workflows";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function PlansPage() {
  return (
    <ErrorBoundary>
      <PlanListPage />
    </ErrorBoundary>
  );
}
