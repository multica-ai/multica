"use client";

import { use } from "react";
import { useSearchParams } from "next/navigation";
import { IssueDetail } from "@multica/views/issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function IssueDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const searchParams = useSearchParams();
  const commentId = searchParams.get("comment") ?? undefined;
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueDetail issueId={id} highlightCommentId={commentId} />
    </ErrorBoundary>
  );
}
