"use client";

import { CompanyPage } from "@multica/views/company";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <CompanyPage />
    </ErrorBoundary>
  );
}
