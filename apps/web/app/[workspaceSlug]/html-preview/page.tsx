"use client";

import { useSearchParams } from "next/navigation";
import { HtmlArtifactPreviewPage } from "@multica/views/attachments";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

// Full-page viewer for inline fenced ```html artifacts. It intentionally lives
// outside the dashboard group so the rendered document gets the full viewport.
export default function HtmlArtifactPreviewWebPage() {
  const search = useSearchParams();
  const key = search.get("key");

  return (
    <ErrorBoundary resetKeys={[key ?? ""]}>
      <HtmlArtifactPreviewPage artifactKey={key} />
    </ErrorBoundary>
  );
}
