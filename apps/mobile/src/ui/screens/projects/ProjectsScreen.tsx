import { useMemo } from "react";
import { useNavigation } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "@multica/core/projects/queries";
import { useActorResolver } from "../../model/people";
import { projectSections } from "../../model/projects";
import { useWorkspace } from "../../model/workspace-context";
import { useStrings } from "../../strings";
import { ProjectsView } from "./ProjectsView";

export function ProjectsScreen() {
  const navigation = useNavigation();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const resolveActor = useActorResolver();
  const projects = useQuery(projectListOptions(workspace.id));
  const sections = useMemo(() => projectSections(projects.data ?? [], resolveActor, strings), [projects.data, resolveActor, strings]);
  return (
    <ProjectsView
      state={projects.data ? "ready" : projects.isError ? "error" : "loading"}
      sections={sections}
      refreshing={projects.isRefetching}
      onRefresh={() => void projects.refetch()}
      onRetry={() => void projects.refetch()}
      onOpen={(projectId) => navigation.navigate("Project", { projectId })}
      onCreate={() => navigation.navigate("ProjectEditor", {})}
    />
  );
}
