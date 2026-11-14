import { useMemo, useState } from "react";
import { Alert } from "react-native";
import { StackActions, useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { useCreateAutopilot, useCreateAutopilotTrigger, useUpdateAutopilot } from "@multica/core/autopilots/mutations";
import { autopilotDetailOptions } from "@multica/core/autopilots/queries";
import { toCron } from "@multica/core/autopilots/schedule";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Autopilot } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { squadListOptions } from "@multica/core/workspace/queries";
import { Failure, Loading } from "../../components/States";
import { rosterValue, useRoster } from "../../model/roster";
import { newSchedule } from "../../model/schedule";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useStrings } from "../../strings";
import { AutopilotEditorView, type AssigneeOption, type AutopilotDraft, type AutopilotEditorPage } from "./AutopilotEditorView";
import { useNextRuns } from "./TriggerEditorScreen";

export function AutopilotEditorScreen() {
  const { autopilotId } = useRoute<RouteProp<RootStackParamList, "AutopilotEditor">>().params;
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const detail = useQuery({ ...autopilotDetailOptions(workspace.id, autopilotId ?? ""), enabled: Boolean(autopilotId) });
  if (autopilotId && !detail.data) {
    return detail.isError ? <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={() => void detail.refetch()} /> : <Loading />;
  }
  return <AutopilotEditor autopilot={detail.data?.autopilot ?? null} />;
}

function AutopilotEditor({ autopilot }: { autopilot: Autopilot | null }) {
  const navigation = useNavigation();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const roster = useRoster(workspace.id);
  const squads = useQuery(squadListOptions(workspace.id));
  const projects = useQuery(projectListOptions(workspace.id));
  const createAutopilot = useCreateAutopilot();
  const updateAutopilot = useUpdateAutopilot();
  const createTrigger = useCreateAutopilotTrigger();
  const [draft, setDraft] = useState<AutopilotDraft>(() => ({
    title: autopilot?.title ?? "",
    description: autopilot?.description ?? "",
    assignee: autopilot ? rosterValue(autopilot.assignee_type, autopilot.assignee_id) : null,
    mode: autopilot?.execution_mode ?? "create_issue",
    project: autopilot?.project_id ?? null,
    schedule: null,
  }));
  const [page, setPage] = useState<AutopilotEditorPage>("form");
  const [busy, setBusy] = useState(false);
  const nextRuns = useNextRuns(workspace.id, draft.schedule);

  const assignees = useMemo<AssigneeOption[]>(
    () => [
      ...roster.activeAgents.map((entry) => ({ value: entry.value, actor: entry.actor, detail: strings.autopilot.agents })),
      ...(squads.data ?? [])
        .filter((squad) => !squad.archived_at)
        .map((squad) => ({
          value: rosterValue("squad", squad.id),
          actor: { kind: "agent" as const, name: squad.name, avatarUrl: resolvePublicFileUrl(squad.avatar_url) },
          detail: strings.autopilot.squads,
        })),
    ],
    [roster.activeAgents, squads.data, strings],
  );

  const save = async () => {
    const [type, id] = (draft.assignee ?? "").split(":");
    if ((type !== "agent" && type !== "squad") || !id) return;
    const fields = {
      title: draft.title.trim(),
      description: draft.description.trim(),
      project_id: draft.project,
      execution_mode: draft.mode,
    };
    setBusy(true);
    try {
      if (autopilot) {
        const assigneeChanged = type !== autopilot.assignee_type || id !== autopilot.assignee_id;
        await updateAutopilot.mutateAsync({ id: autopilot.id, ...fields, ...(assigneeChanged ? { assignee_type: type, assignee_id: id } : {}) });
        navigation.goBack();
        return;
      }
      const created = await createAutopilot.mutateAsync({ ...fields, assignee_type: type, assignee_id: id });
      if (draft.schedule) {
        try {
          await createTrigger.mutateAsync({ autopilotId: created.id, kind: "schedule", cron_expression: toCron(draft.schedule), timezone: draft.schedule.timezone });
        } catch {
          Alert.alert(strings.autopilot.scheduleFailed);
        }
      }
      navigation.dispatch(StackActions.replace("Autopilot", { autopilotId: created.id }));
    } catch {
      Alert.alert(strings.form.saveFailed);
    } finally {
      setBusy(false);
    }
  };

  return (
    <AutopilotEditorView
      mode={autopilot ? "edit" : "create"}
      draft={draft}
      onChange={(patch) => setDraft((current) => ({ ...current, ...patch }))}
      page={page}
      onPage={setPage}
      assignees={assignees}
      projects={(projects.data ?? []).map((project) => ({ id: project.id, title: project.title, icon: project.icon }))}
      nextRuns={nextRuns}
      onAddSchedule={() => setDraft((current) => ({ ...current, schedule: newSchedule() }))}
      busy={busy}
      onCancel={() => navigation.goBack()}
      onSave={() => void save()}
    />
  );
}
