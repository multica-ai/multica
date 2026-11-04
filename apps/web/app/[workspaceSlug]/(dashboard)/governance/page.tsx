"use client";

import { GovernancePage } from "@multica/views/governance";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <GovernancePage />
    </ErrorBoundary>
  );
}
