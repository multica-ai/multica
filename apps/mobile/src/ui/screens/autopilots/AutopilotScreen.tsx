import { useMemo } from "react";
import { Alert, Platform, Share } from "react-native";
import { useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { useCreateAutopilotTrigger, useDeleteAutopilot, useDeleteAutopilotTrigger, useTriggerAutopilot, useUpdateAutopilot, useUpdateAutopilotTrigger } from "@multica/core/autopilots/mutations";
import { autopilotDetailOptions, autopilotRunsOptions } from "@multica/core/autopilots/queries";
import { parseCron } from "@multica/core/autopilots/schedule";
import { buildAutopilotWebhookUrl } from "@multica/core/autopilots";
import { paths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import type { AutopilotTrigger } from "@multica/core/types";
import { endpoints } from "../../../platform/env";
import { confirmDestructive, pick } from "../../components/action-sheet";
import { runModel, triggerModel } from "../../model/autopilots";
import { useAssigneeResolver } from "../../model/roster";
import { describeSchedule, deviceTimezone } from "../../model/schedule";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useStrings } from "../../strings";
import { AutopilotView } from "./AutopilotView";

const RUNS = 20;

function shareUrl(url: string) {
  void Share.share({ url, message: Platform.OS === "ios" ? undefined : url });
}

export function AutopilotScreen() {
  const navigation = useNavigation();
  const { autopilotId } = useRoute<RouteProp<RootStackParamList, "Autopilot">>().params;
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const resolveAssignee = useAssigneeResolver(workspace.id);
  const detail = useQuery(autopilotDetailOptions(workspace.id, autopilotId));
  const runs = useQuery(autopilotRunsOptions(workspace.id, autopilotId));
  const projects = useQuery(projectListOptions(workspace.id));
  const updateAutopilot = useUpdateAutopilot();
  const deleteAutopilot = useDeleteAutopilot();
  const runNow = useTriggerAutopilot();
  const createTrigger = useCreateAutopilotTrigger();
  const updateTrigger = useUpdateAutopilotTrigger();
  const deleteTrigger = useDeleteAutopilotTrigger();

  const autopilot = detail.data?.autopilot;
  const rawTriggers = useMemo(() => detail.data?.triggers ?? [], [detail.data]);
  const canWrite = autopilot?.can_write !== false;
  const describe = (trigger: AutopilotTrigger) =>
    trigger.kind === "schedule" ? describeSchedule(parseCron(trigger.cron_expression ?? "", trigger.timezone ?? deviceTimezone()), strings) : strings.autopilot.kind[trigger.kind] ?? trigger.kind;
  const triggers = rawTriggers.map((trigger) => triggerModel(trigger, describe, strings));
  const runItems = useMemo(() => (runs.data ?? []).slice(0, RUNS).map((run) => runModel(run, strings)), [runs.data, strings]);
  const project = autopilot?.project_id ? projects.data?.find((candidate) => candidate.id === autopilot.project_id)?.title ?? null : null;
  const webhookUrl = (trigger: AutopilotTrigger) => buildAutopilotWebhookUrl({ trigger, apiBaseUrl: endpoints.apiUrl });
  const failed = () => Alert.alert(strings.form.saveFailed);

  const togglePause = () => {
    if (!autopilot) return;
    updateAutopilot.mutate({ id: autopilotId, status: autopilot.status === "active" ? "paused" : "active" }, { onError: failed });
  };

  return (
    <AutopilotView
      state={autopilot ? "ready" : detail.isError ? "error" : "loading"}
      autopilot={
        autopilot
          ? {
              title: autopilot.title,
              description: autopilot.description,
              paused: autopilot.status !== "active",
              assignee: resolveAssignee(autopilot.assignee_type, autopilot.assignee_id),
              mode: strings.autopilot.modes[autopilot.execution_mode] ?? autopilot.execution_mode,
              project,
            }
          : null
      }
      triggers={triggers}
      runs={{ state: runs.data ? "ready" : runs.isError ? "error" : "loading", items: runItems }}
      canWrite={canWrite}
      starting={runNow.isPending}
      refreshing={detail.isRefetching || runs.isRefetching}
      onRefresh={() => {
        void detail.refetch();
        void runs.refetch();
      }}
      onBack={() => navigation.goBack()}
      onRetry={() => {
        void detail.refetch();
        void runs.refetch();
      }}
      onRunNow={() => runNow.mutate(autopilotId, { onError: () => Alert.alert(strings.autopilot.runFailed) })}
      onTogglePause={togglePause}
      onAddTrigger={async () => {
        const kind = await pick(
          strings.autopilot.addTrigger,
          [
            { value: "schedule" as const, label: strings.autopilot.kind.schedule! },
            { value: "webhook" as const, label: strings.autopilot.kind.webhook! },
          ],
          strings.common.cancel,
        );
        if (kind === "schedule") navigation.navigate("TriggerEditor", { autopilotId });
        if (kind === "webhook") {
          createTrigger.mutate(
            { autopilotId, kind: "webhook" },
            {
              onSuccess: (trigger) => {
                const url = webhookUrl(trigger);
                if (url) shareUrl(url);
              },
              onError: failed,
            },
          );
        }
      }}
      onTrigger={async (model) => {
        const trigger = rawTriggers.find((candidate) => candidate.id === model.id);
        if (!trigger) return;
        const url = webhookUrl(trigger);
        const action = await pick(
          model.label,
          [
            ...(trigger.kind === "schedule" ? [{ value: "edit" as const, label: strings.autopilot.editSchedule }] : []),
            ...(url ? [{ value: "share" as const, label: strings.autopilot.shareUrl }] : []),
            { value: "delete" as const, label: strings.autopilot.deleteTrigger, destructive: true },
          ],
          strings.common.cancel,
        );
        if (action === "edit") navigation.navigate("TriggerEditor", { autopilotId, triggerId: trigger.id });
        if (action === "share" && url) shareUrl(url);
        if (action === "delete" && (await confirmDestructive(strings.autopilot.deleteTriggerConfirm, undefined, strings.form.delete, strings.common.cancel))) {
          deleteTrigger.mutate({ autopilotId, triggerId: trigger.id }, { onError: failed });
        }
      }}
      onToggleTrigger={(trigger, enabled) => updateTrigger.mutate({ autopilotId, triggerId: trigger.id, enabled }, { onError: failed })}
      onOpenRun={(run) => {
        if (run.issueId) navigation.navigate("Issue", { issueId: run.issueId });
      }}
      onMore={async () => {
        const webUrl = `${endpoints.webUrl}${paths.workspace(workspace.slug).autopilotDetail(autopilotId)}`;
        const action = await pick(
          undefined,
          [
            ...(canWrite ? [{ value: "edit" as const, label: strings.autopilot.edit }] : []),
            { value: "share" as const, label: strings.form.share },
            ...(canWrite ? [{ value: "delete" as const, label: strings.form.delete, destructive: true }] : []),
          ],
          strings.common.cancel,
        );
        if (action === "edit") navigation.navigate("AutopilotEditor", { autopilotId });
        if (action === "share") shareUrl(webUrl);
        if (action === "delete" && (await confirmDestructive(strings.autopilot.deleteConfirm, strings.autopilot.deleteDetail, strings.form.delete, strings.common.cancel))) {
          deleteAutopilot.mutate(autopilotId, { onSuccess: () => navigation.goBack(), onError: failed });
        }
      }}
    />
  );
}
