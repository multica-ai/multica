import { useState } from "react";
import {
  Alert,
  Clipboard,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { api } from "@multica/core/api";
import {
  autopilotDeliveriesOptions,
  autopilotDetailOptions,
  autopilotRunsOptions,
  buildAutopilotWebhookUrl,
  useCancelAutopilotRun,
  useCreateAutopilotTrigger,
  useDeleteAutopilot,
  useDeleteAutopilotTrigger,
  useReplayAutopilotDelivery,
  useRotateAutopilotTriggerWebhookToken,
  useTriggerAutopilot,
  useUpdateAutopilot,
} from "@multica/core/autopilots";
import { useCoreQuery } from "@multica/core/provider";
import type {
  AutopilotRun,
  AutopilotTrigger,
  WebhookDelivery,
} from "@multica/core/types";
import { projectListOptions } from "@multica/core/projects";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import {
  Ban,
  CheckCircle2,
  ChevronRight,
  CircleHelp,
  Clock,
  Copy,
  Loader2,
  Pencil,
  Plus,
  RotateCw,
  Trash2,
  Webhook,
  XCircle,
  Zap,
} from "lucide-react-native";
import { Button, EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";
import { AutopilotScheduleFields } from "./autopilot-schedule-fields";
import {
  AUTOPILOT_EVENT_FILTER_DOC_URL,
  describeTrigger,
  formatDateTime,
  formatDuration,
  getActorName,
  getDefaultTriggerConfig,
  getProjectTitle,
  localizedDeliveryStatusLabel,
  localizedExecutionModeLabel,
  localizedRunStatusLabel,
  localizedSignatureStatusLabel,
  localizedStatusLabel,
  localizedTriggerKindLabel,
  toCronExpression,
  type TriggerFormConfig,
} from "./autopilot-mobile-utils";

type Props = NativeStackScreenProps<RootStackParamList, "AutopilotDetail">;

export function AutopilotDetailScreen({ navigation, route }: Props) {
  const { t } = useTranslation();
  const { workspace } = useMobileWorkspace();
  const autopilotId = route.params.autopilotId;
  const { data, isError, isLoading, refetch } = useCoreQuery(
    autopilotDetailOptions(workspace.id, autopilotId),
  );
  const { data: runs = [], refetch: refetchRuns } = useCoreQuery(
    autopilotRunsOptions(workspace.id, autopilotId),
  );
  const { data: agents = [] } = useCoreQuery(agentListOptions(workspace.id));
  const { data: squads = [] } = useCoreQuery(squadListOptions(workspace.id));
  const { data: projects = [] } = useCoreQuery(projectListOptions(workspace.id));

  const updateAutopilot = useUpdateAutopilot();
  const triggerAutopilot = useTriggerAutopilot();
  const deleteAutopilot = useDeleteAutopilot();
  const [addTriggerOpen, setAddTriggerOpen] = useState(false);
  const [deliveryOpen, setDeliveryOpen] = useState<WebhookDelivery | null>(null);

  const hasWebhookTrigger = data?.triggers.some((trigger) => trigger.kind === "webhook") ?? false;
  const { data: deliveries = [], refetch: refetchDeliveries } = useCoreQuery(
    autopilotDeliveriesOptions(workspace.id, autopilotId, { enabled: hasWebhookTrigger }),
  );

  if (isLoading) return <LoadingState />;
  if (isError || !data) {
    return (
      <Screen padded={false} safeArea={false}>
        <ScreenTitleBar onBack={() => navigation.goBack()} title={t("autopilots.detail_title")} />
        <EmptyState detail={t("common.pull_to_retry")} title={t("autopilots.unable_to_load")} />
      </Screen>
    );
  }

  const { autopilot, triggers } = data;
  const assigneeName = getActorName(autopilot.assignee_type, autopilot.assignee_id, agents, squads);
  const projectTitle = getProjectTitle(autopilot.project_id, projects);

  async function toggleStatus() {
    if (autopilot.status === "archived") return;
    await updateAutopilot.mutateAsync({
      id: autopilotId,
      status: autopilot.status === "active" ? "paused" : "active",
    });
  }

  function runNow() {
    void triggerAutopilot.mutateAsync(autopilotId).then(() => {
      void refetch();
      void refetchRuns();
    });
  }

  function confirmDelete() {
    Alert.alert(t("autopilots.delete_title"), t("autopilots.delete_detail"), [
      { style: "cancel", text: t("common.cancel") },
      {
        style: "destructive",
        text: t("autopilots.delete"),
        onPress: () => {
          void deleteAutopilot.mutateAsync(autopilotId).then(() => {
            navigation.goBack();
          });
        },
      },
    ]);
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar
        onBack={() => navigation.goBack()}
        right={
          <Pressable
            accessibilityLabel={t("autopilots.edit")}
            accessibilityRole="button"
            onPress={() => navigation.navigate("AutopilotForm", { autopilotId })}
            style={({ pressed }) => [styles.titleIconButton, pressed && styles.pressed]}
          >
            <Pencil color={colors.foreground} size={19} />
          </Pressable>
        }
        title={autopilot.title}
      />
      <ScrollView contentContainerStyle={styles.content}>
        <View style={styles.heroCard}>
          <View style={styles.heroHeader}>
            <View style={styles.heroTitleRow}>
              <View style={styles.heroIcon}>
                <Zap color={colors.foreground} size={20} />
              </View>
              <View style={styles.heroText}>
                <Text numberOfLines={2} style={styles.heroTitle}>
                  {autopilot.title}
                </Text>
                <Text style={styles.heroSubtitle}>{describeTrigger(triggers[0] ?? null)}</Text>
              </View>
            </View>
            <StatusPill status={autopilot.status} />
          </View>
          <View style={styles.actionRow}>
            <Button
              disabled={autopilot.status === "archived" || updateAutopilot.isPending}
              onPress={() => {
                void toggleStatus();
              }}
              variant="secondary"
            >
              {autopilot.status === "active"
                ? t("autopilots.pause")
                : t("autopilots.activate")}
            </Button>
            <Button
              disabled={autopilot.status !== "active" || triggerAutopilot.isPending}
              onPress={runNow}
            >
              {triggerAutopilot.isPending ? t("autopilots.running") : t("autopilots.run_now")}
            </Button>
          </View>
        </View>

        <Section title={t("autopilots.properties")}>
          <InfoRow label={t("autopilots.assignee")} value={assigneeName} />
          <InfoRow
            label={t("autopilots.output_mode")}
            value={localizedExecutionModeLabel(t, autopilot.execution_mode)}
          />
          {autopilot.execution_mode === "create_issue" ? (
            <InfoRow label={t("autopilots.project")} value={projectTitle} />
          ) : null}
          {autopilot.description ? (
            <View style={styles.promptBox}>
              <Text style={styles.promptLabel}>{t("autopilots.prompt")}</Text>
              <Text style={styles.promptText}>{autopilot.description}</Text>
            </View>
          ) : null}
        </Section>

        <Section
          right={
            <Pressable
              accessibilityRole="button"
              onPress={() => setAddTriggerOpen(true)}
              style={({ pressed }) => [styles.smallAction, pressed && styles.pressed]}
            >
              <Plus color={colors.foreground} size={15} />
              <Text style={styles.smallActionText}>{t("autopilots.add_trigger")}</Text>
            </Pressable>
          }
          title={t("autopilots.triggers")}
        >
          {triggers.length === 0 ? (
            <Text style={styles.emptyText}>{t("autopilots.no_triggers")}</Text>
          ) : (
            triggers.map((trigger) => (
              <TriggerCard
                autopilotId={autopilotId}
                key={trigger.id}
                onChanged={() => {
                  void refetch();
                }}
                trigger={trigger}
              />
            ))
          )}
        </Section>

        {hasWebhookTrigger ? (
          <Section title={t("autopilots.deliveries")}>
            {deliveries.length === 0 ? (
              <Text style={styles.emptyText}>{t("autopilots.no_deliveries")}</Text>
            ) : (
              deliveries.map((delivery) => (
                <DeliveryRow
                  delivery={delivery}
                  key={delivery.id}
                  onPress={() => setDeliveryOpen(delivery)}
                />
              ))
            )}
          </Section>
        ) : null}

        <Section title={t("autopilots.run_history")}>
          {runs.length === 0 ? (
            <Text style={styles.emptyText}>{t("autopilots.no_runs")}</Text>
          ) : (
            runs.map((run) => (
              <RunRow
                key={run.id}
                onIssuePress={
                  run.issue_id
                    ? () => navigation.navigate("IssueDetail", { issueId: run.issue_id! })
                    : undefined
                }
                onUpdated={() => {
                  void refetchRuns();
                }}
                run={run}
              />
            ))
          )}
        </Section>

        <Section title={t("autopilots.danger_zone")}>
          <Pressable
            accessibilityRole="button"
            onPress={confirmDelete}
            style={({ pressed }) => [styles.destructiveButton, pressed && styles.pressed]}
          >
            <Trash2 color={colors.destructive} size={16} />
            <Text style={styles.destructiveText}>{t("autopilots.delete")}</Text>
          </Pressable>
        </Section>
      </ScrollView>

      <AddTriggerModal
        autopilotId={autopilotId}
        onClose={() => setAddTriggerOpen(false)}
        onOpenDocs={() => {
          navigation.navigate("ExternalWeb", {
            title: t("autopilots.event_filter_docs"),
            url: AUTOPILOT_EVENT_FILTER_DOC_URL,
          });
        }}
        onSaved={() => {
          setAddTriggerOpen(false);
          void refetch();
        }}
        open={addTriggerOpen}
      />
      {deliveryOpen ? (
        <DeliveryDetailModal
          autopilotId={autopilotId}
          delivery={deliveryOpen}
          onChanged={() => {
            void refetchDeliveries();
            void refetchRuns();
          }}
          onClose={() => setDeliveryOpen(null)}
        />
      ) : null}
    </Screen>
  );
}

function Section({
  children,
  right,
  title,
}: {
  children: React.ReactNode;
  right?: React.ReactNode;
  title: string;
}) {
  return (
    <View style={styles.section}>
      <View style={styles.sectionHeader}>
        <Text style={styles.sectionTitle}>{title}</Text>
        {right}
      </View>
      {children}
    </View>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <View style={styles.infoRow}>
      <Text style={styles.infoLabel}>{label}</Text>
      <Text numberOfLines={2} style={styles.infoValue}>
        {value}
      </Text>
    </View>
  );
}

function StatusPill({ status }: { status: string }) {
  const { t } = useTranslation();
  const color =
    status === "active"
      ? colors.success
      : status === "paused"
        ? colors.warning
        : colors.mutedForeground;
  return (
    <View style={[styles.statusPill, { borderColor: color }]}>
      <Text style={[styles.statusPillText, { color }]}>{localizedStatusLabel(t, status)}</Text>
    </View>
  );
}

function TriggerCard({
  autopilotId,
  onChanged,
  trigger,
}: {
  autopilotId: string;
  onChanged: () => void;
  trigger: AutopilotTrigger;
}) {
  const { t } = useTranslation();
  const deleteTrigger = useDeleteAutopilotTrigger();
  const rotateToken = useRotateAutopilotTriggerWebhookToken();
  const webhookUrl =
    trigger.kind === "webhook"
      ? buildAutopilotWebhookUrl({ trigger, apiBaseUrl: api.getBaseUrl() })
      : null;

  function confirmDelete() {
    Alert.alert(t("autopilots.delete_trigger"), t("autopilots.delete_trigger_detail"), [
      { style: "cancel", text: t("common.cancel") },
      {
        style: "destructive",
        text: t("autopilots.delete"),
        onPress: () => {
          void deleteTrigger.mutateAsync({ autopilotId, triggerId: trigger.id }).then(onChanged);
        },
      },
    ]);
  }

  function rotateWebhook() {
    Alert.alert(t("autopilots.rotate_webhook"), t("autopilots.rotate_webhook_detail"), [
      { style: "cancel", text: t("common.cancel") },
      {
        text: t("autopilots.rotate"),
        onPress: () => {
          void rotateToken.mutateAsync({ autopilotId, triggerId: trigger.id }).then(onChanged);
        },
      },
    ]);
  }

  return (
    <View style={styles.itemCard}>
      <View style={styles.itemHeader}>
        <View style={styles.itemTitleRow}>
          {trigger.kind === "webhook" ? (
            <Webhook color={colors.foreground} size={16} />
          ) : (
            <Clock color={colors.foreground} size={16} />
          )}
          <Text style={styles.itemTitle}>{localizedTriggerKindLabel(t, trigger.kind)}</Text>
          {!trigger.enabled ? <Text style={styles.disabledBadge}>{t("autopilots.disabled")}</Text> : null}
        </View>
        <Pressable accessibilityRole="button" onPress={confirmDelete} style={styles.iconButton}>
          <Trash2 color={colors.mutedForeground} size={16} />
        </Pressable>
      </View>
      {trigger.label ? <Text style={styles.itemMuted}>{trigger.label}</Text> : null}
      <Text style={styles.itemBody}>{describeTrigger(trigger)}</Text>
      {trigger.next_run_at ? (
        <Text style={styles.itemMuted}>
          {t("autopilots.next_run")}: {formatDateTime(trigger.next_run_at)}
        </Text>
      ) : null}
      {webhookUrl ? (
        <View style={styles.urlBox}>
          <Text numberOfLines={1} style={styles.urlText}>
            {webhookUrl}
          </Text>
          <Pressable
            accessibilityRole="button"
            onPress={() => Clipboard.setString(webhookUrl)}
            style={styles.iconButton}
          >
            <Copy color={colors.foreground} size={16} />
          </Pressable>
          <Pressable
            accessibilityRole="button"
            onPress={rotateWebhook}
            style={styles.iconButton}
          >
            <RotateCw color={colors.foreground} size={16} />
          </Pressable>
        </View>
      ) : null}
    </View>
  );
}

function RunRow({
  onIssuePress,
  onUpdated,
  run,
}: {
  onIssuePress?: () => void;
  onUpdated: () => void;
  run: AutopilotRun;
}) {
  const { t } = useTranslation();
  const cancelRun = useCancelAutopilotRun();
  const visual = getRunVisual(run.status);
  const Icon = visual.icon;

  return (
    <Pressable
      accessibilityRole={onIssuePress ? "button" : undefined}
      disabled={!onIssuePress}
      onPress={onIssuePress}
      style={styles.itemCard}
    >
      <View style={styles.itemHeader}>
        <View style={styles.itemTitleRow}>
          <Icon color={visual.color} size={16} />
          <Text style={[styles.itemTitle, { color: visual.color }]}>
            {localizedRunStatusLabel(t, run.status)}
          </Text>
        </View>
        {onIssuePress ? <ChevronRight color={colors.mutedForeground} size={16} /> : null}
      </View>
      <Text style={styles.itemMuted}>
        {run.source.toUpperCase()} · {formatDateTime(run.triggered_at || run.created_at)}
      </Text>
      <Text style={styles.itemMuted}>
        {t("autopilots.completed_at")}: {formatDateTime(run.completed_at)} ·{" "}
        {t("autopilots.duration")}: {formatDuration(run.triggered_at, run.completed_at)}
      </Text>
      {run.failure_reason ? <Text style={styles.errorText}>{run.failure_reason}</Text> : null}
      {run.issue_id ? <Text style={styles.itemBody}>{t("autopilots.issue_linked")}</Text> : null}
      {run.status === "running" ? (
        <Pressable
          accessibilityRole="button"
          onPress={() => {
            void cancelRun.mutateAsync({ autopilotId: run.autopilot_id, runId: run.id }).then(onUpdated);
          }}
          style={({ pressed }) => [styles.cancelRunButton, pressed && styles.pressed]}
        >
          <Ban color={colors.destructive} size={15} />
          <Text style={styles.cancelRunText}>{t("autopilots.cancel_run")}</Text>
        </Pressable>
      ) : null}
    </Pressable>
  );
}

function getRunVisual(status: string) {
  switch (status) {
    case "running":
      return { color: colors.info, icon: Loader2 };
    case "completed":
    case "issue_created":
      return { color: colors.success, icon: CheckCircle2 };
    case "failed":
      return { color: colors.destructive, icon: XCircle };
    case "skipped":
      return { color: colors.mutedForeground, icon: Ban };
    default:
      return { color: colors.mutedForeground, icon: Clock };
  }
}

function DeliveryRow({
  delivery,
  onPress,
}: {
  delivery: WebhookDelivery;
  onPress: () => void;
}) {
  const { t } = useTranslation();
  const visual = getDeliveryVisual(delivery.status);
  const Icon = visual.icon;
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.itemCard, pressed && styles.pressed]}
    >
      <View style={styles.itemHeader}>
        <View style={styles.itemTitleRow}>
          <Icon color={visual.color} size={16} />
          <Text style={[styles.itemTitle, { color: visual.color }]}>
            {localizedDeliveryStatusLabel(t, delivery.status)}
          </Text>
        </View>
        <ChevronRight color={colors.mutedForeground} size={16} />
      </View>
      <Text numberOfLines={1} style={styles.itemBody}>
        {delivery.event || "webhook.received"}
      </Text>
      <Text style={styles.itemMuted}>
        {delivery.provider || "--"} · {formatDateTime(delivery.received_at || delivery.created_at)}
      </Text>
    </Pressable>
  );
}

