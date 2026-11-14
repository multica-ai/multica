import { useState } from "react";
import { Alert } from "react-native";
import { useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { useCreateAutopilotTrigger, useUpdateAutopilotTrigger } from "@multica/core/autopilots/mutations";
import { autopilotDetailOptions, cronPreviewOptions } from "@multica/core/autopilots/queries";
import { parseCron, toCron, type ScheduleConfig } from "@multica/core/autopilots/schedule";
import type { AutopilotTrigger } from "@multica/core/types";
import { Failure, Loading } from "../../components/States";
import { formatMoment } from "../../model/dates";
import { deviceTimezone, newSchedule, scheduleRepeat } from "../../model/schedule";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useStrings } from "../../strings";
import type { NextRunsModel } from "./ScheduleForm";
import { TriggerEditorView } from "./TriggerEditorView";

const PREVIEW_RUNS = 3;

/** The server's next fire times for an editable schedule. */
export function useNextRuns(wsId: string, config: ScheduleConfig | null): NextRunsModel {
  const strings = useStrings();
  const editable = config !== null && scheduleRepeat(config) !== null;
  const preview = useQuery(cronPreviewOptions(wsId, editable ? toCron(config) : "", config?.timezone ?? "", { enabled: editable }));
  if (preview.isError) return { state: "error", items: [] };
  if (!preview.data) return { state: "loading", items: [] };
  return { state: "ready", items: (preview.data.next_runs ?? []).slice(0, PREVIEW_RUNS).map((iso) => formatMoment(iso, strings)) };
}

export function TriggerEditorScreen() {
  const { autopilotId, triggerId } = useRoute<RouteProp<RootStackParamList, "TriggerEditor">>().params;
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const detail = useQuery({ ...autopilotDetailOptions(workspace.id, autopilotId), enabled: Boolean(triggerId) });
  if (!triggerId) return <TriggerEditor autopilotId={autopilotId} trigger={null} />;
  const trigger = detail.data?.triggers.find((candidate) => candidate.id === triggerId);
  if (trigger) return <TriggerEditor autopilotId={autopilotId} trigger={trigger} />;
  return detail.isError || detail.data ? <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={() => void detail.refetch()} /> : <Loading />;
}

function TriggerEditor({ autopilotId, trigger }: { autopilotId: string; trigger: AutopilotTrigger | null }) {
  const navigation = useNavigation();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const createTrigger = useCreateAutopilotTrigger();
  const updateTrigger = useUpdateAutopilotTrigger();
  const [config, setConfig] = useState<ScheduleConfig>(() =>
    trigger?.cron_expression ? parseCron(trigger.cron_expression, trigger.timezone ?? deviceTimezone()) : newSchedule(),
  );
  const nextRuns = useNextRuns(workspace.id, config);
  const busy = createTrigger.isPending || updateTrigger.isPending;

  const save = () => {
    const fields = { cron_expression: toCron(config), timezone: config.timezone };
    const options = { onSuccess: () => navigation.goBack(), onError: () => Alert.alert(strings.form.saveFailed) };
    if (trigger) updateTrigger.mutate({ autopilotId, triggerId: trigger.id, ...fields }, options);
    else createTrigger.mutate({ autopilotId, kind: "schedule", ...fields }, options);
  };

  return (
    <TriggerEditorView
      editing={trigger !== null}
      config={config}
      onChange={setConfig}
      nextRuns={nextRuns}
      busy={busy}
      onCancel={() => navigation.goBack()}
      onSave={save}
    />
  );
}
