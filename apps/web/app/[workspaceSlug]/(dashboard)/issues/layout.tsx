"use client";

import { useState, type ReactNode } from "react";
import { useSelectedLayoutSegment } from "next/navigation";
import { IssueDetailRoute } from "@multica/views/issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function IssuesLayout({ children }: { children: ReactNode }) {
  const routeId = useSelectedLayoutSegment();
  const [retainedId, setRetainedId] = useState(routeId);

  if (routeId && retainedId !== routeId) {
    // ponytail: retain only the last detail; use a bounded LRU only if
    // multi-detail backtracking becomes a measured requirement.
    setRetainedId(routeId);
  }

  const detailId = routeId ?? retainedId;

  return (
    <>
      {!routeId && children}
      {detailId && (
        <div
          aria-hidden={!routeId || undefined}
          className={routeId ? "contents" : "hidden"}
        >
          <ErrorBoundary resetKeys={[detailId]}>
            <IssueDetailRoute routeId={detailId} />
          </ErrorBoundary>
        </div>
      )}
    </>
  );
}