function getDeliveryVisual(status: string) {
  switch (status) {
    case "queued":
      return { color: colors.info, icon: Loader2 };
    case "dispatched":
      return { color: colors.success, icon: CheckCircle2 };
    case "rejected":
    case "failed":
      return { color: colors.destructive, icon: XCircle };
    case "ignored":
      return { color: colors.mutedForeground, icon: Ban };
    default:
      return { color: colors.mutedForeground, icon: Webhook };
  }
}

function AddTriggerModal({
  autopilotId,
  onClose,
  onOpenDocs,
  onSaved,
  open,
}: {
  autopilotId: string;
  onClose: () => void;
  onOpenDocs: () => void;
  onSaved: () => void;
  open: boolean;
}) {
  const { t } = useTranslation();
  const createTrigger = useCreateAutopilotTrigger();
  const [kind, setKind] = useState<"schedule" | "webhook">("schedule");
  const [label, setLabel] = useState("");
  const [config, setConfig] = useState<TriggerFormConfig>(getDefaultTriggerConfig);

  async function save() {
    if (kind === "schedule" && !toCronExpression(config)) return;
    if (kind === "schedule") {
      await createTrigger.mutateAsync({
        autopilotId,
        kind,
        cron_expression: toCronExpression(config),
        timezone: config.timezone,
        label: label.trim() || undefined,
      });
    } else {
      await createTrigger.mutateAsync({
        autopilotId,
        kind,
        label: label.trim() || undefined,
      });
    }
    onSaved();
  }

  return (
    <Modal animationType="slide" onRequestClose={onClose} transparent visible={open}>
      <View style={styles.modalBackdrop}>
        <View style={styles.modalSheet}>
          <Text style={styles.modalTitle}>{t("autopilots.add_trigger")}</Text>
          <Segment
            options={[
              { label: t("autopilots.schedule"), value: "schedule" },
              { label: t("autopilots.webhook"), value: "webhook" },
            ]}
            value={kind}
            onChange={setKind}
          />
          <Field label={t("autopilots.label_optional")} value={label} onChangeText={setLabel} />
          {kind === "schedule" ? (
            <AutopilotScheduleFields config={config} onChange={setConfig} />
          ) : (
            <View style={styles.formGap}>
              <Text style={styles.itemMuted}>{t("autopilots.webhook_hint")}</Text>
              <Pressable
                accessibilityRole="button"
                onPress={onOpenDocs}
                style={({ pressed }) => [styles.helpButton, pressed && styles.pressed]}
              >
                <CircleHelp color={colors.foreground} size={15} />
                <Text style={styles.helpButtonText}>{t("autopilots.event_filter_docs")}</Text>
              </Pressable>
            </View>
          )}
          <View style={styles.modalActions}>
            <Button onPress={onClose} variant="secondary">
              {t("common.cancel")}
            </Button>
            <Button disabled={createTrigger.isPending} onPress={() => void save()}>
              {createTrigger.isPending ? t("autopilots.saving") : t("common.save")}
            </Button>
          </View>
        </View>
      </View>
    </Modal>
  );
}

