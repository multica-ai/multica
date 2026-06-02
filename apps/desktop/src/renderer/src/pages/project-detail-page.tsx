import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ProjectDetail } from "@wallts/views/projects/components";
import { useWorkspaceId } from "@wallts/core/hooks";
import { projectDetailOptions } from "@wallts/core/projects/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: project } = useQuery(projectDetailOptions(wsId, id!));

  useDocumentTitle(project ? `${project.icon || "📁"} ${project.title}` : "Project");

  if (!id) return null;
  return <ProjectDetail projectId={id} />;
}
