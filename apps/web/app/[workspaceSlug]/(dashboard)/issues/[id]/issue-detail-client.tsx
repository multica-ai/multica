"use client";

import { IssueDetail } from "@multica/views/issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export function IssueDetailClient({ issueId }: { issueId: string }) {
  return (
    <ErrorBoundary resetKeys={[issueId]}>
      <IssueDetail issueId={issueId} />
    </ErrorBoundary>
  );
}
