"use client";

import { use } from "react";
import { IssueDetailRoute } from "@multica/views/issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

// `/{ws}/issues/{id}/deliverables/{name}`: the issue page, opened on the
// newest version of the deliverable called `name`.
//
// The App Router hands a dynamic param over percent-encoded: it decodes the
// matched segment and runs `encodeURIComponent` on it again, so a file called
// `調査 報告.html` arrives as `%E8%AA%BF...%20...html`. One decode gives the
// file name back and, because the value is always `encodeURIComponent`
// output, cannot throw. Exactly one: a second would turn a literal
// `report%20final.html` into `report final.html` and look up the wrong file.
export default function IssueDeliverablePage({
  params,
}: {
  params: Promise<{ id: string; name: string }>;
}) {
  const { id, name } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueDetailRoute routeId={id} deliverable={decodeURIComponent(name)} />
    </ErrorBoundary>
  );
}
