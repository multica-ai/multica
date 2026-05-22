import { useSearchParams } from "react-router-dom";
import { HtmlArtifactPreviewPage } from "@multica/views/attachments";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export function HtmlArtifactPreviewRoute() {
  const [searchParams] = useSearchParams();
  const key = searchParams.get("key");

  return (
    <ErrorBoundary resetKeys={[key ?? ""]}>
      <HtmlArtifactPreviewPage artifactKey={key} />
    </ErrorBoundary>
  );
}
