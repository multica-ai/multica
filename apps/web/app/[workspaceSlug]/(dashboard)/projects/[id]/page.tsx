"use client";

import { Suspense, use } from "react";
import { ProjectDetail } from "@multica/views/projects/components";
import { useIssueViewUrlSync } from "../../../../../platform/use-issue-view-url-sync";

function IssueViewUrlSync({ projectId }: { projectId: string }) {
  // useSearchParams requires a Suspense boundary in the app router.
  useIssueViewUrlSync({ scope_type: "project", scope_id: projectId });
  return null;
}

export default function ProjectDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return (
    <>
      <Suspense fallback={null}>
        <IssueViewUrlSync projectId={id} />
      </Suspense>
      <ProjectDetail projectId={id} />
    </>
  );
}
