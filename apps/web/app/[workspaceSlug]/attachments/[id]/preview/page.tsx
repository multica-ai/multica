"use client";

import { use } from "react";
import { useSearchParams } from "next/navigation";
import { AttachmentPreviewPage } from "@multica/views/attachments";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

// Lives at /:slug/attachments/:id/preview — OUTSIDE the (dashboard) group on
// purpose. The dashboard layout adds a left sidebar + top chrome; this page
// wants the full viewport for the HTML iframe. Workspace resolution still
// happens in the parent [workspaceSlug] layout so useWorkspaceId() works.
export default function AttachmentPreviewWebPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const search = useSearchParams();
  const filename = search.get("name") ?? undefined;

  return (
    <ErrorBoundary resetKeys={[id]}>
      <AttachmentPreviewPage attachmentId={id} filename={filename} />
    </ErrorBoundary>
  );
}
