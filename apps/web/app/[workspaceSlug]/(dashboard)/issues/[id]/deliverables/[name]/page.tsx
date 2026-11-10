"use client";

import { use } from "react";
import { IssueDetailRoute } from "@multica/views/issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

// `/{ws}/issues/{id}/deliverables/{name}`: the issue page, opened on the
// newest version of the deliverable called `name`.
//
// Next hands dynamic params over already decoded, so `name` is the file name
// itself. Decoding it again would turn a literal `report%20final.html` into
// `report final.html` and look up the wrong file.
export default function IssueDeliverablePage({
  params,
}: {
  params: Promise<{ id: string; name: string }>;
}) {
  const { id, name } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueDetailRoute routeId={id} deliverable={name} />
    </ErrorBoundary>
  );
}
