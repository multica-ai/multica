"use client";
import { PlanDetailPage } from "@multica/views/workflows";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function Page({ params }: { params: { id: string } }) {
  return (
    <ErrorBoundary>
      <PlanDetailPage planId={params.id} />
    </ErrorBoundary>
  );
}
