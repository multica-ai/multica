"use client";

import { IssuesPage } from "@wallts/views/issues/components";
import { ErrorBoundary } from "@wallts/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <IssuesPage />
    </ErrorBoundary>
  );
}
