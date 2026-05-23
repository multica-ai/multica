"use client";

import { IssuesPage } from "@multica/views/issues/components/issues-page";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { IssueListReferenceFilter } from "@multica/cerebro-references/views";

export default function Page() {
  const referencesEnabled = useFeatureFlag("cerebro_references");
  return (
    <ErrorBoundary>
      <IssuesPage
        referenceFilterControl={
          referencesEnabled ? <IssueListReferenceFilter /> : null
        }
      />
    </ErrorBoundary>
  );
}
