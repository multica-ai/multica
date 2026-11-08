"use client";

import { use } from "react";
import { IssueDetailRoute } from "@multica/views/issues/components";
import { decodeRouteSegment } from "@multica/core/paths";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

// `/{ws}/issues/{id}/deliverables/{name}`: the issue page, opened on the
// newest version of the deliverable called `name`.
export default function IssueDeliverablePage({
  params,
}: {
  params: Promise<{ id: string; name: string }>;
}) {
  const { id, name } = use(params);
  const filename = decodeRouteSegment(name);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueDetailRoute routeId={id} deliverable={filename} />
    </ErrorBoundary>
  );
}
