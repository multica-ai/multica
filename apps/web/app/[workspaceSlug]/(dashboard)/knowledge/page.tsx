"use client";

import { KnowledgePage } from "@multica/views/knowledge";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <KnowledgePage />
    </ErrorBoundary>
  );
}
