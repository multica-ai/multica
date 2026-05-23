"use client";

import { use } from "react";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { ReferencesByObjectPage } from "@multica/cerebro-references/views/pages";

export default function ReferenceReverseLookupRoute({
  params,
}: {
  params: Promise<{ object: string; refId: string }>;
}) {
  const { object, refId } = use(params);
  return (
    <ErrorBoundary resetKeys={[object, refId]}>
      <ReferencesByObjectPage
        object={decodeURIComponent(object)}
        refId={decodeURIComponent(refId)}
      />
    </ErrorBoundary>
  );
}
