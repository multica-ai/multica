"use client";

import { PipelinePage } from "@multica/views/pipeline";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <PipelinePage />
    </ErrorBoundary>
  );
}
