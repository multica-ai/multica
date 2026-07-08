/**
 * Project priority picker route — presented as a formSheet by the parent
 * Stack. Self-contained: reads project from cache, fires useUpdateProject
 * on selection, then router.back()s.
 */
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { ProjectPriorityPickerBody } from "@/components/project/pickers/project-priority-picker-body";
import { projectDetailOptions } from "@/data/queries/projects";
import { useUpdateProject } from "@/data/mutations/projects";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function ProjectPriorityPickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: project } = useQuery(projectDetailOptions(wsId, id));
  const updateProject = useUpdateProject(id);

  return (
    <ProjectPriorityPickerBody
      value={project?.priority ?? "none"}
      onChange={(next) => {
        updateProject.mutate({ priority: next });
        router.back();
      }}
    />
  );
}
