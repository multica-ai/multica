import { useMemo } from "react";
import { Alert, Linking, Platform, Share } from "react-native";
import { useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { sidebarIssuesOptions } from "@multica/core/issues/queries";
import { projectSidebarSource, type SidebarIssuesRequest } from "@multica/core/issues/sidebar-list";
import { paths } from "@multica/core/paths";
import { canDeleteProject } from "@multica/core/projects/deletion-permissions";
import { useDeleteProject, useUpdateProject } from "@multica/core/projects/mutations";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { projectResourcesOptions } from "@multica/core/projects/resource-queries";
import type { GithubRepoResourceRef, LocalDirectoryResourceRef, ProjectPriority, ProjectResource, ProjectStatus } from "@multica/core/types";
import { endpoints } from "../../../platform/env";
import { confirmDestructive, pick } from "../../components/action-sheet";
import { groupIssuesByStatus, phaseCounts } from "../../model/issue-groups";
import { useActorResolver } from "../../model/people";
import { PROJECT_STATUSES, projectDates } from "../../model/projects";
import { useRoster } from "../../model/roster";
import { useStatusPresentation } from "../../model/status";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useStrings, type Strings } from "../../strings";
import { ProjectView, type ProjectResourceModel } from "./ProjectView";

/** Enough for any project a phone needs to scroll through. */
const ISSUE_LIMIT = 500;
const PRIORITIES: readonly ProjectPriority[] = ["urgent", "high", "medium", "low", "none"];

function resourceModel(resource: ProjectResource, strings: Strings): ProjectResourceModel {
  if (resource.resource_type === "github_repo") {
    const ref = resource.resource_ref as GithubRepoResourceRef;
    const name = ref.url.replace(/^https?:\/\/(www\.)?github\.com\//, "").replace(/\.git$/, "");
    return { id: resource.id, icon: "FolderGit", label: resource.label || name, detail: strings.project.repo, url: ref.url };
  }
  const ref = resource.resource_ref as LocalDirectoryResourceRef;
  return { id: resource.id, icon: "Folder", label: resource.label || ref.label || ref.local_path, detail: `${strings.project.localDirectory} · ${ref.local_path}`, url: null };
}

export function ProjectScreen() {
  const navigation = useNavigation();
  const { projectId } = useRoute<RouteProp<RootStackParamList, "Project">>().params;
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const resolveActor = useActorResolver();
  const status = useStatusPresentation(workspace.id);
  const catalog = useIssueStatuses(workspace.id);
  const roster = useRoster(workspace.id);

  const detail = useQuery(projectDetailOptions(workspace.id, projectId));
  const resources = useQuery(projectResourcesOptions(workspace.id, projectId));
  const request: SidebarIssuesRequest = {
    table: { scope: { kind: "project", project_id: projectId }, filters: { include_sub_issues: true }, sort: { field: "updated_at", direction: "desc" } },
  };
  const issues = useQuery(sidebarIssuesOptions(workspace.id, projectSidebarSource(projectId), request, ISSUE_LIMIT));
  const updateProject = useUpdateProject();
  const deleteProject = useDeleteProject();

  const project = detail.data;
  const groups = useMemo(() => groupIssuesByStatus(issues.data?.issues ?? [], catalog, status, resolveActor), [issues.data, catalog, status, resolveActor]);
  const progress = useMemo(() => phaseCounts(issues.data?.issues ?? [], catalog.categoryOf), [issues.data, catalog]);
  const missing = detail.error instanceof ApiError && detail.error.status === 404;
  const webUrl = `${endpoints.webUrl}${paths.workspace(workspace.slug).projectDetail(projectId)}`;
  const deletable = project ? canDeleteProject(project, roster.userId, roster.role, roster.agents) : false;

  const update = (fields: { status?: ProjectStatus; priority?: ProjectPriority }) =>
    updateProject.mutate({ id: projectId, ...fields }, { onError: () => Alert.alert(strings.form.saveFailed) });

  return (
    <ProjectView
      state={project ? "ready" : missing ? "missing" : detail.isError ? "error" : "loading"}
      project={
        project
          ? {
              icon: project.icon,
              title: project.title,
              description: project.description,
              status: project.status,
              priority: project.priority,
              lead: project.lead_type && project.lead_id ? resolveActor(project.lead_type, project.lead_id) : null,
              dates: projectDates(project, strings),
            }
          : null
      }
      progress={progress}
      resources={(resources.data ?? []).map((resource) => resourceModel(resource, strings))}
      issues={{ state: issues.data ? "ready" : issues.isError ? "error" : "loading", groups }}
      refreshing={detail.isRefetching || issues.isRefetching}
      onRefresh={() => {
        void detail.refetch();
        void issues.refetch();
        void resources.refetch();
      }}
      onBack={() => navigation.goBack()}
      onRetry={() => {
        void detail.refetch();
        void issues.refetch();
      }}
      onStatus={async () => {
        const next = await pick(strings.project.statusTitle, PROJECT_STATUSES.map((value) => ({ value, label: strings.project.status[value] ?? value })), strings.common.cancel);
        if (next && next !== project?.status) update({ status: next });
      }}
      onPriority={async () => {
        const next = await pick(strings.issue.priorityTitle, PRIORITIES.map((value) => ({ value, label: strings.priority[value] })), strings.common.cancel);
        if (next && next !== project?.priority) update({ priority: next });
      }}
      onLead={() => navigation.navigate("ProjectEditor", { projectId })}
      onDates={() => navigation.navigate("ProjectEditor", { projectId })}
      onMore={async () => {
        const action = await pick(
          undefined,
          [
            { value: "edit" as const, label: strings.project.edit },
            { value: "share" as const, label: strings.form.share },
            ...(deletable ? [{ value: "delete" as const, label: strings.form.delete, destructive: true }] : []),
          ],
          strings.common.cancel,
        );
        if (action === "edit") navigation.navigate("ProjectEditor", { projectId });
        if (action === "share") void Share.share({ url: webUrl, message: Platform.OS === "ios" ? undefined : webUrl });
        if (action === "delete" && (await confirmDestructive(strings.project.deleteConfirm, strings.project.deleteDetail, strings.form.delete, strings.common.cancel))) {
          deleteProject.mutate(projectId, {
            onSuccess: () => navigation.goBack(),
            onError: () => Alert.alert(strings.project.deleteFailed),
          });
        }
      }}
      onOpenResource={(resource) => {
        if (resource.url) void Linking.openURL(resource.url);
      }}
      onOpenIssue={(issue) => navigation.navigate("Issue", { issueId: issue.id })}
    />
  );
}
