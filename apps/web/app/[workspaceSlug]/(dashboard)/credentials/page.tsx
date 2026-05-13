"use client";

import { CredentialsListPage } from "@multica/cerebro-credentials/views";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <CredentialsListPage />
    </ErrorBoundary>
  );
}