function DeliveryDetailModal({
  autopilotId,
  delivery,
  onChanged,
  onClose,
}: {
  autopilotId: string;
  delivery: WebhookDelivery;
  onChanged: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const replay = useReplayAutopilotDelivery();
  const canReplay =
    delivery.status !== "queued" &&
    delivery.status !== "rejected" &&
    delivery.signature_status !== "invalid";
  const rawBody = typeof delivery.raw_body === "string" ? delivery.raw_body : null;
  const responseBody = typeof delivery.response_body === "string" ? delivery.response_body : null;
  const headers = delivery.selected_headers
    ? JSON.stringify(delivery.selected_headers, null, 2)
    : null;

  return (
    <Modal animationType="slide" onRequestClose={onClose} transparent visible>
      <View style={styles.modalBackdrop}>
        <View style={styles.modalSheet}>
          <ScrollView contentContainerStyle={styles.modalScroll}>
            <Text style={styles.modalTitle}>{t("autopilots.delivery_detail")}</Text>
            <InfoRow label={t("autopilots.status_label")} value={localizedDeliveryStatusLabel(t, delivery.status)} />
            <InfoRow label={t("autopilots.signature")} value={localizedSignatureStatusLabel(t, delivery.signature_status)} />
            <InfoRow label={t("autopilots.event")} value={delivery.event || "webhook.received"} />
            <InfoRow label={t("autopilots.received_at")} value={formatDateTime(delivery.received_at)} />
            <InfoRow label={t("autopilots.attempts")} value={String(delivery.attempt_count)} />
            {delivery.error ? <Text style={styles.errorText}>{delivery.error}</Text> : null}
            <PayloadBlock label={t("autopilots.headers")} value={headers} />
            <PayloadBlock label={t("autopilots.raw_body")} value={rawBody} />
            <PayloadBlock label={t("autopilots.response_body")} value={responseBody} />
            <View style={styles.modalActions}>
              <Button onPress={onClose} variant="secondary">
                {t("common.close")}
              </Button>
              <Button
                disabled={!canReplay || replay.isPending}
                onPress={() => {
                  void replay.mutateAsync({ autopilotId, deliveryId: delivery.id }).then(() => {
                    onChanged();
                    onClose();
                  });
                }}
              >
                {replay.isPending ? t("autopilots.replaying") : t("autopilots.replay")}
              </Button>
            </View>
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

function PayloadBlock({ label, value }: { label: string; value: string | null }) {
  if (!value) return null;
  return (
    <View style={styles.payloadBlock}>
      <Text style={styles.promptLabel}>{label}</Text>
      <Text selectable style={styles.payloadText}>
        {value}
      </Text>
    </View>
  );
}

function Field({
  label,
  multiline,
  onChangeText,
  value,
}: {
  label: string;
  multiline?: boolean;
  onChangeText: (value: string) => void;
  value: string;
}) {
  return (
    <View style={styles.fieldWrap}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        multiline={multiline}
        onChangeText={onChangeText}
        placeholderTextColor={colors.mutedForeground}
        style={[styles.field, multiline && styles.multilineField]}
        value={value}
      />
    </View>
  );
}

function Segment<T extends string>({
  onChange,
  options,
  value,
}: {
  onChange: (value: T) => void;
  options: Array<{ label: string; value: T }>;
  value: T;
}) {
  return (
    <View style={styles.segment}>
      {options.map((option) => (
        <Pressable
          accessibilityRole="button"
          key={option.value}
          onPress={() => onChange(option.value)}
          style={[
            styles.segmentItem,
            option.value === value && styles.segmentItemActive,
          ]}
        >
          <Text
            numberOfLines={1}
            style={[
              styles.segmentText,
              option.value === value && styles.segmentTextActive,
            ]}
          >
            {option.label}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  titleIconButton: {
    alignItems: "center",
    borderRadius: radii.md,
    height: 40,
    justifyContent: "center",
    width: 40,
  },
  pressed: {
    opacity: 0.72,
  },
  content: {
    gap: spacing.md,
    padding: spacing.lg,
  },
  heroCard: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.md,
    padding: spacing.md,
  },
  heroHeader: {
    alignItems: "flex-start",
    flexDirection: "row",
    gap: spacing.sm,
    justifyContent: "space-between",
  },
  heroTitleRow: {
    flex: 1,
    flexDirection: "row",
    gap: spacing.sm,
    minWidth: 0,
  },
  heroIcon: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 38,
    justifyContent: "center",
    width: 38,
  },
  heroText: {
    flex: 1,
    minWidth: 0,
  },
  heroTitle: {
    color: colors.foreground,
    fontSize: 18,
    fontWeight: "600",
    lineHeight: 24,
  },
  heroSubtitle: {
    color: colors.mutedForeground,
    fontSize: 13,
    marginTop: 3,
  },
  actionRow: {
    flexDirection: "row",
    gap: spacing.sm,
  },
  statusPill: {
    borderRadius: radii.sm,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
  },
  statusPillText: {
    fontSize: 12,
    fontWeight: "600",
  },
  section: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    padding: spacing.md,
  },
  sectionHeader: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
  },
  sectionTitle: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  smallAction: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    flexDirection: "row",
    gap: spacing.xs,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.xs,
  },
  smallActionText: {
    color: colors.foreground,
    fontSize: 12,
    fontWeight: "600",
  },
  infoRow: {
    flexDirection: "row",
    gap: spacing.md,
    justifyContent: "space-between",
  },
  infoLabel: {
    color: colors.mutedForeground,
    fontSize: 13,
  },
  infoValue: {
    color: colors.foreground,
    flex: 1,
    fontSize: 13,
    fontWeight: "500",
    textAlign: "right",
  },
  promptBox: {
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    gap: spacing.xs,
    padding: spacing.sm,
  },
  promptLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  promptText: {
    color: colors.foreground,
    fontSize: 13,
    lineHeight: 19,
  },
  emptyText: {
    color: colors.mutedForeground,
    fontSize: 13,
    lineHeight: 19,
  },
  itemCard: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    gap: spacing.xs,
    padding: spacing.sm,
  },
  itemHeader: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
  },
  itemTitleRow: {
    alignItems: "center",
    flex: 1,
    flexDirection: "row",
    gap: spacing.xs,
    minWidth: 0,
  },
  itemTitle: {
    color: colors.foreground,
    fontSize: 13,
    fontWeight: "600",
  },
  itemBody: {
    color: colors.foreground,
    fontSize: 13,
    lineHeight: 18,
  },
  itemMuted: {
    color: colors.mutedForeground,
    fontSize: 12,
    lineHeight: 17,
  },
  errorText: {
    color: colors.destructive,
    fontSize: 12,
    lineHeight: 17,
  },
  disabledBadge: {
    backgroundColor: colors.card,
    borderRadius: radii.sm,
    color: colors.mutedForeground,
    fontSize: 11,
    overflow: "hidden",
    paddingHorizontal: spacing.xs,
    paddingVertical: 2,
  },
  iconButton: {
    alignItems: "center",
    borderRadius: radii.sm,
    height: 32,
    justifyContent: "center",
    width: 32,
  },
  urlBox: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderRadius: radii.sm,
    flexDirection: "row",
    gap: spacing.xs,
    paddingLeft: spacing.sm,
  },
  urlText: {
    color: colors.foreground,
    flex: 1,
    fontSize: 12,
  },
  cancelRunButton: {
    alignItems: "center",
    alignSelf: "flex-start",
    flexDirection: "row",
    gap: spacing.xs,
    paddingVertical: spacing.xs,
  },
  cancelRunText: {
    color: colors.destructive,
    fontSize: 12,
    fontWeight: "600",
  },
  destructiveButton: {
    alignItems: "center",
    alignSelf: "flex-start",
    flexDirection: "row",
    gap: spacing.xs,
    paddingVertical: spacing.xs,
  },
  destructiveText: {
    color: colors.destructive,
    fontSize: 14,
    fontWeight: "600",
  },
  modalBackdrop: {
    backgroundColor: "rgba(24,24,27,0.35)",
    flex: 1,
    justifyContent: "flex-end",
  },
  modalSheet: {
    backgroundColor: colors.background,
    borderTopLeftRadius: radii.md,
    borderTopRightRadius: radii.md,
    gap: spacing.md,
    maxHeight: "88%",
    padding: spacing.lg,
  },
  modalScroll: {
    gap: spacing.md,
  },
  modalTitle: {
    color: colors.foreground,
    fontSize: 18,
    fontWeight: "600",
  },
  modalActions: {
    flexDirection: "row",
    gap: spacing.sm,
    justifyContent: "flex-end",
  },
  fieldWrap: {
    gap: spacing.xs,
  },
  fieldLabel: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  field: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    color: colors.foreground,
    fontSize: 15,
    minHeight: 44,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  multilineField: {
    minHeight: 100,
    textAlignVertical: "top",
  },
  formGap: {
    gap: spacing.md,
  },
  helpButton: {
    alignItems: "center",
    alignSelf: "flex-start",
    backgroundColor: colors.card,
    borderRadius: radii.sm,
    flexDirection: "row",
    gap: spacing.xs,
    minHeight: 34,
    paddingHorizontal: spacing.sm,
  },
  helpButtonText: {
    color: colors.foreground,
    fontSize: 12,
    fontWeight: "600",
  },
  segment: {
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.xs,
    padding: spacing.xs,
  },
  segmentItem: {
    alignItems: "center",
    borderRadius: radii.sm,
    flexGrow: 1,
    minHeight: 34,
    minWidth: 82,
    justifyContent: "center",
    paddingHorizontal: spacing.sm,
  },
  segmentItemActive: {
    backgroundColor: colors.card,
  },
  segmentText: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  segmentTextActive: {
    color: colors.foreground,
  },
  payloadBlock: {
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    gap: spacing.xs,
    padding: spacing.sm,
  },
  payloadText: {
    color: colors.foreground,
    fontFamily: "Menlo",
    fontSize: 11,
    lineHeight: 16,
  },
});
