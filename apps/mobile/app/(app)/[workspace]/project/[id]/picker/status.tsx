/**
 * Project status picker route — presented as a formSheet by the parent
 * Stack. Self-contained: reads project from cache, fires useUpdateProject
 * on selection, then router.back()s.
 */
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { ProjectStatusPickerBody } from "@/components/project/pickers/project-status-picker-body";
import { projectDetailOptions } from "@/data/queries/projects";
import { useUpdateProject } from "@/data/mutations/projects";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function ProjectStatusPickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: project } = useQuery(projectDetailOptions(wsId, id));
  const updateProject = useUpdateProject(id);

  return (
    <ProjectStatusPickerBody
      value={project?.status ?? "planned"}
      onChange={(next) => {
        updateProject.mutate({ status: next });
        router.back();
      }}
    />
  );
}
