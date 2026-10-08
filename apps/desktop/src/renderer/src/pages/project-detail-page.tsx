import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ProjectDetail } from "@multica/views/projects/components";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: project } = useQuery(projectDetailOptions(wsId, id!));

  // Plain text only — the project's icon is shown by the tab's leading visual,
  // not concatenated into the title (MUL-4370).
  useDocumentTitle(project ? project.title : "Project");

  if (!id) return null;
  return <ProjectDetail projectId={id} />;
}
