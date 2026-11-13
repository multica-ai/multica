import { useState } from "react";
import { Alert } from "react-native";
import { StackActions, useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { useCreateProject, useUpdateProject } from "@multica/core/projects/mutations";
import { projectDetailOptions } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { Failure, Loading } from "../../components/States";
import { parseRosterValue, rosterValue, useRoster } from "../../model/roster";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useStrings } from "../../strings";
import { ProjectEditorView, type ProjectDraft, type ProjectEditorPage } from "./ProjectEditorView";

const EMPTY_DRAFT: ProjectDraft = { icon: "", title: "", description: "", status: "planned", priority: "none", lead: null, start: null, due: null };

function draftOf(project: Project): ProjectDraft {
  return {
    icon: project.icon ?? "",
    title: project.title,
    description: project.description ?? "",
    status: project.status,
    priority: project.priority,
    lead: project.lead_type && project.lead_id ? rosterValue(project.lead_type, project.lead_id) : null,
    start: project.start_date,
    due: project.due_date,
  };
}

export function ProjectEditorScreen() {
  const { projectId } = useRoute<RouteProp<RootStackParamList, "ProjectEditor">>().params;
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const existing = useQuery({ ...projectDetailOptions(workspace.id, projectId ?? ""), enabled: Boolean(projectId) });
  if (projectId && !existing.data) {
    return existing.isError ? <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={() => void existing.refetch()} /> : <Loading />;
  }
  return <ProjectEditor project={existing.data ?? null} />;
}

/** The form itself, mounted once the project being edited (if any) has loaded. */
function ProjectEditor({ project }: { project: Project | null }) {
  const navigation = useNavigation();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const roster = useRoster(workspace.id);
  const createProject = useCreateProject();
  const updateProject = useUpdateProject();
  const [original] = useState<ProjectDraft>(() => (project ? draftOf(project) : EMPTY_DRAFT));
  const [draft, setDraft] = useState<ProjectDraft>(original);
  const [page, setPage] = useState<ProjectEditorPage>("form");
  const projectId = project?.id;

  const save = async () => {
    const lead = draft.lead ? parseRosterValue(draft.lead) : null;
    const fields = {
      title: draft.title.trim(),
      description: draft.description.trim(),
      icon: draft.icon.trim(),
      status: draft.status,
      priority: draft.priority,
    };
    try {
      if (projectId) {
        // The lead of a private project is access control; only send it when it changed.
        await updateProject.mutateAsync({
          id: projectId,
          ...fields,
          description: fields.description || null,
          icon: fields.icon || null,
          ...(draft.lead !== original.lead ? { lead_type: lead?.type ?? null, lead_id: lead?.id ?? null } : {}),
          ...(draft.start !== original.start ? { start_date: draft.start } : {}),
          ...(draft.due !== original.due ? { due_date: draft.due } : {}),
        });
        navigation.goBack();
      } else {
        const project = await createProject.mutateAsync({
          ...fields,
          description: fields.description || undefined,
          icon: fields.icon || undefined,
          ...(lead ? { lead_type: lead.type, lead_id: lead.id } : {}),
          ...(draft.start ? { start_date: draft.start } : {}),
          ...(draft.due ? { due_date: draft.due } : {}),
        });
        navigation.dispatch(StackActions.replace("Project", { projectId: project.id }));
      }
    } catch {
      Alert.alert(strings.form.saveFailed);
    }
  };

  return (
    <ProjectEditorView
      mode={projectId ? "edit" : "create"}
      draft={draft}
      onChange={(patch) => setDraft((current) => ({ ...current, ...patch }))}
      page={page}
      onPage={setPage}
      leads={[...roster.members, ...roster.activeAgents]}
      busy={createProject.isPending || updateProject.isPending}
      onCancel={() => navigation.goBack()}
      onSave={() => void save()}
    />
  );
}
