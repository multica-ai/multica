"use client";

import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { UgWikiGraphPage } from "@multica/views/ug-wiki-graph";

export default function Page() {
  return (
    <ErrorBoundary>
      <UgWikiGraphPage />
    </ErrorBoundary>
  );
}

