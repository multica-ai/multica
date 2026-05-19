"use client";

import { use } from "react";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { CerebroIssueDetailRoute } from "./cerebro-issue-detail-route";

export default function IssueDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <CerebroIssueDetailRoute id={id} />
    </ErrorBoundary>
  );
}
