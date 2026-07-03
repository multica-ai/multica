"use client";

import { use } from "react";
import { IssueDetail } from "@multica/views/issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

// Issue detail — identifier-first (Linear-style /issue/NAI-3), also accepts
// a UUID. The team rides in the identifier, not in a path segment, so a
// team move never orphans the URL (old identifiers resolve via the
// server-side alias).
export default function IssueDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueDetail issueId={id} />
    </ErrorBoundary>
  );
}
