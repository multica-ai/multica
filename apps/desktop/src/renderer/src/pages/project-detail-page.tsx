import { useParams } from "react-router-dom";
import { ProjectDetail } from "@multica/views/projects/components";
import { IssueViewUrlSync } from "@/platform/issue-view-tabs";

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();

  if (!id) return null;
  return (
    <IssueViewUrlSync scope={{ scope_type: "project", scope_id: id }}>
      <ProjectDetail projectId={id} />
    </IssueViewUrlSync>
  );
}
