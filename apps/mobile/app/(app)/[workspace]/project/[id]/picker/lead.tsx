/**
 * Project lead picker route — presented as a formSheet by the parent Stack
 * with iOS-native nav header + UISearchController via `useNativeSearchBar`.
 * Self-contained: reads project from cache, fires useUpdateProject directly.
 */
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { ProjectLeadPickerBody } from "@/components/project/pickers/project-lead-picker-body";
import { projectDetailOptions } from "@/data/queries/projects";
import { useUpdateProject } from "@/data/mutations/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

export default function ProjectLeadPickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: project } = useQuery(projectDetailOptions(wsId, id));
  const updateProject = useUpdateProject(id);
  const query = useNativeSearchBar("Search members or agents", {
    autoFocus: true,
  });

  const value =
    project?.lead_type && project?.lead_id
      ? { type: project.lead_type, id: project.lead_id }
      : null;

  return (
    <ProjectLeadPickerBody
      value={value}
      query={query}
      onChange={(next) => {
        if (next === null) {
          updateProject.mutate({ lead_type: null, lead_id: null });
        } else {
          updateProject.mutate({ lead_type: next.type, lead_id: next.id });
        }
        router.back();
      }}
    />
  );
}
